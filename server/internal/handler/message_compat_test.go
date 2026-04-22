package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// compatTestChannel creates a fresh channel bound to the test workspace and
// registers a cleanup hook. Returns the channel ID.
func compatTestChannel(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	var channelID string
	err := testPool.QueryRow(ctx,
		`INSERT INTO channel (workspace_id, name, description, created_by, created_by_type)
		 VALUES ($1, $2, '', $3, 'member') RETURNING id`,
		testWorkspaceID, name, testUserID,
	).Scan(&channelID)
	if err != nil {
		t.Fatalf("compatTestChannel: insert channel failed: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel WHERE id = $1`, channelID)
	})
	return channelID
}

// compatTestThread creates an empty thread on the given channel and returns
// its ID. Channel deletion cascades so no per-thread cleanup is registered.
func compatTestThread(t *testing.T, ctx context.Context, channelID string) string {
	t.Helper()
	thread, err := testHandler.Queries.CreateThread(ctx, db.CreateThreadParams{
		ChannelID:   parseUUID(channelID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("compatTestThread: CreateThread failed: %v", err)
	}
	return uuidToString(thread.ID)
}

// TestIncrementThreadCounters_Member verifies that a "member" sender bumps
// thread.reply_count and updates last_reply_at.
func TestIncrementThreadCounters_Member(t *testing.T) {
	ctx := context.Background()

	channelID := compatTestChannel(t, ctx, "compat-counters-member")
	threadIDStr := compatTestThread(t, ctx, channelID)
	threadID := parseUUID(threadIDStr)

	before, err := testHandler.Queries.GetThread(ctx, threadID)
	if err != nil {
		t.Fatalf("GetThread (before): %v", err)
	}

	testHandler.incrementThreadCounters(ctx, threadID, "member")

	after, err := testHandler.Queries.GetThread(ctx, threadID)
	if err != nil {
		t.Fatalf("GetThread (after): %v", err)
	}
	if after.ReplyCount != before.ReplyCount+1 {
		t.Fatalf("reply_count: expected %d, got %d", before.ReplyCount+1, after.ReplyCount)
	}
	if !after.LastReplyAt.Valid {
		t.Fatalf("last_reply_at: expected non-null after member post, got null")
	}
	if before.LastReplyAt.Valid && !after.LastReplyAt.Time.After(before.LastReplyAt.Time) {
		t.Fatalf("last_reply_at: expected to advance, got %v -> %v", before.LastReplyAt.Time, after.LastReplyAt.Time)
	}
}

// TestIncrementThreadCounters_System verifies that a "system" sender leaves
// reply_count unchanged but still advances last_activity_at.
func TestIncrementThreadCounters_System(t *testing.T) {
	ctx := context.Background()

	channelID := compatTestChannel(t, ctx, "compat-counters-system")
	threadIDStr := compatTestThread(t, ctx, channelID)
	threadID := parseUUID(threadIDStr)

	before, err := testHandler.Queries.GetThread(ctx, threadID)
	if err != nil {
		t.Fatalf("GetThread (before): %v", err)
	}

	// Sleep-free: rely on now() monotonic within the same transaction —
	// TouchThreadActivity uses now() which always advances between queries.
	testHandler.incrementThreadCounters(ctx, threadID, "system")

	after, err := testHandler.Queries.GetThread(ctx, threadID)
	if err != nil {
		t.Fatalf("GetThread (after): %v", err)
	}
	if after.ReplyCount != before.ReplyCount {
		t.Fatalf("reply_count: expected UNCHANGED (%d) for system sender, got %d", before.ReplyCount, after.ReplyCount)
	}
	if !after.LastActivityAt.Valid {
		t.Fatalf("last_activity_at: expected non-null after system touch, got null")
	}
	if before.LastActivityAt.Valid && after.LastActivityAt.Time.Before(before.LastActivityAt.Time) {
		t.Fatalf("last_activity_at: expected to advance or match, got %v -> %v", before.LastActivityAt.Time, after.LastActivityAt.Time)
	}
}

// TestIncrementThreadCounters_InvalidThread verifies that passing an invalid
// UUID is a no-op (no panic, no DB error).
func TestIncrementThreadCounters_InvalidThread(t *testing.T) {
	ctx := context.Background()
	// Invalid pgtype.UUID (Valid=false) must be ignored.
	testHandler.incrementThreadCounters(ctx, pgtype.UUID{}, "member")
}

// TestCreateMessage_WithParentMessage_PopulatesThreadID is the regression test
// for the coupled bug where CreateMessage's sqlc INSERT was missing the
// thread_id column and UpsertThread was missing workspace_id. Before the fix:
//   - messages posted with parent_message_id had thread_id = NULL;
//   - the first attempt to materialize a thread crashed because
//     thread.workspace_id is NOT NULL (migration 051).
//
// After the fix: the thread is created with the correct workspace_id and the
// reply message carries the resolved thread_id.
func TestCreateMessage_WithParentMessage_PopulatesThreadID(t *testing.T) {
	ctx := context.Background()
	channelID := compatTestChannel(t, ctx, "compat-create-thread-id")

	// Seed a parent message directly via CreateMessage (no parent_message_id,
	// no thread) — this becomes the root of the soon-to-be thread.
	parent, err := testHandler.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		SenderID:    parseUUID(testUserID),
		SenderType:  "member",
		ChannelID:   parseUUID(channelID),
		Content:     "parent message",
		ContentType: "text",
		Type:        "user",
	})
	if err != nil {
		t.Fatalf("create parent message: %v", err)
	}
	parentIDStr := uuidToString(parent.ID)

	// POST /api/messages with parent_message_id set — should resolve/create
	// a thread AND stamp thread_id on the new message.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/messages", map[string]any{
		"channel_id":        channelID,
		"content":           "reply message",
		"parent_message_id": parentIDStr,
	})
	testHandler.CreateMessage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateMessage: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	replyIDStr, _ := resp["id"].(string)
	if replyIDStr == "" {
		t.Fatalf("response missing id: %+v", resp)
	}

	// Reload the message from the DB (the response map does not surface
	// thread_id — we only trust the persisted row).
	stored, err := testHandler.Queries.GetMessage(ctx, parseUUID(replyIDStr))
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if !stored.ThreadID.Valid {
		t.Fatalf("stored message thread_id: expected non-null, got NULL")
	}
	if uuidToString(stored.ThreadID) != parentIDStr {
		t.Fatalf("thread_id: expected %s (== parent_message_id), got %s",
			parentIDStr, uuidToString(stored.ThreadID))
	}

	// Thread row must exist with the workspace_id populated (regression guard
	// for the UpsertThread missing-workspace_id bug).
	thread, err := testHandler.Queries.GetThread(ctx, parseUUID(parentIDStr))
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}
	if !thread.WorkspaceID.Valid {
		t.Fatalf("thread.workspace_id: expected non-null, got NULL")
	}
	if uuidToString(thread.WorkspaceID) != testWorkspaceID {
		t.Fatalf("thread.workspace_id: expected %s, got %s",
			testWorkspaceID, uuidToString(thread.WorkspaceID))
	}

	// ListMessagesByThread must find the reply (the silent-bypass path that
	// triggered bug #1's downstream effects).
	threadMessages, err := testHandler.Queries.ListMessagesByThread(ctx, db.ListMessagesByThreadParams{
		ThreadID: parseUUID(parentIDStr),
		Limit:    10,
		Offset:   0,
	})
	if err != nil {
		t.Fatalf("ListMessagesByThread: %v", err)
	}
	found := false
	for _, m := range threadMessages {
		if uuidToString(m.ID) == replyIDStr {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ListMessagesByThread did not return reply %s; got %d messages",
			replyIDStr, len(threadMessages))
	}
}
