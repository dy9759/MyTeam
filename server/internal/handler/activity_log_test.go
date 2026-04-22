package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/MyAIOSHub/MyTeam/server/internal/middleware"
	"github.com/MyAIOSHub/MyTeam/server/internal/service"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
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
		INSERT INTO project (workspace_id, title, description, status, creator_owner_id)
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

// requestAs builds a request with a custom X-User-ID header (overriding the
// owner default supplied by newRequest). Also injects the workspace member
// context so resolveWorkspaceID (which reads only context after the #20 fix)
// resolves correctly for the impersonated user.
func requestAs(t *testing.T, userID, method, path string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(req.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("requestAs: member lookup for user %s failed: %v", userID, err)
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, member))
}

// listActivityProjectIDs decodes the entries response and returns the
// related_project_id values (skipping null) so callers can compare visibility.
func listActivityProjectIDs(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string][]ActivityLogEntryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ids := []string{}
	for _, e := range resp["entries"] {
		if e.RelatedProjectID != nil {
			ids = append(ids, *e.RelatedProjectID)
		}
	}
	return ids
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestListActivityLog_MemberRowIsolation verifies PRD §3.4: a member can only
// see activity_log rows that belong to a project they participate in (here:
// projects they created). Owners/admins see everything.
func TestListActivityLog_MemberRowIsolation(t *testing.T) {
	ctx := context.Background()

	// Create two member-role users in the existing test workspace.
	var member1ID, member2ID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Activity Iso Member 1", "activity-iso-member1@myteam.ai").Scan(&member1ID); err != nil {
		t.Fatalf("create member1 user: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Activity Iso Member 2", "activity-iso-member2@myteam.ai").Scan(&member2ID); err != nil {
		t.Fatalf("create member2 user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id IN ($1, $2)`, member1ID, member2ID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member'), ($1, $3, 'member')
	`, testWorkspaceID, member1ID, member2ID); err != nil {
		t.Fatalf("create members: %v", err)
	}

	// Two projects, one owned by each member.
	var proj1ID, proj2ID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, created_by, creator_owner_id)
		VALUES ($1, 'iso project 1', '', 'not_started', $2, $2) RETURNING id
	`, testWorkspaceID, member1ID).Scan(&proj1ID); err != nil {
		t.Fatalf("create proj1: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, created_by, creator_owner_id)
		VALUES ($1, 'iso project 2', '', 'not_started', $2, $2) RETURNING id
	`, testWorkspaceID, member2ID).Scan(&proj2ID); err != nil {
		t.Fatalf("create proj2: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM project WHERE id IN ($1, $2)`, proj1ID, proj2ID)
	})
	t.Cleanup(func() {
		activityCleanup(t, proj1ID, "", "")
		activityCleanup(t, proj2ID, "", "")
	})

	// Activity rows: each authored by some unrelated actor (the workspace owner)
	// so visibility must come from project ownership, not actor self-match.
	wsUUID := uuid.MustParse(testWorkspaceID)
	ownerActor := uuid.MustParse(testUserID)
	writer := service.NewActivityWriter(testHandler.Queries)
	writer.Write(ctx, service.ActivityEntry{
		WorkspaceID:      wsUUID,
		EventType:        "iso:proj1_event",
		ActorID:          ownerActor,
		ActorType:        "member",
		RelatedProjectID: uuid.MustParse(proj1ID),
	})
	writer.Write(ctx, service.ActivityEntry{
		WorkspaceID:      wsUUID,
		EventType:        "iso:proj2_event",
		ActorID:          ownerActor,
		ActorType:        "member",
		RelatedProjectID: uuid.MustParse(proj2ID),
	})

	// member1 should see proj1's row when filtering by event_type, not proj2's.
	w := httptest.NewRecorder()
	testHandler.ListActivityLog(w, requestAs(t, member1ID, "GET", "/api/activity-log?event_type=iso:%25"))
	got := listActivityProjectIDs(t, w)
	if !contains(got, proj1ID) {
		t.Errorf("member1: expected to see proj1 row, got %v", got)
	}
	if contains(got, proj2ID) {
		t.Errorf("member1: must NOT see proj2 row, got %v", got)
	}

	// member2 sees proj2's row, not proj1's.
	w = httptest.NewRecorder()
	testHandler.ListActivityLog(w, requestAs(t, member2ID, "GET", "/api/activity-log?event_type=iso:%25"))
	got = listActivityProjectIDs(t, w)
	if !contains(got, proj2ID) {
		t.Errorf("member2: expected to see proj2 row, got %v", got)
	}
	if contains(got, proj1ID) {
		t.Errorf("member2: must NOT see proj1 row, got %v", got)
	}

	// Owner (admin-equivalent) sees both.
	w = httptest.NewRecorder()
	testHandler.ListActivityLog(w, requestAs(t, testUserID, "GET", "/api/activity-log?event_type=iso:%25"))
	got = listActivityProjectIDs(t, w)
	if !contains(got, proj1ID) || !contains(got, proj2ID) {
		t.Errorf("owner: expected both proj rows, got %v", got)
	}

	// member1 also gets project_id-filtered isolation: querying their own
	// project returns the row; querying member2's project returns nothing.
	w = httptest.NewRecorder()
	testHandler.ListActivityLog(w, requestAs(t, member1ID, "GET", "/api/activity-log?project_id="+proj1ID))
	if got = listActivityProjectIDs(t, w); !contains(got, proj1ID) || len(got) != 1 {
		t.Errorf("member1 by project_id=proj1: expected only proj1, got %v", got)
	}
	w = httptest.NewRecorder()
	testHandler.ListActivityLog(w, requestAs(t, member1ID, "GET", "/api/activity-log?project_id="+proj2ID))
	if got = listActivityProjectIDs(t, w); len(got) != 0 {
		t.Errorf("member1 by project_id=proj2: expected empty, got %v", got)
	}
}

// TestListActivityLog_MemberChannelMembershipVisibility covers the second
// branch of the accessible_projects CTE: a member who is NOT the project
// creator but IS a member of a channel linked to that project should still
// see activity rows for that project. A second member without channel
// membership must not.
func TestListActivityLog_MemberChannelMembershipVisibility(t *testing.T) {
	ctx := context.Background()

	// Two member-role users; the workspace owner (testUserID) acts as admin
	// and creates the project so neither member is the creator.
	var member1ID, member2ID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Activity Channel Member 1", "activity-channel-member1@myteam.ai").Scan(&member1ID); err != nil {
		t.Fatalf("create member1 user: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Activity Channel Member 2", "activity-channel-member2@myteam.ai").Scan(&member2ID); err != nil {
		t.Fatalf("create member2 user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id IN ($1, $2)`, member1ID, member2ID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member'), ($1, $3, 'member')
	`, testWorkspaceID, member1ID, member2ID); err != nil {
		t.Fatalf("create members: %v", err)
	}

	// Project created by the admin, NOT by either member — so visibility
	// must come from channel_member, not creator_owner_id.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, created_by, creator_owner_id)
		VALUES ($1, 'channel-iso project', '', 'not_started', $2, $2) RETURNING id
	`, testWorkspaceID, testUserID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
	})
	t.Cleanup(func() { activityCleanup(t, projectID, "", "") })

	// Channel linked to the project. member1 joins, member2 does not.
	var channelID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel (workspace_id, name, description, created_by, created_by_type, project_id)
		VALUES ($1, $2, '', $3, 'member', $4) RETURNING id
	`, testWorkspaceID, "channel-iso-test", testUserID, projectID).Scan(&channelID); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel WHERE id = $1`, channelID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO channel_member (channel_id, member_id, member_type) VALUES ($1, $2, 'member')
	`, channelID, member1ID); err != nil {
		t.Fatalf("add member1 to channel: %v", err)
	}

	// Activity row authored by the admin so visibility cannot leak through
	// the actor_id self-match path.
	wsUUID := uuid.MustParse(testWorkspaceID)
	ownerActor := uuid.MustParse(testUserID)
	writer := service.NewActivityWriter(testHandler.Queries)
	writer.Write(ctx, service.ActivityEntry{
		WorkspaceID:      wsUUID,
		EventType:        "iso:channel_event",
		ActorID:          ownerActor,
		ActorType:        "member",
		RelatedProjectID: uuid.MustParse(projectID),
	})

	// member1 (in channel) sees the row.
	w := httptest.NewRecorder()
	testHandler.ListActivityLog(w, requestAs(t, member1ID, "GET", "/api/activity-log?event_type=iso:channel_event"))
	got := listActivityProjectIDs(t, w)
	if !contains(got, projectID) {
		t.Errorf("member1 (channel member): expected to see project row, got %v", got)
	}

	// member2 (NOT in channel) does not.
	w = httptest.NewRecorder()
	testHandler.ListActivityLog(w, requestAs(t, member2ID, "GET", "/api/activity-log?event_type=iso:channel_event"))
	got = listActivityProjectIDs(t, w)
	if contains(got, projectID) {
		t.Errorf("member2 (no channel membership): must NOT see project row, got %v", got)
	}
}
