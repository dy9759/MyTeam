// review_test.go — DB-backed tests for ReviewService. Requires DATABASE_URL
// pointing at a migrated multica DB (migration 058+ for the artifact /
// review tables).
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// reviewTestEnv bundles the IDs needed for ReviewService tests: workspace,
// member, plan, project, run, task, artifact, and a human_review slot.
// Each test gets its own row set via the test-name-derived suffixes used
// in createTestWorkspace / createTestUser, so tests can run in parallel
// against a shared dev DB.
type reviewTestEnv struct {
	WorkspaceID pgtype.UUID
	MemberID    pgtype.UUID
	PlanID      pgtype.UUID
	ProjectID   pgtype.UUID
	RunID       pgtype.UUID
	TaskID      pgtype.UUID
	ArtifactID  pgtype.UUID
	SlotID      pgtype.UUID
}

// setupReviewEnv builds the minimal set of FK-satisfying rows needed to
// insert a review and have the slot/task transitions land cleanly.
// It seeds one human_review slot in 'submitted' state — the natural state
// the slot is in when a review is being recorded against it.
func setupReviewEnv(t *testing.T, q *db.Queries) reviewTestEnv {
	t.Helper()
	ctx := context.Background()

	wsID := createTestWorkspace(t, q)
	memberID := createTestUser(t, q, "review+"+t.Name()+"@example.com", "Review Tester")

	plan, err := q.CreatePlan(ctx, db.CreatePlanParams{
		WorkspaceID:    wsID,
		Title:          "Plan for " + t.Name(),
		Description:    pgtype.Text{String: "test plan", Valid: true},
		SourceType:     pgtype.Text{},
		SourceRefID:    pgtype.UUID{},
		Constraints:    pgtype.Text{},
		ExpectedOutput: pgtype.Text{String: "an artifact", Valid: true},
		CreatedBy:      memberID,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// project.created_by is NOT NULL but the sqlc CreateProject query does
	// not set it. Insert via raw SQL so the project_run FK is satisfied.
	pool := openTestPool(t)
	var projectID pgtype.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, created_by, schedule_type, source_conversations, creator_owner_id)
		VALUES ($1, $2, '', 'active', $3, 'one_time', '[]'::jsonb, $3)
		RETURNING id
	`, wsID, "Project for "+t.Name(), memberID).Scan(&projectID)
	if err != nil {
		t.Fatalf("create project (raw): %v", err)
	}

	run, err := q.CreateProjectRun(ctx, db.CreateProjectRunParams{
		PlanID:    plan.ID,
		ProjectID: projectID,
		Status:    "running",
	})
	if err != nil {
		t.Fatalf("create project_run: %v", err)
	}

	task, err := q.CreateTask(ctx, db.CreateTaskParams{
		PlanID:      plan.ID,
		RunID:       run.ID,
		WorkspaceID: wsID,
		Title:       "Task for " + t.Name(),
		Description: pgtype.Text{String: "do work", Valid: true},
		StepOrder:   pgtype.Int4{Int32: 0, Valid: true},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	artifact, err := q.CreateArtifact(ctx, db.CreateArtifactParams{
		TaskID:        task.ID,
		SlotID:        pgtype.UUID{},
		ExecutionID:   pgtype.UUID{},
		RunID:         run.ID,
		ArtifactType:  "report",
		Version:       1,
		Title:         pgtype.Text{String: "v1 output", Valid: true},
		Summary:       pgtype.Text{String: "first draft", Valid: true},
		Content:       []byte(`{"step":1}`),
		FileIndexID:   pgtype.UUID{},
		FileSnapshotID: pgtype.UUID{},
		RetentionClass: pgtype.Text{String: "permanent", Valid: true},
		CreatedByID:   memberID,
		CreatedByType: pgtype.Text{String: "member", Valid: true},
	})
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}

	// human_review slot tied to this task, seeded in 'submitted' so the
	// review decision lands on a realistic state.
	slot, err := q.CreateParticipantSlot(ctx, db.CreateParticipantSlotParams{
		TaskID:    task.ID,
		SlotType:  SlotTypeHumanReview,
		SlotOrder: pgtype.Int4{Int32: 0, Valid: true},
		Trigger:   pgtype.Text{String: SlotTriggerBeforeDone, Valid: true},
	})
	if err != nil {
		t.Fatalf("create slot: %v", err)
	}
	if _, err := q.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
		ID: slot.ID, Status: SlotStatusSubmitted,
	}); err != nil {
		t.Fatalf("seed submitted: %v", err)
	}

	return reviewTestEnv{
		WorkspaceID: wsID,
		MemberID:    memberID,
		PlanID:      plan.ID,
		ProjectID:   projectID,
		RunID:       run.ID,
		TaskID:      task.ID,
		ArtifactID:  artifact.ID,
		SlotID:      slot.ID,
	}
}

// addBlockingSlot inserts an extra blocking, required human_review slot on
// the same task in status='waiting' — used to verify that an approve on
// one slot does NOT complete the task while another blocking slot is open.
func addBlockingSlot(t *testing.T, q *db.Queries, taskID pgtype.UUID) db.ParticipantSlot {
	t.Helper()
	slot, err := q.CreateParticipantSlot(context.Background(), db.CreateParticipantSlotParams{
		TaskID:    taskID,
		SlotType:  SlotTypeHumanReview,
		SlotOrder: pgtype.Int4{Int32: 1, Valid: true},
		Trigger:   pgtype.Text{String: SlotTriggerBeforeDone, Valid: true},
		Blocking:  pgtype.Bool{Bool: true, Valid: true},
		Required:  pgtype.Bool{Bool: true, Valid: true},
	})
	if err != nil {
		t.Fatalf("create extra blocking slot: %v", err)
	}
	return slot
}

func TestSubmit_Approve_TerminalCompletes(t *testing.T) {
	q := testDB(t)
	env := setupReviewEnv(t, q)
	svc := NewReviewService(q, NewSlotService(q))
	ctx := context.Background()

	res, err := svc.Submit(ctx, SubmitReviewRequest{
		TaskID:       pgxToUUID(t, env.TaskID),
		ArtifactID:   pgxToUUID(t, env.ArtifactID),
		SlotID:       pgxToUUID(t, env.SlotID),
		ReviewerID:   pgxToUUID(t, env.MemberID),
		ReviewerType: ReviewerTypeMember,
		Decision:     ReviewDecisionApprove,
		Comment:      "looks good",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res.Review.Decision != ReviewDecisionApprove {
		t.Fatalf("review decision: want %s, got %s", ReviewDecisionApprove, res.Review.Decision)
	}
	if res.SlotUpdated == nil || res.SlotUpdated.Status != SlotStatusApproved {
		t.Fatalf("slot status: want approved, got %+v", res.SlotUpdated)
	}
	if res.TaskNewStatus != TaskStatusCompleted {
		t.Fatalf("task new status: want completed, got %s", res.TaskNewStatus)
	}

	// Confirm in DB.
	task, err := q.GetTask(ctx, env.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != TaskStatusCompleted {
		t.Fatalf("task DB status: want completed, got %s", task.Status)
	}
}

func TestSubmit_Approve_OtherPendingSlots_KeepsRunning(t *testing.T) {
	q := testDB(t)
	env := setupReviewEnv(t, q)
	// Add a second blocking slot that's still 'waiting' — task must NOT
	// complete just because the first slot is approved.
	_ = addBlockingSlot(t, q, env.TaskID)

	svc := NewReviewService(q, NewSlotService(q))
	ctx := context.Background()

	res, err := svc.Submit(ctx, SubmitReviewRequest{
		TaskID:       pgxToUUID(t, env.TaskID),
		ArtifactID:   pgxToUUID(t, env.ArtifactID),
		SlotID:       pgxToUUID(t, env.SlotID),
		ReviewerID:   pgxToUUID(t, env.MemberID),
		ReviewerType: ReviewerTypeMember,
		Decision:     ReviewDecisionApprove,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res.TaskNewStatus != TaskStatusRunning {
		t.Fatalf("task new status: want running (other slot pending), got %s", res.TaskNewStatus)
	}

	task, err := q.GetTask(ctx, env.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != TaskStatusRunning {
		t.Fatalf("task DB status: want running, got %s", task.Status)
	}
}

func TestSubmit_RequestChanges_TaskBecomesRunning_SlotRevisionRequested(t *testing.T) {
	q := testDB(t)
	env := setupReviewEnv(t, q)
	svc := NewReviewService(q, NewSlotService(q))
	ctx := context.Background()

	res, err := svc.Submit(ctx, SubmitReviewRequest{
		TaskID:       pgxToUUID(t, env.TaskID),
		ArtifactID:   pgxToUUID(t, env.ArtifactID),
		SlotID:       pgxToUUID(t, env.SlotID),
		ReviewerID:   pgxToUUID(t, env.MemberID),
		ReviewerType: ReviewerTypeMember,
		Decision:     ReviewDecisionRequestChanges,
		Comment:      "please tighten section 2",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res.SlotUpdated == nil || res.SlotUpdated.Status != SlotStatusRevisionRequested {
		t.Fatalf("slot status: want revision_requested, got %+v", res.SlotUpdated)
	}
	if res.TaskNewStatus != TaskStatusRunning {
		t.Fatalf("task new status: want running, got %s", res.TaskNewStatus)
	}
	task, err := q.GetTask(ctx, env.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != TaskStatusRunning {
		t.Fatalf("task DB status: want running, got %s", task.Status)
	}
}

func TestSubmit_Reject_TaskNeedsAttention_SlotRejected(t *testing.T) {
	q := testDB(t)
	env := setupReviewEnv(t, q)
	svc := NewReviewService(q, NewSlotService(q))
	ctx := context.Background()

	res, err := svc.Submit(ctx, SubmitReviewRequest{
		TaskID:       pgxToUUID(t, env.TaskID),
		ArtifactID:   pgxToUUID(t, env.ArtifactID),
		SlotID:       pgxToUUID(t, env.SlotID),
		ReviewerID:   pgxToUUID(t, env.MemberID),
		ReviewerType: ReviewerTypeMember,
		Decision:     ReviewDecisionReject,
		Comment:      "not viable",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res.SlotUpdated == nil || res.SlotUpdated.Status != SlotStatusRejected {
		t.Fatalf("slot status: want rejected, got %+v", res.SlotUpdated)
	}
	if res.TaskNewStatus != TaskStatusNeedsAttention {
		t.Fatalf("task new status: want needs_attention, got %s", res.TaskNewStatus)
	}
	task, err := q.GetTask(ctx, env.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != TaskStatusNeedsAttention {
		t.Fatalf("task DB status: want needs_attention, got %s", task.Status)
	}
}

func TestSubmit_InvalidDecision_Errors(t *testing.T) {
	q := testDB(t)
	env := setupReviewEnv(t, q)
	svc := NewReviewService(q, NewSlotService(q))

	_, err := svc.Submit(context.Background(), SubmitReviewRequest{
		TaskID:       pgxToUUID(t, env.TaskID),
		ArtifactID:   pgxToUUID(t, env.ArtifactID),
		ReviewerID:   pgxToUUID(t, env.MemberID),
		ReviewerType: ReviewerTypeMember,
		Decision:     "vibes",
	})
	if !errors.Is(err, ErrInvalidReviewDecision) {
		t.Fatalf("want ErrInvalidReviewDecision, got %v", err)
	}
}

func TestSubmit_InvalidReviewerType_Errors(t *testing.T) {
	q := testDB(t)
	env := setupReviewEnv(t, q)
	svc := NewReviewService(q, NewSlotService(q))

	_, err := svc.Submit(context.Background(), SubmitReviewRequest{
		TaskID:       pgxToUUID(t, env.TaskID),
		ArtifactID:   pgxToUUID(t, env.ArtifactID),
		ReviewerID:   pgxToUUID(t, env.MemberID),
		ReviewerType: "bot", // not in member|agent
		Decision:     ReviewDecisionApprove,
	})
	if !errors.Is(err, ErrInvalidReviewerType) {
		t.Fatalf("want ErrInvalidReviewerType, got %v", err)
	}
}

// guard: missing required IDs surface a single clear error rather than a
// confusing FK violation downstream.
func TestSubmit_MissingIDs_Errors(t *testing.T) {
	q := testDB(t)
	svc := NewReviewService(q, NewSlotService(q))

	_, err := svc.Submit(context.Background(), SubmitReviewRequest{
		TaskID:       uuid.Nil,
		ArtifactID:   uuid.Nil,
		ReviewerID:   uuid.Nil,
		ReviewerType: ReviewerTypeMember,
		Decision:     ReviewDecisionApprove,
	})
	if !errors.Is(err, ErrMissingReviewIDs) {
		t.Fatalf("want ErrMissingReviewIDs, got %v", err)
	}
}
