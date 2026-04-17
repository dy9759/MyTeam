package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

// inboxTestSeed inserts inbox_item rows for the given recipient and returns their IDs.
// Cleans up via t.Cleanup so the test database stays tidy.
func inboxTestSeed(t *testing.T, recipientID, workspaceID string, count int) []string {
	t.Helper()
	ctx := context.Background()
	ids := make([]string, 0, count)
	for i := 0; i < count; i++ {
		var id string
		err := testPool.QueryRow(ctx, `
			INSERT INTO inbox_item (
				workspace_id, recipient_type, recipient_id,
				type, severity, title, body
			) VALUES ($1, 'member', $2, 'test', 'info', $3, NULL)
			RETURNING id
		`, workspaceID, recipientID, fmt.Sprintf("inbox-test #%d", i)).Scan(&id)
		if err != nil {
			t.Fatalf("inboxTestSeed: insert failed: %v", err)
		}
		ids = append(ids, id)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE recipient_id = $1 AND type = 'test'`, recipientID)
	})
	return ids
}

func TestListInboxUnresolved(t *testing.T) {
	ctx := context.Background()
	ids := inboxTestSeed(t, testUserID, testWorkspaceID, 3)

	// Resolve one of them so the unresolved list excludes it.
	if _, err := testPool.Exec(ctx, `
		UPDATE inbox_item SET resolved_at = now(), resolution = 'approved'
		WHERE id = $1
	`, ids[0]); err != nil {
		t.Fatalf("seed resolve: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/inbox?unresolved=true&limit=50&offset=0", nil)
	testHandler.ListInbox(w, req)
	if w.Code != 200 {
		t.Fatalf("ListInbox(unresolved): expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []InboxItemResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// We should see at least the two unresolved seeds, never the resolved one.
	seen := map[string]bool{}
	for _, item := range resp {
		seen[item.ID] = true
	}
	if seen[ids[0]] {
		t.Fatalf("ListInbox(unresolved): resolved item %s should not be in result", ids[0])
	}
	if !seen[ids[1]] || !seen[ids[2]] {
		t.Fatalf("ListInbox(unresolved): expected both unresolved items present, got ids=%v", resp)
	}
}

func TestResolveInboxItem(t *testing.T) {
	ctx := context.Background()
	ids := inboxTestSeed(t, testUserID, testWorkspaceID, 1)
	itemID := ids[0]

	// Valid resolution succeeds.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/inbox/"+itemID+"/resolve", map[string]any{"resolution": "approved"})
	req = withURLParam(req, "id", itemID)
	testHandler.ResolveInboxItem(w, req)
	if w.Code != 200 {
		t.Fatalf("ResolveInboxItem: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "resolved" || body["resolution"] != "approved" {
		t.Fatalf("ResolveInboxItem: unexpected body %v", body)
	}

	// Verify DB was actually updated.
	var resolution string
	var resolvedAtIsSet bool
	err := testPool.QueryRow(ctx, `
		SELECT resolution, resolved_at IS NOT NULL FROM inbox_item WHERE id = $1
	`, itemID).Scan(&resolution, &resolvedAtIsSet)
	if err != nil {
		t.Fatalf("verify update: %v", err)
	}
	if resolution != "approved" {
		t.Fatalf("expected resolution=approved, got %q", resolution)
	}
	if !resolvedAtIsSet {
		t.Fatal("expected resolved_at to be populated")
	}

	// Subsequent ListInbox(unresolved) should not include this item.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/inbox?unresolved=true", nil)
	testHandler.ListInbox(w, req)
	if w.Code != 200 {
		t.Fatalf("ListInbox: expected 200, got %d", w.Code)
	}
	var listed []InboxItemResponse
	json.NewDecoder(w.Body).Decode(&listed)
	for _, item := range listed {
		if item.ID == itemID {
			t.Fatalf("resolved item %s should not appear in unresolved list", itemID)
		}
	}
}

func TestResolveInboxItemInvalidResolution(t *testing.T) {
	ids := inboxTestSeed(t, testUserID, testWorkspaceID, 1)
	itemID := ids[0]

	// Invalid resolution rejected.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/inbox/"+itemID+"/resolve", map[string]any{"resolution": "garbage"})
	req = withURLParam(req, "id", itemID)
	testHandler.ResolveInboxItem(w, req)
	if w.Code != 400 {
		t.Fatalf("invalid resolution: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "approved|rejected|dismissed") {
		t.Fatalf("invalid resolution: unexpected error body: %s", w.Body.String())
	}

	// auto_resolved is system-only and must not be exposed via this endpoint.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/inbox/"+itemID+"/resolve", map[string]any{"resolution": "auto_resolved"})
	req = withURLParam(req, "id", itemID)
	testHandler.ResolveInboxItem(w, req)
	if w.Code != 400 {
		t.Fatalf("auto_resolved: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMarkInboxItemRead(t *testing.T) {
	ctx := context.Background()
	ids := inboxTestSeed(t, testUserID, testWorkspaceID, 1)
	itemID := ids[0]

	// First call marks read.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/inbox/"+itemID+"/read", nil)
	req = withURLParam(req, "id", itemID)
	testHandler.MarkInboxItemRead(w, req)
	if w.Code != 200 {
		t.Fatalf("MarkInboxItemRead: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var read bool
	if err := testPool.QueryRow(ctx, `SELECT read FROM inbox_item WHERE id = $1`, itemID).Scan(&read); err != nil {
		t.Fatalf("verify read: %v", err)
	}
	if !read {
		t.Fatal("expected read=true after MarkInboxItemRead")
	}

	// Second call is a no-op (idempotent) and still returns 200.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/inbox/"+itemID+"/read", nil)
	req = withURLParam(req, "id", itemID)
	testHandler.MarkInboxItemRead(w, req)
	if w.Code != 200 {
		t.Fatalf("MarkInboxItemRead (idempotent): expected 200, got %d", w.Code)
	}
}

func TestMarkAllInboxItemsRead(t *testing.T) {
	ctx := context.Background()
	ids := inboxTestSeed(t, testUserID, testWorkspaceID, 3)

	// Sanity: all start unread.
	for _, id := range ids {
		var read bool
		if err := testPool.QueryRow(ctx, `SELECT read FROM inbox_item WHERE id = $1`, id).Scan(&read); err != nil {
			t.Fatalf("seed verify: %v", err)
		}
		if read {
			t.Fatalf("seed should be unread, got read=true for %s", id)
		}
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/inbox/mark-all-read-recipient", nil)
	testHandler.MarkAllInboxItemsRead(w, req)
	if w.Code != 200 {
		t.Fatalf("MarkAllInboxItemsRead: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	// count is JSON-decoded as float64.
	if c, ok := body["count"].(float64); !ok || c < 3 {
		t.Fatalf("MarkAllInboxItemsRead: expected count >= 3, got body=%v", body)
	}

	// All items now read.
	for _, id := range ids {
		var read bool
		if err := testPool.QueryRow(ctx, `SELECT read FROM inbox_item WHERE id = $1`, id).Scan(&read); err != nil {
			t.Fatalf("post verify: %v", err)
		}
		if !read {
			t.Fatalf("expected read=true after mark-all for %s", id)
		}
	}
}
