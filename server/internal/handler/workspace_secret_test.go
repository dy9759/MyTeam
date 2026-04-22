package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/MyAIOSHub/MyTeam/server/pkg/crypto"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// init seeds MYTEAM_SECRET_KEY for the entire test binary so secret CRUD
// works without requiring the operator to set it. Tests that exercise the
// "missing key" path temporarily unset and restore it.
func init() {
	if os.Getenv(secretKeyEnvVar) != "" {
		return
	}
	key := make([]byte, crypto.KeySize)
	if _, err := rand.Read(key); err != nil {
		panic("failed to seed test secret key: " + err.Error())
	}
	_ = os.Setenv(secretKeyEnvVar, base64.StdEncoding.EncodeToString(key))
}

// withSecretURLParams attaches the workspace id and secret key to a chi
// route context so handlers can pick them up via chi.URLParam. Both params
// are placed in a single route context — chaining withURLParam twice would
// clobber the first one (each call installs a fresh chi.RouteContext).
func withSecretURLParams(req *http.Request, workspaceID, key string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", workspaceID)
	if key != "" {
		rctx.URLParams.Add("key", key)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// cleanupSecrets wipes any secrets left behind by a test. Called from each
// test's t.Cleanup so failures don't leak into the next case.
func cleanupSecrets(t *testing.T) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`DELETE FROM workspace_secret WHERE workspace_id = $1`, testWorkspaceID)
	if err != nil {
		t.Logf("cleanup workspace_secret: %v", err)
	}
}

func TestWorkspaceSecret_ListEmpty(t *testing.T) {
	cleanupSecrets(t)
	t.Cleanup(func() { cleanupSecrets(t) })

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/secrets", nil)
	req = withSecretURLParams(req, testWorkspaceID, "")
	testHandler.ListWorkspaceSecrets(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorkspaceSecrets: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var items []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&items); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty list, got %d items", len(items))
	}
}

func TestWorkspaceSecret_SetGetRoundTrip(t *testing.T) {
	cleanupSecrets(t)
	t.Cleanup(func() { cleanupSecrets(t) })

	const key = "OPENAI_API_KEY"
	const value = "sk-test-1234567890abcdef"

	// Set
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/workspaces/"+testWorkspaceID+"/secrets/"+key, map[string]string{"value": value})
	req = withSecretURLParams(req, testWorkspaceID, key)
	testHandler.SetWorkspaceSecret(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SetWorkspaceSecret: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Get
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/secrets/"+key, nil)
	req = withSecretURLParams(req, testWorkspaceID, key)
	testHandler.GetWorkspaceSecret(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetWorkspaceSecret: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got["key"] != key {
		t.Fatalf("expected key %q, got %q", key, got["key"])
	}
	if got["value"] != value {
		t.Fatalf("expected value %q, got %q", value, got["value"])
	}

	// List should now show the key, but never the value.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/secrets", nil)
	req = withSecretURLParams(req, testWorkspaceID, "")
	testHandler.ListWorkspaceSecrets(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorkspaceSecrets after Set: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var items []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&items); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0]["key"] != key {
		t.Fatalf("expected list key %q, got %v", key, items[0]["key"])
	}
	if _, hasValue := items[0]["value"]; hasValue {
		t.Fatal("LIST endpoint must not expose 'value'")
	}
}

func TestWorkspaceSecret_SetEmptyValue(t *testing.T) {
	cleanupSecrets(t)
	t.Cleanup(func() { cleanupSecrets(t) })

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/workspaces/"+testWorkspaceID+"/secrets/EMPTY", map[string]string{"value": ""})
	req = withSecretURLParams(req, testWorkspaceID, "EMPTY")
	testHandler.SetWorkspaceSecret(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty value, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWorkspaceSecret_GetMissing(t *testing.T) {
	cleanupSecrets(t)
	t.Cleanup(func() { cleanupSecrets(t) })

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/secrets/NOPE", nil)
	req = withSecretURLParams(req, testWorkspaceID, "NOPE")
	testHandler.GetWorkspaceSecret(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing secret, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWorkspaceSecret_Delete(t *testing.T) {
	cleanupSecrets(t)
	t.Cleanup(func() { cleanupSecrets(t) })

	const key = "TO_DELETE"

	// Set first
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/workspaces/"+testWorkspaceID+"/secrets/"+key, map[string]string{"value": "x"})
	req = withSecretURLParams(req, testWorkspaceID, key)
	testHandler.SetWorkspaceSecret(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Set before delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Delete
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/secrets/"+key, nil)
	req = withSecretURLParams(req, testWorkspaceID, key)
	testHandler.DeleteWorkspaceSecret(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("Delete: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Subsequent Get should 404
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/secrets/"+key, nil)
	req = withSecretURLParams(req, testWorkspaceID, key)
	testHandler.GetWorkspaceSecret(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("Get after delete: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWorkspaceSecret_AADPreventsTransplant verifies the additional-data
// binding: an attacker (or buggy migration) that swaps two ciphertexts in
// the workspace_secret table can no longer decrypt them, because the AAD
// bound at encrypt time ("<ws>/<key>") is checked at decrypt time.
func TestWorkspaceSecret_AADPreventsTransplant(t *testing.T) {
	cleanupSecrets(t)
	t.Cleanup(func() { cleanupSecrets(t) })

	// Set two distinct secrets.
	for _, kv := range []struct{ k, v string }{
		{"K1", "value-one"},
		{"K2", "value-two"},
	} {
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/workspaces/"+testWorkspaceID+"/secrets/"+kv.k, map[string]string{"value": kv.v})
		req = withSecretURLParams(req, testWorkspaceID, kv.k)
		testHandler.SetWorkspaceSecret(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("Set %s: expected 200, got %d: %s", kv.k, w.Code, w.Body.String())
		}
	}

	// Read both raw ciphertexts directly from the DB and swap them.
	ctx := context.Background()
	row1, err := testHandler.Queries.GetWorkspaceSecret(ctx, db.GetWorkspaceSecretParams{
		WorkspaceID: parseUUID(testWorkspaceID), Key: "K1",
	})
	if err != nil {
		t.Fatalf("read K1: %v", err)
	}
	row2, err := testHandler.Queries.GetWorkspaceSecret(ctx, db.GetWorkspaceSecretParams{
		WorkspaceID: parseUUID(testWorkspaceID), Key: "K2",
	})
	if err != nil {
		t.Fatalf("read K2: %v", err)
	}

	// Swap the encrypted blobs (simulating a row tamper).
	if _, err := testPool.Exec(ctx,
		`UPDATE workspace_secret SET value_encrypted = $1 WHERE workspace_id = $2 AND key = 'K1'`,
		row2.ValueEncrypted, parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("swap into K1: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE workspace_secret SET value_encrypted = $1 WHERE workspace_id = $2 AND key = 'K2'`,
		row1.ValueEncrypted, parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("swap into K2: %v", err)
	}

	// Both reads must now fail to decrypt because the AAD no longer matches.
	for _, k := range []string{"K1", "K2"} {
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/secrets/"+k, nil)
		req = withSecretURLParams(req, testWorkspaceID, k)
		testHandler.GetWorkspaceSecret(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("Get after swap %s: expected 500 (decrypt failed), got %d: %s",
				k, w.Code, w.Body.String())
		}
	}
}

// TestWorkspaceSecret_MissingMasterKey checks that all value-touching
// endpoints return 503 when MYTEAM_SECRET_KEY is unset. The list endpoint
// is excluded — listing keys is independent of the master key.
func TestWorkspaceSecret_MissingMasterKey(t *testing.T) {
	cleanupSecrets(t)
	t.Cleanup(func() { cleanupSecrets(t) })

	prev := os.Getenv(secretKeyEnvVar)
	if err := os.Unsetenv(secretKeyEnvVar); err != nil {
		t.Fatalf("unset env: %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv(secretKeyEnvVar, prev) })

	// Set must 503 because we cannot encrypt without the key.
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/workspaces/"+testWorkspaceID+"/secrets/X",
		map[string]string{"value": "y"})
	req = withSecretURLParams(req, testWorkspaceID, "X")
	testHandler.SetWorkspaceSecret(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("Set without key: expected 503, got %d: %s", w.Code, w.Body.String())
	}

	// Get must also 503.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/secrets/X", nil)
	req = withSecretURLParams(req, testWorkspaceID, "X")
	testHandler.GetWorkspaceSecret(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("Get without key: expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWorkspaceSecret_MemberForbidden verifies that a workspace member
// (non-admin) cannot read or write secrets. A second user is added to the
// fixture workspace as a "member" and used as the caller.
func TestWorkspaceSecret_MemberForbidden(t *testing.T) {
	cleanupSecrets(t)
	t.Cleanup(func() { cleanupSecrets(t) })

	ctx := context.Background()

	// Create a second user with member role.
	const memberEmail = "secret-member@myteam.ai"
	var memberID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Secret Member", memberEmail).Scan(&memberID); err != nil {
		t.Fatalf("create member user: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, memberID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx,
			`DELETE FROM member WHERE user_id = $1 AND workspace_id = $2`,
			memberID, testWorkspaceID)
		_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, memberID)
	})

	// Build a request as the member (override X-User-ID).
	asMember := func(req *http.Request) *http.Request {
		req.Header.Set("X-User-ID", memberID)
		return req
	}

	// LIST must 403.
	w := httptest.NewRecorder()
	req := asMember(newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/secrets", nil))
	req = withSecretURLParams(req, testWorkspaceID, "")
	testHandler.ListWorkspaceSecrets(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Member List: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// PUT must 403.
	w = httptest.NewRecorder()
	req = asMember(newRequest("PUT", "/api/workspaces/"+testWorkspaceID+"/secrets/X",
		map[string]string{"value": "y"}))
	req = withSecretURLParams(req, testWorkspaceID, "X")
	testHandler.SetWorkspaceSecret(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Member Set: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// GET must 403.
	w = httptest.NewRecorder()
	req = asMember(newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/secrets/X", nil))
	req = withSecretURLParams(req, testWorkspaceID, "X")
	testHandler.GetWorkspaceSecret(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Member Get: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// DELETE must 403.
	w = httptest.NewRecorder()
	req = asMember(newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/secrets/X", nil))
	req = withSecretURLParams(req, testWorkspaceID, "X")
	testHandler.DeleteWorkspaceSecret(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("Member Delete: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
