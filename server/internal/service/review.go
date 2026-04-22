// Package service: review.go — ReviewService records review decisions and
// cascades into Slot/Task state per Plan 5 PRD §4.9 / §9.2.
//
// A Review is a verdict (approve / request_changes / reject) on a single
// Artifact version, optionally tied to the human_review slot whose
// decision it represents. Recording a review has two side effects:
//
//   - Slot transition (when slot_id is set): delegated to SlotService.
//     ApplyReviewDecision so the slot state machine stays in one place.
//   - Task transition: derived purely from the review decision and the
//     other slots on the task. No persistence of intermediate state.
//
// The slot constants (ReviewDecisionApprove, ReviewDecisionRequestChanges,
// ReviewDecisionReject) live in slot.go because SlotService is the
// canonical owner of the decision vocabulary.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// Reviewer-type strings — mirror the CHECK constraint in migration 058.
const (
	ReviewerTypeMember = "member"
	ReviewerTypeAgent  = "agent"
)

// Task statuses ReviewService cascades into. Mirrored from migration 055.
const (
	TaskStatusRunning         = "running"
	TaskStatusCompleted       = "completed"
	TaskStatusNeedsAttention  = "needs_attention"
)

var (
	// ErrInvalidReviewDecision is returned when Submit is called with a
	// decision string outside the canonical set.
	ErrInvalidReviewDecision = errors.New("review: decision must be approve|request_changes|reject")
	// ErrInvalidReviewerType is returned when reviewer_type is not member|agent.
	ErrInvalidReviewerType = errors.New("review: reviewer_type must be member|agent")
	// ErrMissingReviewIDs is returned when task_id, artifact_id, or
	// reviewer_id is missing.
	ErrMissingReviewIDs = errors.New("review: task_id, artifact_id, reviewer_id required")
)

// ReviewService records reviews and cascades the resulting slot/task state.
// Slots is injected by the caller so the dependency direction stays
// review → slot (and not the reverse).
type ReviewService struct {
	Q     *db.Queries
	Slots *SlotService
}

// NewReviewService constructs a ReviewService bound to the given Queries
// and SlotService. Slots may be nil if the caller does not want slot
// cascading (Submit will then only insert the review row + update the task).
func NewReviewService(q *db.Queries, slots *SlotService) *ReviewService {
	return &ReviewService{Q: q, Slots: slots}
}

// SubmitReviewRequest captures a single review decision on an artifact.
// SlotID is optional — leave as uuid.Nil when the review is not tied to
// a specific human_review slot.
type SubmitReviewRequest struct {
	TaskID       uuid.UUID
	ArtifactID   uuid.UUID
	SlotID       uuid.UUID // optional — the human_review slot being decided
	ReviewerID   uuid.UUID
	ReviewerType string
	Decision     string
	Comment      string
}

// SubmitReviewResult bundles the inserted review with the resulting state
// changes. SlotUpdated is nil when no slot transition was applied (either
// SlotID was nil, Slots was nil, or the slot transition errored — see
// Submit's contract). TaskNewStatus is empty when the decision did not
// produce a task transition (currently never happens; reserved).
type SubmitReviewResult struct {
	Review        db.Review
	SlotUpdated   *db.ParticipantSlot
	TaskNewStatus string
}

// Submit creates a review row and applies the side-effects per the decision:
//
//	approve         → slot.status=approved, task.status=completed (if no
//	                  other blocking slots pending; else task stays running)
//	request_changes → slot.status=revision_requested, task.status=running
//	reject          → slot.status=rejected, task.status=needs_attention
//
// Slot transition is delegated to SlotService.ApplyReviewDecision and is
// best-effort: if it errors, Submit logs and continues so the review row
// and task transition still land. Task transition errors are returned to
// the caller because the task state is the contract the rest of the
// system reads.
//
// The caller decides whether to wrap the call in a transaction.
func (s *ReviewService) Submit(ctx context.Context, req SubmitReviewRequest) (*SubmitReviewResult, error) {
	if req.TaskID == uuid.Nil || req.ArtifactID == uuid.Nil || req.ReviewerID == uuid.Nil {
		return nil, ErrMissingReviewIDs
	}
	switch req.Decision {
	case ReviewDecisionApprove, ReviewDecisionRequestChanges, ReviewDecisionReject:
	default:
		return nil, ErrInvalidReviewDecision
	}
	switch req.ReviewerType {
	case ReviewerTypeMember, ReviewerTypeAgent:
	default:
		return nil, ErrInvalidReviewerType
	}

	review, err := s.Q.CreateReview(ctx, db.CreateReviewParams{
		TaskID:       toPgUUID(req.TaskID),
		ArtifactID:   toPgUUID(req.ArtifactID),
		SlotID:       toPgNullUUID(req.SlotID),
		ReviewerID:   toPgUUID(req.ReviewerID),
		ReviewerType: toPgNullText(req.ReviewerType),
		Decision:     req.Decision,
		Comment:      toPgNullText(req.Comment),
	})
	if err != nil {
		return nil, fmt.Errorf("create review: %w", err)
	}

	result := &SubmitReviewResult{Review: review}

	// Slot transition (if slot_id was provided and a SlotService is wired).
	// Best-effort: log and continue on error so the task transition still
	// happens — the slot state can be reconciled later from the review row.
	if req.SlotID != uuid.Nil && s.Slots != nil {
		slot, err := s.Slots.ApplyReviewDecision(ctx, req.SlotID, req.Decision)
		if err != nil {
			slog.Warn("review.Submit: slot transition failed",
				"slot_id", req.SlotID,
				"decision", req.Decision,
				"err", err,
			)
		} else {
			result.SlotUpdated = slot
		}
	}

	// Task transition. Approve only completes the task when no other
	// blocking, required slot is still pending; otherwise it stays running
	// and the next slot's outcome will drive the next transition.
	var newTaskStatus string
	switch req.Decision {
	case ReviewDecisionApprove:
		pending, perr := s.taskHasOtherBlockingSlotsPending(ctx, req.TaskID, req.SlotID)
		if perr != nil {
			return result, fmt.Errorf("check task slots: %w", perr)
		}
		if pending {
			newTaskStatus = TaskStatusRunning
		} else {
			newTaskStatus = TaskStatusCompleted
		}
	case ReviewDecisionRequestChanges:
		newTaskStatus = TaskStatusRunning
	case ReviewDecisionReject:
		newTaskStatus = TaskStatusNeedsAttention
	}

	if newTaskStatus != "" {
		if _, err := s.Q.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
			ID:     toPgUUID(req.TaskID),
			Status: newTaskStatus,
		}); err != nil {
			return result, fmt.Errorf("update task status: %w", err)
		}
		result.TaskNewStatus = newTaskStatus
	}

	return result, nil
}

// taskHasOtherBlockingSlotsPending returns true if any blocking, required
// slot on the task is still in a non-terminal state. The slot whose review
// is being submitted (currentSlotID) is excluded from the check so its
// own pending->approved transition does not block its own task completion.
//
// Terminal statuses (approved, rejected, expired, skipped) are considered
// "settled". submitted slots count as pending — they have output but no
// decision yet, so they still hold the task open. waiting / ready /
// in_progress / revision_requested are obviously pending.
func (s *ReviewService) taskHasOtherBlockingSlotsPending(ctx context.Context, taskID, currentSlotID uuid.UUID) (bool, error) {
	slots, err := s.Q.ListSlotsByTask(ctx, toPgUUID(taskID))
	if err != nil {
		return false, err
	}
	for _, slot := range slots {
		if currentSlotID != uuid.Nil && uuid.UUID(slot.ID.Bytes) == currentSlotID {
			continue
		}
		if !slot.Blocking || !slot.Required {
			continue
		}
		switch slot.Status {
		case SlotStatusApproved, SlotStatusRejected, SlotStatusExpired, SlotStatusSkipped:
			// terminal — fine
		default:
			// waiting / ready / in_progress / submitted / revision_requested → pending
			return true, nil
		}
	}
	return false, nil
}

// ListForArtifact returns reviews for the artifact, newest first.
func (s *ReviewService) ListForArtifact(ctx context.Context, artifactID uuid.UUID) ([]db.Review, error) {
	return s.Q.ListReviewsForArtifact(ctx, toPgUUID(artifactID))
}

// LatestForArtifact returns the most recent review for the artifact, or
// pgx.ErrNoRows when none exist.
func (s *ReviewService) LatestForArtifact(ctx context.Context, artifactID uuid.UUID) (db.Review, error) {
	return s.Q.GetLatestReviewForArtifact(ctx, toPgUUID(artifactID))
}
