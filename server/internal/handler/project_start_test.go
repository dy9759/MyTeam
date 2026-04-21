package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type projectStartTestEnv struct {
	WorkspaceID string
	PlanID      string
	ProjectID   string
	TaskID      string
	AgentID     string
	RuntimeID   string
}

func setupProjectStartEnv(t *testing.T) projectStartTestEnv {
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

	var memberID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID, testUserID,
	).Scan(&memberID); err != nil {
		t.Fatalf("look up member id: %v", err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, created_by, schedule_type, source_conversations, creator_owner_id)
		VALUES ($1, $2, '', 'not_started', $3, 'one_time', '[]'::jsonb, $3)
		RETURNING id
	`, testWorkspaceID, "Project Start Test", testUserID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	var planID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO plan (workspace_id, title, description, expected_output, created_by, approval_status, project_id)
		VALUES ($1, $2, '', 'artifacts', $3, 'approved', $4)
		RETURNING id
	`, testWorkspaceID, "Project Start Plan", memberID, projectID).Scan(&planID); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		UPDATE project SET plan_id = $2 WHERE id = $1
	`, projectID, planID); err != nil {
		t.Fatalf("link project plan: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (plan_id, workspace_id, title, description, step_order, primary_assignee_id, status)
		VALUES ($1, $2, $3, 'do thing', 0, $4, 'draft')
		RETURNING id
	`, planID, testWorkspaceID, "Project Start Task", agentID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM plan WHERE id = $1`, planID)
	})

	return projectStartTestEnv{
		WorkspaceID: testWorkspaceID,
		PlanID:      planID,
		ProjectID:   projectID,
		TaskID:      taskID,
		AgentID:     agentID,
		RuntimeID:   runtimeID,
	}
}

func TestStartProjectExecution_CreatesRunAndSchedules(t *testing.T) {
	env := setupProjectStartEnv(t)
	ctx := context.Background()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+env.ProjectID+"/start", nil)
	req = withURLParam(req, "projectID", env.ProjectID)
	testHandler.StartProjectExecution(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("StartProjectExecution: want 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp ProjectRunResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ProjectID != env.ProjectID {
		t.Fatalf("response project_id: want %s, got %s", env.ProjectID, resp.ProjectID)
	}
	if resp.PlanID != env.PlanID {
		t.Fatalf("response plan_id: want %s, got %s", env.PlanID, resp.PlanID)
	}
	if resp.Status != "pending" {
		t.Fatalf("response status: want pending, got %s", resp.Status)
	}

	var runCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM project_run WHERE project_id = $1`, env.ProjectID,
	).Scan(&runCount); err != nil {
		t.Fatalf("count project runs: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("want exactly 1 project run, got %d", runCount)
	}

	var taskStatus string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM task WHERE id = $1`, env.TaskID,
	).Scan(&taskStatus); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if taskStatus != "queued" {
		t.Fatalf("task after StartProjectExecution: want queued, got %s", taskStatus)
	}

	var executionCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM execution WHERE task_id = $1`, env.TaskID,
	).Scan(&executionCount); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if executionCount < 1 {
		t.Fatalf("expected at least 1 execution after project start, got %d", executionCount)
	}
}
