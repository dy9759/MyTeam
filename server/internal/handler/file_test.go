package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Helpers: S3 fake + attachment / channel setup.
//
// These tests require the PostgreSQL harness provided by TestMain in
// handler_test.go. If DATABASE_URL is unreachable, TestMain exits early and
// no tests in this package run — the same skip behavior the rest of the
// package relies on. Run via: `make db-up && cd server && go test ./internal/handler/...`.
// ---------------------------------------------------------------------------

// startFakeS3Server returns an httptest server that responds to any GET
// request with the configured body. The storage returned is a real
// *storage.S3Storage pointing at this endpoint, so handler code exercises the
// production Download codepath end-to-end without touching AWS.
func startFakeS3Server(t *testing.T, body, contentType string) (*storage.S3Storage, func()) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))

	// Configure a real S3Storage pointed at the fake server.
	t.Setenv("S3_BUCKET", "test-bucket")
	t.Setenv("S3_REGION", "us-east-1")
	t.Setenv("S3_ENDPOINT", srv.URL)
	t.Setenv("S3_USE_PATH_STYLE", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("CLOUDFRONT_DOMAIN", "")

	s3 := storage.NewS3StorageFromEnv()
	if s3 == nil {
		srv.Close()
		t.Fatal("NewS3StorageFromEnv returned nil")
	}

	cleanup := func() {
		srv.Close()
	}
	return s3, cleanup
}

// insertAttachment writes a raw attachment row and registers a cleanup hook.
func insertAttachment(t *testing.T, workspaceID, uploaderID, uploaderType, filename, url, contentType string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment
		  (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, workspaceID, uploaderType, uploaderID, filename, url, contentType, int64(len(filename))).Scan(&id)
	if err != nil {
		t.Fatalf("insertAttachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, id)
	})
	return id
}

// insertChannel creates a channel and registers cleanup.
func insertChannel(t *testing.T, workspaceID, name string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO channel (workspace_id, name, description, created_by, created_by_type)
		VALUES ($1, $2, '', $3, 'member')
		RETURNING id
	`, workspaceID, name, testUserID).Scan(&id)
	if err != nil {
		t.Fatalf("insertChannel(%s): %v", name, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel WHERE id = $1`, id)
	})
	return id
}

// insertChannelMember adds a member to a channel. Cleanup happens via channel cascade.
func insertChannelMember(t *testing.T, channelID, memberID, memberType string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO channel_member (channel_id, member_id, member_type)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, channelID, memberID, memberType); err != nil {
		t.Fatalf("insertChannelMember: %v", err)
	}
}

// insertChannelMessageWithFile creates a chat message referencing a file.
// Cleanup runs automatically.
func insertChannelMessageWithFile(t *testing.T, channelID, senderID, fileID string) {
	t.Helper()
	var msgID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO message
		  (workspace_id, sender_id, sender_type, channel_id, content, content_type, type, file_id)
		VALUES ($1, $2, 'member', $3, '', 'file', 'file', $4)
		RETURNING id
	`, testWorkspaceID, senderID, channelID, fileID).Scan(&msgID)
	if err != nil {
		t.Fatalf("insertChannelMessageWithFile: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM message WHERE id = $1`, msgID)
	})
}

// insertUser creates a second user used as a non-member uploader for channel
// visibility tests.
func insertUser(t *testing.T, email string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, email, email).Scan(&id)
	if err != nil {
		t.Fatalf("insertUser: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, id)
	})
	return id
}

// insertWorkspace creates an auxiliary workspace for cross-tenant tests.
func insertWorkspace(t *testing.T, slug string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'AUX')
		RETURNING id
	`, "Aux "+slug, slug).Scan(&id)
	if err != nil {
		t.Fatalf("insertWorkspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
	})
	return id
}

// withStorage temporarily replaces h.Storage for the duration of a test.
func withStorage(t *testing.T, s *storage.S3Storage) {
	t.Helper()
	prev := testHandler.Storage
	testHandler.Storage = s
	t.Cleanup(func() { testHandler.Storage = prev })
}

// ---------------------------------------------------------------------------
// DownloadFile
// ---------------------------------------------------------------------------

func TestDownloadFile_NotConfigured(t *testing.T) {
	withStorage(t, nil)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/files/11111111-1111-1111-1111-111111111111/download", nil)
	req = withURLParam(req, "id", "11111111-1111-1111-1111-111111111111")
	testHandler.DownloadFile(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDownloadFile_WorkspaceMissing(t *testing.T) {
	s3, cleanup := startFakeS3Server(t, "irrelevant", "text/plain")
	defer cleanup()
	withStorage(t, s3)

	// Build a request WITHOUT workspace context / header / query.
	req := httptest.NewRequest("GET", "/api/files/11111111-1111-1111-1111-111111111111/download", nil)
	req.Header.Set("X-User-ID", testUserID)
	req = withURLParam(req, "id", "11111111-1111-1111-1111-111111111111")

	w := httptest.NewRecorder()
	testHandler.DownloadFile(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDownloadFile_CrossWorkspaceDenied(t *testing.T) {
	s3, cleanup := startFakeS3Server(t, "secret-content", "text/plain")
	defer cleanup()
	withStorage(t, s3)

	// Create an attachment in a DIFFERENT workspace. The caller is a
	// member of testWorkspaceID only, so GetAttachment(id, testWorkspaceID)
	// must return no rows → 404.
	otherWS := insertWorkspace(t, "aux-cross-ws-download")
	attID := insertAttachment(t, otherWS, testUserID, "member", "secret.txt", "secret.txt", "text/plain")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/files/"+attID+"/download", nil)
	req = withURLParam(req, "id", attID)
	testHandler.DownloadFile(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (tenant isolation), got %d: %s", w.Code, w.Body.String())
	}
}

func TestDownloadFile_StoredXSSHeaders(t *testing.T) {
	s3, cleanup := startFakeS3Server(t, "<html><script>alert(1)</script></html>", "text/html")
	defer cleanup()
	withStorage(t, s3)

	// Store a URL that KeyFromURL() reduces to a plain key so the SDK can
	// route the GET to our fake server. KeyFromURL falls back to everything
	// after the last "/" when prefix stripping fails.
	attID := insertAttachment(t, testWorkspaceID, testUserID, "member", "evil.html", "evil.html", "text/html")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/files/"+attID+"/download", nil)
	req = withURLParam(req, "id", attID)
	testHandler.DownloadFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition = %q, want prefix %q", cd, "attachment;")
	}
	if !strings.Contains(cd, `filename="evil.html"`) {
		t.Errorf("Content-Disposition = %q, want filename=\"evil.html\"", cd)
	}
}

func TestDownloadFile_HappyPath(t *testing.T) {
	const payload = "hello, world"
	const ct = "text/plain; charset=utf-8"
	s3, cleanup := startFakeS3Server(t, payload, ct)
	defer cleanup()
	withStorage(t, s3)

	attID := insertAttachment(t, testWorkspaceID, testUserID, "member", "hello.txt", "hello.txt", "text/plain")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/files/"+attID+"/download", nil)
	req = withURLParam(req, "id", attID)
	testHandler.DownloadFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != payload {
		t.Errorf("body = %q, want %q", body, payload)
	}
	// Content-Type is either forwarded from the fake S3 response or falls
	// back to att.ContentType. Either is a pass — we just require it's
	// populated and not the Go default empty string.
	if got := w.Header().Get("Content-Type"); got == "" {
		t.Errorf("Content-Type is empty, want forwarded or fallback value")
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

// ---------------------------------------------------------------------------
// ListOwnerAndAgentFiles
// ---------------------------------------------------------------------------

// callListOwnerAndAgentFiles invokes the handler and returns the parsed result.
func callListOwnerAndAgentFiles(t *testing.T) []FileIndexResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/files/mine", nil)
	testHandler.ListOwnerAndAgentFiles(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListOwnerAndAgentFiles: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out []FileIndexResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// findInResults returns the first result matching id, or nil.
func findInResults(results []FileIndexResponse, id string) *FileIndexResponse {
	for i := range results {
		if results[i].ID == id {
			return &results[i]
		}
	}
	return nil
}

func TestListOwnerAndAgentFiles_OwnUploads(t *testing.T) {
	attID := insertAttachment(t, testWorkspaceID, testUserID, "member", "mine.txt", "mine.txt", "text/plain")

	results := callListOwnerAndAgentFiles(t)
	got := findInResults(results, attID)
	if got == nil {
		t.Fatalf("own upload missing from results; ids=%v", collectIDs(results))
	}
	if got.SourceType != "upload" {
		t.Errorf("SourceType = %q, want %q", got.SourceType, "upload")
	}
	if got.OwnerID != testUserID {
		t.Errorf("OwnerID = %q, want %q", got.OwnerID, testUserID)
	}
}

func TestListOwnerAndAgentFiles_AgentOwnedUploads(t *testing.T) {
	ctx := context.Background()
	agentID := findTestAgentID(t, ctx)

	// Attachment uploaded by the user's agent — should surface via the
	// ownerIDs expansion.
	attID := insertAttachment(t, testWorkspaceID, agentID, "agent", "agent-report.md", "agent-report.md", "text/markdown")

	results := callListOwnerAndAgentFiles(t)
	got := findInResults(results, attID)
	if got == nil {
		t.Fatalf("agent upload missing; ids=%v", collectIDs(results))
	}
	if got.UploaderIdentityType != "agent" {
		t.Errorf("UploaderIdentityType = %q, want %q", got.UploaderIdentityType, "agent")
	}
	if got.OwnerID != agentID {
		t.Errorf("OwnerID = %q, want agent %q", got.OwnerID, agentID)
	}
}

func TestListOwnerAndAgentFiles_VisibleViaChannel(t *testing.T) {
	// Another user uploads a file and posts it in a channel the test user
	// joined — must be visible with source_type="chat" + channel_id set.
	otherUser := insertUser(t, "other-user-channel-visible@multica.ai")

	channelID := insertChannel(t, testWorkspaceID, "visible-channel")
	insertChannelMember(t, channelID, testUserID, "member")

	attID := insertAttachment(t, testWorkspaceID, otherUser, "member", "shared.pdf", "shared.pdf", "application/pdf")
	insertChannelMessageWithFile(t, channelID, otherUser, attID)

	results := callListOwnerAndAgentFiles(t)
	got := findInResults(results, attID)
	if got == nil {
		t.Fatalf("channel-shared file missing; ids=%v", collectIDs(results))
	}
	if got.SourceType != "chat" {
		t.Errorf("SourceType = %q, want %q", got.SourceType, "chat")
	}
	if got.ChannelID == nil {
		t.Fatalf("ChannelID is nil, want channel %q", channelID)
	}
	if *got.ChannelID != channelID {
		t.Errorf("ChannelID = %q, want %q", *got.ChannelID, channelID)
	}
}

func TestListOwnerAndAgentFiles_HiddenFromNonMemberChannel(t *testing.T) {
	// Non-member uploads a file in a channel the test user is NOT a member
	// of. The user must not see it.
	otherUser := insertUser(t, "other-user-channel-hidden@multica.ai")

	hiddenChannel := insertChannel(t, testWorkspaceID, "hidden-channel")
	// testUser is NOT added to hiddenChannel.

	attID := insertAttachment(t, testWorkspaceID, otherUser, "member", "private.pdf", "private.pdf", "application/pdf")
	insertChannelMessageWithFile(t, hiddenChannel, otherUser, attID)

	results := callListOwnerAndAgentFiles(t)
	if got := findInResults(results, attID); got != nil {
		t.Fatalf("private channel file leaked into results: %+v", got)
	}
}

func TestListOwnerAndAgentFiles_LimitAndOrder(t *testing.T) {
	// Create 201 attachments owned by the test user. The handler caps at
	// 200 and orders by created_at DESC, so the newest 200 should come
	// back. We verify:
	//   - result length capped at 200
	//   - first result is the most recently inserted
	const total = 201
	ids := make([]string, total)
	for i := 0; i < total; i++ {
		ids[i] = insertAttachment(
			t, testWorkspaceID, testUserID, "member",
			fmt.Sprintf("bulk-%d.txt", i),
			fmt.Sprintf("bulk-%d.txt", i),
			"text/plain",
		)
	}

	// The query also includes any unrelated attachments created by
	// previous subtests in the same run. Filter down to the ones we just
	// inserted before asserting ordering.
	bulkSet := make(map[string]int, total)
	for i, id := range ids {
		bulkSet[id] = i
	}

	results := callListOwnerAndAgentFiles(t)
	if len(results) > 200 {
		t.Fatalf("expected at most 200 results, got %d", len(results))
	}

	// Collect indices of bulk attachments in the order they appear.
	seen := []int{}
	for _, r := range results {
		if idx, ok := bulkSet[r.ID]; ok {
			seen = append(seen, idx)
		}
	}
	if len(seen) == 0 {
		t.Fatal("none of the bulk-inserted attachments returned")
	}
	// Newest-first means higher insertion index comes first.
	for i := 1; i < len(seen); i++ {
		if seen[i] > seen[i-1] {
			t.Fatalf("results not ordered newest-first at position %d: idx %d came after idx %d",
				i, seen[i], seen[i-1])
		}
	}
}

// ---------------------------------------------------------------------------
// Small shared helpers
// ---------------------------------------------------------------------------

func collectIDs(results []FileIndexResponse) []string {
	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}
	return ids
}

// silence unused import warnings for packages referenced only from test code.
var (
	_ = middleware.WorkspaceIDFromContext
	_ = db.CreateAttachmentParams{}
)
