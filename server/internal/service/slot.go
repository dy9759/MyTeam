// Package service: slot.go — SlotService manages ParticipantSlot lifecycle
// per Plan 5 PRD §4.6 (slot definition) and §5.2 (state machine).
//
// A ParticipantSlot represents a hand-off point inside a Task — either a
// human input slot (form/upload), an agent execution slot, or a human
// review slot. Each slot has a trigger phase (before_execution,
// during_execution, before_done) and follows the state machine:
//
//	waiting → ready → in_progress → submitted → approved
//	                                          → revision_requested
//	                                          → rejected
//	                  ready/in_progress → expired (timeout)
//	                  any → skipped (caller-driven)
//
// SlotService is invoked by:
//   - ProjectLifecycleService when a Task transitions to ready/running
//     (activates before_execution / during_execution slots).
//   - ReviewService / handler when a Review is created on a slot
//     (applies the decision: approve / request_changes / reject).
//   - A periodic ticker (CheckTimeouts) to expire slots past their deadline.
//   - ProjectLifecycleService at the start of a new ProjectRun
//     (ResetForNewRun puts every slot back to waiting).
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// Slot status enum aliases — mirror the CHECK constraint in
// migration 056. Using constants keeps callers from drifting on string
// literals and makes the state machine grep-able.
const (
	SlotStatusWaiting           = "waiting"
	SlotStatusReady             = "ready"
	SlotStatusInProgress        = "in_progress"
	SlotStatusSubmitted         = "submitted"
	SlotStatusApproved          = "approved"
	SlotStatusRevisionRequested = "revision_requested"
	SlotStatusRejected          = "rejected"
	SlotStatusExpired           = "expired"
	SlotStatusSkipped           = "skipped"
)

// Slot type enum aliases.
const (
	SlotTypeHumanInput     = "human_input"
	SlotTypeAgentExecution = "agent_execution"
	SlotTypeHumanReview    = "human_review"
)

// Slot trigger phase enum aliases.
const (
	SlotTriggerBeforeExecution = "before_execution"
	SlotTriggerDuringExecution = "during_execution"
	SlotTriggerBeforeDone      = "before_done"
)

// Review decision aliases — these are the canonical decision strings the
// review handler produces and the ones ApplyReviewDecision understands.
const (
	ReviewDecisionApprove        = "approve"
	ReviewDecisionRequestChanges = "request_changes"
	ReviewDecisionReject         = "reject"
)

// SlotService manages ParticipantSlot state transitions.
type SlotService struct {
	Q *db.Queries
}

// NewSlotService constructs a SlotService bound to the given Queries.
func NewSlotService(q *db.Queries) *SlotService {
	return &SlotService{Q: q}
}

var (
	// ErrSlotInvalidTransition is returned when the caller asks for a
	// transition the state machine forbids (e.g. submit a slot already
	// approved). Wrapped with %w so callers can errors.Is on it.
	ErrSlotInvalidTransition = errors.New("slot: invalid state transition")
	// ErrUnknownReviewDecision is returned by ApplyReviewDecision when
	// the decision string is not one of the canonical values.
	ErrUnknownReviewDecision = errors.New("slot: unknown review decision")
)

// ActivateBeforeExecution finds before_execution slots on a task and
// transitions them from waiting → ready. Returns the list of slots that
// became ready. Slots already past waiting (or with a different trigger)
// are skipped silently.
func (s *SlotService) ActivateBeforeExecution(ctx context.Context, taskID uuid.UUID) ([]db.ParticipantSlot, error) {
	return s.activateByTrigger(ctx, taskID, SlotTriggerBeforeExecution)
}

// ActivateDuringExecution flips during_execution slots from waiting → ready.
func (s *SlotService) ActivateDuringExecution(ctx context.Context, taskID uuid.UUID) ([]db.ParticipantSlot, error) {
	return s.activateByTrigger(ctx, taskID, SlotTriggerDuringExecution)
}

// ActivateBeforeDone flips before_done slots from waiting → ready —
// typically used for human_review slots that gate task completion.
func (s *SlotService) ActivateBeforeDone(ctx context.Context, taskID uuid.UUID) ([]db.ParticipantSlot, error) {
	return s.activateByTrigger(ctx, taskID, SlotTriggerBeforeDone)
}

// activateByTrigger is the shared body for the three Activate* helpers.
// It iterates the task's slots, filters by trigger + status='waiting', and
// promotes each to 'ready'. Per-slot failures are logged and skipped so a
// single bad row doesn't poison the whole batch.
func (s *SlotService) activateByTrigger(ctx context.Context, taskID uuid.UUID, trigger string) ([]db.ParticipantSlot, error) {
	slots, err := s.Q.ListSlotsByTask(ctx, toPgUUID(taskID))
	if err != nil {
		return nil, fmt.Errorf("list slots for task %s: %w", taskID, err)
	}
	var activated []db.ParticipantSlot
	for _, slot := range slots {
		if slot.Trigger != trigger || slot.Status != SlotStatusWaiting {
			continue
		}
		updated, err := s.Q.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
			ID:     slot.ID,
			Status: SlotStatusReady,
		})
		if err != nil {
			slog.Warn("slot.activate: update failed", "slot", slot.ID, "trigger", trigger, "err", err)
			continue
		}
		activated = append(activated, updated)
	}
	return activated, nil
}

// MarkSubmitted transitions a slot from ready or in_progress → submitted.
// Used when an agent finishes execution work or a human submits their
// input. Human input callers may pass JSON content to persist on the slot.
// Returns ErrSlotInvalidTransition if the slot is in any other status
// (approved, expired, etc.).
func (s *SlotService) MarkSubmitted(ctx context.Context, slotID uuid.UUID, content ...[]byte) (*db.ParticipantSlot, error) {
	slot, err := s.Q.GetSlot(ctx, toPgUUID(slotID))
	if err != nil {
		return nil, fmt.Errorf("get slot %s: %w", slotID, err)
	}
	if slot.Status != SlotStatusReady && slot.Status != SlotStatusInProgress {
		return nil, fmt.Errorf("%w: %s → submitted", ErrSlotInvalidTransition, slot.Status)
	}

	var updated db.ParticipantSlot
	if len(content) > 0 {
		updated, err = s.Q.UpdateSlotSubmission(ctx, db.UpdateSlotSubmissionParams{
			ID:      toPgUUID(slotID),
			Content: content[0],
		})
	} else {
		updated, err = s.Q.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
			ID:     toPgUUID(slotID),
			Status: SlotStatusSubmitted,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("update slot %s → submitted: %w", slotID, err)
	}

	if slot.SlotType == SlotTypeHumanInput {
		task, err := s.Q.GetTask(ctx, slot.TaskID)
		if err != nil {
			return nil, fmt.Errorf("get task for slot %s: %w", slotID, err)
		}
		if task.Status == TaskStatusNeedsHuman {
			if _, err := s.Q.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
				ID:     slot.TaskID,
				Status: TaskStatusRunning,
			}); err != nil {
				return nil, fmt.Errorf("resume task for slot %s: %w", slotID, err)
			}
		}
	}

	return &updated, nil
}

// SubmitHumanInput persists an append-only submission history row for the
// slot, then transitions the slot to submitted and resumes the parent task
// when it was blocked on human input.
//
// Callers that need atomicity should construct the service with Queries
// bound to a transaction via db.Queries.WithTx.
func (s *SlotService) SubmitHumanInput(
	ctx context.Context,
	slotID uuid.UUID,
	submittedBy uuid.UUID,
	content []byte,
	comment string,
) (*db.ParticipantSlot, error) {
	slot, err := s.Q.GetSlot(ctx, toPgUUID(slotID))
	if err != nil {
		return nil, fmt.Errorf("get slot %s: %w", slotID, err)
	}
	if slot.SlotType != SlotTypeHumanInput {
		return nil, fmt.Errorf("%w: %s slot cannot accept human input", ErrSlotInvalidTransition, slot.SlotType)
	}
	if slot.Status != SlotStatusReady && slot.Status != SlotStatusInProgress {
		return nil, fmt.Errorf("%w: %s → submitted", ErrSlotInvalidTransition, slot.Status)
	}

	task, err := s.Q.GetTask(ctx, slot.TaskID)
	if err != nil {
		return nil, fmt.Errorf("get task for slot %s: %w", slotID, err)
	}

	if _, err := s.Q.CreateParticipantSlotSubmission(ctx, db.CreateParticipantSlotSubmissionParams{
		SlotID:      toPgUUID(slotID),
		TaskID:      slot.TaskID,
		RunID:       task.RunID,
		SubmittedBy: toPgUUID(submittedBy),
		Content:     content,
		Comment:     toPgNullText(comment),
	}); err != nil {
		return nil, fmt.Errorf("create submission history for slot %s: %w", slotID, err)
	}

	updated, err := s.Q.UpdateSlotSubmission(ctx, db.UpdateSlotSubmissionParams{
		ID:      toPgUUID(slotID),
		Content: content,
	})
	if err != nil {
		return nil, fmt.Errorf("update slot %s → submitted: %w", slotID, err)
	}

	if task.Status == TaskStatusNeedsHuman {
		if _, err := s.Q.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
			ID:     slot.TaskID,
			Status: TaskStatusRunning,
		}); err != nil {
			return nil, fmt.Errorf("resume task for slot %s: %w", slotID, err)
		}
	}

	return &updated, nil
}

// ApplyReviewDecision maps a Review.decision onto slot.status:
//
//	approve         → approved
//	request_changes → revision_requested
//	reject          → rejected
//
// Any other decision string yields ErrUnknownReviewDecision.
func (s *SlotService) ApplyReviewDecision(ctx context.Context, slotID uuid.UUID, decision string) (*db.ParticipantSlot, error) {
	var newStatus string
	switch decision {
	case ReviewDecisionApprove:
		newStatus = SlotStatusApproved
	case ReviewDecisionRequestChanges:
		newStatus = SlotStatusRevisionRequested
	case ReviewDecisionReject:
		newStatus = SlotStatusRejected
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownReviewDecision, decision)
	}
	updated, err := s.Q.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
		ID:     toPgUUID(slotID),
		Status: newStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("update slot %s → %s: %w", slotID, newStatus, err)
	}
	return &updated, nil
}

// CheckTimeouts scans the supplied slots and transitions any that are
// past their deadline to status='expired'. Only ready/in_progress slots
// with a positive timeout_seconds are considered. Returns the number of
// slots that were expired in this call.
//
// The deadline reference is started_at when set (for in_progress slots),
// otherwise updated_at (when the slot was last transitioned, e.g. from
// waiting → ready). Per-slot failures are logged and skipped.
//
// The caller (typically a periodic tick in ProjectLifecycleService) is
// responsible for fetching the right scope of slots — usually all slots
// for active tasks in a workspace — and feeding them in here.
func (s *SlotService) CheckTimeouts(ctx context.Context, slots []db.ParticipantSlot) int {
	now := time.Now()
	expired := 0
	for _, slot := range slots {
		if slot.Status != SlotStatusReady && slot.Status != SlotStatusInProgress {
			continue
		}
		if !slot.TimeoutSeconds.Valid || slot.TimeoutSeconds.Int32 <= 0 {
			continue
		}
		ref := slotDeadlineRef(slot)
		if ref.IsZero() {
			continue
		}
		deadline := ref.Add(time.Duration(slot.TimeoutSeconds.Int32) * time.Second)
		if now.Before(deadline) {
			continue
		}
		if _, err := s.Q.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
			ID:     slot.ID,
			Status: SlotStatusExpired,
		}); err != nil {
			slog.Warn("slot.CheckTimeouts: expire failed", "slot", slot.ID, "err", err)
			continue
		}
		expired++
	}
	return expired
}

// slotDeadlineRef returns the reference time the timeout window is
// measured from: started_at if the slot has begun, otherwise updated_at
// (when the slot was last transitioned, which is the most recent moment
// it could have entered 'ready'). Returns the zero time if neither is
// valid, telling the caller to skip the row.
func slotDeadlineRef(slot db.ParticipantSlot) time.Time {
	if slot.StartedAt.Valid {
		return slot.StartedAt.Time
	}
	if slot.UpdatedAt.Valid {
		return slot.UpdatedAt.Time
	}
	return time.Time{}
}

// ResetForNewRun resets every slot belonging to the given tasks back to
// status='waiting' and clears started_at / completed_at. This is invoked
// at the start of a new ProjectRun so the slots are fresh for the new
// pass through the plan. No-op when taskIDs is empty.
func (s *SlotService) ResetForNewRun(ctx context.Context, taskIDs []uuid.UUID) error {
	if len(taskIDs) == 0 {
		return nil
	}
	pgIDs := make([]pgtype.UUID, len(taskIDs))
	for i, id := range taskIDs {
		pgIDs[i] = toPgUUID(id)
	}
	if err := s.Q.ResetSlotsForNewRun(ctx, pgIDs); err != nil {
		return fmt.Errorf("reset slots for new run: %w", err)
	}
	return nil
}
