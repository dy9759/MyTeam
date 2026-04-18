// scheduler_test.go — DB-backed tests for SchedulerService. Requires
// DATABASE_URL pointing at a migrated multica DB (migration 059+ for
// task / participant_slot / execution / artifact / review tables).
package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// schedTestEnv holds the seed rows shared across scheduler tests: a
// workspace, member, plan, project, project_run, plus an idle agent on a
// healthy runtime that the scheduler can claim.
//
// Per-test row isolation is achieved via the test-name suffix used in
// createTestWorkspace / createTestUser, so tests are safe to run against
// the shared dev DB.
type schedTestEnv struct {
	WorkspaceID pgtype.UUID
	MemberID    pgtype.UUID
	PlanID      pgtype.UUID
	ProjectID   pgtype.UUID
	RunID       pgtype.UUID
	AgentID     pgtype.UUID
	RuntimeID   pgtype.UUID
}

func setupSchedEnv(t *testing.T, q *db.Queries) schedTestEnv {
	t.Helper()
	ctx := context.Background()

	wsID := createTestWorkspace(t, q)
	memberID := createTestUser(t, q, "sched+"+t.Name()+"@example.com", "Sched Tester")

	plan, err := q.CreatePlan(ctx, db.CreatePlanParams{
		WorkspaceID:    wsID,
		Title:          "Plan for " + t.Name(),
		Description:    pgtype.Text{String: "test plan", Valid: true},
		SourceType:     pgtype.Text{},
		SourceRefID:    pgtype.UUID{},
		Constraints:    pgtype.Text{},
		ExpectedOutput: pgtype.Text{String: "artifacts", Valid: true},
		CreatedBy:      memberID,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	pool := openTestPool(t)
	var projectID pgtype.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, created_by, schedule_type, source_conversations, creator_owner_id)
		VALUES ($1, $2, '', 'running', $3, 'one_time', '[]'::jsonb, $3)
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

	runtime, err := q.EnsureCloudRuntime(ctx, wsID)
	if err != nil {
		t.Fatalf("ensure cloud runtime: %v", err)
	}

	agent, err := q.CreatePersonalAgent(ctx, db.CreatePersonalAgentParams{
		WorkspaceID: wsID,
		Name:        "Sched Agent " + t.Name(),
		Description: "agent for scheduler test",
		RuntimeID:   runtime.ID,
		OwnerID:     memberID,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	// Mark agent idle so findAvailableAgent considers it.
	if _, err := q.UpdateAgentStatus(ctx, db.UpdateAgentStatusParams{
		ID:     agent.ID,
		Status: "idle",
	}); err != nil {
		t.Fatalf("set agent idle: %v", err)
	}

	return schedTestEnv{
		WorkspaceID: wsID,
		MemberID:    memberID,
		PlanID:      plan.ID,
		ProjectID:   projectID,
		RunID:       run.ID,
		AgentID:     agent.ID,
		RuntimeID:   runtime.ID,
	}
}

// makeSchedulerService constructs a fully-wired SchedulerService for
// testing. Bus + Hub are nil so we don't need a WebSocket loop running;
// the scheduler tolerates nil Bus/Hub for non-broadcast paths.
func makeSchedulerService(q *db.Queries) *SchedulerService {
	slots := NewSlotService(q)
	artifacts := NewArtifactService(q)
	reviews := NewReviewService(q, slots)
	quota := NewQuotaService(q)
	return NewSchedulerService(q, slots, artifacts, reviews, quota, nil, nil)
}

// makeSchedTask inserts a task on the env's plan/run with the given title
// and primary assignee. depends_on is supplied as a slice of pgtype.UUID
// to simplify dep wiring at the call site.
func makeSchedTask(t *testing.T, q *db.Queries, env schedTestEnv, title string, deps []pgtype.UUID, primary pgtype.UUID) db.Task {
	t.Helper()
	task, err := q.CreateTask(context.Background(), db.CreateTaskParams{
		PlanID:            env.PlanID,
		RunID:             env.RunID,
		WorkspaceID:       env.WorkspaceID,
		Title:             title,
		Description:       pgtype.Text{String: "do " + title, Valid: true},
		StepOrder:         pgtype.Int4{Int32: 0, Valid: true},
		DependsOn:         deps,
		PrimaryAssigneeID: primary,
	})
	if err != nil {
		t.Fatalf("create task %s: %v", title, err)
	}
	return task
}

// makeSlotForTask is a thin wrapper around CreateParticipantSlot used by
// scheduler tests that need to seed a specific slot type/trigger.
func makeSlotForTask(t *testing.T, q *db.Queries, taskID pgtype.UUID, slotType, trigger string, blocking bool) db.ParticipantSlot {
	t.Helper()
	slot, err := q.CreateParticipantSlot(context.Background(), db.CreateParticipantSlotParams{
		TaskID:    taskID,
		SlotType:  slotType,
		SlotOrder: pgtype.Int4{Int32: 0, Valid: true},
		Trigger:   pgtype.Text{String: trigger, Valid: true},
		Blocking:  pgtype.Bool{Bool: blocking, Valid: true},
		Required:  pgtype.Bool{Bool: true, Valid: true},
	})
	if err != nil {
		t.Fatalf("create slot: %v", err)
	}
	return slot
}

func TestScheduleRun_ResetsAndSchedulesReady(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	// Two tasks: B depends on A. After ScheduleRun, A should be queued
	// (an Execution row exists) and B should still be draft.
	taskA := makeSchedTask(t, q, env, "A", nil, env.AgentID)
	taskB := makeSchedTask(t, q, env, "B", []pgtype.UUID{taskA.ID}, env.AgentID)

	if err := svc.ScheduleRun(ctx, pgxToUUID(t, env.PlanID), pgxToUUID(t, env.RunID)); err != nil {
		t.Fatalf("ScheduleRun: %v", err)
	}

	gotA, err := q.GetTask(ctx, taskA.ID)
	if err != nil {
		t.Fatalf("GetTask A: %v", err)
	}
	if gotA.Status != TaskStatusQueued {
		t.Fatalf("task A status: want queued, got %s", gotA.Status)
	}
	if !gotA.ActualAgentID.Valid || gotA.ActualAgentID.Bytes != env.AgentID.Bytes {
		t.Fatalf("task A actual_agent_id: want %x, got %+v", env.AgentID.Bytes, gotA.ActualAgentID)
	}

	gotB, err := q.GetTask(ctx, taskB.ID)
	if err != nil {
		t.Fatalf("GetTask B: %v", err)
	}
	if gotB.Status != TaskStatusDraft {
		t.Fatalf("task B status: want draft (deps unmet), got %s", gotB.Status)
	}

	// Confirm an Execution row was created for task A.
	execs, err := q.ListExecutionsByTask(ctx, taskA.ID)
	if err != nil {
		t.Fatalf("ListExecutionsByTask A: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("task A executions: want 1, got %d", len(execs))
	}
}

func TestScheduleTask_BlockingHumanInput_NeedsHuman(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	task := makeSchedTask(t, q, env, "needs-human", nil, env.AgentID)
	// Blocking before_execution human_input slot — task must transition
	// to needs_human before an agent is assigned.
	slot := makeSlotForTask(t, q, task.ID, SlotTypeHumanInput, SlotTriggerBeforeExecution, true)

	if err := svc.ScheduleTask(ctx, task); err != nil {
		t.Fatalf("ScheduleTask: %v", err)
	}

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusNeedsHuman {
		t.Fatalf("task status: want needs_human, got %s", got.Status)
	}
	if got.ActualAgentID.Valid {
		t.Fatalf("task should not have actual_agent_id when needs_human, got %+v", got.ActualAgentID)
	}

	// Slot should be ready (activated by ActivateBeforeExecution).
	gotSlot, err := q.GetSlot(ctx, slot.ID)
	if err != nil {
		t.Fatalf("GetSlot: %v", err)
	}
	if gotSlot.Status != SlotStatusReady {
		t.Fatalf("slot status: want ready, got %s", gotSlot.Status)
	}

	// No execution should have been created.
	execs, err := q.ListExecutionsByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListExecutionsByTask: %v", err)
	}
	if len(execs) != 0 {
		t.Fatalf("executions: want 0, got %d", len(execs))
	}
}

func TestScheduleTask_NoAgentAvailable_NeedsAttention(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	// Create the task then mark the only candidate agent offline so the
	// scheduler has no one to claim it.
	task := makeSchedTask(t, q, env, "no-agent", nil, env.AgentID)
	if _, err := q.UpdateAgentStatus(ctx, db.UpdateAgentStatusParams{
		ID:     env.AgentID,
		Status: "offline",
	}); err != nil {
		t.Fatalf("set agent offline: %v", err)
	}

	if err := svc.ScheduleTask(ctx, task); err != nil {
		t.Fatalf("ScheduleTask: %v", err)
	}

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusNeedsAttention {
		t.Fatalf("task status: want needs_attention, got %s", got.Status)
	}
}

func TestHandleTaskCompletion_NoReviewSlot_TaskCompleted(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	// Schedule a single task end-to-end.
	task := makeSchedTask(t, q, env, "no-review", nil, env.AgentID)
	if err := svc.ScheduleRun(ctx, pgxToUUID(t, env.PlanID), pgxToUUID(t, env.RunID)); err != nil {
		t.Fatalf("ScheduleRun: %v", err)
	}

	execs, err := q.ListExecutionsByTask(ctx, task.ID)
	if err != nil || len(execs) == 0 {
		t.Fatalf("expected execution to exist: err=%v len=%d", err, len(execs))
	}
	exec := execs[0]

	if err := svc.HandleTaskCompletion(ctx,
		pgxToUUID(t, task.ID),
		pgxToUUID(t, exec.ID),
		map[string]any{"output": "done"},
	); err != nil {
		t.Fatalf("HandleTaskCompletion: %v", err)
	}

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusCompleted {
		t.Fatalf("task status: want completed, got %s", got.Status)
	}

	// An artifact should have been created for the result payload.
	pool := openTestPool(t)
	var artifactCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM artifact WHERE task_id = $1`, task.ID).Scan(&artifactCount); err != nil {
		t.Fatalf("count artifacts: %v", err)
	}
	if artifactCount != 1 {
		t.Fatalf("expected 1 artifact, got %d", artifactCount)
	}
}

func TestHandleTaskCompletion_WithReviewSlot_TaskUnderReview(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	task := makeSchedTask(t, q, env, "with-review", nil, env.AgentID)
	// Add a before_done human_review slot — completion should park the
	// task in under_review until ReviewService drives it forward.
	reviewSlot := makeSlotForTask(t, q, task.ID, SlotTypeHumanReview, SlotTriggerBeforeDone, true)

	if err := svc.ScheduleRun(ctx, pgxToUUID(t, env.PlanID), pgxToUUID(t, env.RunID)); err != nil {
		t.Fatalf("ScheduleRun: %v", err)
	}
	execs, err := q.ListExecutionsByTask(ctx, task.ID)
	if err != nil || len(execs) == 0 {
		t.Fatalf("expected execution to exist: err=%v len=%d", err, len(execs))
	}
	exec := execs[0]

	if err := svc.HandleTaskCompletion(ctx,
		pgxToUUID(t, task.ID),
		pgxToUUID(t, exec.ID),
		map[string]any{"output": "draft"},
	); err != nil {
		t.Fatalf("HandleTaskCompletion: %v", err)
	}

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusUnderReview {
		t.Fatalf("task status: want under_review, got %s", got.Status)
	}

	// The review slot should now be ready.
	gotSlot, err := q.GetSlot(ctx, reviewSlot.ID)
	if err != nil {
		t.Fatalf("GetSlot: %v", err)
	}
	if gotSlot.Status != SlotStatusReady {
		t.Fatalf("review slot status: want ready, got %s", gotSlot.Status)
	}
}

func TestHandleTaskFailure_RetryWithinPolicy(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	task := makeSchedTask(t, q, env, "retryable", nil, env.AgentID)
	if err := svc.ScheduleRun(ctx, pgxToUUID(t, env.PlanID), pgxToUUID(t, env.RunID)); err != nil {
		t.Fatalf("ScheduleRun: %v", err)
	}
	firstExecs, err := q.ListExecutionsByTask(ctx, task.ID)
	if err != nil || len(firstExecs) == 0 {
		t.Fatalf("expected initial execution: err=%v len=%d", err, len(firstExecs))
	}
	firstExec := firstExecs[0]

	// task.current_retry starts at 0 and the default retry_rule.max_retries
	// is 2, so HandleTaskFailure should bump retries + create a fresh
	// Execution rather than parking the task as needs_attention.
	if err := svc.HandleTaskFailure(ctx,
		pgxToUUID(t, task.ID),
		pgxToUUID(t, firstExec.ID),
		"transient error",
	); err != nil {
		t.Fatalf("HandleTaskFailure: %v", err)
	}

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.CurrentRetry != 1 {
		t.Fatalf("current_retry: want 1, got %d", got.CurrentRetry)
	}
	// After retry the task should be back in queued (a new Execution row).
	if got.Status != TaskStatusQueued {
		t.Fatalf("task status after retry: want queued, got %s", got.Status)
	}
	execs, err := q.ListExecutionsByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListExecutionsByTask: %v", err)
	}
	if len(execs) != 2 {
		t.Fatalf("expected 2 executions after retry, got %d", len(execs))
	}
}

func TestHandleTaskFailure_FallbackAgent(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	// Migration 062 requires a distinct owner for each personal_agent in
	// a workspace, so create a fresh user for the fallback.
	fallbackOwner := createTestUser(t, q, "fallback@example.com", "Fallback Owner")

	// Spin up a second agent on the same runtime to act as the fallback.
	fallback, err := q.CreatePersonalAgent(ctx, db.CreatePersonalAgentParams{
		WorkspaceID: env.WorkspaceID,
		Name:        "Fallback " + t.Name(),
		Description: "fallback agent",
		RuntimeID:   env.RuntimeID,
		OwnerID:     fallbackOwner,
	})
	if err != nil {
		t.Fatalf("create fallback agent: %v", err)
	}
	if _, err := q.UpdateAgentStatus(ctx, db.UpdateAgentStatusParams{
		ID:     fallback.ID,
		Status: "idle",
	}); err != nil {
		t.Fatalf("set fallback idle: %v", err)
	}

	task, err := q.CreateTask(ctx, db.CreateTaskParams{
		PlanID:             env.PlanID,
		RunID:              env.RunID,
		WorkspaceID:        env.WorkspaceID,
		Title:              "fallback-task",
		Description:        pgtype.Text{String: "exhaust primary then fallback", Valid: true},
		StepOrder:          pgtype.Int4{Int32: 0, Valid: true},
		PrimaryAssigneeID:  env.AgentID,
		FallbackAgentIds:   []pgtype.UUID{fallback.ID},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := svc.ScheduleRun(ctx, pgxToUUID(t, env.PlanID), pgxToUUID(t, env.RunID)); err != nil {
		t.Fatalf("ScheduleRun: %v", err)
	}

	// Exhaust retries on the primary by calling HandleTaskFailure
	// max_retries+1 times (default max_retries=2).
	failExec := func() {
		execs, err := q.ListExecutionsByTask(ctx, task.ID)
		if err != nil || len(execs) == 0 {
			t.Fatalf("expected execution: err=%v len=%d", err, len(execs))
		}
		latest := execs[0] // ListExecutionsByTask orders by attempt DESC
		if err := svc.HandleTaskFailure(ctx,
			pgxToUUID(t, task.ID),
			pgxToUUID(t, latest.ID),
			"primary failed",
		); err != nil {
			t.Fatalf("HandleTaskFailure: %v", err)
		}
	}

	// initial attempt was created by ScheduleRun. Two retries to bump
	// current_retry to max_retries (=2), one more failure to exhaust the
	// budget and force the fallback switch.
	failExec()
	failExec()
	failExec()

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !got.ActualAgentID.Valid {
		t.Fatalf("expected fallback agent assignment, got NULL")
	}
	if got.ActualAgentID.Bytes != fallback.ID.Bytes {
		t.Fatalf("expected actual_agent_id=%x (fallback), got %x",
			fallback.ID.Bytes, got.ActualAgentID.Bytes)
	}
}

// TestHandleTaskFailure_FallbackResetsRetryBudget verifies that when the
// scheduler swaps to a fallback agent because the primary exhausted its
// retry budget, current_retry is reset to 0 so the fallback gets a fresh
// budget rather than inheriting the exhausted one (codex review IMPORTANT #2).
func TestHandleTaskFailure_FallbackResetsRetryBudget(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	// See migration 062: each personal_agent in a workspace needs a unique owner.
	fallbackOwner := createTestUser(t, q, "fallback-reset@example.com", "Fallback Reset Owner")

	fallback, err := q.CreatePersonalAgent(ctx, db.CreatePersonalAgentParams{
		WorkspaceID: env.WorkspaceID,
		Name:        "Fallback " + t.Name(),
		Description: "fallback agent",
		RuntimeID:   env.RuntimeID,
		OwnerID:     fallbackOwner,
	})
	if err != nil {
		t.Fatalf("create fallback agent: %v", err)
	}
	if _, err := q.UpdateAgentStatus(ctx, db.UpdateAgentStatusParams{
		ID:     fallback.ID,
		Status: "idle",
	}); err != nil {
		t.Fatalf("set fallback idle: %v", err)
	}

	task, err := q.CreateTask(ctx, db.CreateTaskParams{
		PlanID:            env.PlanID,
		RunID:             env.RunID,
		WorkspaceID:       env.WorkspaceID,
		Title:             "fallback-budget",
		Description:       pgtype.Text{String: "exhaust primary, ensure fallback budget reset", Valid: true},
		StepOrder:         pgtype.Int4{Int32: 0, Valid: true},
		PrimaryAssigneeID: env.AgentID,
		FallbackAgentIds:  []pgtype.UUID{fallback.ID},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := svc.ScheduleRun(ctx, pgxToUUID(t, env.PlanID), pgxToUUID(t, env.RunID)); err != nil {
		t.Fatalf("ScheduleRun: %v", err)
	}

	// Default retry policy: max_retries=2. Three failures exhaust the
	// primary's budget and trigger the fallback switch.
	failOnce := func() {
		execs, err := q.ListExecutionsByTask(ctx, task.ID)
		if err != nil || len(execs) == 0 {
			t.Fatalf("expected execution: err=%v len=%d", err, len(execs))
		}
		if err := svc.HandleTaskFailure(ctx,
			pgxToUUID(t, task.ID),
			pgxToUUID(t, execs[0].ID),
			"primary failed",
		); err != nil {
			t.Fatalf("HandleTaskFailure: %v", err)
		}
	}
	failOnce()
	failOnce()
	failOnce()

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !got.ActualAgentID.Valid || got.ActualAgentID.Bytes != fallback.ID.Bytes {
		t.Fatalf("expected fallback agent assigned, got %+v", got.ActualAgentID)
	}
	if got.CurrentRetry != 0 {
		t.Fatalf("expected current_retry reset to 0 after fallback switch, got %d", got.CurrentRetry)
	}
}

// TestHandleTaskFailure_NeedsAttentionNotifiesOwner verifies that when
// HandleTaskFailure exhausts retries and has no fallback, it not only parks
// the task in needs_attention but also creates an inbox notification for
// the agent's owner (project §10.6 — replaces the TODOs in HandleTaskFailure).
func TestHandleTaskFailure_NeedsAttentionNotifiesOwner(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	task := makeSchedTask(t, q, env, "no-fallback", nil, env.AgentID)
	if err := svc.ScheduleRun(ctx, pgxToUUID(t, env.PlanID), pgxToUUID(t, env.RunID)); err != nil {
		t.Fatalf("ScheduleRun: %v", err)
	}

	// max_retries=2 + no fallback → 3 failures (1 initial budget then 2
	// retries) park the task in needs_attention.
	failOnce := func() {
		execs, err := q.ListExecutionsByTask(ctx, task.ID)
		if err != nil || len(execs) == 0 {
			t.Fatalf("expected execution: err=%v len=%d", err, len(execs))
		}
		if err := svc.HandleTaskFailure(ctx,
			pgxToUUID(t, task.ID),
			pgxToUUID(t, execs[0].ID),
			"agent error",
		); err != nil {
			t.Fatalf("HandleTaskFailure: %v", err)
		}
	}
	failOnce()
	failOnce()
	failOnce()

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusNeedsAttention {
		t.Fatalf("status: want needs_attention, got %s", got.Status)
	}

	pool := openTestPool(t)
	var inboxCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM inbox_item
		WHERE task_id = $1 AND type = 'task_attention_needed'
	`, task.ID).Scan(&inboxCount); err != nil {
		t.Fatalf("count inbox: %v", err)
	}
	if inboxCount == 0 {
		t.Fatalf("expected an inbox_item after retries exhausted, got 0")
	}
}

// TestHandleTaskTimeout_RetryAction (default) follows the existing
// HandleTaskFailure path: bumps current_retry and re-queues the task.
func TestHandleTaskTimeout_RetryAction(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	task := makeSchedTask(t, q, env, "timeout-retry", nil, env.AgentID)
	if err := svc.ScheduleRun(ctx, pgxToUUID(t, env.PlanID), pgxToUUID(t, env.RunID)); err != nil {
		t.Fatalf("ScheduleRun: %v", err)
	}

	// timeout_rule defaults to action=retry, so HandleTaskTimeout should
	// take the retry branch.
	if err := svc.HandleTaskTimeout(ctx, pgxToUUID(t, task.ID)); err != nil {
		t.Fatalf("HandleTaskTimeout: %v", err)
	}

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.CurrentRetry != 1 {
		t.Fatalf("current_retry: want 1, got %d", got.CurrentRetry)
	}
	if got.Status != TaskStatusQueued {
		t.Fatalf("status: want queued, got %s", got.Status)
	}
}

// TestHandleTaskTimeout_FailAction marks the task failed without consuming
// the retry budget.
func TestHandleTaskTimeout_FailAction(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	task, err := q.CreateTask(ctx, db.CreateTaskParams{
		PlanID:            env.PlanID,
		RunID:             env.RunID,
		WorkspaceID:       env.WorkspaceID,
		Title:             "timeout-fail",
		Description:       pgtype.Text{String: "fail action", Valid: true},
		StepOrder:         pgtype.Int4{Int32: 0, Valid: true},
		PrimaryAssigneeID: env.AgentID,
		TimeoutRule:       []byte(`{"max_duration_seconds":1800,"action":"fail"}`),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := svc.HandleTaskTimeout(ctx, pgxToUUID(t, task.ID)); err != nil {
		t.Fatalf("HandleTaskTimeout: %v", err)
	}

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusFailed {
		t.Fatalf("status: want failed, got %s", got.Status)
	}
	if got.CurrentRetry != 0 {
		t.Fatalf("current_retry should not be incremented on fail action, got %d", got.CurrentRetry)
	}
}

// TestHandleTaskTimeout_EscalateAction parks the task in needs_attention and
// (per T2d) creates an inbox notification for the owner.
func TestHandleTaskTimeout_EscalateAction(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	task, err := q.CreateTask(ctx, db.CreateTaskParams{
		PlanID:            env.PlanID,
		RunID:             env.RunID,
		WorkspaceID:       env.WorkspaceID,
		Title:             "timeout-escalate",
		Description:       pgtype.Text{String: "escalate action", Valid: true},
		StepOrder:         pgtype.Int4{Int32: 0, Valid: true},
		PrimaryAssigneeID: env.AgentID,
		TimeoutRule:       []byte(`{"max_duration_seconds":1800,"action":"escalate"}`),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	// Pretend the scheduler already assigned the primary — this is required
	// for the inbox notification path to resolve the task owner.
	if err := q.AssignTaskAgent(ctx, db.AssignTaskAgentParams{
		ID:            task.ID,
		ActualAgentID: env.AgentID,
	}); err != nil {
		t.Fatalf("AssignTaskAgent: %v", err)
	}

	if err := svc.HandleTaskTimeout(ctx, pgxToUUID(t, task.ID)); err != nil {
		t.Fatalf("HandleTaskTimeout: %v", err)
	}

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusNeedsAttention {
		t.Fatalf("status: want needs_attention, got %s", got.Status)
	}

	// Confirm the inbox notification was created for the agent owner.
	pool := openTestPool(t)
	var inboxCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM inbox_item
		WHERE task_id = $1 AND type = 'task_attention_needed'
	`, task.ID).Scan(&inboxCount); err != nil {
		t.Fatalf("count inbox: %v", err)
	}
	if inboxCount == 0 {
		t.Fatalf("expected an inbox_item for the timeout escalation, got 0")
	}
}

// guard: nil run id passed to checkRunCompletion must be a no-op rather
// than blowing up on the db.UpdateProjectRunStatus call.
func TestCheckRunCompletion_NilRunID_NoOp(t *testing.T) {
	q := testDB(t)
	svc := makeSchedulerService(q)
	if err := svc.checkRunCompletion(context.Background(), uuid.Nil); err != nil {
		t.Fatalf("expected nil err for empty run id, got %v", err)
	}
}

// TestHandleTaskTimeout_MissingAction verifies that a task whose
// timeout_rule has an empty action string falls through to the retry path
// (default branch in HandleTaskTimeout). Mirrors the production safety net
// where unparseable / missing actions degrade to retry rather than
// silently dropping the timeout.
func TestHandleTaskTimeout_MissingAction(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	task, err := q.CreateTask(ctx, db.CreateTaskParams{
		PlanID:            env.PlanID,
		RunID:             env.RunID,
		WorkspaceID:       env.WorkspaceID,
		Title:             "timeout-missing-action",
		Description:       pgtype.Text{String: "empty action -> retry", Valid: true},
		StepOrder:         pgtype.Int4{Int32: 0, Valid: true},
		PrimaryAssigneeID: env.AgentID,
		// action explicitly empty so the empty-action branch is exercised.
		TimeoutRule: []byte(`{"max_duration_seconds":1800,"action":""}`),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := svc.ScheduleRun(ctx, pgxToUUID(t, env.PlanID), pgxToUUID(t, env.RunID)); err != nil {
		t.Fatalf("ScheduleRun: %v", err)
	}

	if err := svc.HandleTaskTimeout(ctx, pgxToUUID(t, task.ID)); err != nil {
		t.Fatalf("HandleTaskTimeout: %v", err)
	}

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.CurrentRetry != 1 {
		t.Fatalf("current_retry: want 1 (retry path), got %d", got.CurrentRetry)
	}
	if got.Status != TaskStatusQueued {
		t.Fatalf("status: want queued (retry path), got %s", got.Status)
	}
}

// TestHandleTaskTimeout_UnknownAction verifies that an unrecognized
// timeout action ("bogus") also falls through to the retry path. This
// keeps unknown policies from silently dropping timeouts.
func TestHandleTaskTimeout_UnknownAction(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	task, err := q.CreateTask(ctx, db.CreateTaskParams{
		PlanID:            env.PlanID,
		RunID:             env.RunID,
		WorkspaceID:       env.WorkspaceID,
		Title:             "timeout-unknown-action",
		Description:       pgtype.Text{String: "unknown action -> retry", Valid: true},
		StepOrder:         pgtype.Int4{Int32: 0, Valid: true},
		PrimaryAssigneeID: env.AgentID,
		TimeoutRule:       []byte(`{"max_duration_seconds":1800,"action":"bogus"}`),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := svc.ScheduleRun(ctx, pgxToUUID(t, env.PlanID), pgxToUUID(t, env.RunID)); err != nil {
		t.Fatalf("ScheduleRun: %v", err)
	}

	if err := svc.HandleTaskTimeout(ctx, pgxToUUID(t, task.ID)); err != nil {
		t.Fatalf("HandleTaskTimeout: %v", err)
	}

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.CurrentRetry != 1 {
		t.Fatalf("current_retry: want 1 (retry path), got %d", got.CurrentRetry)
	}
	if got.Status != TaskStatusQueued {
		t.Fatalf("status: want queued (retry path), got %s", got.Status)
	}
}

// TestNotifyTaskAttention_SystemAgent_FallsBackToProjectCreator covers the
// regression Codex flagged: when a needs_attention task is assigned to a
// system agent (owner_id IS NULL), notifyTaskAttention used to bail out
// silently. It now falls back to the project creator (per project.run).
func TestNotifyTaskAttention_SystemAgent_FallsBackToProjectCreator(t *testing.T) {
	q := testDB(t)
	env := setupSchedEnv(t, q)
	svc := makeSchedulerService(q)
	ctx := context.Background()

	// Create a workspace-level system agent (owner_id NULL by definition).
	sysAgent, err := q.CreateSystemAgent(ctx, db.CreateSystemAgentParams{
		WorkspaceID: env.WorkspaceID,
		RuntimeID:   env.RuntimeID,
	})
	if err != nil {
		t.Fatalf("create system agent: %v", err)
	}
	if sysAgent.OwnerID.Valid {
		t.Fatalf("system agent should have NULL owner_id, got %+v", sysAgent.OwnerID)
	}

	task, err := q.CreateTask(ctx, db.CreateTaskParams{
		PlanID:            env.PlanID,
		RunID:             env.RunID,
		WorkspaceID:       env.WorkspaceID,
		Title:             "system-agent-escalate",
		Description:       pgtype.Text{String: "system agent -> needs_attention", Valid: true},
		StepOrder:         pgtype.Int4{Int32: 0, Valid: true},
		PrimaryAssigneeID: sysAgent.ID,
		TimeoutRule:       []byte(`{"max_duration_seconds":1800,"action":"escalate"}`),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := q.AssignTaskAgent(ctx, db.AssignTaskAgentParams{
		ID:            task.ID,
		ActualAgentID: sysAgent.ID,
	}); err != nil {
		t.Fatalf("AssignTaskAgent: %v", err)
	}

	if err := svc.HandleTaskTimeout(ctx, pgxToUUID(t, task.ID)); err != nil {
		t.Fatalf("HandleTaskTimeout: %v", err)
	}

	got, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusNeedsAttention {
		t.Fatalf("status: want needs_attention, got %s", got.Status)
	}

	// Confirm an inbox item was created for the project creator (the fallback
	// recipient), not silently dropped because the agent has no owner.
	pool := openTestPool(t)
	rows, err := pool.Query(ctx, `
		SELECT recipient_id
		FROM inbox_item
		WHERE task_id = $1 AND type = 'task_attention_needed'
	`, task.ID)
	if err != nil {
		t.Fatalf("query inbox: %v", err)
	}
	defer rows.Close()
	recipients := []pgtype.UUID{}
	for rows.Next() {
		var rid pgtype.UUID
		if err := rows.Scan(&rid); err != nil {
			t.Fatalf("scan inbox row: %v", err)
		}
		recipients = append(recipients, rid)
	}
	if len(recipients) == 0 {
		t.Fatalf("expected inbox_item for system-agent escalation, got 0")
	}
	// Recipient should be the project creator we seeded in setupSchedEnv
	// (creator_owner_id = MemberID).
	if recipients[0].Bytes != env.MemberID.Bytes {
		t.Fatalf("expected recipient=%x (project creator), got %x",
			env.MemberID.Bytes, recipients[0].Bytes)
	}
}
