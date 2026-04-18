package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type issueToolsFixture struct {
	pool          *pgxpool.Pool
	queries       *db.Queries
	comments      *service.CommentService
	userID        uuid.UUID
	workspaceID   uuid.UUID
	issueID       uuid.UUID
	userEmail     string
	workspaceSlug string
}

// fixtureIDs derives a workspace slug and user email from t.Name(). Each test
// gets unique values so tests using t.Parallel() don't collide on the workspace
// slug UNIQUE constraint or on the cleanup DELETE WHERE slug/email statements.
func fixtureIDs(t *testing.T) (slug, email string) {
	t.Helper()
	// t.Name() may contain '/', spaces, or other characters from subtests.
	// Normalize to a slug-safe form.
	safe := strings.ToLower(t.Name())
	safe = strings.ReplaceAll(safe, "/", "-")
	safe = strings.ReplaceAll(safe, " ", "-")
	slug = "mcp-it-" + safe
	email = "mcp-it-" + safe + "@multica.ai"
	return slug, email
}

func setupIssueToolsFixture(t *testing.T) *issueToolsFixture {
	t.Helper()
	ctx := context.Background()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("could not connect to database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}

	slug, email := fixtureIDs(t)

	if err := cleanupIssueToolsFixture(ctx, pool, slug, email); err != nil {
		pool.Close()
		t.Fatalf("cleanup pre-fixture: %v", err)
	}

	var userIDStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "MCP Issue Tools Test", email).Scan(&userIDStr); err != nil {
		pool.Close()
		t.Fatalf("insert user: %v", err)
	}

	var workspaceIDStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "MCP Issue Tools", slug, "MCP issue-tools test workspace", "MIT").Scan(&workspaceIDStr); err != nil {
		pool.Close()
		t.Fatalf("insert workspace: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceIDStr, userIDStr); err != nil {
		pool.Close()
		t.Fatalf("insert member: %v", err)
	}

	var issueIDStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, 'mcp tools fixture issue', 'todo', 'medium', 'member', $2, 1, 0)
		RETURNING id
	`, workspaceIDStr, userIDStr).Scan(&issueIDStr); err != nil {
		pool.Close()
		t.Fatalf("insert issue: %v", err)
	}

	t.Cleanup(func() {
		if err := cleanupIssueToolsFixture(context.Background(), pool, slug, email); err != nil {
			t.Logf("cleanup post-test: %v", err)
		}
		pool.Close()
	})

	queries := db.New(pool)
	bus := events.New()
	hub := realtime.NewHub()
	tasks := service.NewTaskService(queries, hub, bus)
	comments := service.NewCommentService(queries, bus, tasks)

	return &issueToolsFixture{
		pool:          pool,
		queries:       queries,
		comments:      comments,
		userID:        uuid.MustParse(userIDStr),
		workspaceID:   uuid.MustParse(workspaceIDStr),
		issueID:       uuid.MustParse(issueIDStr),
		userEmail:     email,
		workspaceSlug: slug,
	}
}

func cleanupIssueToolsFixture(ctx context.Context, pool *pgxpool.Pool, slug, email string) error {
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug); err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

func toolCtx(f *issueToolsFixture) mcptool.Context {
	return mcptool.Context{
		WorkspaceID: f.workspaceID,
		UserID:      f.userID,
		RuntimeMode: mcptool.RuntimeCloud,
		Comments:    f.comments,
	}
}

func TestGetIssueHappyPath(t *testing.T) {
	t.Parallel()
	f := setupIssueToolsFixture(t)
	ctx := context.Background()

	res, err := GetIssue{}.Exec(ctx, f.queries, toolCtx(f), map[string]any{
		"issue_id": f.issueID.String(),
	})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	data, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected Data to be a map, got %T", res.Data)
	}
	if data["id"] != f.issueID.String() {
		t.Errorf("issue id = %v, want %v", data["id"], f.issueID)
	}
	if data["status"] != "todo" {
		t.Errorf("status = %v, want todo", data["status"])
	}
	if data["title"] != "mcp tools fixture issue" {
		t.Errorf("title = %v, want fixture title", data["title"])
	}
}

func TestGetIssueWrongWorkspaceReturnsError(t *testing.T) {
	t.Parallel()
	f := setupIssueToolsFixture(t)
	ctx := context.Background()

	wrongCtx := toolCtx(f)
	wrongCtx.WorkspaceID = uuid.New() // some other workspace

	_, err := GetIssue{}.Exec(ctx, f.queries, wrongCtx, map[string]any{
		"issue_id": f.issueID.String(),
	})
	if err == nil {
		t.Fatal("expected error when workspace_id does not match issue's workspace")
	}
}

func TestUpdateIssueStatusHappyPath(t *testing.T) {
	t.Parallel()
	f := setupIssueToolsFixture(t)
	ctx := context.Background()

	res, err := UpdateIssueStatus{}.Exec(ctx, f.queries, toolCtx(f), map[string]any{
		"issue_id": f.issueID.String(),
		"status":   "in_progress",
	})
	if err != nil {
		t.Fatalf("UpdateIssueStatus: %v", err)
	}
	data, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected Data to be a map, got %T", res.Data)
	}
	if data["status"] != "in_progress" {
		t.Errorf("status = %v, want in_progress", data["status"])
	}
}

// TestUpdateIssueStatusInvalidStatusRejected covers the case where the caller
// passes a status string that is not in the issue.status CHECK constraint
// set (see migrations/001_init.up.sql). The DB must reject the write so we
// don't silently store an unknown status.
func TestUpdateIssueStatusInvalidStatusRejected(t *testing.T) {
	t.Parallel()
	f := setupIssueToolsFixture(t)
	ctx := context.Background()

	_, err := UpdateIssueStatus{}.Exec(ctx, f.queries, toolCtx(f), map[string]any{
		"issue_id": f.issueID.String(),
		"status":   "not_a_real_status",
	})
	if err == nil {
		t.Fatal("expected error when status is not in the CHECK constraint set")
	}
}

func TestCreateAndListComments(t *testing.T) {
	t.Parallel()
	f := setupIssueToolsFixture(t)
	ctx := context.Background()

	// Create a comment as a member.
	created, err := CreateComment{}.Exec(ctx, f.queries, toolCtx(f), map[string]any{
		"issue_id": f.issueID.String(),
		"body":     "hello from mcp",
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	createdData, ok := created.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected Data to be a map, got %T", created.Data)
	}
	if createdData["content"] != "hello from mcp" {
		t.Errorf("content = %v, want 'hello from mcp'", createdData["content"])
	}
	if createdData["author_type"] != "member" {
		t.Errorf("author_type = %v, want member", createdData["author_type"])
	}
	if createdData["author_id"] != f.userID.String() {
		t.Errorf("author_id = %v, want user", createdData["author_id"])
	}

	// List comments — should contain the one we just created.
	listed, err := ListIssueComments{}.Exec(ctx, f.queries, toolCtx(f), map[string]any{
		"issue_id": f.issueID.String(),
	})
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	listedData, ok := listed.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected Data to be a map, got %T", listed.Data)
	}
	if total, _ := listedData["total"].(int); total != 1 {
		t.Errorf("total = %v, want 1", listedData["total"])
	}
	comments, ok := listedData["comments"].([]map[string]any)
	if !ok {
		t.Fatalf("expected comments to be []map[string]any, got %T", listedData["comments"])
	}
	if len(comments) != 1 || comments[0]["content"] != "hello from mcp" {
		t.Errorf("unexpected comment list: %#v", comments)
	}
}

// TestCreateCommentEmptyBodyRejected covers the validation guard in
// CreateComment.Exec for empty body strings — the call must fail before
// touching the database.
func TestCreateCommentEmptyBodyRejected(t *testing.T) {
	t.Parallel()
	f := setupIssueToolsFixture(t)
	ctx := context.Background()

	_, err := CreateComment{}.Exec(ctx, f.queries, toolCtx(f), map[string]any{
		"issue_id": f.issueID.String(),
		"body":     "",
	})
	if err == nil {
		t.Fatal("expected error when body is empty")
	}
}

// TestListIssueCommentsOffsetOutOfRange exercises the paging guard in
// paginateComments: when offset exceeds the total comment count, the tool
// should return an empty comments slice (not panic, not negative-index).
func TestListIssueCommentsOffsetOutOfRange(t *testing.T) {
	t.Parallel()
	f := setupIssueToolsFixture(t)
	ctx := context.Background()

	// Seed exactly one comment.
	_, err := CreateComment{}.Exec(ctx, f.queries, toolCtx(f), map[string]any{
		"issue_id": f.issueID.String(),
		"body":     "only comment",
	})
	if err != nil {
		t.Fatalf("seed CreateComment: %v", err)
	}

	listed, err := ListIssueComments{}.Exec(ctx, f.queries, toolCtx(f), map[string]any{
		"issue_id": f.issueID.String(),
		"offset":   100, // far beyond the single seeded comment
	})
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	data, ok := listed.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected Data to be a map, got %T", listed.Data)
	}
	comments, ok := data["comments"].([]map[string]any)
	if !ok {
		t.Fatalf("expected comments to be []map[string]any, got %T", data["comments"])
	}
	if len(comments) != 0 {
		t.Errorf("expected empty comments slice for OOB offset, got %d", len(comments))
	}
	// Total should still reflect the underlying row count, not the sliced view.
	if total, _ := data["total"].(int); total != 1 {
		t.Errorf("total = %v, want 1", data["total"])
	}
}

func TestCreateCommentAsAgent(t *testing.T) {
	t.Parallel()
	f := setupIssueToolsFixture(t)
	ctx := context.Background()

	// Insert an agent so the agent UUID is real for the FK.
	var runtimeIDStr string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, mode, provider, status, device_info, metadata, last_heartbeat_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, now())
		RETURNING id
	`, f.workspaceID.String(), "MCP Test Runtime", "mcp_test_runtime", "MCP test runtime").Scan(&runtimeIDStr); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}

	var agentIDStr string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			agent_type, owner_type
		)
		VALUES ($1, $2, '', $3, 'workspace', 1, $4, 'personal_agent', 'user')
		RETURNING id
	`, f.workspaceID.String(), "MCP Test Agent", runtimeIDStr, f.userID.String()).Scan(&agentIDStr); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	tCtx := toolCtx(f)
	tCtx.AgentID = uuid.MustParse(agentIDStr)

	res, err := CreateComment{}.Exec(ctx, f.queries, tCtx, map[string]any{
		"issue_id": f.issueID.String(),
		"body":     "hello from agent",
	})
	if err != nil {
		t.Fatalf("CreateComment as agent: %v", err)
	}
	data, ok := res.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected Data to be a map, got %T", res.Data)
	}
	if data["author_type"] != "agent" {
		t.Errorf("author_type = %v, want agent", data["author_type"])
	}
	if data["author_id"] != agentIDStr {
		t.Errorf("author_id = %v, want agent id %v", data["author_id"], agentIDStr)
	}
}
