package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// TestVerifyCode_AutoCreatesPersonalAgent verifies that a brand-new user signing
// in via verify-code ends up with a personal agent in their auto-provisioned
// workspace, and that both the agent (status='idle') and its runtime
// (status='online') are ready to receive DMs immediately.
func TestVerifyCode_AutoCreatesPersonalAgent(t *testing.T) {
	const email = "auto-pa-verify@myteam.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
		user, err := testHandler.Queries.GetUserByEmail(ctx, email)
		if err == nil {
			workspaces, listErr := testHandler.Queries.ListWorkspaces(ctx, user.ID)
			if listErr == nil {
				for _, ws := range workspaces {
					_ = testHandler.Queries.DeleteWorkspace(ctx, ws.ID)
				}
			}
		}
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})

	// Send code.
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	dbCode, err := testHandler.Queries.GetLatestVerificationCode(ctx, email)
	if err != nil {
		t.Fatalf("GetLatestVerificationCode: %v", err)
	}

	// Verify code -> creates user + auto workspace + auto personal agent.
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": dbCode.Code})
	req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("VerifyCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	user, err := testHandler.Queries.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}

	workspaces, err := testHandler.Queries.ListWorkspaces(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected exactly 1 auto-provisioned workspace, got %d", len(workspaces))
	}

	wsID := workspaces[0].ID
	// auto-provision is async (per Codex review) — poll up to 5s for the agent to materialize
	var agent db.Agent
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		agent, err = testHandler.Queries.GetPersonalAgent(ctx, db.GetPersonalAgentParams{
			WorkspaceID: wsID,
			OwnerID:     user.ID,
		})
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GetPersonalAgent (after 5s poll): expected agent to exist, got: %v", err)
	}
	if agent.Status != "idle" {
		t.Fatalf("personal agent status: expected 'idle', got %q", agent.Status)
	}
	if agent.AgentType != "personal_agent" {
		t.Fatalf("agent_type: expected 'personal_agent', got %q", agent.AgentType)
	}
	if !agent.AutoReplyEnabled.Bool {
		t.Fatalf("auto_reply_enabled: expected true, got false")
	}
	if !agent.RuntimeID.Valid {
		t.Fatalf("agent runtime_id should be set")
	}

	rt, err := testHandler.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		t.Fatalf("GetAgentRuntime: %v", err)
	}
	if rt.Status != "online" {
		t.Fatalf("runtime status: expected 'online', got %q", rt.Status)
	}
	if rt.Mode.String != "cloud" {
		t.Fatalf("runtime mode: expected 'cloud', got %q", rt.Mode.String)
	}
}

// TestCreateWorkspace_AutoCreatesPersonalAgent verifies that explicit workspace
// creation also auto-provisions the creator's personal agent so they can DM
// it the instant the workspace appears.
func TestCreateWorkspace_AutoCreatesPersonalAgent(t *testing.T) {
	const email = "auto-pa-ws@myteam.ai"
	ctx := context.Background()

	// Create a fresh user via direct INSERT so we don't depend on verify-code.
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, "Auto PA User", email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	uniqueSlug := fmt.Sprintf("auto-pa-ws-%d", time.Now().UnixNano())

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, uniqueSlug)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})

	// Call CreateWorkspace as the new user.
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]any{
		"name": "Auto PA Test Workspace",
		"slug": uniqueSlug,
	})
	req := httptest.NewRequest("POST", "/api/workspaces", &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkspace: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var ws WorkspaceResponse
	if err := json.NewDecoder(w.Body).Decode(&ws); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Auto-provisioning is best-effort and runs synchronously inside
	// CreateWorkspace, so the agent should already exist when we look.
	wsUUID := parseUUID(ws.ID)
	userUUID := parseUUID(userID)
	agent, err := pollPersonalAgent(ctx, testHandler.Queries, wsUUID, userUUID, 2*time.Second)
	if err != nil {
		t.Fatalf("personal agent should exist after CreateWorkspace: %v", err)
	}
	if agent.Status != "idle" {
		t.Fatalf("personal agent status: expected 'idle', got %q", agent.Status)
	}
	if !agent.RuntimeID.Valid {
		t.Fatalf("agent runtime_id should be set")
	}
	rt, err := testHandler.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		t.Fatalf("GetAgentRuntime: %v", err)
	}
	if rt.Status != "online" {
		t.Fatalf("runtime status: expected 'online', got %q", rt.Status)
	}
}

// TestCreateMember_AutoCreatesPersonalAgentForNewMember verifies that adding a
// member to an existing workspace provisions a personal agent for that user
// in the workspace they were just added to.
func TestCreateMember_AutoCreatesPersonalAgentForNewMember(t *testing.T) {
	const inviteeEmail = "auto-pa-invitee@myteam.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		// Cleanup: agent rows cascade via workspace; just clear the invitee user.
		user, err := testHandler.Queries.GetUserByEmail(ctx, inviteeEmail)
		if err == nil {
			testPool.Exec(ctx, `DELETE FROM member WHERE user_id = $1`, uuidToString(user.ID))
			testPool.Exec(ctx, `DELETE FROM agent WHERE owner_id = $1`, uuidToString(user.ID))
		}
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, inviteeEmail)
	})

	// AddMember as the existing testUserID owner against testWorkspaceID.
	w := httptest.NewRecorder()
	body := map[string]any{"email": inviteeEmail, "role": "member"}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(body)
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", body)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.CreateMember(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateMember: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	user, err := testHandler.Queries.GetUserByEmail(ctx, inviteeEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail invitee: %v", err)
	}

	wsUUID := parseUUID(testWorkspaceID)
	agent, err := pollPersonalAgent(ctx, testHandler.Queries, wsUUID, user.ID, 2*time.Second)
	if err != nil {
		t.Fatalf("personal agent should exist for new member: %v", err)
	}
	if agent.Status != "idle" {
		t.Fatalf("personal agent status: expected 'idle', got %q", agent.Status)
	}
	if uuidToString(agent.OwnerID) != uuidToString(user.ID) {
		t.Fatalf("owner mismatch: agent.owner_id=%s, user.id=%s",
			uuidToString(agent.OwnerID), uuidToString(user.ID))
	}
}

// pollPersonalAgent retries GetPersonalAgent for up to `wait` to absorb the
// best-effort goroutine-based provisioning paths (CreateWorkspace fans out
// auto-create work in a background goroutine).
func pollPersonalAgent(ctx context.Context, q *db.Queries, workspaceID, ownerID pgtype.UUID, wait time.Duration) (db.Agent, error) {
	deadline := time.Now().Add(wait)
	var lastErr error
	for time.Now().Before(deadline) {
		agent, err := q.GetPersonalAgent(ctx, db.GetPersonalAgentParams{
			WorkspaceID: workspaceID,
			OwnerID:     ownerID,
		})
		if err == nil {
			return agent, nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("not found")
	}
	return db.Agent{}, lastErr
}

