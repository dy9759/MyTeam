package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/MyAIOSHub/MyTeam/server/internal/middleware"
)

// fetchTestAgentID resolves the handler fixture's only agent. Kept
// local to this file so interaction tests don't rely on handler_test's
// private helpers beyond what it already exports through package state.
func fetchTestAgentID(t *testing.T, ctx context.Context) string {
	t.Helper()
	agents, err := testHandler.Queries.ListAgents(ctx, parseUUID(testWorkspaceID))
	if err != nil || len(agents) == 0 {
		t.Fatalf("load fixture agent: err=%v count=%d", err, len(agents))
	}
	return uuidToString(agents[0].ID)
}

// newInteractionRequest wraps newRequest() with a proper chi route
// context — the inbox/ack handlers read the `id` URL param via
// chi.URLParam.
func newInteractionRequest(method, path string, body any, pathKey, pathValue string) *http.Request {
	req := newRequest(method, path, body)
	if pathKey != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add(pathKey, pathValue)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	return req
}

func TestSendInteraction_InvalidType(t *testing.T) {
	ctx := context.Background()
	agentID := fetchTestAgentID(t, ctx)

	req := newRequest("POST", "/api/interactions", map[string]any{
		"type":    "nonsense",
		"target":  map[string]any{"agent_id": agentID},
		"payload": map[string]any{"text": "hi"},
	})
	w := httptest.NewRecorder()
	testHandler.SendInteraction(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestSendInteraction_MissingTarget(t *testing.T) {
	req := newRequest("POST", "/api/interactions", map[string]any{
		"type":    "message",
		"target":  map[string]any{},
		"payload": map[string]any{"text": "hi"},
	})
	w := httptest.NewRecorder()
	testHandler.SendInteraction(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestSendInteraction_MultipleTargets(t *testing.T) {
	ctx := context.Background()
	agentID := fetchTestAgentID(t, ctx)

	req := newRequest("POST", "/api/interactions", map[string]any{
		"type": "message",
		"target": map[string]any{
			"agent_id":   agentID,
			"capability": "debug",
		},
		"payload": map[string]any{"text": "hi"},
	})
	w := httptest.NewRecorder()
	testHandler.SendInteraction(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestSendInteraction_InvalidJSONPayload(t *testing.T) {
	ctx := context.Background()
	agentID := fetchTestAgentID(t, ctx)

	// Build the body by hand so we can inject a malformed payload
	// raw-message that json.Unmarshal will reject. The struct encoder
	// would wrap any Go value in valid JSON, masking the bug path.
	rawBody := []byte(`{
		"type": "message",
		"content_type": "json",
		"target": {"agent_id": "` + agentID + `"},
		"payload": {not valid json
	}`)
	req := httptest.NewRequest("POST", "/api/interactions", bytes.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, testMember))

	w := httptest.NewRecorder()
	testHandler.SendInteraction(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", w.Code, w.Body.String())
	}
}

// TestSendInteraction_JSONContentTypeRejectsNonObject proves that a
// syntactically-valid JSON value still gets rejected when the caller
// asserts content_type=json but sends something we consider malformed.
// The body decoder already parsed, so we rely on the probe inside the
// handler to re-check.
func TestSendInteraction_JSONContentTypeAcceptsValidJSON(t *testing.T) {
	ctx := context.Background()
	agentID := fetchTestAgentID(t, ctx)

	req := newRequest("POST", "/api/interactions", map[string]any{
		"type":         "message",
		"content_type": "json",
		"target":       map[string]any{"agent_id": agentID},
		"payload":      map[string]any{"ok": true},
	})
	w := httptest.NewRecorder()
	testHandler.SendInteraction(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d (%s)", w.Code, w.Body.String())
	}
}


func TestSendInteraction_Happy_InboxRoundtrip(t *testing.T) {
	ctx := context.Background()
	agentID := fetchTestAgentID(t, ctx)

	// Send — since there's no WS connection bound to agentID, push
	// delivers 0, status stays 'pending'.
	req := newRequest("POST", "/api/interactions", map[string]any{
		"type":    "message",
		"target":  map[string]any{"agent_id": agentID},
		"payload": map[string]any{"text": "hello"},
	})
	w := httptest.NewRecorder()
	testHandler.SendInteraction(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("send: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var sendResp interactionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &sendResp); err != nil {
		t.Fatalf("decode send: %v", err)
	}
	if sendResp.ID == "" {
		t.Fatalf("missing interaction id")
	}
	// #18: workspace_id must not leak into the response.
	if w.Body.String() != "" && bytesContain(w.Body.Bytes(), "workspace_id") {
		t.Fatalf("response leaks workspace_id: %s", w.Body.String())
	}

	// Inbox — caller owns the agent (fixture assigns owner_id=testUserID)
	// so CanActAsAgent grants access.
	inboxReq := newInteractionRequest("GET", "/api/agents/"+agentID+"/inbox", nil, "id", agentID)
	inboxW := httptest.NewRecorder()
	testHandler.GetAgentInbox(inboxW, inboxReq)
	if inboxW.Code != http.StatusOK {
		t.Fatalf("inbox: want 200, got %d (%s)", inboxW.Code, inboxW.Body.String())
	}
	var inboxResp struct {
		Interactions []interactionResponse `json:"interactions"`
		Count        int                   `json:"count"`
	}
	if err := json.Unmarshal(inboxW.Body.Bytes(), &inboxResp); err != nil {
		t.Fatalf("decode inbox: %v", err)
	}
	// Our send should be in the list. Previous tests may have left
	// rows behind, so we assert ≥1 and find ours by id.
	if inboxResp.Count < 1 {
		t.Fatalf("inbox empty, want ≥1 item")
	}
	found := false
	for _, it := range inboxResp.Interactions {
		if it.ID == sendResp.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("sent interaction %s not in inbox", sendResp.ID)
	}

	// Ack — caller is the owner so must succeed.
	ackReq := newInteractionRequest("POST", "/api/interactions/"+sendResp.ID+"/ack", nil, "id", sendResp.ID)
	ackW := httptest.NewRecorder()
	testHandler.AckInteraction(ackW, ackReq)
	if ackW.Code != http.StatusOK {
		t.Fatalf("ack: want 200, got %d (%s)", ackW.Code, ackW.Body.String())
	}
}

func TestGetAgentInbox_Forbidden(t *testing.T) {
	ctx := context.Background()
	agentID := fetchTestAgentID(t, ctx)

	// Switch the request user to someone who isn't the agent's owner
	// and isn't an admin. Simulate by mutating the X-User-ID header
	// after newRequest populated it.
	req := newInteractionRequest("GET", "/api/agents/"+agentID+"/inbox", nil, "id", agentID)
	req.Header.Set("X-User-ID", "00000000-0000-0000-0000-000000000001")
	// Swap context member to a fake role to make sure admin override
	// doesn't grant.
	fakeMember := testMember
	fakeMember.Role = "member"
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, fakeMember))

	w := httptest.NewRecorder()
	testHandler.GetAgentInbox(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d (%s)", w.Code, w.Body.String())
	}
}

func bytesContain(body []byte, needle string) bool {
	return len(body) > 0 && len(needle) > 0 && (func() bool {
		for i := 0; i+len(needle) <= len(body); i++ {
			if string(body[i:i+len(needle)]) == needle {
				return true
			}
		}
		return false
	})()
}
