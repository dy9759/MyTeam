// task_test.go — DB-backed handler tests for the Plan 5 Project five-layer
// HTTP API (task / slot / execution / artifact / review). Uses the shared
// handler test fixture (TestMain) for the workspace + agent + runtime, and
// reuses setupDaemonExecEnv from daemon_executions_test.go for the
// per-test plan/project/run/task graph.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// projectTaskTestEnv mirrors daemonExecTestEnv but fields a member id so
// review tests can use it as the reviewer (handler resolves user id from
// X-User-ID, not member id, so we keep the raw user id in newRequest).
type projectTaskTestEnv struct {
	WorkspaceID string
	PlanID      string
	ProjectID   string
	RunID       string
	TaskID      string
	AgentID     string
	RuntimeID   string
	MemberID    string
}

// setupProjectTaskEnv creates a self-contained graph (plan/project/run/task)
// for the project-task handler tests. Mirrors setupDaemonExecEnv but skips
// the stricter agent.status / runtime.current_load reset because none of
// the project-task tests exercise the scheduler's findAvailableAgent path.
func setupProjectTaskEnv(t *testing.T) projectTaskTestEnv {
	t.Helper()
	ctx := context.Background()

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

	// Make agent idle and runtime online with spare capacity so the
	// scheduler can claim it in StartRun tests.
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET status = 'idle' WHERE id = $1`, agentID,
	); err != nil {
		t.Fatalf("mark agent idle: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_runtime SET current_load = 0, concurrency_limit = 4, status = 'online' WHERE id = $1`,
		runtimeID,
	); err != nil {
		t.Fatalf("reset runtime load: %v", err)
	}

	suffix := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())

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
	`, testWorkspaceID, "ProjPlan "+suffix, memberID).Scan(&planID); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, created_by, schedule_type, source_conversations, creator_owner_id)
		VALUES ($1, $2, '', 'running', $3, 'one_time', '[]'::jsonb, $3)
		RETURNING id
	`, testWorkspaceID, "ProjProject "+suffix, testUserID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project_run (plan_id, project_id, status)
		VALUES ($1, $2, 'pending')
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
		// project FK references plan, so drop project first then plan
		// cascade clears everything else.
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM plan WHERE id = $1`, planID)
	})

	return projectTaskTestEnv{
		WorkspaceID: testWorkspaceID,
		PlanID:      planID,
		ProjectID:   projectID,
		RunID:       runID,
		TaskID:      taskID,
		AgentID:     agentID,
		RuntimeID:   runtimeID,
		MemberID:    memberID,
	}
}

// TestCreateTask_Success exercises the happy path: POST /api/tasks with a
// valid plan_id should create a draft task and return its JSON shape.
func TestCreateTask_Success(t *testing.T) {
	env := setupProjectTaskEnv(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks", map[string]any{
		"plan_id":     env.PlanID,
		"title":       "New Project Task",
		"description": "via handler test",
		"step_order":  3,
	})
	testHandler.CreateTaskHandler(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: want 201, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	decodeJSON(t, w.Body, &got)
	if got["title"] != "New Project Task" {
		t.Errorf("title: want %q, got %v", "New Project Task", got["title"])
	}
	if got["status"] != "draft" {
		t.Errorf("status: want draft, got %v", got["status"])
	}
	if got["plan_id"] != env.PlanID {
		t.Errorf("plan_id: want %s, got %v", env.PlanID, got["plan_id"])
	}
	if got["workspace_id"] != env.WorkspaceID {
		t.Errorf("workspace_id: want %s, got %v", env.WorkspaceID, got["workspace_id"])
	}
}

// TestCreateTask_InvalidPlanID rejects a malformed plan_id with 400.
func TestCreateTask_InvalidPlanID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks", map[string]any{
		"plan_id": "not-a-uuid",
		"title":   "doomed",
	})
	testHandler.CreateTaskHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask invalid plan id: want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateTask_MissingTitle rejects an empty title.
func TestCreateTask_MissingTitle(t *testing.T) {
	env := setupProjectTaskEnv(t)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks", map[string]any{
		"plan_id": env.PlanID,
	})
	testHandler.CreateTaskHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask missing title: want 400, got %d", w.Code)
	}
}

// TestListTasksByPlan_ReturnsWrappedArray verifies the response is wrapped
// in {tasks: [...]} per the contract documented in the route map.
func TestListTasksByPlan_ReturnsWrappedArray(t *testing.T) {
	env := setupProjectTaskEnv(t)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/plans/"+env.PlanID+"/tasks", nil)
	req = withURLParam(req, "planID", env.PlanID)
	testHandler.ListTasksByPlan(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListTasksByPlan: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	decodeJSON(t, w.Body, &resp)
	tasks, ok := resp["tasks"].([]any)
	if !ok {
		t.Fatalf("response missing tasks array: %v", resp)
	}
	if len(tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(tasks))
	}
	first, _ := tasks[0].(map[string]any)
	if first["id"] != env.TaskID {
		t.Errorf("first task id: want %s, got %v", env.TaskID, first["id"])
	}
}

// TestUpdateTask_RejectsNonCancelledStatus ensures PATCH refuses anything
// other than status=cancelled. The scheduler is the canonical owner of
// other transitions.
func TestUpdateTask_RejectsNonCancelledStatus(t *testing.T) {
	env := setupProjectTaskEnv(t)

	for _, badStatus := range []string{"running", "completed", "ready"} {
		w := httptest.NewRecorder()
		req := newRequest("PATCH", "/api/tasks/"+env.TaskID, map[string]any{"status": badStatus})
		req = withURLParam(req, "id", env.TaskID)
		testHandler.UpdateTaskHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("PATCH status=%s: want 400, got %d: %s", badStatus, w.Code, w.Body.String())
		}
	}

	// Cancelled should succeed.
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/tasks/"+env.TaskID, map[string]any{"status": "cancelled"})
	req = withURLParam(req, "id", env.TaskID)
	testHandler.UpdateTaskHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH cancelled: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	decodeJSON(t, w.Body, &got)
	if got["status"] != "cancelled" {
		t.Fatalf("status after cancel: want cancelled, got %v", got["status"])
	}
}

// TestGetTask_NotFound returns 404 for a random uuid.
func TestGetTask_NotFound(t *testing.T) {
	w := httptest.NewRecorder()
	missing := "00000000-0000-0000-0000-000000000099"
	req := newRequest("GET", "/api/tasks/"+missing, nil)
	req = withURLParam(req, "id", missing)
	testHandler.GetTaskHandler(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetTask missing: want 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListTaskSlots_EmptyAndCreate verifies the slot list/create round-trip:
// an empty task returns {slots: []}, then POST creates a slot and the next
// GET sees it.
func TestListTaskSlots_EmptyAndCreate(t *testing.T) {
	env := setupProjectTaskEnv(t)

	// Empty list.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/tasks/"+env.TaskID+"/slots", nil)
	req = withURLParam(req, "id", env.TaskID)
	testHandler.ListTaskSlots(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListTaskSlots empty: want 200, got %d", w.Code)
	}
	var resp map[string]any
	decodeJSON(t, w.Body, &resp)
	slots, _ := resp["slots"].([]any)
	if len(slots) != 0 {
		t.Fatalf("empty list: want 0 slots, got %d", len(slots))
	}

	// Create.
	blocking := true
	required := true
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks/"+env.TaskID+"/slots", map[string]any{
		"slot_type":  "human_review",
		"slot_order": 0,
		"trigger":    "before_done",
		"blocking":   blocking,
		"required":   required,
	})
	req = withURLParam(req, "id", env.TaskID)
	testHandler.CreateTaskSlot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTaskSlot: want 201, got %d: %s", w.Code, w.Body.String())
	}
	var slot map[string]any
	decodeJSON(t, w.Body, &slot)
	if slot["slot_type"] != "human_review" || slot["trigger"] != "before_done" {
		t.Fatalf("created slot wrong shape: %+v", slot)
	}

	// List again.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/tasks/"+env.TaskID+"/slots", nil)
	req = withURLParam(req, "id", env.TaskID)
	testHandler.ListTaskSlots(w, req)
	decodeJSON(t, w.Body, &resp)
	slots, _ = resp["slots"].([]any)
	if len(slots) != 1 {
		t.Fatalf("after create: want 1 slot, got %d", len(slots))
	}
}

// TestCreateReview_Approve_ReturnsTaskNewStatus drives a review through
// ReviewService.Submit and asserts the response includes both the inserted
// review and the resulting task status.
func TestCreateReview_Approve_ReturnsTaskNewStatus(t *testing.T) {
	env := setupProjectTaskEnv(t)
	ctx := context.Background()

	// Insert an artifact bound to the env's task/run so we have a
	// review target. Headless content satisfies the table CHECK.
	var artifactID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO artifact (task_id, run_id, artifact_type, version, content, created_by_id, created_by_type)
		VALUES ($1, $2, 'report', 1, '{"summary":"done"}'::jsonb, $3, 'agent')
		RETURNING id
	`, env.TaskID, env.RunID, env.AgentID).Scan(&artifactID); err != nil {
		t.Fatalf("insert artifact: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/artifacts/"+artifactID+"/reviews", map[string]any{
		"task_id":  env.TaskID,
		"decision": "approve",
		"comment":  "lgtm",
	})
	req = withURLParam(req, "id", artifactID)
	testHandler.CreateReviewHandler(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateReview: want 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	decodeJSON(t, w.Body, &resp)
	if _, ok := resp["review"]; !ok {
		t.Fatalf("response missing review key: %v", resp)
	}
	review, _ := resp["review"].(map[string]any)
	if review["decision"] != "approve" || review["comment"] != "lgtm" {
		t.Errorf("review shape wrong: %+v", review)
	}
	// approve with no other blocking slots should complete the task.
	if resp["task_new_status"] != "completed" {
		t.Errorf("task_new_status: want completed, got %v", resp["task_new_status"])
	}

	// And the task row should reflect the new status.
	var status string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM task WHERE id = $1`, env.TaskID,
	).Scan(&status); err != nil {
		t.Fatalf("read task after review: %v", err)
	}
	if status != "completed" {
		t.Fatalf("task status after approve: want completed, got %s", status)
	}
}

// TestCreateReview_NotFound returns 404 when the artifact does not exist
// instead of letting ReviewService.Submit produce a generic FK error.
func TestCreateReview_ArtifactNotFound(t *testing.T) {
	env := setupProjectTaskEnv(t)
	missing := "00000000-0000-0000-0000-000000000099"
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/artifacts/"+missing+"/reviews", map[string]any{
		"task_id":  env.TaskID,
		"decision": "approve",
	})
	req = withURLParam(req, "id", missing)
	testHandler.CreateReviewHandler(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("CreateReview missing artifact: want 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestStartRun_DispatchesToScheduler proves POST /api/runs/{id}/start
// reaches SchedulerService.ScheduleRun by observing the side effect: the
// task moves from queued (set in setup) → reset → ready/queued.
func TestStartRun_DispatchesToScheduler(t *testing.T) {
	env := setupProjectTaskEnv(t)
	ctx := context.Background()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runs/"+env.RunID+"/start", nil)
	req = withURLParam(req, "runID", env.RunID)
	testHandler.StartRunHandler(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("StartRun: want 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "scheduling" {
		t.Errorf("status field: want scheduling, got %q", resp["status"])
	}

	// SchedulerService.ScheduleRun resets all tasks to draft, then
	// promotes those with no unmet deps. With our single task this
	// should produce status='queued' and a fresh execution row.
	var status string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM task WHERE id = $1`, env.TaskID,
	).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task after StartRun: want queued, got %s", status)
	}

	var execCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM execution WHERE task_id = $1`, env.TaskID,
	).Scan(&execCount); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if execCount < 1 {
		t.Fatalf("expected at least 1 execution after StartRun, got %d", execCount)
	}
}

// TestStartRun_NotFound returns 404 when the run id is unknown.
func TestStartRun_NotFound(t *testing.T) {
	missing := "00000000-0000-0000-0000-000000000099"
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runs/"+missing+"/start", nil)
	req = withURLParam(req, "runID", missing)
	testHandler.StartRunHandler(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("StartRun missing: want 404, got %d: %s", w.Code, w.Body.String())
	}
}
