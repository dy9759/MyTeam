package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetProjectIncludesLinkedPlan(t *testing.T) {
	ctx := context.Background()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, created_by, schedule_type, source_conversations, creator_owner_id)
		VALUES ($1, $2, '', 'not_started', $3, 'one_time', '[]'::jsonb, $3)
		RETURNING id
	`, testWorkspaceID, "Project Detail Test", testUserID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	var planID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO plan (workspace_id, title, created_by, approval_status, project_id)
		VALUES ($1, $2, $3, 'pending', $4)
		RETURNING id
	`, testWorkspaceID, "Plan Detail Test", testUserID, projectID).Scan(&planID); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		UPDATE project SET plan_id = $2 WHERE id = $1
	`, projectID, planID); err != nil {
		t.Fatalf("link project plan: %v", err)
	}

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project_run (plan_id, project_id, status)
		VALUES ($1, $2, 'pending')
		RETURNING id
	`, planID, projectID).Scan(&runID); err != nil {
		t.Fatalf("create project run: %v", err)
	}

	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID); err != nil {
			t.Fatalf("cleanup project: %v", err)
		}
	})

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+projectID, nil)
	req = withURLParam(req, "projectID", projectID)
	testHandler.GetProject(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetProject: want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ID   string `json:"id"`
		Plan *struct {
			ID             string `json:"id"`
			ApprovalStatus string `json:"approval_status"`
			ProjectID      string `json:"project_id"`
		} `json:"plan"`
		ActiveRun *struct {
			ID        string `json:"id"`
			PlanID    string `json:"plan_id"`
			ProjectID string `json:"project_id"`
			Status    string `json:"status"`
		} `json:"active_run"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ID != projectID {
		t.Fatalf("GetProject: want id=%s, got %s", projectID, resp.ID)
	}
	if resp.Plan == nil {
		t.Fatalf("GetProject: expected linked plan in response")
	}
	if resp.Plan.ID != planID {
		t.Fatalf("GetProject: want plan.id=%s, got %s", planID, resp.Plan.ID)
	}
	if resp.Plan.ApprovalStatus != "pending" {
		t.Fatalf("GetProject: want plan.approval_status=pending, got %s", resp.Plan.ApprovalStatus)
	}
	if resp.Plan.ProjectID != projectID {
		t.Fatalf("GetProject: want plan.project_id=%s, got %s", projectID, resp.Plan.ProjectID)
	}
	if resp.ActiveRun == nil {
		t.Fatalf("GetProject: expected active_run in response")
	}
	if resp.ActiveRun.ID != runID {
		t.Fatalf("GetProject: want active_run.id=%s, got %s", runID, resp.ActiveRun.ID)
	}
	if resp.ActiveRun.PlanID != planID {
		t.Fatalf("GetProject: want active_run.plan_id=%s, got %s", planID, resp.ActiveRun.PlanID)
	}
	if resp.ActiveRun.ProjectID != projectID {
		t.Fatalf("GetProject: want active_run.project_id=%s, got %s", projectID, resp.ActiveRun.ProjectID)
	}
	if resp.ActiveRun.Status != "pending" {
		t.Fatalf("GetProject: want active_run.status=pending, got %s", resp.ActiveRun.Status)
	}
}
