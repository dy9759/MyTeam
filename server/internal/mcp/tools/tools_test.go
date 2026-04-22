// Package tools tests: tools_test.go — shared test fixtures for the MCP
// tool happy-path tests. Each tool's exec test consumes setupTaskEnv and
// constructs a mcptool.Context that mimics what the dispatcher would build.
//
// All tests skip when DATABASE_URL is unset, matching the convention used
// throughout server/internal/service/*_test.go.
package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// testDB opens a pool to DATABASE_URL. Mirrors the helper of the same
// name in service/personal_agent_test.go.
func testDB(t *testing.T) *db.Queries {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("db pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return db.New(pool)
}

// openTestPool returns a raw pool for inserts the sqlc layer can't perform
// (project rows whose CreatedBy column is unset by the generated query).
func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// toolTestEnv bundles the IDs every tool test needs: a workspace, the
// task owner (member), an agent, and a task whose primary_assignee_id is
// the agent. The task is in run R of project P, which lets request_approval
// resolve a recipient via task → run → project.creator_owner_id.
type toolTestEnv struct {
	WorkspaceID pgtype.UUID
	OwnerID     pgtype.UUID
	AgentID     pgtype.UUID
	RuntimeID   pgtype.UUID
	PlanID      pgtype.UUID
	ProjectID   pgtype.UUID
	RunID       pgtype.UUID
	TaskID      pgtype.UUID
}

// uniqSuffix returns a deterministic-but-unique suffix per test that
// satisfies the workspace.slug uniqueness constraint when the suite runs
// twice against a persistent dev DB.
func uniqSuffix(t *testing.T) string {
	t.Helper()
	slug := strings.ReplaceAll(strings.ToLower(t.Name()), "/", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	return slug + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
}

// setupTaskEnv inserts the rows needed for a happy-path tool test:
// workspace + member + plan + project + run + idle agent on healthy
// runtime + task assigned (primary) to that agent.
func setupTaskEnv(t *testing.T, q *db.Queries) toolTestEnv {
	t.Helper()
	ctx := context.Background()
	suffix := uniqSuffix(t)

	ws, err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        "MCP Tools WS " + t.Name(),
		Slug:        "mcp-" + suffix,
		Description: pgtype.Text{},
		Context:     pgtype.Text{},
		IssuePrefix: "MCP",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	user, err := q.CreateUser(ctx, db.CreateUserParams{
		Name:      "MCP Tester",
		Email:     "mcp+" + suffix + "@example.com",
		AvatarUrl: pgtype.Text{},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	plan, err := q.CreatePlan(ctx, db.CreatePlanParams{
		WorkspaceID:    ws.ID,
		Title:          "Plan " + t.Name(),
		Description:    pgtype.Text{String: "test plan", Valid: true},
		ExpectedOutput: pgtype.Text{String: "artifacts", Valid: true},
		CreatedBy:      user.ID,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	pool := openTestPool(t)
	var projectID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, status, created_by, schedule_type, source_conversations, creator_owner_id)
		VALUES ($1, $2, '', 'running', $3, 'one_time', '[]'::jsonb, $3)
		RETURNING id
	`, ws.ID, "Project "+t.Name(), user.ID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	run, err := q.CreateProjectRun(ctx, db.CreateProjectRunParams{
		PlanID:    plan.ID,
		ProjectID: projectID,
		Status:    "running",
	})
	if err != nil {
		t.Fatalf("create project_run: %v", err)
	}

	runtime, err := q.EnsureCloudRuntime(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ensure cloud runtime: %v", err)
	}

	agent, err := q.CreatePersonalAgent(ctx, db.CreatePersonalAgentParams{
		WorkspaceID: ws.ID,
		Name:        "MCP Agent " + t.Name(),
		Description: "agent for mcp tool test",
		RuntimeID:   runtime.ID,
		OwnerID:     user.ID,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if _, err := q.UpdateAgentStatus(ctx, db.UpdateAgentStatusParams{
		ID:     agent.ID,
		Status: "idle",
	}); err != nil {
		t.Fatalf("set agent idle: %v", err)
	}

	task, err := q.CreateTask(ctx, db.CreateTaskParams{
		PlanID:            plan.ID,
		RunID:             run.ID,
		WorkspaceID:       ws.ID,
		Title:             "Task " + t.Name(),
		Description:       pgtype.Text{String: "do work", Valid: true},
		StepOrder:         pgtype.Int4{Int32: 0, Valid: true},
		PrimaryAssigneeID: agent.ID,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	return toolTestEnv{
		WorkspaceID: ws.ID,
		OwnerID:     user.ID,
		AgentID:     agent.ID,
		RuntimeID:   runtime.ID,
		PlanID:      plan.ID,
		ProjectID:   projectID,
		RunID:       run.ID,
		TaskID:      task.ID,
	}
}

// pgxToUUID converts a pgtype.UUID to uuid.UUID for use in tool args.
func pgxToUUID(t *testing.T, p pgtype.UUID) uuid.UUID {
	t.Helper()
	if !p.Valid {
		t.Fatalf("pgxToUUID: not valid")
	}
	return uuid.UUID(p.Bytes)
}
