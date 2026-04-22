package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// withURLParams sets multiple chi URL params on a single request context.
// Necessary because the helper withURLParam in handler_test.go replaces the
// chi context entirely on each call, dropping prior params.
func withURLParams(req *http.Request, kv map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range kv {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// threadV2TestChannel creates a fresh channel for use by these tests and returns
// its UUID. Caller is responsible for cleanup via t.Cleanup.
func threadV2TestChannel(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	var channelID string
	err := testPool.QueryRow(ctx,
		`INSERT INTO channel (workspace_id, name, description, created_by, created_by_type)
		 VALUES ($1, $2, '', $3, 'member') RETURNING id`,
		testWorkspaceID, name, testUserID,
	).Scan(&channelID)
	if err != nil {
		t.Fatalf("setup channel failed: %v", err)
	}
	t.Cleanup(func() {
		// Cascades to thread / thread_context_item / message via FKs.
		testPool.Exec(context.Background(), `DELETE FROM channel WHERE id = $1`, channelID)
	})
	return channelID
}

// threadV2CreateThread invokes the CreateThread handler and returns the parsed
// response body. Fails the test on non-201 responses.
func threadV2CreateThread(t *testing.T, channelID string, body map[string]any) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", fmt.Sprintf("/api/channels/%s/threads", channelID), body)
	req = withURLParam(req, "channelID", channelID)
	testHandler.CreateThread(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateThread: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("CreateThread: decode body failed: %v", err)
	}
	return resp
}

func TestCreateThread_Defaults(t *testing.T) {
	ctx := context.Background()
	channelID := threadV2TestChannel(t, ctx, "thread-create-defaults")

	thread := threadV2CreateThread(t, channelID, map[string]any{})

	if thread["id"] == nil || thread["id"] == "" {
		t.Fatalf("CreateThread: expected non-empty id, got %v", thread["id"])
	}
	if thread["channel_id"] != channelID {
		t.Fatalf("CreateThread: expected channel_id %s, got %v", channelID, thread["channel_id"])
	}
	if thread["status"] != "active" {
		t.Fatalf("CreateThread: expected status 'active', got %v", thread["status"])
	}
	if thread["title"] != nil {
		t.Fatalf("CreateThread: expected null title, got %v", thread["title"])
	}
	// reply_count is a JSON number => float64 once decoded.
	if rc, ok := thread["reply_count"].(float64); !ok || rc != 0 {
		t.Fatalf("CreateThread: expected reply_count 0, got %v", thread["reply_count"])
	}
}

func TestCreateThread_WithTitle(t *testing.T) {
	ctx := context.Background()
	channelID := threadV2TestChannel(t, ctx, "thread-create-title")

	thread := threadV2CreateThread(t, channelID, map[string]any{
		"title": "Q4 planning",
	})

	if thread["title"] != "Q4 planning" {
		t.Fatalf("CreateThread: expected title 'Q4 planning', got %v", thread["title"])
	}
}

func TestGetThread_NotFound(t *testing.T) {
	missingID := "00000000-0000-0000-0000-000000000099"

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/threads/"+missingID, nil)
	req = withURLParam(req, "threadID", missingID)
	testHandler.GetThread(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetThread (missing): expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetThread_Found(t *testing.T) {
	ctx := context.Background()
	channelID := threadV2TestChannel(t, ctx, "thread-get-found")
	created := threadV2CreateThread(t, channelID, map[string]any{"title": "Found"})
	threadID := created["id"].(string)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/threads/"+threadID, nil)
	req = withURLParam(req, "threadID", threadID)
	testHandler.GetThread(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetThread: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var fetched map[string]any
	if err := json.NewDecoder(w.Body).Decode(&fetched); err != nil {
		t.Fatalf("GetThread: decode body failed: %v", err)
	}
	if fetched["id"] != threadID {
		t.Fatalf("GetThread: expected id %s, got %v", threadID, fetched["id"])
	}
	if fetched["title"] != "Found" {
		t.Fatalf("GetThread: expected title 'Found', got %v", fetched["title"])
	}
}

func TestListThreadContextItems_Empty(t *testing.T) {
	ctx := context.Background()
	channelID := threadV2TestChannel(t, ctx, "thread-ctx-empty")
	created := threadV2CreateThread(t, channelID, map[string]any{})
	threadID := created["id"].(string)

	w := httptest.NewRecorder()
	req := newRequest("GET", fmt.Sprintf("/api/threads/%s/context-items", threadID), nil)
	req = withURLParam(req, "threadID", threadID)
	testHandler.ListThreadContextItems(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListThreadContextItems: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("ListThreadContextItems: decode body failed: %v", err)
	}
	items, ok := resp["items"].([]any)
	if !ok {
		t.Fatalf("ListThreadContextItems: expected 'items' array, got %T (%v)", resp["items"], resp["items"])
	}
	if len(items) != 0 {
		t.Fatalf("ListThreadContextItems: expected empty array, got %d items", len(items))
	}
}

func TestCreateThreadContextItem_DecisionPermanentDefault(t *testing.T) {
	ctx := context.Background()
	channelID := threadV2TestChannel(t, ctx, "thread-ctx-decision")
	created := threadV2CreateThread(t, channelID, map[string]any{})
	threadID := created["id"].(string)

	w := httptest.NewRecorder()
	req := newRequest("POST", fmt.Sprintf("/api/threads/%s/context-items", threadID), map[string]any{
		"item_type": "decision",
		"title":     "Adopt sqlc",
		"body":      "We will use sqlc for all DB access.",
	})
	req = withURLParam(req, "threadID", threadID)
	testHandler.CreateThreadContextItem(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateThreadContextItem (decision): expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var item map[string]any
	if err := json.NewDecoder(w.Body).Decode(&item); err != nil {
		t.Fatalf("CreateThreadContextItem: decode body failed: %v", err)
	}
	if item["item_type"] != "decision" {
		t.Fatalf("CreateThreadContextItem: expected item_type 'decision', got %v", item["item_type"])
	}
	if item["retention_class"] != "permanent" {
		t.Fatalf("CreateThreadContextItem: expected default retention_class 'permanent' for decision, got %v", item["retention_class"])
	}
}

func TestCreateThreadContextItem_FileTTLDefault(t *testing.T) {
	ctx := context.Background()
	channelID := threadV2TestChannel(t, ctx, "thread-ctx-file")
	created := threadV2CreateThread(t, channelID, map[string]any{})
	threadID := created["id"].(string)

	w := httptest.NewRecorder()
	req := newRequest("POST", fmt.Sprintf("/api/threads/%s/context-items", threadID), map[string]any{
		"item_type": "file",
		"title":     "spec.pdf",
	})
	req = withURLParam(req, "threadID", threadID)
	testHandler.CreateThreadContextItem(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateThreadContextItem (file): expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var item map[string]any
	if err := json.NewDecoder(w.Body).Decode(&item); err != nil {
		t.Fatalf("CreateThreadContextItem: decode body failed: %v", err)
	}
	if item["retention_class"] != "ttl" {
		t.Fatalf("CreateThreadContextItem: expected default retention_class 'ttl' for file, got %v", item["retention_class"])
	}
}

func TestDeleteThreadContextItem_Success(t *testing.T) {
	ctx := context.Background()
	channelID := threadV2TestChannel(t, ctx, "thread-ctx-del")
	created := threadV2CreateThread(t, channelID, map[string]any{})
	threadID := created["id"].(string)

	// Create an item to delete.
	w := httptest.NewRecorder()
	req := newRequest("POST", fmt.Sprintf("/api/threads/%s/context-items", threadID), map[string]any{
		"item_type": "reference",
		"title":     "Doc link",
	})
	req = withURLParam(req, "threadID", threadID)
	testHandler.CreateThreadContextItem(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup: CreateThreadContextItem: expected 201, got %d", w.Code)
	}
	var created2 map[string]any
	json.NewDecoder(w.Body).Decode(&created2)
	itemID := created2["id"].(string)

	// Delete it.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", fmt.Sprintf("/api/threads/%s/context-items/%s", threadID, itemID), nil)
	req = withURLParams(req, map[string]string{"threadID": threadID, "itemID": itemID})
	testHandler.DeleteThreadContextItem(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteThreadContextItem: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify it is gone.
	_, err := testHandler.Queries.GetThreadContextItem(ctx, parseUUID(itemID))
	if err == nil {
		t.Fatalf("DeleteThreadContextItem: expected item to be deleted, but it still exists")
	}
}

func TestDeleteThreadContextItem_WrongThreadReturns404(t *testing.T) {
	ctx := context.Background()
	channelA := threadV2TestChannel(t, ctx, "thread-ctx-del-a")
	channelB := threadV2TestChannel(t, ctx, "thread-ctx-del-b")
	threadA := threadV2CreateThread(t, channelA, map[string]any{})["id"].(string)
	threadB := threadV2CreateThread(t, channelB, map[string]any{})["id"].(string)

	// Create an item under threadA.
	item, err := testHandler.Queries.CreateThreadContextItem(ctx, db.CreateThreadContextItemParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		ThreadID:       parseUUID(threadA),
		ItemType:       "decision",
		Title:          strToText("Owned by A"),
		RetentionClass: pgtype.Text{String: "permanent", Valid: true},
		CreatedBy:      parseUUID(testUserID),
		CreatedByType:  pgtype.Text{String: "member", Valid: true},
		Metadata:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("setup: CreateThreadContextItem failed: %v", err)
	}
	itemID := uuidToString(item.ID)

	// Try to delete via threadB.
	w := httptest.NewRecorder()
	req := newRequest("DELETE", fmt.Sprintf("/api/threads/%s/context-items/%s", threadB, itemID), nil)
	req = withURLParams(req, map[string]string{"threadID": threadB, "itemID": itemID})
	testHandler.DeleteThreadContextItem(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DeleteThreadContextItem (wrong thread): expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Item should still exist.
	if _, err := testHandler.Queries.GetThreadContextItem(ctx, item.ID); err != nil {
		t.Fatalf("expected item to survive failed delete, but got: %v", err)
	}
}

func TestPostThreadMessage_IncrementsReplyCount(t *testing.T) {
	ctx := context.Background()
	channelID := threadV2TestChannel(t, ctx, "thread-post-msg")
	created := threadV2CreateThread(t, channelID, map[string]any{"title": "msg-thread"})
	threadID := created["id"].(string)

	w := httptest.NewRecorder()
	req := newRequest("POST", fmt.Sprintf("/api/threads/%s/messages", threadID), map[string]any{
		"content": "Hello thread",
	})
	req = withURLParam(req, "threadID", threadID)
	testHandler.PostThreadMessage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("PostThreadMessage: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var msg map[string]any
	if err := json.NewDecoder(w.Body).Decode(&msg); err != nil {
		t.Fatalf("PostThreadMessage: decode body failed: %v", err)
	}
	if msg["content"] != "Hello thread" {
		t.Fatalf("PostThreadMessage: expected content 'Hello thread', got %v", msg["content"])
	}
	if msg["channel_id"] != channelID {
		t.Fatalf("PostThreadMessage: expected channel_id %s, got %v", channelID, msg["channel_id"])
	}

	// Verify thread was updated: reply_count should be 1 (member sender).
	updated, err := testHandler.Queries.GetThread(ctx, parseUUID(threadID))
	if err != nil {
		t.Fatalf("GetThread after post: %v", err)
	}
	if updated.ReplyCount != 1 {
		t.Fatalf("expected reply_count=1 after member post, got %d", updated.ReplyCount)
	}

	// Verify message is returned via ListThreadMessages.
	w = httptest.NewRecorder()
	req = newRequest("GET", fmt.Sprintf("/api/threads/%s/messages", threadID), nil)
	req = withURLParam(req, "threadID", threadID)
	testHandler.ListThreadMessages(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListThreadMessages: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var lresp map[string]any
	json.NewDecoder(w.Body).Decode(&lresp)
	msgs := lresp["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("ListThreadMessages: expected 1 message, got %d", len(msgs))
	}
}
