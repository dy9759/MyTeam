package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
)

// activityCleanup deletes any activity_log rows linked to the given
// project / task / event_type so tests don't leak across runs.
func activityCleanup(t *testing.T, projectID, taskID, eventTypePattern string) {
	t.Helper()
	ctx := context.Background()
	if projectID != "" {
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE related_project_id = $1`, projectID)
	}
	if taskID != "" {
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE related_task_id = $1`, taskID)
	}
	if eventTypePattern != "" {
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE event_type LIKE $1`, eventTypePattern)
	}
}

// createTestProject inserts a minimal project row for fixture purposes.
func createTestProject(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var projectID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, created_by)
		VALUES ($1, 'activity-log test project', '', 'not_started', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&projectID)
	if err != nil {
		t.Fatalf("createTestProject: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID
}

func TestActivityWriter_RoundTrip(t *testing.T) {
	ctx := context.Background()
	projectID := createTestProject(t)
	t.Cleanup(func() { activityCleanup(t, projectID, "", "") })

	wsUUID, err := uuid.Parse(testWorkspaceID)
	if err != nil {
		t.Fatalf("parse workspace UUID: %v", err)
	}
	projUUID, err := uuid.Parse(projectID)
	if err != nil {
		t.Fatalf("parse project UUID: %v", err)
	}
	actorUUID, err := uuid.Parse(testUserID)
	if err != nil {
		t.Fatalf("parse user UUID: %v", err)
	}

	writer := service.NewActivityWriter(testHandler.Queries)
	writer.Write(ctx, service.ActivityEntry{
		WorkspaceID:      wsUUID,
		EventType:        "test:roundtrip",
		ActorID:          actorUUID,
		ActorType:        "member",
		RelatedProjectID: projUUID,
		Payload:          map[string]any{"k": "v", "n": 42},
	})

	// Read back via the handler.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/activity-log?project_id="+projectID, nil)
	testHandler.ListActivityLog(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListActivityLog: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string][]ActivityLogEntryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	entries := resp["entries"]
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	got := entries[0]
	if got.EventType != "test:roundtrip" {
		t.Errorf("event_type = %q, want %q", got.EventType, "test:roundtrip")
	}
	if got.RelatedProjectID == nil || *got.RelatedProjectID != projectID {
		t.Errorf("related_project_id = %v, want %q", got.RelatedProjectID, projectID)
	}
	if got.ActorID == nil || *got.ActorID != testUserID {
		t.Errorf("actor_id = %v, want %q", got.ActorID, testUserID)
	}
	if got.ActorType == nil || *got.ActorType != "member" {
		t.Errorf("actor_type = %v, want %q", got.ActorType, "member")
	}
	if got.RetentionClass != "permanent" {
		t.Errorf("retention_class = %q, want %q", got.RetentionClass, "permanent")
	}
	// Payload should round-trip the JSON bag.
	var payload map[string]any
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if payload["k"] != "v" {
		t.Errorf("payload[k] = %v, want v", payload["k"])
	}
}

func TestListActivityLog_NoFilterReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/activity-log", nil)
	testHandler.ListActivityLog(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListActivityLog_ByEventType_DescOrder(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { activityCleanup(t, "", "", "test:order:%") })

	wsUUID := uuid.MustParse(testWorkspaceID)
	writer := service.NewActivityWriter(testHandler.Queries)

	for i, et := range []string{"test:order:a", "test:order:b", "test:order:c"} {
		writer.Write(ctx, service.ActivityEntry{
			WorkspaceID: wsUUID,
			EventType:   et,
			Payload:     map[string]any{"i": i},
		})
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/activity-log?event_type=test:order:%25", nil)
	testHandler.ListActivityLog(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string][]ActivityLogEntryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	entries := resp["entries"]
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// DESC by created_at — last inserted first.
	if entries[0].EventType != "test:order:c" {
		t.Errorf("first entry event_type = %q, want %q", entries[0].EventType, "test:order:c")
	}
	if entries[2].EventType != "test:order:a" {
		t.Errorf("last entry event_type = %q, want %q", entries[2].EventType, "test:order:a")
	}
}

func TestListActivityLog_PaginationLimitOffset(t *testing.T) {
	ctx := context.Background()
	projectID := createTestProject(t)
	t.Cleanup(func() { activityCleanup(t, projectID, "", "") })

	wsUUID := uuid.MustParse(testWorkspaceID)
	projUUID := uuid.MustParse(projectID)
	writer := service.NewActivityWriter(testHandler.Queries)

	for i := 0; i < 5; i++ {
		writer.Write(ctx, service.ActivityEntry{
			WorkspaceID:      wsUUID,
			EventType:        "test:page",
			RelatedProjectID: projUUID,
			Payload:          map[string]any{"i": i},
		})
	}

	// limit=2 → 2 entries.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/activity-log?project_id="+projectID+"&limit=2", nil)
	testHandler.ListActivityLog(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string][]ActivityLogEntryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp["entries"]) != 2 {
		t.Errorf("limit=2: expected 2 entries, got %d", len(resp["entries"]))
	}

	// offset=4, limit=2 → 1 entry remaining.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/activity-log?project_id="+projectID+"&limit=2&offset=4", nil)
	testHandler.ListActivityLog(w, req)
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp["entries"]) != 1 {
		t.Errorf("offset=4: expected 1 entry, got %d", len(resp["entries"]))
	}
}

func TestListActivityLog_ByTask(t *testing.T) {
	ctx := context.Background()
	taskUUID := uuid.New()
	taskID := taskUUID.String()
	t.Cleanup(func() { activityCleanup(t, "", taskID, "") })

	wsUUID := uuid.MustParse(testWorkspaceID)
	writer := service.NewActivityWriter(testHandler.Queries)
	writer.Write(ctx, service.ActivityEntry{
		WorkspaceID:   wsUUID,
		EventType:     "task:by_task_test",
		RelatedTaskID: taskUUID,
	})

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/activity-log?task_id="+taskID, nil)
	testHandler.ListActivityLog(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string][]ActivityLogEntryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	entries := resp["entries"]
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].RelatedTaskID == nil || *entries[0].RelatedTaskID != taskID {
		t.Errorf("related_task_id = %v, want %q", entries[0].RelatedTaskID, taskID)
	}
}
