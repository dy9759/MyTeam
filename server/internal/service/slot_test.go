package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// slotTestFixture bundles the queries handle plus a fresh workspace + plan +
// task wired up for slot-level tests. Each test gets its own row set so they
// can run in parallel against a shared dev DB without interference.
type slotTestFixture struct {
	q       *db.Queries
	wsID    pgtype.UUID
	planID  pgtype.UUID
	taskID  pgtype.UUID
	taskUID uuid.UUID
}

// newSlotFixture creates workspace + user + plan + task and returns a fixture
// the test can use to insert ParticipantSlot rows.
func newSlotFixture(t *testing.T) slotTestFixture {
	t.Helper()
	q := testDB(t)
	ctx := context.Background()
	wsID := createTestWorkspace(t, q)
	userID := createTestUser(t, q, "slotsvc+"+t.Name()+"@example.com", "Slot Tester")

	plan, err := q.CreatePlan(ctx, db.CreatePlanParams{
		WorkspaceID: wsID,
		Title:       "Slot Plan " + t.Name(),
		Description: pgtype.Text{},
		SourceType:  pgtype.Text{},
		SourceRefID: pgtype.UUID{},
		Constraints: pgtype.Text{},
		ExpectedOutput: pgtype.Text{},
		CreatedBy:   userID,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	task, err := q.CreateTask(ctx, db.CreateTaskParams{
		PlanID:      plan.ID,
		RunID:       pgtype.UUID{}, // nullable; no run needed for slot lifecycle tests
		WorkspaceID: wsID,
		Title:       "Slot Task " + t.Name(),
		Description: pgtype.Text{},
		StepOrder:   pgtype.Int4{Int32: 0, Valid: true},
		// All other COALESCE-defaulted fields can be left zero — sqlc will
		// pass NULL and the SQL applies the default.
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	taskUID, err := uuid.FromBytes(task.ID.Bytes[:])
	if err != nil {
		t.Fatalf("uuid.FromBytes(task.ID): %v", err)
	}

	return slotTestFixture{
		q:       q,
		wsID:    wsID,
		planID:  plan.ID,
		taskID:  task.ID,
		taskUID: taskUID,
	}
}

// makeSlot inserts a ParticipantSlot on the fixture's task with the given
// type/trigger/order and optional timeout. Returns the created row.
func (f slotTestFixture) makeSlot(t *testing.T, slotType, trigger string, order int32, timeoutSeconds int32) db.ParticipantSlot {
	t.Helper()
	params := db.CreateParticipantSlotParams{
		TaskID:    f.taskID,
		SlotType:  slotType,
		SlotOrder: pgtype.Int4{Int32: order, Valid: true},
		Trigger:   pgtype.Text{String: trigger, Valid: true},
	}
	if timeoutSeconds > 0 {
		params.TimeoutSeconds = pgtype.Int4{Int32: timeoutSeconds, Valid: true}
	}
	slot, err := f.q.CreateParticipantSlot(context.Background(), params)
	if err != nil {
		t.Fatalf("create slot: %v", err)
	}
	return slot
}

func slotUUID(t *testing.T, id pgtype.UUID) uuid.UUID {
	t.Helper()
	u, err := uuid.FromBytes(id.Bytes[:])
	if err != nil {
		t.Fatalf("uuid.FromBytes: %v", err)
	}
	return u
}

func TestActivateBeforeExecution_FlipsWaitingToReady(t *testing.T) {
	f := newSlotFixture(t)
	svc := NewSlotService(f.q)
	ctx := context.Background()

	// Two before_execution slots (both should activate) plus one
	// during_execution slot that must remain waiting.
	beforeA := f.makeSlot(t, SlotTypeHumanInput, SlotTriggerBeforeExecution, 0, 0)
	beforeB := f.makeSlot(t, SlotTypeHumanInput, SlotTriggerBeforeExecution, 1, 0)
	during := f.makeSlot(t, SlotTypeAgentExecution, SlotTriggerDuringExecution, 2, 0)

	activated, err := svc.ActivateBeforeExecution(ctx, f.taskUID)
	if err != nil {
		t.Fatalf("ActivateBeforeExecution: %v", err)
	}
	if len(activated) != 2 {
		t.Fatalf("activated count: want 2, got %d", len(activated))
	}
	for _, s := range activated {
		if s.Status != SlotStatusReady {
			t.Fatalf("activated slot %s: want ready, got %s", s.ID.Bytes, s.Status)
		}
	}

	// Verify via DB read that the during_execution slot is untouched.
	got, err := f.q.GetSlot(ctx, during.ID)
	if err != nil {
		t.Fatalf("GetSlot during: %v", err)
	}
	if got.Status != SlotStatusWaiting {
		t.Fatalf("during slot: want waiting, got %s", got.Status)
	}
	// Sanity: the two before slots really did flip in the DB.
	for _, s := range []db.ParticipantSlot{beforeA, beforeB} {
		row, err := f.q.GetSlot(ctx, s.ID)
		if err != nil {
			t.Fatalf("GetSlot: %v", err)
		}
		if row.Status != SlotStatusReady {
			t.Fatalf("before slot: want ready, got %s", row.Status)
		}
	}
}

func TestActivateDuringExecution_OnlyTouchesDuringExecution(t *testing.T) {
	f := newSlotFixture(t)
	svc := NewSlotService(f.q)
	ctx := context.Background()

	before := f.makeSlot(t, SlotTypeHumanInput, SlotTriggerBeforeExecution, 0, 0)
	during := f.makeSlot(t, SlotTypeAgentExecution, SlotTriggerDuringExecution, 1, 0)
	beforeDone := f.makeSlot(t, SlotTypeHumanReview, SlotTriggerBeforeDone, 2, 0)

	activated, err := svc.ActivateDuringExecution(ctx, f.taskUID)
	if err != nil {
		t.Fatalf("ActivateDuringExecution: %v", err)
	}
	if len(activated) != 1 {
		t.Fatalf("activated count: want 1, got %d", len(activated))
	}
	if activated[0].ID.Bytes != during.ID.Bytes {
		t.Fatalf("wrong slot activated: want %x, got %x", during.ID.Bytes, activated[0].ID.Bytes)
	}

	// before / before_done must remain waiting.
	for _, s := range []db.ParticipantSlot{before, beforeDone} {
		row, err := f.q.GetSlot(ctx, s.ID)
		if err != nil {
			t.Fatalf("GetSlot: %v", err)
		}
		if row.Status != SlotStatusWaiting {
			t.Fatalf("untouched slot %s: want waiting, got %s", s.Trigger, row.Status)
		}
	}
}

func TestMarkSubmitted_FromReady(t *testing.T) {
	f := newSlotFixture(t)
	svc := NewSlotService(f.q)
	ctx := context.Background()

	slot := f.makeSlot(t, SlotTypeHumanInput, SlotTriggerBeforeExecution, 0, 0)

	// Move to ready first.
	if _, err := f.q.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
		ID: slot.ID, Status: SlotStatusReady,
	}); err != nil {
		t.Fatalf("seed ready: %v", err)
	}

	got, err := svc.MarkSubmitted(ctx, slotUUID(t, slot.ID))
	if err != nil {
		t.Fatalf("MarkSubmitted: %v", err)
	}
	if got.Status != SlotStatusSubmitted {
		t.Fatalf("status: want submitted, got %s", got.Status)
	}
	if !got.CompletedAt.Valid {
		t.Fatalf("completed_at should be stamped on submitted")
	}
}

func TestMarkSubmitted_FromInvalidStateErrors(t *testing.T) {
	f := newSlotFixture(t)
	svc := NewSlotService(f.q)
	ctx := context.Background()

	slot := f.makeSlot(t, SlotTypeHumanInput, SlotTriggerBeforeExecution, 0, 0)

	// Slot is still 'waiting' — MarkSubmitted should refuse.
	_, err := svc.MarkSubmitted(ctx, slotUUID(t, slot.ID))
	if !errors.Is(err, ErrSlotInvalidTransition) {
		t.Fatalf("want ErrSlotInvalidTransition, got %v", err)
	}

	// And from 'approved' too.
	if _, err := f.q.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
		ID: slot.ID, Status: SlotStatusApproved,
	}); err != nil {
		t.Fatalf("seed approved: %v", err)
	}
	_, err = svc.MarkSubmitted(ctx, slotUUID(t, slot.ID))
	if !errors.Is(err, ErrSlotInvalidTransition) {
		t.Fatalf("want ErrSlotInvalidTransition from approved, got %v", err)
	}
}

func TestApplyReviewDecision(t *testing.T) {
	cases := []struct {
		name     string
		decision string
		want     string
	}{
		{"approve", ReviewDecisionApprove, SlotStatusApproved},
		{"request_changes", ReviewDecisionRequestChanges, SlotStatusRevisionRequested},
		{"reject", ReviewDecisionReject, SlotStatusRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSlotFixture(t)
			svc := NewSlotService(f.q)
			ctx := context.Background()

			slot := f.makeSlot(t, SlotTypeHumanReview, SlotTriggerBeforeDone, 0, 0)
			// Move to submitted so the review decision lands on a realistic state.
			if _, err := f.q.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
				ID: slot.ID, Status: SlotStatusSubmitted,
			}); err != nil {
				t.Fatalf("seed submitted: %v", err)
			}

			got, err := svc.ApplyReviewDecision(ctx, slotUUID(t, slot.ID), tc.decision)
			if err != nil {
				t.Fatalf("ApplyReviewDecision: %v", err)
			}
			if got.Status != tc.want {
				t.Fatalf("status: want %s, got %s", tc.want, got.Status)
			}
		})
	}

	t.Run("unknown_decision_errors", func(t *testing.T) {
		f := newSlotFixture(t)
		svc := NewSlotService(f.q)
		slot := f.makeSlot(t, SlotTypeHumanReview, SlotTriggerBeforeDone, 0, 0)

		_, err := svc.ApplyReviewDecision(context.Background(), slotUUID(t, slot.ID), "vibes")
		if !errors.Is(err, ErrUnknownReviewDecision) {
			t.Fatalf("want ErrUnknownReviewDecision, got %v", err)
		}
	})
}

func TestCheckTimeouts_ExpiresPastDeadline(t *testing.T) {
	f := newSlotFixture(t)
	svc := NewSlotService(f.q)
	ctx := context.Background()

	// expired: ready, 1s timeout, started 5s ago → past deadline
	expiredSlot := f.makeSlot(t, SlotTypeHumanInput, SlotTriggerDuringExecution, 0, 1)
	// in-window: ready, 600s timeout, started just now → still has time
	freshSlot := f.makeSlot(t, SlotTypeHumanInput, SlotTriggerDuringExecution, 1, 600)
	// no-timeout: ready, no timeout_seconds → never expires
	noTimeoutSlot := f.makeSlot(t, SlotTypeHumanInput, SlotTriggerDuringExecution, 2, 0)
	// done: already approved, even with a timeout it must not be touched
	doneSlot := f.makeSlot(t, SlotTypeHumanInput, SlotTriggerDuringExecution, 3, 1)
	if _, err := f.q.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
		ID: doneSlot.ID, Status: SlotStatusApproved,
	}); err != nil {
		t.Fatalf("seed approved: %v", err)
	}

	// Build the in-memory slot list with synthetic reference times.
	// CheckTimeouts uses started_at if valid, else updated_at.
	now := time.Now()
	expiredSlot.Status = SlotStatusInProgress
	expiredSlot.StartedAt = pgtype.Timestamptz{Time: now.Add(-5 * time.Second), Valid: true}
	freshSlot.Status = SlotStatusReady
	freshSlot.UpdatedAt = pgtype.Timestamptz{Time: now, Valid: true}
	noTimeoutSlot.Status = SlotStatusReady
	noTimeoutSlot.UpdatedAt = pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}
	// doneSlot we leave with whatever the DB returned.

	expired := svc.CheckTimeouts(ctx, []db.ParticipantSlot{
		expiredSlot, freshSlot, noTimeoutSlot, doneSlot,
	})
	if expired != 1 {
		t.Fatalf("expired count: want 1, got %d", expired)
	}

	// Confirm DB state.
	row, err := f.q.GetSlot(ctx, expiredSlot.ID)
	if err != nil {
		t.Fatalf("GetSlot expired: %v", err)
	}
	if row.Status != SlotStatusExpired {
		t.Fatalf("expired slot: want expired, got %s", row.Status)
	}
	row, err = f.q.GetSlot(ctx, freshSlot.ID)
	if err != nil {
		t.Fatalf("GetSlot fresh: %v", err)
	}
	if row.Status == SlotStatusExpired {
		t.Fatalf("fresh slot must not be expired")
	}
}

func TestResetForNewRun_ResetsAllSlotsToWaiting(t *testing.T) {
	f := newSlotFixture(t)
	svc := NewSlotService(f.q)
	ctx := context.Background()

	// Create three slots and move them through the state machine.
	s1 := f.makeSlot(t, SlotTypeHumanInput, SlotTriggerBeforeExecution, 0, 0)
	s2 := f.makeSlot(t, SlotTypeAgentExecution, SlotTriggerDuringExecution, 1, 0)
	s3 := f.makeSlot(t, SlotTypeHumanReview, SlotTriggerBeforeDone, 2, 0)
	for _, s := range []db.ParticipantSlot{s1, s2, s3} {
		if _, err := f.q.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
			ID: s.ID, Status: SlotStatusApproved,
		}); err != nil {
			t.Fatalf("seed approved %s: %v", s.ID.Bytes, err)
		}
	}

	if err := svc.ResetForNewRun(ctx, []uuid.UUID{f.taskUID}); err != nil {
		t.Fatalf("ResetForNewRun: %v", err)
	}

	// All three should now be waiting with started_at/completed_at cleared.
	for _, s := range []db.ParticipantSlot{s1, s2, s3} {
		row, err := f.q.GetSlot(ctx, s.ID)
		if err != nil {
			t.Fatalf("GetSlot: %v", err)
		}
		if row.Status != SlotStatusWaiting {
			t.Fatalf("post-reset status: want waiting, got %s", row.Status)
		}
		if row.StartedAt.Valid {
			t.Fatalf("started_at should be cleared on reset, got %v", row.StartedAt.Time)
		}
		if row.CompletedAt.Valid {
			t.Fatalf("completed_at should be cleared on reset, got %v", row.CompletedAt.Time)
		}
	}
}

func TestResetForNewRun_EmptyTaskList_NoOp(t *testing.T) {
	f := newSlotFixture(t)
	svc := NewSlotService(f.q)

	// Should silently do nothing — no DB call, no error.
	if err := svc.ResetForNewRun(context.Background(), nil); err != nil {
		t.Fatalf("ResetForNewRun(nil): %v", err)
	}
}
