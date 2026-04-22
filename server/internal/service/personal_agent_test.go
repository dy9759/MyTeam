package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// testDB opens a pool to the dev database. Expects DATABASE_URL env var.
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

func setEnvVars(t *testing.T, vals map[string]string) {
	t.Helper()
	for k, v := range vals {
		t.Setenv(k, v)
	}
}

// createTestWorkspace / createTestUser use exact generated CreateWorkspaceParams /
// CreateUserParams field shapes.
func createTestWorkspace(t *testing.T, q *db.Queries) pgtype.UUID {
	t.Helper()
	slugSuffix := strings.ReplaceAll(strings.ToLower(t.Name()), "/", "-")
	slugSuffix = strings.ReplaceAll(slugSuffix, "_", "-")
	// Append nanosecond timestamp so re-runs against a persistent dev DB don't collide.
	uniq := fmt.Sprintf("%d", time.Now().UnixNano())
	ws, err := q.CreateWorkspace(context.Background(), db.CreateWorkspaceParams{
		Name:        "Test Workspace " + t.Name(),
		Slug:        "test-" + slugSuffix + "-" + uniq,
		Description: pgtype.Text{},
		Context:     pgtype.Text{},
		IssuePrefix: "TST",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return ws.ID
}

func createTestUser(t *testing.T, q *db.Queries, email, name string) pgtype.UUID {
	t.Helper()
	// Dedupe email across runs against persistent dev DB.
	at := strings.Index(email, "@")
	if at > 0 {
		email = email[:at] + fmt.Sprintf("+%d", time.Now().UnixNano()) + email[at:]
	}
	u, err := q.CreateUser(context.Background(), db.CreateUserParams{
		Name:      name,
		Email:     email,
		AvatarUrl: pgtype.Text{},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

// loadAgentRuntimeCfg helper for tests: reads cloud_llm_config from runtime.metadata.
func loadAgentRuntimeCfg(t *testing.T, q *db.Queries, agent db.Agent) CloudLLMConfig {
	t.Helper()
	if !agent.RuntimeID.Valid {
		t.Fatal("agent missing runtime_id")
	}
	rt, err := q.GetAgentRuntime(context.Background(), agent.RuntimeID)
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	return cloudLLMConfigFromRuntime(rt)
}

func TestEnsurePersonalAgent_SnapshotsServerEnv(t *testing.T) {
	q := testDB(t)
	setEnvVars(t, map[string]string{
		"AGENT_KERNEL":        "openai_compat",
		"AGENT_LLM_BASE_URL":  "http://bailian.test",
		"AGENT_LLM_API_KEY":   "sk-test-snapshot",
		"AGENT_LLM_MODEL":     "qwen-plus",
		"AGENT_SYSTEM_PROMPT": "be terse",
	})

	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "alice+"+t.Name()+"@example.com", "Alice")

	agent, err := EnsurePersonalAgent(context.Background(), q, wsID, ownerID, "Alice")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if agent.Name != "Alice's Assistant" {
		t.Fatalf("unexpected name: %s", agent.Name)
	}

	// cloud_llm_config moved to runtime.metadata in account phase 2.
	cfg := loadAgentRuntimeCfg(t, q, agent)
	if cfg.Kernel != "openai_compat" ||
		cfg.BaseURL != "http://bailian.test" ||
		cfg.APIKey != "sk-test-snapshot" ||
		cfg.Model != "qwen-plus" ||
		cfg.SystemPrompt != "be terse" {
		t.Fatalf("config mismatch: %+v", cfg)
	}
}

func TestEnsurePersonalAgent_EmptyEnvAllowed(t *testing.T) {
	q := testDB(t)
	for _, v := range []string{"AGENT_KERNEL", "AGENT_LLM_BASE_URL", "AGENT_LLM_API_KEY", "AGENT_LLM_MODEL", "AGENT_SYSTEM_PROMPT"} {
		t.Setenv(v, "")
	}

	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "bob+"+t.Name()+"@example.com", "Bob")

	agent, err := EnsurePersonalAgent(context.Background(), q, wsID, ownerID, "Bob")
	if err != nil {
		t.Fatalf("ensure should not error on empty env: %v", err)
	}
	// cloud_llm_config moved to runtime.metadata in account phase 2.
	cfg := loadAgentRuntimeCfg(t, q, agent)
	if cfg.APIKey != "" {
		t.Fatalf("expected empty api key, got %q", cfg.APIKey)
	}
	if cfg.Kernel != "openai_compat" {
		t.Fatalf("expected default kernel, got %q", cfg.Kernel)
	}
}

func TestEnsurePersonalAgent_Idempotent(t *testing.T) {
	q := testDB(t)
	t.Setenv("AGENT_LLM_API_KEY", "sk-idem")

	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "carol+"+t.Name()+"@example.com", "Carol")

	a1, err := EnsurePersonalAgent(context.Background(), q, wsID, ownerID, "Carol")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	a2, err := EnsurePersonalAgent(context.Background(), q, wsID, ownerID, "Carol")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a1.ID.Bytes != a2.ID.Bytes {
		t.Fatal("second call should return same agent")
	}
}

func TestEnsurePersonalAgent_SwitchesKernelBasedOnEnv(t *testing.T) {
	q := testDB(t)
	setEnvVars(t, map[string]string{
		"AGENT_KERNEL":       "anthropic",
		"AGENT_LLM_BASE_URL": "https://api.anthropic.com",
		"AGENT_LLM_API_KEY":  "sk-ant-test",
		"AGENT_LLM_MODEL":    "claude-sonnet-4-5",
	})

	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "dave+"+t.Name()+"@example.com", "Dave")

	agent, err := EnsurePersonalAgent(context.Background(), q, wsID, ownerID, "Dave")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// cloud_llm_config moved to runtime.metadata in account phase 2.
	cfg := loadAgentRuntimeCfg(t, q, agent)
	if cfg.Kernel != "anthropic" {
		t.Fatalf("expected kernel='anthropic', got %q", cfg.Kernel)
	}
	if cfg.Model != "claude-sonnet-4-5" {
		t.Fatalf("expected anthropic model, got %q", cfg.Model)
	}
}
