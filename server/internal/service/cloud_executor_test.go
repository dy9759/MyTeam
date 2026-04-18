// cloud_executor_test.go — DB-backed tests for the execution-table polling
// path of CloudExecutorService (added in plan5 D3). Requires DATABASE_URL
// pointing at a migrated multica DB with the execution + workspace_quota
// tables present.
//
// These tests do NOT cover the legacy agent_task_queue path (pollAndExecute);
// that path is exercised by the existing Issue tests.
package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// cloudExecEnv extends schedTestEnv with the bits cloud_executor_test
// specifically needs (a CloudExecutorService bound to the SchedulerService
// from the same workspace so completion cascades work end-to-end).
type cloudExecEnv struct {
	schedTestEnv
	Svc       *CloudExecutorService
	Scheduler *SchedulerService
}

func setupCloudExecEnv(t *testing.T, q *db.Queries) cloudExecEnv {
	t.Helper()
	env := setupSchedEnv(t, q)
	scheduler := makeSchedulerService(q)

	// TaskService is required by CloudExecutorService for the legacy path
	// but not by the execution-table path; passing nil would crash the
	// constructor's downstream NewQuotaService call wiring, so we instantiate
	// a real TaskService against the same queries.
	taskSvc := NewTaskService(q, nil, nil)
	svc := NewCloudExecutorService(q, nil, nil, taskSvc, scheduler)

	return cloudExecEnv{
		schedTestEnv: env,
		Svc:          svc,
		Scheduler:    scheduler,
	}
}

// seedQueuedExecution seeds a Task → Execution(status=queued) pair on the
// env's plan/run/agent. Returns the freshly-created execution row.
func seedQueuedExecution(t *testing.T, q *db.Queries, env cloudExecEnv, title string) (db.Task, db.Execution) {
	t.Helper()
	ctx := context.Background()

	task := makeSchedTask(t, q, env.schedTestEnv, title, nil, env.AgentID)

	payload, err := json.Marshal(map[string]any{
		"task_id":     uuid.UUID(task.ID.Bytes).String(),
		"title":       title,
		"description": "from " + t.Name(),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	exec, err := q.CreateExecution(ctx, db.CreateExecutionParams{
		TaskID:    task.ID,
		RunID:     env.RunID,
		AgentID:   env.AgentID,
		RuntimeID: env.RuntimeID,
		Payload:   payload,
	})
	if err != nil {
		t.Fatalf("create execution: %v", err)
	}
	if exec.Status != "queued" {
		t.Fatalf("seeded execution should start queued, got %s", exec.Status)
	}
	return task, exec
}

// fakeScheduler captures HandleTaskCompletion / HandleTaskFailure calls so
// tests can assert the cascade fired without depending on the real scheduler
// touching downstream state. It still needs the real scheduler dependencies
// because CloudExecutorService treats Scheduler as a *SchedulerService — so
// instead of mocking the type, we wrap by using the real scheduler and
// verifying observable side effects (artifact created, task status changed).

// waitForExecStatus polls the execution row until it transitions out of
// 'queued'/'claimed'/'running' or the deadline elapses. Returns the latest
// row regardless of outcome so callers can assert on the final state.
func waitForExecStatus(t *testing.T, q *db.Queries, execID pgtype.UUID, want string, deadline time.Duration) db.Execution {
	t.Helper()
	ctx := context.Background()
	end := time.Now().Add(deadline)
	var latest db.Execution
	for time.Now().Before(end) {
		row, err := q.GetExecution(ctx, execID)
		if err != nil {
			t.Fatalf("get execution: %v", err)
		}
		latest = row
		if row.Status == want {
			return row
		}
		time.Sleep(25 * time.Millisecond)
	}
	return latest
}

func TestPollAndExecuteExecutions_ClaimAndComplete(t *testing.T) {
	q := testDB(t)
	env := setupCloudExecEnv(t, q)
	ctx := context.Background()

	// Generous quota so the gate doesn't interfere.
	setQuota(t, q, env.WorkspaceID, 100.0, 0.0, 10)

	task, exec := seedQueuedExecution(t, q, env, "claim-and-complete")

	env.Svc.pollAndExecuteExecutions(ctx)

	// runExecutionAsync runs in a goroutine; wait for the completed transition.
	final := waitForExecStatus(t, q, exec.ID, "completed", 3*time.Second)
	if final.Status != "completed" {
		t.Fatalf("execution status: want completed, got %s", final.Status)
	}

	// Result should be the stub map we wrote.
	var result map[string]any
	if err := json.Unmarshal(final.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if stub, _ := result["stub"].(bool); !stub {
		t.Fatalf("expected stub result, got %+v", result)
	}

	// Scheduler.HandleTaskCompletion must have fired — observable via the
	// task transitioning to 'completed' (no review slot path) and a single
	// artifact persisted for the result payload.
	gotTask, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if gotTask.Status != TaskStatusCompleted {
		t.Fatalf("task status: want completed, got %s", gotTask.Status)
	}

	pool := openTestPool(t)
	var artifactCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM artifact WHERE task_id = $1`, task.ID).Scan(&artifactCount); err != nil {
		t.Fatalf("count artifacts: %v", err)
	}
	if artifactCount != 1 {
		t.Fatalf("expected 1 artifact (cascade from scheduler), got %d", artifactCount)
	}
}

func TestPollAndExecuteExecutions_QuotaBlocks(t *testing.T) {
	q := testDB(t)
	env := setupCloudExecEnv(t, q)
	ctx := context.Background()

	// Hard zero on max_concurrent_cloud_exec → CheckBeforeClaim returns
	// ErrQuotaConcurrentLimit even with 0 inflight, so the runtime should be
	// skipped entirely.
	setQuota(t, q, env.WorkspaceID, 100.0, 0.0, 0)

	_, exec := seedQueuedExecution(t, q, env, "quota-blocks")

	env.Svc.pollAndExecuteExecutions(ctx)

	// Give the goroutine a beat in case the gate didn't actually fire (this
	// failure mode is what we want to catch).
	time.Sleep(100 * time.Millisecond)

	got, err := q.GetExecution(ctx, exec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if got.Status != "queued" {
		t.Fatalf("execution should remain queued when quota blocks, got %s", got.Status)
	}
}

func TestRunExecutionAsync_FailureCascadesToScheduler(t *testing.T) {
	q := testDB(t)
	env := setupCloudExecEnv(t, q)
	ctx := context.Background()

	// Build an execution where GetAgent will fail (we point AgentID at a
	// random UUID that doesn't exist). The execution itself only has FK
	// constraints on task/run/agent/runtime — but agent has ON DELETE
	// CASCADE-ish semantics from earlier migrations, so we need a different
	// approach: create the execution against the real agent, then delete the
	// agent row before the goroutine runs. That's racy. Instead, we call
	// failExecution directly with a hand-crafted execution row that has a
	// valid task_id but a synthetic execution id, exercising the cascade
	// code path without depending on goroutine timing.

	task := makeSchedTask(t, q, env.schedTestEnv, "fail-cascade", nil, env.AgentID)

	payload, _ := json.Marshal(map[string]any{"task_id": uuid.UUID(task.ID.Bytes).String()})
	exec, err := q.CreateExecution(ctx, db.CreateExecutionParams{
		TaskID:    task.ID,
		RunID:     env.RunID,
		AgentID:   env.AgentID,
		RuntimeID: env.RuntimeID,
		Payload:   payload,
	})
	if err != nil {
		t.Fatalf("create execution: %v", err)
	}

	// Drive the failure path directly — this is the same path
	// runExecutionAsync uses on agent/runtime lookup failure.
	env.Svc.failExecution(ctx, exec, "synthetic failure for test")

	// Execution must be marked failed with the error captured.
	got, err := q.GetExecution(ctx, exec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("execution status: want failed, got %s", got.Status)
	}
	if !got.Error.Valid || got.Error.String != "synthetic failure for test" {
		t.Fatalf("execution error: want 'synthetic failure for test', got %+v", got.Error)
	}

	// Scheduler.HandleTaskFailure must have fired. With max_retries=2 (the
	// default) and current_retry=0, the failure path increments retry +
	// re-schedules a fresh execution rather than parking the task — so we
	// expect the task to either remain in a non-terminal state and a new
	// execution row to exist for the same task.
	execs, err := q.ListExecutionsByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if len(execs) < 2 {
		t.Fatalf("expected scheduler to create a retry execution; only %d exec(s) exist", len(execs))
	}

	gotTask, err := q.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if gotTask.Status == TaskStatusCompleted {
		t.Fatalf("task should not be completed after a failure cascade; got %s", gotTask.Status)
	}
}

// TestPollAndExecuteExecutions_NilSchedulerToleranceTolerated ensures the
// poll loop doesn't NPE when CloudExecutorService was constructed without a
// scheduler (e.g. in legacy startup paths). The execution should still be
// claimed and marked completed; only the cascade is skipped.
func TestPollAndExecuteExecutions_NilSchedulerToleranceTolerated(t *testing.T) {
	q := testDB(t)
	env := setupCloudExecEnv(t, q)
	env.Svc.Scheduler = nil // simulate legacy wiring
	ctx := context.Background()

	setQuota(t, q, env.WorkspaceID, 100.0, 0.0, 10)

	_, exec := seedQueuedExecution(t, q, env, "nil-scheduler")

	env.Svc.pollAndExecuteExecutions(ctx)
	final := waitForExecStatus(t, q, exec.ID, "completed", 3*time.Second)
	if final.Status != "completed" {
		t.Fatalf("execution status: want completed, got %s", final.Status)
	}
}

// guard: the poll loop must serialize claims across a single tick — i.e.
// two queued executions on the same runtime should NOT both be claimed in
// one tick (ListCloudRuntimes returns the runtime once, ClaimExecution picks
// one row). This is a smoke test that the SKIP LOCKED query honors LIMIT 1.
func TestPollAndExecuteExecutions_OneClaimPerTick(t *testing.T) {
	q := testDB(t)
	env := setupCloudExecEnv(t, q)
	ctx := context.Background()
	setQuota(t, q, env.WorkspaceID, 100.0, 0.0, 10)

	_, e1 := seedQueuedExecution(t, q, env, "one-per-tick-1")
	_, e2 := seedQueuedExecution(t, q, env, "one-per-tick-2")

	// Drive a single tick.
	env.Svc.pollAndExecuteExecutions(ctx)

	// Wait briefly so the goroutine has a chance to run.
	time.Sleep(50 * time.Millisecond)

	got1, _ := q.GetExecution(ctx, e1.ID)
	got2, _ := q.GetExecution(ctx, e2.ID)

	claimed := 0
	for _, s := range []string{got1.Status, got2.Status} {
		// 'queued' means untouched; anything else means the row was claimed.
		if s != "queued" {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("expected exactly 1 execution claimed per tick, got %d (e1=%s e2=%s)",
			claimed, got1.Status, got2.Status)
	}
}

// guard: keep the linter happy if sync is imported elsewhere later. Not used
// directly in the assertions, but kept here as a reminder that future tests
// may want to fan out concurrent ticks.
var _ = sync.WaitGroup{}
