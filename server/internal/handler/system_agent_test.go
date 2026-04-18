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

func createHandlerTestMember(t *testing.T, role string) string {
	t.Helper()
	ctx := context.Background()
	email := fmt.Sprintf("handler-%s-%d@multica.ai", role, time.Now().UnixNano())

	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Handler "+role, email).Scan(&userID); err != nil {
		t.Fatalf("create %s user: %v", role, err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
	`, testWorkspaceID, userID, role); err != nil {
		t.Fatalf("create %s member: %v", role, err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, userID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	return userID
}

func handlerTestRuntimeID(t *testing.T) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM agent_runtime
		WHERE workspace_id = $1
		ORDER BY created_at ASC
		LIMIT 1
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime id: %v", err)
	}
	return runtimeID
}

func TestSystemAgentCRUDRequiresAdminAndOwnerGuards(t *testing.T) {
	adminID := createHandlerTestMember(t, "admin")
	runtimeID := handlerTestRuntimeID(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/system-agents", map[string]any{
		"name":         "Workspace Steward",
		"description":  "Coordinates workspace automation",
		"instructions": "Keep the workspace operating smoothly.",
		"runtime_id":   runtimeID,
	})
	req.Header.Set("X-User-ID", adminID)
	testHandler.CreateSystemAgent(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSystemAgent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created system agent: %v", err)
	}
	if created.Name != "Workspace Steward" {
		t.Fatalf("created name: expected Workspace Steward, got %q", created.Name)
	}
	if created.AgentType != "system_agent" {
		t.Fatalf("created agent_type: expected system_agent, got %q", created.AgentType)
	}
	if created.OwnerID != nil {
		t.Fatalf("system agent owner_id must be nil, got %q", *created.OwnerID)
	}

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/system-agents/"+created.ID, map[string]any{
		"name":         "Workspace Operator",
		"instructions": "Coordinate workspace automations and escalation.",
	})
	req.Header.Set("X-User-ID", adminID)
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateSystemAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSystemAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated system agent: %v", err)
	}
	if updated.Name != "Workspace Operator" {
		t.Fatalf("updated name: expected Workspace Operator, got %q", updated.Name)
	}
	if updated.Instructions != "Coordinate workspace automations and escalation." {
		t.Fatalf("updated instructions not returned: %q", updated.Instructions)
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/system-agents/"+created.ID, nil)
	req.Header.Set("X-User-ID", adminID)
	req = withURLParam(req, "id", created.ID)
	testHandler.ArchiveSystemAgent(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("ArchiveSystemAgent as admin: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/system-agents/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.ArchiveSystemAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveSystemAgent as owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var archived AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&archived); err != nil {
		t.Fatalf("decode archived system agent: %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("expected archived_at to be set")
	}
}
