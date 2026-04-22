package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// findTestAgentID returns the agent created by the test fixture.
func findTestAgentID(t *testing.T, ctx context.Context) string {
	t.Helper()
	var agentID string
	err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'`,
		testWorkspaceID,
	).Scan(&agentID)
	if err != nil {
		t.Fatalf("findTestAgentID: %v", err)
	}
	return agentID
}

// insertDM inserts a DM row directly into the message table. senderType /
// recipientType must be "member" or "agent".
func insertDM(t *testing.T, ctx context.Context, senderID, senderType, recipientID, recipientType, content string) string {
	t.Helper()
	var msgID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO message
		  (workspace_id, sender_id, sender_type, recipient_id, recipient_type, content, content_type, type)
		VALUES ($1, $2, $3, $4, $5, $6, 'text', 'text')
		RETURNING id
	`, testWorkspaceID, senderID, senderType, recipientID, recipientType, content).Scan(&msgID)
	if err != nil {
		t.Fatalf("insertDM: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM message WHERE id = $1`, msgID)
	})
	return msgID
}

// callListMessages invokes the handler with the given recipient and optional
// peer_type query parameter. Returns the decoded "messages" slice.
func callListMessages(t *testing.T, recipientID, peerType string) []map[string]any {
	t.Helper()
	url := "/api/messages?recipient_id=" + recipientID
	if peerType != "" {
		url += "&peer_type=" + peerType
	}
	w := httptest.NewRecorder()
	req := newRequest("GET", url, nil)
	testHandler.ListMessages(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListMessages: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("ListMessages: decode response: %v", err)
	}
	return resp.Messages
}

// TestListMessages_DefaultPeerTypeIsMember verifies backward compatibility:
// omitting peer_type should behave like peer_type=member.
func TestListMessages_DefaultPeerTypeIsMember(t *testing.T) {
	ctx := context.Background()

	// Create a second user/member to DM with.
	var peerUserID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('DM Peer User', 'dm-peer@myteam.ai')
		RETURNING id
	`).Scan(&peerUserID)
	if err != nil {
		t.Fatalf("create peer user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, peerUserID)
	})

	memberMsgID := insertDM(t, ctx, testUserID, "member", peerUserID, "member", "hello member peer")
	// Insert an unrelated agent DM that must NOT appear in the member-default result.
	agentID := findTestAgentID(t, ctx)
	insertDM(t, ctx, testUserID, "member", agentID, "agent", "hello agent peer (should be excluded)")

	msgs := callListMessages(t, peerUserID, "")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 member DM, got %d: %+v", len(msgs), msgs)
	}
	if id, _ := msgs[0]["id"].(string); id != memberMsgID {
		t.Fatalf("expected message %s, got %s", memberMsgID, id)
	}
}

// TestListMessages_PeerTypeAgent reproduces the original bug: a user DMing an
// agent should see the agent DM history when peer_type=agent is supplied.
func TestListMessages_PeerTypeAgent(t *testing.T) {
	ctx := context.Background()
	agentID := findTestAgentID(t, ctx)

	sentID := insertDM(t, ctx, testUserID, "member", agentID, "agent", "user -> agent: ping")
	receivedID := insertDM(t, ctx, agentID, "agent", testUserID, "member", "agent -> user: pong")

	msgs := callListMessages(t, agentID, "agent")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in user<->agent DM, got %d: %+v", len(msgs), msgs)
	}

	gotIDs := map[string]bool{}
	for _, m := range msgs {
		if id, ok := m["id"].(string); ok {
			gotIDs[id] = true
		}
	}
	if !gotIDs[sentID] {
		t.Errorf("missing sent message %s in result", sentID)
	}
	if !gotIDs[receivedID] {
		t.Errorf("missing received message %s in result", receivedID)
	}
}

// TestListMessages_PeerTypeAgentExcludesMemberDMs guards against the inverse
// of the original bug — passing peer_type=agent must not pick up member DMs
// that happen to share the same recipient_id.
func TestListMessages_PeerTypeAgentExcludesMemberDMs(t *testing.T) {
	ctx := context.Background()
	agentID := findTestAgentID(t, ctx)

	// Insert an agent DM and a (synthetic) member DM with recipient_id == agentID.
	// The latter would not occur in production, but it is the cleanest way to
	// prove the recipient_type filter is being applied.
	agentMsgID := insertDM(t, ctx, testUserID, "member", agentID, "agent", "user -> agent")
	insertDM(t, ctx, testUserID, "member", agentID, "member", "user -> member with shared id")

	msgs := callListMessages(t, agentID, "agent")
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 agent DM, got %d", len(msgs))
	}
	if id, _ := msgs[0]["id"].(string); id != agentMsgID {
		t.Fatalf("expected message %s, got %s", agentMsgID, id)
	}
}
