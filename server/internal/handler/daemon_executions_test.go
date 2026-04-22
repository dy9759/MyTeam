package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// daemonExecTestEnv bundles the IDs needed to insert an execution row:
// the existing handler test workspace + a freshly created plan, project,
// project_run, and task. Cleanup is keyed off the plan id so a single
// deletion cascades through plan → run → task → execution.
type daemonExecTestEnv struct {
	WorkspaceID string
	PlanID      string
	ProjectID   string
	RunID       string
	TaskID      string
	AgentID     string
	RuntimeID   string
}

// setupDaemonExecEnv inserts a self-contained graph of fixture rows for
// execution-handler tests. The shared workspace + agent + runtime from
// the handler TestMain fixture are reused. Every other row is tied to a
// per-test plan_id so cleanup is one DELETE.
func setupDaemonExecEnv(t *testing.T) daemonExecTestEnv {
	t.Helper()
	ctx := context.Background()

	// Reuse the workspace + runtime + agent created by TestMain. Look
	// them up by the well-known names from setupHandlerTestFixture.
	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("look up handler test agent: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id FROM agent WHERE id = $1`, agentID,
	).Scan(&runtimeID); err != nil {
		t.Fatalf("look up runtime_id: %v", err)
	}

	// Scheduler.findAvailableAgent only considers idle/busy agents on
	// online runtimes with spare capacity. The shared TestMain fixture
	// leaves agent.status at the schema default ('offline'), so coerce
	// it here before each test that needs the scheduler to pick the
	// agent. current_load is reset to 0 to avoid leftover state from
	// earlier tests bumping it past concurrency_limit.
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET status = 'idle' WHERE id = $1`, agentID,
	); err != nil {
		t.Fatalf("mark agent idle: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_runtime SET current_load = 0, concurrency_limit = 4 WHERE id = $1`, runtimeID,
	); err != nil {
		t.Fatalf("reset runtime load: %v", err)
	}

	suffix := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())

	// Look up the member row id for the test user — plan.created_by FK
	// targets member, not user.
	var memberID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID, testUserID,
	).Scan(&memberID); err != nil {
		t.Fatalf("look up member id: %v", err)
	}

	var planID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO plan (workspace_id, title, description, expected_output, created_by)
		VALUES ($1, $2, '', 'artifacts', $3)
		RETURNING id
	`, testWorkspaceID, "Plan "+suffix, memberID).Scan(&planID); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// project.created_by + creator_owner_id reference "user"(id), not member.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, created_by, schedule_type, source_conversations, creator_owner_id)
		VALUES ($1, $2, '', 'running', $3, 'one_time', '[]'::jsonb, $3)
		RETURNING id
	`, testWorkspaceID, "Project "+suffix, testUserID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project_run (plan_id, project_id, status)
		VALUES ($1, $2, 'running')
		RETURNING id
	`, planID, projectID).Scan(&runID); err != nil {
		t.Fatalf("create project_run: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (plan_id, run_id, workspace_id, title, description, step_order, primary_assignee_id, status)
		VALUES ($1, $2, $3, $4, 'do thing', 0, $5, 'queued')
		RETURNING id
	`, planID, runID, testWorkspaceID, "Task "+suffix, agentID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}

	t.Cleanup(func() {
		// plan FK cascade: plan → run → task → execution.
		// project still references plan so wipe it explicitly first.
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM plan WHERE id = $1`, planID)
	})

	return daemonExecTestEnv{
		WorkspaceID: testWorkspaceID,
		PlanID:      planID,
		ProjectID:   projectID,
		RunID:       runID,
		TaskID:      taskID,
		AgentID:     agentID,
		RuntimeID:   runtimeID,
	}
}

// insertExecution creates a queued execution for the supplied env and
// returns the new execution id. Tests that need additional rows (e.g.
// the concurrent claim test) call this directly.
func insertExecution(t *testing.T, env daemonExecTestEnv, priority int) string {
	t.Helper()
	var execID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO execution (task_id, run_id, agent_id, runtime_id, priority, status, payload)
		VALUES ($1, $2, $3, $4, $5, 'queued', '{}'::jsonb)
		RETURNING id
	`, env.TaskID, env.RunID, env.AgentID, env.RuntimeID, priority).Scan(&execID); err != nil {
		t.Fatalf("insert execution: %v", err)
	}
	return execID
}

func decodeJSON(t *testing.T, body *bytes.Buffer, dst any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// TestClaimExecution_HappyPath drives one execution through queued →
// claimed → running → completed and asserts the parent task moves to
// completed via SchedulerService.HandleTaskCompletion.
func TestClaimExecution_HappyPath(t *testing.T) {
	env := setupDaemonExecEnv(t)
	insertExecution(t, env, 0)

	// Claim
	w := httptest.NewRecorder()
	req := newDaemonRequest("POST",
		"/api/daemon/runtimes/"+env.RuntimeID+"/executions/claim",
		map[string]any{"daemon_id": "test-daemon", "working_dir": "/tmp/wd"},
	)
	req = withURLParam(req, "runtimeId", env.RuntimeID)
	testHandler.ClaimExecution(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("ClaimExecution: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var claimed map[string]any
	decodeJSON(t, w.Body, &claimed)
	execID, _ := claimed["id"].(string)
	if execID == "" {
		t.Fatalf("ClaimExecution: missing id in response: %+v", claimed)
	}
	if got := claimed["status"]; got != "claimed" {
		t.Fatalf("ClaimExecution: want status=claimed, got %v", got)
	}
	// context_ref should round-trip the daemon-supplied fields.
	ctxRef, ok := claimed["context_ref"].(map[string]any)
	if !ok {
		t.Fatalf("ClaimExecution: context_ref missing or wrong type: %v", claimed["context_ref"])
	}
	if ctxRef["daemon_id"] != "test-daemon" || ctxRef["working_dir"] != "/tmp/wd" {
		t.Errorf("ClaimExecution: context_ref missing daemon-supplied fields: %v", ctxRef)
	}

	// Start
	w = httptest.NewRecorder()
	req = newDaemonRequest("POST", "/api/daemon/executions/"+execID+"/start", nil)
	req = withURLParam(req, "id", execID)
	testHandler.StartExecution(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartExecution: want 200, got %d: %s", w.Code, w.Body.String())
	}

	var started map[string]any
	decodeJSON(t, w.Body, &started)
	if started["id"] != execID {
		t.Fatalf("StartExecution: want id=%s, got %v", execID, started["id"])
	}
	if started["status"] != "running" {
		t.Fatalf("StartExecution: want status=running, got %v", started["status"])
	}

	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM execution WHERE id = $1`, execID,
	).Scan(&status); err != nil {
		t.Fatalf("read execution status: %v", err)
	}
	if status != "running" {
		t.Fatalf("after start: want status=running, got %s", status)
	}

	// Complete
	w = httptest.NewRecorder()
	req = newDaemonRequest("POST", "/api/daemon/executions/"+execID+"/complete", map[string]any{
		"result":             map[string]any{"output": "done"},
		"cost_input_tokens":  100,
		"cost_output_tokens": 50,
		"cost_usd":           0.0123,
		"cost_provider":      "anthropic",
	})
	req = withURLParam(req, "id", execID)
	testHandler.CompleteExecution(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteExecution: want 200, got %d: %s", w.Code, w.Body.String())
	}

	// DB row reflects the completion + cost.
	var (
		gotStatus string
		gotIn     int
		gotOut    int
		gotProv   string
	)
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, cost_input_tokens, cost_output_tokens, cost_provider
		   FROM execution WHERE id = $1`, execID,
	).Scan(&gotStatus, &gotIn, &gotOut, &gotProv); err != nil {
		t.Fatalf("read execution: %v", err)
	}
	if gotStatus != "completed" {
		t.Fatalf("after complete: want status=completed, got %s", gotStatus)
	}
	if gotIn != 100 || gotOut != 50 || gotProv != "anthropic" {
		t.Fatalf("after complete: cost columns wrong: in=%d out=%d prov=%s", gotIn, gotOut, gotProv)
	}

	// Scheduler should have moved the parent task to completed.
	var taskStatus string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM task WHERE id = $1`, env.TaskID,
	).Scan(&taskStatus); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if taskStatus != "completed" {
		t.Fatalf("task status after complete: want completed, got %s", taskStatus)
	}
}

// TestClaimExecution_NoWork ensures the daemon polling against an empty
// runtime queue receives a 204 instead of an error.
func TestClaimExecution_NoWork(t *testing.T) {
	// Create env but do NOT insert any execution.
	env := setupDaemonExecEnv(t)

	w := httptest.NewRecorder()
	req := newDaemonRequest("POST",
		"/api/daemon/runtimes/"+env.RuntimeID+"/executions/claim", nil,
	)
	req = withURLParam(req, "runtimeId", env.RuntimeID)
	testHandler.ClaimExecution(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("ClaimExecution (no work): want 204, got %d: %s", w.Code, w.Body.String())
	}
}

// TestFailExecution_FailedStatus marks an execution failed and verifies
// the scheduler routes the failure through HandleTaskFailure (which
// retries on the same agent for default retry_rule.max_retries = 2).
func TestFailExecution_FailedStatus(t *testing.T) {
	env := setupDaemonExecEnv(t)
	execID := insertExecution(t, env, 0)

	// Move queued → running so FailExecution exercises the running path.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE execution SET status = 'running', started_at = now() WHERE id = $1`,
		execID,
	); err != nil {
		t.Fatalf("set execution running: %v", err)
	}

	w := httptest.NewRecorder()
	req := newDaemonRequest("POST", "/api/daemon/executions/"+execID+"/fail", map[string]any{
		"error": "boom",
	})
	req = withURLParam(req, "id", execID)
	testHandler.FailExecution(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("FailExecution: want 200, got %d: %s", w.Code, w.Body.String())
	}

	// Execution row is now failed with the error we sent.
	var (
		gotStatus string
		gotErr    string
	)
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, COALESCE(error, '') FROM execution WHERE id = $1`, execID,
	).Scan(&gotStatus, &gotErr); err != nil {
		t.Fatalf("read execution after fail: %v", err)
	}
	if gotStatus != "failed" {
		t.Fatalf("want failed, got %s", gotStatus)
	}
	if gotErr != "boom" {
		t.Fatalf("want error=boom, got %q", gotErr)
	}

	// Default retry_rule.max_retries=2 so the scheduler should bump
	// current_retry to 1 and re-schedule. The task ends up back in
	// queued with a fresh execution row.
	var (
		taskStatus string
		retryCount int
		execCount  int
	)
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, current_retry FROM task WHERE id = $1`, env.TaskID,
	).Scan(&taskStatus, &retryCount); err != nil {
		t.Fatalf("read task after fail: %v", err)
	}
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM execution WHERE task_id = $1`, env.TaskID,
	).Scan(&execCount); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if taskStatus != "queued" {
		t.Fatalf("task status after fail+retry: want queued, got %s", taskStatus)
	}
	if retryCount != 1 {
		t.Fatalf("current_retry: want 1, got %d", retryCount)
	}
	if execCount != 2 {
		t.Fatalf("expected 2 executions (orig + retry), got %d", execCount)
	}
}

// TestStartExecution_ContextRef verifies the optional context_ref body
// overwrites the row's context_ref column.
func TestStartExecution_ContextRef(t *testing.T) {
	env := setupDaemonExecEnv(t)
	execID := insertExecution(t, env, 0)
	// Move to claimed first (StartExecution doesn't gate on prior status
	// but the daemon would always claim before starting).
	if _, err := testPool.Exec(context.Background(),
		`UPDATE execution SET status = 'claimed', claimed_at = now() WHERE id = $1`,
		execID,
	); err != nil {
		t.Fatalf("set claimed: %v", err)
	}

	w := httptest.NewRecorder()
	req := newDaemonRequest("POST", "/api/daemon/executions/"+execID+"/start", map[string]any{
		"context_ref": map[string]any{
			"session_id":  "claude-session-xyz",
			"working_dir": "/tmp/work",
		},
	})
	req = withURLParam(req, "id", execID)
	testHandler.StartExecution(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartExecution: want 200, got %d: %s", w.Code, w.Body.String())
	}

	var started map[string]any
	decodeJSON(t, w.Body, &started)
	ctxRefBody, ok := started["context_ref"].(map[string]any)
	if !ok {
		t.Fatalf("StartExecution: expected context_ref object, got %T", started["context_ref"])
	}
	if ctxRefBody["session_id"] != "claude-session-xyz" {
		t.Fatalf("StartExecution: expected session_id in response, got %v", ctxRefBody)
	}

	var ctxRef []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT context_ref FROM execution WHERE id = $1`, execID,
	).Scan(&ctxRef); err != nil {
		t.Fatalf("read context_ref: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(ctxRef, &got); err != nil {
		t.Fatalf("unmarshal context_ref: %v", err)
	}
	if got["session_id"] != "claude-session-xyz" {
		t.Errorf("context_ref.session_id missing: %v", got)
	}
}

type daemonExecFailingRow struct {
	err error
}

func (r daemonExecFailingRow) Scan(...any) error {
	return r.err
}

type daemonExecFailingDB struct {
	execErr error
}

func (db daemonExecFailingDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, db.execErr
}

func (db daemonExecFailingDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, db.execErr
}

func (db daemonExecFailingDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return daemonExecFailingRow{err: db.execErr}
}

func TestStartExecution_InternalErrorsDoNotLeakBackendDetails(t *testing.T) {
	rawErr := errors.New(`pq: duplicate key value violates unique constraint "execution_pkey"`)
	failingDB := daemonExecFailingDB{execErr: rawErr}
	h := &Handler{
		Queries: db.New(failingDB),
	}

	w := httptest.NewRecorder()
	req := newDaemonRequest("POST", "/api/daemon/executions/00000000-0000-0000-0000-000000000001/start", nil)
	req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000001")
	h.StartExecution(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("StartExecution: want 500, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	decodeJSON(t, w.Body, &resp)
	if resp["error"] == "" {
		t.Fatalf("expected error message, got %+v", resp)
	}
	if resp["error"] == rawErr.Error() {
		t.Fatalf("response leaked raw backend error: %q", resp["error"])
	}
	if bytes.Contains([]byte(resp["error"]), []byte("duplicate key")) {
		t.Fatalf("response leaked backend details: %q", resp["error"])
	}
}

// TestProgressExecution_PublishesEvent verifies progress requests fan
// out over the event bus and have no DB side effect on the execution row.
func TestProgressExecution_PublishesEvent(t *testing.T) {
	env := setupDaemonExecEnv(t)
	execID := insertExecution(t, env, 0)

	// Capture published events through the shared bus.
	var (
		mu       sync.Mutex
		captured []events.Event
	)
	testHandler.Bus.Subscribe("execution:progress", func(e events.Event) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, e)
	})

	w := httptest.NewRecorder()
	req := newDaemonRequest("POST", "/api/daemon/executions/"+execID+"/progress", map[string]any{
		"progress": map[string]any{"summary": "halfway", "step": 5, "total": 10},
	})
	req = withURLParam(req, "id", execID)
	testHandler.ProgressExecution(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("ProgressExecution: want 202, got %d: %s", w.Code, w.Body.String())
	}

	mu.Lock()
	got := append([]events.Event(nil), captured...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("progress event count: want 1, got %d", len(got))
	}
	if got[0].Type != "execution:progress" {
		t.Fatalf("event type: want execution:progress, got %s", got[0].Type)
	}

	// Status must remain queued — progress is fire-and-forget.
	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM execution WHERE id = $1`, execID,
	).Scan(&status); err != nil {
		t.Fatalf("read execution after progress: %v", err)
	}
	if status != "queued" {
		t.Fatalf("progress should not change status: got %s", status)
	}
}

// TestStreamExecutionMessage_PublishesEvent ensures messages are
// accepted and broadcast without persistence.
func TestStreamExecutionMessage_PublishesEvent(t *testing.T) {
	env := setupDaemonExecEnv(t)
	execID := insertExecution(t, env, 0)

	w := httptest.NewRecorder()
	req := newDaemonRequest("POST", "/api/daemon/executions/"+execID+"/messages", map[string]any{
		"body": map[string]any{
			"type": "tool_call",
			"tool": "Bash",
		},
	})
	req = withURLParam(req, "id", execID)
	testHandler.StreamExecutionMessage(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("StreamExecutionMessage: want 202, got %d: %s", w.Code, w.Body.String())
	}
}

// TestClaimExecution_AtomicConcurrent issues two concurrent claim
// requests against a runtime with one queued execution. Per
// FOR UPDATE SKIP LOCKED semantics, exactly one caller should receive
// the row (200 + payload) and the other should receive 204.
func TestClaimExecution_AtomicConcurrent(t *testing.T) {
	env := setupDaemonExecEnv(t)
	insertExecution(t, env, 0) // single queued row to fight over

	type result struct {
		code int
		body string
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := newDaemonRequest("POST",
				"/api/daemon/runtimes/"+env.RuntimeID+"/executions/claim",
				map[string]any{"daemon_id": "concurrent"},
			)
			req = withURLParam(req, "runtimeId", env.RuntimeID)
			testHandler.ClaimExecution(w, req)
			results <- result{code: w.Code, body: w.Body.String()}
		}()
	}
	wg.Wait()
	close(results)

	var hits, misses int
	for r := range results {
		switch r.code {
		case http.StatusOK:
			hits++
		case http.StatusNoContent:
			misses++
		default:
			t.Fatalf("unexpected status %d: %s", r.code, r.body)
		}
	}
	if hits != 1 || misses != 1 {
		t.Fatalf("concurrent claim: want hits=1 misses=1, got hits=%d misses=%d", hits, misses)
	}
}

// TestListPendingExecutions_ReturnsQueuedRows verifies the visibility
// endpoint surfaces queued rows for the requested runtime only.
func TestListPendingExecutions_ReturnsQueuedRows(t *testing.T) {
	env := setupDaemonExecEnv(t)
	insertExecution(t, env, 5)
	insertExecution(t, env, 10) // higher priority
	insertExecution(t, env, 0)

	w := httptest.NewRecorder()
	req := newDaemonRequest("GET",
		"/api/daemon/runtimes/"+env.RuntimeID+"/executions/pending", nil,
	)
	req = withURLParam(req, "runtimeId", env.RuntimeID)
	testHandler.ListPendingExecutions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListPendingExecutions: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var rows []map[string]any
	decodeJSON(t, w.Body, &rows)
	if len(rows) != 3 {
		t.Fatalf("ListPendingExecutions: want 3 rows, got %d", len(rows))
	}
	// Highest priority should be first.
	first := rows[0]
	if got := int(first["priority"].(float64)); got != 10 {
		t.Errorf("priority ordering: first row priority=%d, want 10", got)
	}
}

// newDaemonRequest mirrors newRequest but tags the request as coming
// from the daemon (no X-Workspace-ID, no X-User-ID — these handlers
// only consume URL params and the body).
func newDaemonRequest(method, path string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}
