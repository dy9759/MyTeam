# Personal Agent + Claude Agent SDK Kernel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Auto-provision a personal agent on desktop open, and route `@-mention` replies through Claude Agent SDK (Python subprocess) with Bailian / OpenAI-compatible kernel configured via server env vars.

**Architecture:** New `server/agent_runner/` package: a `Runner` Go type spawns a Python child (`claude_reply.py`) that uses `claude-agent-sdk`, piping the prompt via stdin and reading NDJSON events from stdout. `AutoReplyService` is refactored to accept a `AgentRunner` interface (allowing test mocks) and reads LLM config from `agent.cloud_llm_config` (where `EnsurePersonalAgent` now snapshots server env vars). Desktop triggers agent creation via a new `getOrCreateSystemAgent` API call in the bootstrap path.

**Tech Stack:** Go 1.26 (exec.CommandContext, bufio.Scanner), Python 3.10+ with `claude-agent-sdk>=0.1.0`, bash fake runner for Go tests, TypeScript (desktop client), pnpm workspaces.

**Spec:** [docs/superpowers/specs/2026-04-15-personal-agent-claude-sdk-design.md](../specs/2026-04-15-personal-agent-claude-sdk-design.md)

**Working directory:** All commands assume a dedicated worktree will be created for implementation (branched from `main`). Controller will create `.claude/worktrees/personal-agent` on branch `feat/personal-agent` before Task 1.

---

## File structure

### Create

```
server/agent_runner/
├── runner.go
├── runner_test.go
├── claude_reply.py
├── requirements.txt
└── testdata/
    └── fake_runner.sh

server/internal/service/
├── auto_reply_test.go        (new — no existing test file)
└── personal_agent_test.go    (new — no existing test file)
```

### Modify

- `server/internal/service/personal_agent.go` — reshape `CloudLLMConfig`, read new env vars.
- `server/internal/service/auto_reply.go` — rewrite `replyAsMentionedAgent` to use `AgentRunner`; add `postSystemNotification` helper.
- `server/cmd/server/main.go` (or wherever `AutoReplyService` is constructed) — inject `agent_runner.NewRunner()`.
- `packages/client-core/src/desktop-api-client.ts` — add `getOrCreateSystemAgent`.
- `apps/desktop/src/lib/desktop-client.ts` — call it in `bootstrapDesktopApp`.
- `.env.example` — add 4 new env vars.
- `Makefile` — add `setup-agent-runner` target.
- `README.md` — Python prereqs note.

### Existing dev data (note for operators, no task)

`EnsurePersonalAgent` stores `cloud_llm_config` with a new JSON shape. Old rows in a dev DB with the old `{endpoint,api_key,model}` shape will make AutoReplyService's parser see an empty `base_url` and fail with `"Agent is not configured: missing API key."` until the agent is recreated. Fix via `DELETE FROM agent WHERE owner_id = '<owner>'` then restart desktop (agent auto-recreates).

---

## Task 1: Add Python runtime files

**Files:**
- Create: `server/agent_runner/claude_reply.py`
- Create: `server/agent_runner/requirements.txt`
- Create: `server/agent_runner/testdata/fake_runner.sh`

No tests in this task — the Python script is driven by other tasks' test suites and manual acceptance. The bash script is scaffolding for Task 2's tests.

- [ ] **Step 1: Create `requirements.txt`**

```
claude-agent-sdk>=0.1.0
```

- [ ] **Step 2: Create `claude_reply.py`**

```python
"""
MyTeam personal agent reply runner.

Invocation: python3 claude_reply.py
  stdin  = user prompt (UTF-8 text)
  env    = OPENAI_BASE_URL / OPENAI_API_KEY / OPENAI_MODEL
           (or ANTHROPIC_BASE_URL / ANTHROPIC_API_KEY / ANTHROPIC_MODEL)
           AGENT_SYSTEM_PROMPT (optional)
  stdout = NDJSON events; final line = {"type":"done","text":"..."}
           intermediate {"type":"status"} / {"type":"error"} allowed
  exit 0 = success; non-zero = failure
"""
import asyncio
import json
import os
import sys


def emit(event: dict) -> None:
    print(json.dumps(event, ensure_ascii=False), flush=True)


async def main() -> int:
    try:
        from claude_agent_sdk import ClaudeAgentOptions, ClaudeSDKClient
    except ImportError:
        emit({"type": "error", "message": "claude-agent-sdk not installed"})
        return 2

    prompt = sys.stdin.read().strip()
    if not prompt:
        emit({"type": "error", "message": "empty prompt"})
        return 3

    system_prompt = os.environ.get(
        "AGENT_SYSTEM_PROMPT",
        "You are a helpful AI assistant on the MyTeam platform. Reply concisely.",
    )
    model = (
        os.environ.get("ANTHROPIC_MODEL")
        or os.environ.get("OPENAI_MODEL")
        or "sonnet"
    )

    options = ClaudeAgentOptions(
        system_prompt=system_prompt,
        max_turns=1,
        model=model,
    )

    try:
        reply = ""
        async with ClaudeSDKClient(options=options) as client:
            await client.query(prompt=prompt)
            async for message in client.receive_response():
                if hasattr(message, "content"):
                    for block in message.content:
                        if hasattr(block, "text"):
                            reply += block.text
        emit({"type": "done", "text": reply})
        return 0
    except Exception as e:  # noqa: BLE001
        emit({"type": "error", "message": str(e)})
        return 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
```

- [ ] **Step 3: Create `testdata/fake_runner.sh`**

This fake replaces `python3 claude_reply.py` in Go tests. Mode is selected by the first argument.

```bash
#!/bin/bash
# fake_runner.sh — deterministic stand-in for claude_reply.py, used by runner_test.go.
# Mode is passed as first arg; prompt is read from stdin.
set -u

MODE="${1:-success}"
PROMPT="$(cat)"

case "$MODE" in
  success)
    # Echo prompt back.
    printf '{"type":"done","text":"echo: %s"}\n' "$PROMPT"
    exit 0
    ;;
  error)
    printf '{"type":"error","message":"missing sdk"}\n'
    exit 2
    ;;
  nodone)
    printf '{"type":"status","message":"started"}\n'
    exit 0
    ;;
  timeout)
    sleep 10
    ;;
  env_openai)
    printf '{"type":"done","text":"%s|%s|%s"}\n' \
      "${OPENAI_BASE_URL:-}" "${OPENAI_API_KEY:-}" "${OPENAI_MODEL:-}"
    exit 0
    ;;
  env_anthropic)
    printf '{"type":"done","text":"%s|%s|%s"}\n' \
      "${ANTHROPIC_BASE_URL:-}" "${ANTHROPIC_API_KEY:-}" "${ANTHROPIC_MODEL:-}"
    exit 0
    ;;
  noisy)
    printf 'not valid json\n'
    printf '{"type":"done","text":"ok"}\n'
    exit 0
    ;;
  *)
    printf '{"type":"error","message":"unknown mode %s"}\n' "$MODE"
    exit 4
    ;;
esac
```

Make it executable:

```bash
chmod +x server/agent_runner/testdata/fake_runner.sh
```

- [ ] **Step 4: Commit**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
git add server/agent_runner/claude_reply.py \
        server/agent_runner/requirements.txt \
        server/agent_runner/testdata/fake_runner.sh && \
git commit -m "feat(agent-runner): add Python SDK entry + requirements + test fake"
```

---

## Task 2: Go Runner (TDD)

**Files:**
- Create: `server/agent_runner/runner.go`
- Create: `server/agent_runner/runner_test.go`

Interface-first so `AutoReplyService` can mock in its own tests. Tests drive the `Runner` struct using `fake_runner.sh` from Task 1.

- [ ] **Step 1: Write failing test**

```go
// server/agent_runner/runner_test.go
package agent_runner

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeRunner(t *testing.T, timeout time.Duration) *Runner {
	t.Helper()
	script, err := filepath.Abs("testdata/fake_runner.sh")
	if err != nil {
		t.Fatal(err)
	}
	return &Runner{
		PythonPath: script,
		ScriptPath: "success",
		Timeout:    timeout,
	}
}

func TestRun_Success(t *testing.T) {
	r := fakeRunner(t, 5*time.Second)
	out, err := r.Run(context.Background(), "hello", Config{
		Kernel: "openai_compat", BaseURL: "b", APIKey: "k", Model: "m",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "echo: hello" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRun_EmitsError(t *testing.T) {
	r := fakeRunner(t, 5*time.Second)
	r.ScriptPath = "error"
	_, err := r.Run(context.Background(), "x", Config{APIKey: "k"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing sdk") {
		t.Fatalf("error should mention sdk: %v", err)
	}
}

func TestRun_NoDoneEvent(t *testing.T) {
	r := fakeRunner(t, 5*time.Second)
	r.ScriptPath = "nodone"
	_, err := r.Run(context.Background(), "x", Config{APIKey: "k"})
	if err == nil || !strings.Contains(err.Error(), "no reply text") {
		t.Fatalf("expected 'no reply text' error, got: %v", err)
	}
}

func TestRun_Timeout(t *testing.T) {
	r := fakeRunner(t, 200*time.Millisecond)
	r.ScriptPath = "timeout"
	_, err := r.Run(context.Background(), "x", Config{APIKey: "k"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

func TestRun_EnvInjectionOpenAI(t *testing.T) {
	r := fakeRunner(t, 5*time.Second)
	r.ScriptPath = "env_openai"
	out, err := r.Run(context.Background(), "x", Config{
		Kernel: "openai_compat", BaseURL: "http://b", APIKey: "sk-123", Model: "qwen-plus",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "http://b|sk-123|qwen-plus" {
		t.Fatalf("env not injected: %q", out)
	}
}

func TestRun_EnvInjectionAnthropic(t *testing.T) {
	r := fakeRunner(t, 5*time.Second)
	r.ScriptPath = "env_anthropic"
	out, err := r.Run(context.Background(), "x", Config{
		Kernel: "anthropic", BaseURL: "http://a", APIKey: "sk-ant", Model: "sonnet",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "http://a|sk-ant|sonnet" {
		t.Fatalf("anthropic env not injected: %q", out)
	}
}

func TestRun_NoisyStdout(t *testing.T) {
	r := fakeRunner(t, 5*time.Second)
	r.ScriptPath = "noisy"
	out, err := r.Run(context.Background(), "x", Config{APIKey: "k"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok" {
		t.Fatalf("expected 'ok', got %q", out)
	}
}
```

Note on the test layout: `fakeRunner` puts the bash path into `PythonPath` and the mode string into `ScriptPath`. That matches how `Runner.Run` will invoke the subprocess — `exec.Command(r.PythonPath, r.ScriptPath)` runs `fake_runner.sh <mode>`.

- [ ] **Step 2: Run — expect FAIL**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent/server && \
go test ./agent_runner/...
```

Expected: `./runner_test.go:XX:XX: undefined: Runner` (runner.go doesn't exist yet).

- [ ] **Step 3: Implement Runner**

Create `server/agent_runner/runner.go`:

```go
package agent_runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Config carries the per-invocation LLM config pulled from agent.cloud_llm_config.
type Config struct {
	Kernel       string // "openai_compat" (default) or "anthropic"
	BaseURL      string
	APIKey       string
	Model        string
	SystemPrompt string
}

// AgentRunner is the interface consumed by AutoReplyService.
type AgentRunner interface {
	Run(ctx context.Context, prompt string, cfg Config) (string, error)
}

// Runner spawns the Python claude-agent-sdk child process.
type Runner struct {
	PythonPath string
	ScriptPath string
	Timeout    time.Duration
}

// NewRunner returns a Runner with defaults resolved from env vars and the source location.
func NewRunner() *Runner {
	scriptPath := os.Getenv("AGENT_RUNNER_SCRIPT_PATH")
	if scriptPath == "" {
		_, thisFile, _, _ := runtime.Caller(0)
		scriptPath = filepath.Join(filepath.Dir(thisFile), "claude_reply.py")
	}
	python := os.Getenv("AGENT_RUNNER_PYTHON")
	if python == "" {
		python = "python3"
	}
	return &Runner{
		PythonPath: python,
		ScriptPath: scriptPath,
		Timeout:    60 * time.Second,
	}
}

// Event is the NDJSON schema the Python side emits on stdout.
type Event struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Message string `json:"message,omitempty"`
}

// Run sends prompt to the Python subprocess and returns the reply.
func (r *Runner) Run(ctx context.Context, prompt string, cfg Config) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, r.PythonPath, r.ScriptPath)
	env := os.Environ()
	switch cfg.Kernel {
	case "anthropic":
		env = append(env,
			"ANTHROPIC_BASE_URL="+cfg.BaseURL,
			"ANTHROPIC_API_KEY="+cfg.APIKey,
			"ANTHROPIC_MODEL="+cfg.Model,
		)
	default:
		env = append(env,
			"OPENAI_BASE_URL="+cfg.BaseURL,
			"OPENAI_API_KEY="+cfg.APIKey,
			"OPENAI_MODEL="+cfg.Model,
		)
	}
	if cfg.SystemPrompt != "" {
		env = append(env, "AGENT_SYSTEM_PROMPT="+cfg.SystemPrompt)
	}
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("spawn runner: %w", err)
	}

	go func() {
		defer stdin.Close()
		_, _ = stdin.Write([]byte(prompt))
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var lastDone, lastErrMsg string
	for scanner.Scan() {
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "done":
			lastDone = ev.Text
		case "error":
			lastErrMsg = ev.Message
		}
	}

	waitErr := cmd.Wait()
	if runCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("claude runner timed out after %s", r.Timeout)
	}
	if waitErr != nil {
		if lastErrMsg != "" {
			return "", fmt.Errorf("runner failed: %s", lastErrMsg)
		}
		return "", fmt.Errorf("runner exited non-zero: %w", waitErr)
	}
	if lastDone == "" {
		return "", errors.New("runner finished with no reply text")
	}
	return lastDone, nil
}
```

- [ ] **Step 4: Run — expect PASS**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent/server && \
go test ./agent_runner/... -v
```

Expected: all 7 tests pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
git add server/agent_runner/runner.go server/agent_runner/runner_test.go && \
git commit -m "feat(agent-runner): add Runner with AgentRunner interface"
```

---

## Task 3: Reshape CloudLLMConfig and rewrite EnsurePersonalAgent (TDD)

**Files:**
- Modify: `server/internal/service/personal_agent.go`
- Create: `server/internal/service/personal_agent_test.go`

- [ ] **Step 1: Check how other service tests set up a DB**

```bash
grep -rn "testdb\|TestDB\|setupTestDB" /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent/server/internal/ --include="*.go" | head -10
```

If there's an existing test helper (likely in `internal/handler/` test suites), follow its pattern to get a `*db.Queries` bound to the `POSTGRES_TEST_URL` database. If no helper exists, use an in-function `pgxpool.New` call with `os.Getenv("DATABASE_URL")`.

- [ ] **Step 2: Write failing tests**

Create `server/internal/service/personal_agent_test.go`:

```go
package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// testDB opens a pool to the dev database. Expects POSTGRES env vars to be set.
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
	ownerID := createTestUser(t, q, "alice@example.com", "Alice")

	agent, err := EnsurePersonalAgent(context.Background(), q, wsID, ownerID, "Alice")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if agent.Name != "Alice's Assistant" {
		t.Fatalf("unexpected name: %s", agent.Name)
	}

	var cfg CloudLLMConfig
	if err := json.Unmarshal(agent.CloudLlmConfig, &cfg); err != nil {
		t.Fatalf("parse cfg: %v", err)
	}
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
	// Do NOT set any AGENT_* env vars.
	for _, v := range []string{"AGENT_KERNEL", "AGENT_LLM_BASE_URL", "AGENT_LLM_API_KEY", "AGENT_LLM_MODEL", "AGENT_SYSTEM_PROMPT"} {
		t.Setenv(v, "")
	}

	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "bob@example.com", "Bob")

	agent, err := EnsurePersonalAgent(context.Background(), q, wsID, ownerID, "Bob")
	if err != nil {
		t.Fatalf("ensure should not error on empty env: %v", err)
	}
	var cfg CloudLLMConfig
	_ = json.Unmarshal(agent.CloudLlmConfig, &cfg)
	if cfg.APIKey != "" {
		t.Fatalf("expected empty api key, got %q", cfg.APIKey)
	}
	// Kernel default is openai_compat when env is empty.
	if cfg.Kernel != "openai_compat" {
		t.Fatalf("expected default kernel, got %q", cfg.Kernel)
	}
}

func TestEnsurePersonalAgent_Idempotent(t *testing.T) {
	q := testDB(t)
	t.Setenv("AGENT_LLM_API_KEY", "sk-idem")

	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "carol@example.com", "Carol")

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
	ownerID := createTestUser(t, q, "dave@example.com", "Dave")

	agent, err := EnsurePersonalAgent(context.Background(), q, wsID, ownerID, "Dave")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	var cfg CloudLLMConfig
	_ = json.Unmarshal(agent.CloudLlmConfig, &cfg)
	if cfg.Kernel != "anthropic" {
		t.Fatalf("expected kernel='anthropic', got %q", cfg.Kernel)
	}
	if cfg.Model != "claude-sonnet-4-5" {
		t.Fatalf("expected anthropic model, got %q", cfg.Model)
	}
}

// Helpers — use exact field shapes from server/pkg/db/generated/.
// CreateWorkspaceParams: Name, Slug, Description (pgtype.Text), Context (pgtype.Text), IssuePrefix.
// CreateUserParams: Name, Email, AvatarUrl (pgtype.Text).
func createTestWorkspace(t *testing.T, q *db.Queries) pgtype.UUID {
	t.Helper()
	ws, err := q.CreateWorkspace(context.Background(), db.CreateWorkspaceParams{
		Name:        "Test Workspace " + t.Name(),
		Slug:        "test-" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "-"),
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
```

If the `CreateWorkspace`/`CreateUser` signatures don't match (plan author should `grep "CreateWorkspace\|CreateUser" server/pkg/db/generated/` to confirm exact field names), adapt the helpers to match the actual generated types. If the generated query doesn't exist for a field, use the next-closest query or construct a row via raw `pool.Exec`.

- [ ] **Step 3: Run — expect FAIL**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent/server && \
go test ./internal/service/ -run TestEnsurePersonalAgent
```

Expected: fail because current `CloudLLMConfig` struct has `Endpoint`/`APIKey`/`Model` (no `Kernel`/`BaseURL`/`SystemPrompt`), tests reference fields that don't exist.

- [ ] **Step 4: Replace `personal_agent.go`**

```go
// server/internal/service/personal_agent.go
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CloudLLMConfig is the JSON shape stored in agent.cloud_llm_config.
// This is the server env snapshot at agent creation time.
type CloudLLMConfig struct {
	Kernel       string `json:"kernel,omitempty"`        // "openai_compat" (default) or "anthropic"
	BaseURL      string `json:"base_url,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// LoadCloudLLMConfigFromEnv reads the four AGENT_* env vars and returns a CloudLLMConfig snapshot.
func LoadCloudLLMConfigFromEnv() CloudLLMConfig {
	return CloudLLMConfig{
		Kernel:       envOr("AGENT_KERNEL", "openai_compat"),
		BaseURL:      os.Getenv("AGENT_LLM_BASE_URL"),
		APIKey:       os.Getenv("AGENT_LLM_API_KEY"),
		Model:        os.Getenv("AGENT_LLM_MODEL"),
		SystemPrompt: os.Getenv("AGENT_SYSTEM_PROMPT"),
	}
}

// EnsurePersonalAgent creates a personal agent for the owner if one doesn't exist.
// It snapshots AGENT_* server env vars into agent.cloud_llm_config so the auto-reply
// runner can read per-agent config from the DB.
func EnsurePersonalAgent(ctx context.Context, queries *db.Queries, workspaceID, ownerID pgtype.UUID, userName string) (db.Agent, error) {
	existing, err := queries.GetPersonalAgent(ctx, db.GetPersonalAgentParams{
		WorkspaceID: workspaceID,
		OwnerID:     ownerID,
	})
	if err == nil {
		return existing, nil
	}

	runtime, err := queries.EnsureCloudRuntime(ctx, workspaceID)
	if err != nil {
		return db.Agent{}, fmt.Errorf("ensure cloud runtime: %w", err)
	}

	cfg := LoadCloudLLMConfigFromEnv()
	if cfg.APIKey == "" {
		slog.Warn("personal agent: AGENT_LLM_API_KEY not set; agent will be unable to reply until configured",
			"workspace_id", util.UUIDToString(workspaceID),
			"owner_id", util.UUIDToString(ownerID),
		)
	}
	configJSON, _ := json.Marshal(cfg)

	triggers, _ := json.Marshal([]map[string]any{
		{"type": "on_assign", "enabled": true},
		{"type": "on_comment", "enabled": true},
		{"type": "on_mention", "enabled": true},
	})

	agentName := userName + "'s Assistant"
	agent, err := queries.CreatePersonalAgent(ctx, db.CreatePersonalAgentParams{
		WorkspaceID:    workspaceID,
		Name:           agentName,
		Description:    "Personal AI assistant powered by Claude Agent SDK",
		RuntimeID:      runtime.ID,
		OwnerID:        ownerID,
		CloudLlmConfig: configJSON,
		Triggers:       triggers,
	})
	if err != nil {
		return db.Agent{}, fmt.Errorf("create personal agent: %w", err)
	}

	slog.Info("personal agent created",
		"agent_id", util.UUIDToString(agent.ID),
		"owner_id", util.UUIDToString(ownerID),
		"workspace_id", util.UUIDToString(workspaceID),
		"kernel", cfg.Kernel,
		"model", cfg.Model,
	)

	return agent, nil
}
```

- [ ] **Step 5: Run — expect PASS**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent/server && \
DATABASE_URL="$(grep '^DATABASE_URL=' ../../../.env | head -1 | cut -d= -f2-)" \
go test ./internal/service/ -run TestEnsurePersonalAgent -v
```

Expected: all 3 tests pass. If the test DB doesn't have a working `.env`, use the value from the running dev stack.

- [ ] **Step 6: Fix callers of the old `CloudLLMConfig` fields**

Search for any other code reading `CloudLLMConfig.Endpoint` — the only caller in the tree is the old `auto_reply.go`, which we rewrite in Task 4. If any other caller exists, adapt it now.

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
grep -rn "CloudLLMConfig" server/ --include="*.go"
```

Expected: only references inside `personal_agent.go` + `personal_agent_test.go` + future `auto_reply.go` changes. If there's a handler or another service reading it, fix to use new field names.

- [ ] **Step 7: Typecheck the whole server builds**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent/server && \
go build ./...
```

Expected: succeed, OR fail with the old auto_reply.go (which still has the old struct reference) — that's fine for this task, Task 4 fixes it. If it fails for any OTHER reason, stop and report.

- [ ] **Step 8: Commit**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
git add server/internal/service/personal_agent.go server/internal/service/personal_agent_test.go && \
git commit -m "feat(server): reshape CloudLLMConfig + env-var snapshot in EnsurePersonalAgent"
```

---

## Task 4: Rewrite AutoReplyService.replyAsMentionedAgent (TDD)

**Files:**
- Modify: `server/internal/service/auto_reply.go`
- Create: `server/internal/service/auto_reply_test.go`

- [ ] **Step 1: Write failing test**

Create `server/internal/service/auto_reply_test.go`:

```go
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent_runner"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeRunner lets tests stub the Python child.
type fakeRunner struct {
	reply     string
	err       error
	lastCfg   agent_runner.Config
	lastPrompt string
}

func (f *fakeRunner) Run(ctx context.Context, prompt string, cfg agent_runner.Config) (string, error) {
	f.lastCfg = cfg
	f.lastPrompt = prompt
	return f.reply, f.err
}

// Helper: insert an agent directly via Queries and return it.
// CreatePersonalAgentParams fields: WorkspaceID, Name, Description, RuntimeID,
// OwnerID, CloudLlmConfig ([]byte), Triggers ([]byte).
func insertTestAgent(t *testing.T, q *db.Queries, wsID, runtimeID, ownerID pgtype.UUID, name string, cfg CloudLLMConfig) db.Agent {
	t.Helper()
	cfgJSON, _ := json.Marshal(cfg)
	triggers, _ := json.Marshal([]map[string]any{{"type": "on_mention", "enabled": true}})
	a, err := q.CreatePersonalAgent(context.Background(), db.CreatePersonalAgentParams{
		WorkspaceID:    wsID,
		Name:           name,
		Description:    "test agent",
		RuntimeID:      runtimeID,
		OwnerID:        ownerID,
		CloudLlmConfig: cfgJSON,
		Triggers:       triggers,
	})
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	return a
}

func TestReplyAsMentionedAgent_DispatchesToRunner(t *testing.T) {
	q := testDB(t)
	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "test@x.com", "Tester")
	runtime, _ := q.EnsureCloudRuntime(context.Background(), wsID)

	cfg := CloudLLMConfig{Kernel: "openai_compat", BaseURL: "http://b", APIKey: "sk-X", Model: "qwen-plus"}
	agent := insertTestAgent(t, q, wsID, runtime.ID, ownerID, "Bot", cfg)
	// channel + trigger message
	ch := createTestChannel(t, q, wsID, ownerID)
	trigger := createTestMessage(t, q, wsID, ownerID, ch.ID, "@Bot hello")

	runner := &fakeRunner{reply: "hi there"}
	svc := &AutoReplyService{Queries: q, Runner: runner}

	if err := svc.replyAsMentionedAgent(context.Background(), "Bot", uuidToStr(wsID), uuidToStr(ch.ID), trigger); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if runner.lastCfg.APIKey != "sk-X" {
		t.Fatalf("runner did not receive agent config: %+v", runner.lastCfg)
	}

	// Reply message should be stored.
	msgs, _ := q.ListChannelMessages(context.Background(), db.ListChannelMessagesParams{ChannelID: ch.ID, Limit: 10})
	found := false
	for _, m := range msgs {
		if m.Content == "hi there" {
			found = true
			if string(m.SenderType) != "agent" {
				t.Fatalf("sender_type expected 'agent', got %q", m.SenderType)
			}
		}
	}
	if !found {
		t.Fatal("reply message not inserted")
	}
}

func TestReplyAsMentionedAgent_NoAPIKey_PostsSystemNotification(t *testing.T) {
	q := testDB(t)
	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "t2@x.com", "T2")
	runtime, _ := q.EnsureCloudRuntime(context.Background(), wsID)
	agent := insertTestAgent(t, q, wsID.Bytes, runtime.ID.Bytes, ownerID.Bytes, "Bot", CloudLLMConfig{Kernel: "openai_compat", APIKey: ""})
	ch := createTestChannel(t, q, wsID, ownerID)
	trigger := createTestMessage(t, q, wsID, ownerID, ch.ID, "@Bot hi")

	runner := &fakeRunner{}
	svc := &AutoReplyService{Queries: q, Runner: runner}

	_ = svc.replyAsMentionedAgent(context.Background(), "Bot", uuidToStr(wsID), uuidToStr(ch.ID), trigger)
	if runner.lastPrompt != "" {
		t.Fatal("runner should not be called when api key missing")
	}
	msgs, _ := q.ListChannelMessages(context.Background(), db.ListChannelMessagesParams{ChannelID: ch.ID, Limit: 10})
	found := false
	for _, m := range msgs {
		if strings.Contains(m.Content, "not configured") {
			found = true
		}
	}
	if !found {
		t.Fatal("system_notification not posted")
	}
	_ = agent
}

func TestReplyAsMentionedAgent_RunnerError_PostsSystemNotification(t *testing.T) {
	q := testDB(t)
	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "t3@x.com", "T3")
	runtime, _ := q.EnsureCloudRuntime(context.Background(), wsID)
	cfg := CloudLLMConfig{Kernel: "openai_compat", APIKey: "sk-X", Model: "m"}
	_ = insertTestAgent(t, q, wsID, runtime.ID, ownerID, "Bot", cfg)
	ch := createTestChannel(t, q, wsID, ownerID)
	trigger := createTestMessage(t, q, wsID, ownerID, ch.ID, "@Bot hi")

	runner := &fakeRunner{err: errors.New("boom sk-abcdefgh")}
	svc := &AutoReplyService{Queries: q, Runner: runner}

	_ = svc.replyAsMentionedAgent(context.Background(), "Bot", uuidToStr(wsID), uuidToStr(ch.ID), trigger)
	msgs, _ := q.ListChannelMessages(context.Background(), db.ListChannelMessagesParams{ChannelID: ch.ID, Limit: 10})
	found := false
	for _, m := range msgs {
		if strings.Contains(m.Content, "Agent reply failed") {
			found = true
			// Key must be redacted.
			if strings.Contains(m.Content, "sk-abcdefgh") {
				t.Fatal("api key not redacted in error message")
			}
		}
	}
	if !found {
		t.Fatal("error system_notification not posted")
	}
}

func TestReplyAsMentionedAgent_AgentNotFound_Silent(t *testing.T) {
	q := testDB(t)
	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "t4@x.com", "T4")
	ch := createTestChannel(t, q, wsID, ownerID)
	trigger := createTestMessage(t, q, wsID, ownerID, ch.ID, "@GhostBot hi")

	runner := &fakeRunner{}
	svc := &AutoReplyService{Queries: q, Runner: runner}

	err := svc.replyAsMentionedAgent(context.Background(), "GhostBot", uuidToStr(wsID), uuidToStr(ch.ID), trigger)
	if err != nil {
		t.Fatalf("agent-not-found should be silent (nil err), got: %v", err)
	}
	// Channel should only contain the trigger message.
	msgs, _ := q.ListChannelMessages(context.Background(), db.ListChannelMessagesParams{ChannelID: ch.ID, Limit: 10})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (trigger only), got %d", len(msgs))
	}
}

func TestReplyAsMentionedAgent_OnMentionDisabled_Silent(t *testing.T) {
	q := testDB(t)
	wsID := createTestWorkspace(t, q)
	ownerID := createTestUser(t, q, "t5@x.com", "T5")
	runtime, _ := q.EnsureCloudRuntime(context.Background(), wsID)
	// Insert agent with on_mention EXPLICITLY disabled in triggers JSONB.
	cfgJSON, _ := json.Marshal(CloudLLMConfig{APIKey: "sk-X", Kernel: "openai_compat", Model: "m"})
	triggers, _ := json.Marshal([]map[string]any{{"type": "on_mention", "enabled": false}})
	agent, err := q.CreatePersonalAgent(context.Background(), db.CreatePersonalAgentParams{
		WorkspaceID:    wsID,
		Name:           "Muted",
		Description:    "test",
		RuntimeID:      runtime.ID,
		OwnerID:        ownerID,
		CloudLlmConfig: cfgJSON,
		Triggers:       triggers,
	})
	if err != nil {
		t.Fatalf("create muted agent: %v", err)
	}
	ch := createTestChannel(t, q, wsID, ownerID)
	trigger := createTestMessage(t, q, wsID, ownerID, ch.ID, "@Muted hi")

	runner := &fakeRunner{}
	svc := &AutoReplyService{Queries: q, Runner: runner}

	_ = svc.replyAsMentionedAgent(context.Background(), "Muted", uuidToStr(wsID), uuidToStr(ch.ID), trigger)
	if runner.lastPrompt != "" {
		t.Fatal("runner should not run when on_mention disabled")
	}
	msgs, _ := q.ListChannelMessages(context.Background(), db.ListChannelMessagesParams{ChannelID: ch.ID, Limit: 10})
	if len(msgs) != 1 {
		t.Fatalf("expected only trigger message, got %d", len(msgs))
	}
	_ = agent
}

// Helpers — match generated params exactly.
// CreateChannelParams: WorkspaceID, Name, Description, CreatedBy, CreatedByType.
// CreateMessageParams: WorkspaceID, SenderID, SenderType, ChannelID, RecipientID, RecipientType,
//                     SessionID, Content, ContentType, FileID, FileName, FileSize, FileContentType,
//                     Metadata, ParentID, Type.
func createTestChannel(t *testing.T, q *db.Queries, wsID pgtype.UUID, creatorID pgtype.UUID) db.Channel {
	t.Helper()
	ch, err := q.CreateChannel(context.Background(), db.CreateChannelParams{
		WorkspaceID:   wsID,
		Name:          "test-channel-" + strings.ReplaceAll(t.Name(), "/", "-"),
		Description:   pgtype.Text{},
		CreatedBy:     creatorID,
		CreatedByType: "member",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return ch
}

func createTestMessage(t *testing.T, q *db.Queries, wsID, senderID, channelID pgtype.UUID, content string) db.Message {
	t.Helper()
	m, err := q.CreateMessage(context.Background(), db.CreateMessageParams{
		WorkspaceID: wsID,
		SenderID:    senderID,
		SenderType:  "member",
		ChannelID:   channelID,
		Content:     content,
		ContentType: "text",
		Type:        "user",
	})
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	return m
}

func uuidToStr(u pgtype.UUID) string {
	return util.UUIDToString(u)
}
```

Note: the helper stubs use `panic("implement ...")` as placeholders only for the plan author to know WHERE to write real code. Before running the test, the plan author fills in the helpers by inspecting `server/pkg/db/generated/` for actual field shapes. The goal of this step is **the test SKELETON compiles** and has a clear shape — the red state is "panics or fails assertion", not "doesn't compile".

Actually, to satisfy the TDD flow, let the plan author keep panics in the helpers initially. When they run the test in Step 2, tests will panic on the helper calls — that IS the failure state. After Task 4's Step 4 (implementation), they fill the helpers. Clean two-phase.

- [ ] **Step 2: Run — expect FAIL (or panic)**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent/server && \
go test ./internal/service/ -run TestReplyAsMentionedAgent
```

Expected: either compile error (`Runner field doesn't exist on AutoReplyService` — fixed in Step 4) or test panics in helpers (fixed in Step 5).

- [ ] **Step 3: Rewrite `replyAsMentionedAgent`**

Replace the whole function in `server/internal/service/auto_reply.go`. Remove the `llmclient` usage and import. Add `Runner` field to `AutoReplyService`.

First, update the struct:

```go
import (
	// ... existing ...
	"github.com/multica-ai/multica/server/pkg/agent_runner"
)

type AutoReplyService struct {
	Queries *db.Queries
	Hub     *realtime.Hub
	Runner  agent_runner.AgentRunner
}

func NewAutoReplyService(q *db.Queries, hub *realtime.Hub, runner agent_runner.AgentRunner) *AutoReplyService {
	return &AutoReplyService{Queries: q, Hub: hub, Runner: runner}
}
```

Then replace `replyAsMentionedAgent`:

```go
var apiKeyRedactRE = regexp.MustCompile(`sk-[A-Za-z0-9\-]{6,}`)

func redactKey(s string) string {
	return apiKeyRedactRE.ReplaceAllString(s, "sk-***")
}

func (s *AutoReplyService) replyAsMentionedAgent(ctx context.Context, agentName string, workspaceID string, channelID string, trigger db.Message) error {
	agent, err := s.Queries.GetAgentByName(ctx, db.GetAgentByNameParams{
		WorkspaceID: trigger.WorkspaceID,
		Name:        agentName,
	})
	if err != nil {
		slog.Debug("auto-reply: agent not found", "name", agentName, "error", err)
		return nil
	}

	// Respect on_mention trigger (existing semantics).
	if !agentHasTriggerEnabled(agent.Triggers, "on_mention") {
		slog.Debug("auto-reply: on_mention disabled", "agent", agentName)
		return nil
	}

	// Rate limit (existing): 3 consecutive agent messages skip.
	recent, _ := s.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
		ChannelID: util.ParseUUID(channelID),
		Limit:     5,
		Offset:    0,
	})
	consecutive := 0
	for i := len(recent) - 1; i >= 0; i-- {
		if util.UUIDToString(recent[i].SenderID) == util.UUIDToString(agent.ID) {
			consecutive++
		} else {
			break
		}
	}
	if consecutive >= 3 {
		slog.Info("auto-reply: rate limited", "agent", agentName, "consecutive", consecutive)
		return nil
	}

	// Load per-agent config from cloud_llm_config.
	var cfg CloudLLMConfig
	if len(agent.CloudLlmConfig) > 0 {
		if err := json.Unmarshal(agent.CloudLlmConfig, &cfg); err != nil {
			s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), util.ParseUUID(workspaceID),
				"Agent configuration is invalid: "+redactKey(err.Error()))
			return nil
		}
	}
	if cfg.APIKey == "" {
		s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), util.ParseUUID(workspaceID),
			"Agent is not configured: missing API key.")
		return nil
	}

	// Build prompt with recent context (existing pattern).
	history, _ := s.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
		ChannelID: util.ParseUUID(channelID),
		Limit:     20,
		Offset:    0,
	})
	var sb strings.Builder
	for _, m := range history {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", util.UUIDToString(m.SenderID), m.Content))
	}
	prompt := fmt.Sprintf("Conversation history:\n%sLatest message from %s: %s\n\nReply as %s:",
		sb.String(),
		util.UUIDToString(trigger.SenderID), trigger.Content,
		agentName,
	)

	runnerCfg := agent_runner.Config{
		Kernel:       cfg.Kernel,
		BaseURL:      cfg.BaseURL,
		APIKey:       cfg.APIKey,
		Model:        cfg.Model,
		SystemPrompt: cfg.SystemPrompt,
	}
	if runnerCfg.SystemPrompt == "" {
		runnerCfg.SystemPrompt = fmt.Sprintf("You are %s, an AI assistant on MyTeam. Reply concisely and helpfully.", agentName)
	}

	slog.Info("auto-reply dispatching",
		"agent", agentName,
		"channel", channelID,
		"kernel", cfg.Kernel,
		"model", cfg.Model,
	)

	reply, err := s.Runner.Run(ctx, prompt, runnerCfg)
	if err != nil {
		msg := fmt.Sprintf("Agent reply failed: %s", redactKey(err.Error()))
		slog.Warn("auto-reply runner failed", "agent", agentName, "error", err)
		s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), util.ParseUUID(workspaceID), msg)
		return nil
	}
	if reply == "" {
		s.postSystemNotification(ctx, agent, util.ParseUUID(channelID), util.ParseUUID(workspaceID),
			"Agent returned empty reply.")
		return nil
	}

	// Insert agent's reply.
	replyMsg, err := s.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID: util.ParseUUID(workspaceID),
		SenderID:    agent.ID,
		SenderType:  "agent",
		ChannelID:   util.ParseUUID(channelID),
		Content:     reply,
		ContentType: "text",
		Type:        "agent_reply",
	})
	if err != nil {
		slog.Warn("auto-reply: failed to insert reply message", "error", err)
		return err
	}

	// Broadcast via WS hub.
	if s.Hub != nil {
		s.Hub.Broadcast(workspaceID, "message:created", messageToMap(replyMsg))
	}

	slog.Info("auto-reply sent", "agent", agentName, "channel", channelID)
	return nil
}

// postSystemNotification sends a visible-to-user message from the agent that explains
// why a reply didn't happen. Uses content_type="text" with metadata marker.
func (s *AutoReplyService) postSystemNotification(ctx context.Context, agent db.Agent, channelID, workspaceID pgtype.UUID, message string) {
	meta, _ := json.Marshal(map[string]any{"system_notification": true})
	msg, err := s.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID: workspaceID,
		SenderID:    agent.ID,
		SenderType:  "agent",
		ChannelID:   channelID,
		Content:     message,
		ContentType: "text",
		Type:        "system_notification",
		Metadata:    meta,
	})
	if err != nil {
		slog.Warn("post system_notification failed", "error", err)
		return
	}
	if s.Hub != nil {
		s.Hub.Broadcast(util.UUIDToString(workspaceID), "message:created", messageToMap(msg))
	}
}

// messageToMap transforms a db.Message into the map shape the Hub broadcasts.
// Shared between the normal reply path and postSystemNotification.
func messageToMap(m db.Message) map[string]any {
	return map[string]any{
		"id":           util.UUIDToString(m.ID),
		"workspace_id": util.UUIDToString(m.WorkspaceID),
		"channel_id":   util.UUIDToString(m.ChannelID),
		"sender_id":    util.UUIDToString(m.SenderID),
		"sender_type":  m.SenderType,
		"content":      m.Content,
		"content_type": m.ContentType,
		"metadata":     json.RawMessage(m.Metadata),
		"created_at":   m.CreatedAt.Time,
	}
}
```

Delete the obsolete `AutoReplyConfig` type. Delete the `llmclient` import. Keep the rest of the file (StartPollDaemon, pollAndReply, agentHasTriggerEnabled helper).

**Database constraints to check before running tests:**
- Confirm `CreateMessageParams` has a `Metadata` field. If it doesn't, run `make sqlc` after verifying the SQL query at `server/pkg/db/queries/message.sql` includes `metadata`. If the query doesn't include it, add it (one small migration or query adjustment) — this is in-scope for the plan.

- [ ] **Step 4: Fill in the test helpers**

Replace the `panic("implement ...")` stubs in `auto_reply_test.go` with real implementations using the exact generated params. Inspect `server/pkg/db/generated/message.sql.go`, `channel.sql.go` for field names.

Example shape (plan author adapts to match actual `db.CreateChannelParams`):

```go
func createTestChannel(t *testing.T, q *db.Queries, wsID pgtype.UUID) db.Channel {
	t.Helper()
	// Replace field names with what CreateChannelParams actually declares:
	ch, err := q.CreateChannel(context.Background(), db.CreateChannelParams{
		WorkspaceID: wsID,
		Name:        "test-channel-" + t.Name(),
		Visibility:  "private",
		CreatedBy:   /* ... */,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return ch
}

func createTestMessage(t *testing.T, q *db.Queries, wsID, senderID, channelID [16]byte, content string) db.Message {
	t.Helper()
	m, err := q.CreateMessage(context.Background(), db.CreateMessageParams{
		WorkspaceID: util.BytesToUUID(wsID),
		SenderID:    util.BytesToUUID(senderID),
		SenderType:  "member",
		ChannelID:   util.BytesToUUID(channelID),
		Content:     content,
		ContentType: "text",
	})
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	return m
}

func uuidToStr(u pgtype.UUID) string {
	return util.UUIDToString(u)
}
```

If `util.BytesToUUID` doesn't exist, use `pgtype.UUID{Bytes: wsID, Valid: true}` directly.

- [ ] **Step 5: Run — expect PASS**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent/server && \
DATABASE_URL="<dev db url>" \
go test ./internal/service/ -run TestReplyAsMentionedAgent -v
```

Expected: all 4 tests pass.

- [ ] **Step 6: Commit**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
git add server/internal/service/auto_reply.go server/internal/service/auto_reply_test.go && \
git commit -m "feat(server): route auto-reply through AgentRunner; system_notification on failure"
```

---

## Task 5: Wire NewRunner into the server

**Files:**
- Modify: `server/cmd/server/main.go` (or wherever `AutoReplyService` is currently constructed)

- [ ] **Step 1: Find the existing construction site**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
grep -rn "NewAutoReplyService\|AutoReplyService{" server/ --include="*.go" | grep -v _test.go
```

The call will be somewhere like `autoReply := service.NewAutoReplyService(queries, hub)`.

- [ ] **Step 2: Inject runner**

Change the call site to also pass a new runner:

```go
import (
	// ... existing ...
	"github.com/multica-ai/multica/server/pkg/agent_runner"
)

// ...
runner := agent_runner.NewRunner()
autoReply := service.NewAutoReplyService(queries, hub, runner)
```

If `service.NewAutoReplyService` has a different signature today, update both the constructor (done in Task 4) and this call site to match.

- [ ] **Step 3: Build**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent/server && \
go build ./...
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
git add server/cmd/server/main.go && \
git commit -m "chore(server): inject agent_runner.NewRunner() into AutoReplyService"
```

---

## Task 6: Desktop API client — `getOrCreateSystemAgent`

**Files:**
- Modify: `packages/client-core/src/desktop-api-client.ts`

No test is added in this task — the method is a one-liner thin wrapper. Integration coverage comes from Task 7's bootstrap test.

- [ ] **Step 1: Read current API client**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
grep -n "async listMessages\|async sendMessage" packages/client-core/src/desktop-api-client.ts
```

This pins the style — just before `sendMessage`, add:

- [ ] **Step 2: Add the method**

In `packages/client-core/src/desktop-api-client.ts`, after `listMessages` and before `sendMessage`, insert:

```ts
  async getOrCreateSystemAgent(): Promise<{
    id: string;
    name: string;
    agent_type?: string;
  }> {
    return this.request("/api/system-agent");
  }
```

- [ ] **Step 3: Typecheck**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
pnpm --filter @myteam/client-core typecheck
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
git add packages/client-core/src/desktop-api-client.ts && \
git commit -m "feat(client-core): add getOrCreateSystemAgent to DesktopApiClient"
```

---

## Task 7: Desktop bootstrap calls getOrCreateSystemAgent

**Files:**
- Modify: `apps/desktop/src/lib/desktop-client.ts`

- [ ] **Step 1: Read current bootstrap**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
sed -n '115,145p' apps/desktop/src/lib/desktop-client.ts
```

The function ends around `ensureWSClient().connect();`. Insert the new call between `workspace.bootstrap(...)` and `ensureWSClient().connect()`.

- [ ] **Step 2: Update bootstrap**

In `apps/desktop/src/lib/desktop-client.ts`, find:

```ts
  if (useDesktopAuthStore.getState().user) {
    await useDesktopWorkspaceStore.getState().bootstrap(storedWorkspaceId);
    // WS connects via the auth-store subscription above when user populates.
    // Trigger here too in case this app starts with a valid session.
    ensureWSClient().connect();
  }
```

Replace with:

```ts
  if (useDesktopAuthStore.getState().user) {
    await useDesktopWorkspaceStore.getState().bootstrap(storedWorkspaceId);
    // Fire-and-forget: ensure the personal agent is provisioned on the server.
    // Failure is non-fatal; Session UI will still load without an Assistant DM.
    void desktopApi.getOrCreateSystemAgent().catch((err) => {
      // eslint-disable-next-line no-console
      console.warn("[bootstrap] ensure system agent failed:", err);
    });
    // WS connects via the auth-store subscription above when user populates.
    // Trigger here too in case this app starts with a valid session.
    ensureWSClient().connect();
  }
```

- [ ] **Step 3: Typecheck + test**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
pnpm --filter @myteam/desktop typecheck && \
pnpm --filter @myteam/desktop test
```

Expected: typecheck clean, existing tests pass (18/18 from sub-project #1).

- [ ] **Step 4: Commit**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
git add apps/desktop/src/lib/desktop-client.ts && \
git commit -m "feat(desktop): provision personal agent on bootstrap"
```

---

## Task 8: env + Makefile + README

**Files:**
- Modify: `.env.example`
- Modify: `Makefile`
- Modify: `README.md`

- [ ] **Step 1: `.env.example`**

Append to the end of `.env.example`:

```bash

# ──────────────────────────────────────────────────────────────────────
# Personal Agent — Claude Agent SDK kernel
# ──────────────────────────────────────────────────────────────────────
AGENT_KERNEL=openai_compat                             # openai_compat | anthropic
AGENT_LLM_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
AGENT_LLM_API_KEY=                                     # set to your Bailian or Anthropic key
AGENT_LLM_MODEL=qwen-plus                              # qwen-plus / qwen-max / claude-sonnet-4-5
AGENT_SYSTEM_PROMPT=                                   # optional; overrides Python default
# AGENT_RUNNER_PYTHON=python3
# AGENT_RUNNER_SCRIPT_PATH=
```

- [ ] **Step 2: Makefile target**

In `Makefile`, find the `.PHONY` line near the top and append `setup-agent-runner` to the list. Then at the end of the file, add:

```makefile
# Install Python deps for the personal agent runner (claude-agent-sdk).
setup-agent-runner:
	@command -v python3 >/dev/null 2>&1 || { echo "python3 is required; install Python 3.10+"; exit 1; }
	@python3 -m pip install --upgrade pip >/dev/null
	@python3 -m pip install -r server/agent_runner/requirements.txt
	@echo "✓ agent runner Python deps installed"
```

If the existing `setup` target exists (it does per earlier exploration), add `@$(MAKE) setup-agent-runner` as a non-fatal final step — wrap in `|| echo "(skipping agent runner setup; run manually if desired)"` so missing Python doesn't block the dev flow:

```makefile
setup:
    # ... existing lines ...
	@$(MAKE) setup-agent-runner || echo "(personal agent runner not installed; run 'make setup-agent-runner' manually when ready)"
```

- [ ] **Step 3: README**

Append to the "Setup" or "Prerequisites" section of `README.md`:

```markdown
### Personal agent runner

The personal agent `<User>'s Assistant` replies via the Claude Agent SDK (Python). To enable replies:

1. Install Python 3.10+ and the SDK:
   ```bash
   make setup-agent-runner
   ```
2. Set `AGENT_LLM_API_KEY`, `AGENT_LLM_BASE_URL`, and `AGENT_LLM_MODEL` in `.env` (see `.env.example`). Bailian (OpenAI-compatible) is the default; Anthropic API is supported by setting `AGENT_KERNEL=anthropic`.
3. Restart the backend.

If the API key is missing, the agent is created but replies fail with a `system_notification` message in the conversation rather than silently dropping.
```

- [ ] **Step 4: Commit**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
git add .env.example Makefile README.md && \
git commit -m "docs: document personal agent runner setup"
```

---

## Task 9: Final verification + manual acceptance

- [ ] **Step 1: Full backend test**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent/server && \
DATABASE_URL="<dev>" \
go test ./...
```

Expected: all green.

- [ ] **Step 2: Full frontend test matrix**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
pnpm --filter @myteam/client-core test && \
pnpm --filter @multica/web test && \
pnpm --filter @myteam/desktop test && \
pnpm --filter @myteam/client-core typecheck && \
pnpm --filter @multica/web typecheck && \
pnpm --filter @myteam/desktop typecheck
```

Expected: all green.

- [ ] **Step 3: Python script smoke test (optional)**

If the dev machine has `claude-agent-sdk` installed:

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/personal-agent && \
OPENAI_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1 \
OPENAI_API_KEY=<bailian key> \
OPENAI_MODEL=qwen-plus \
echo "say hi in one sentence" | python3 server/agent_runner/claude_reply.py
```

Expected: final stdout line is `{"type":"done","text":"<some reply>"}` with exit 0.

If not installed or no API key: skip.

- [ ] **Step 4: Manual acceptance — run the stack end-to-end**

Start backend with real Bailian key in `.env`:

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam && \
make stop || true && \
# Edit .env to set AGENT_LLM_API_KEY=<real bailian key>
make start &
cd /Users/chauncey2025/Documents/GitHub/MyTeam && \
pnpm --filter @myteam/desktop dev &
```

Walk the 8 acceptance steps from spec §6.6:

1. Restart backend with real `AGENT_LLM_API_KEY` → no warns.
2. Fresh workspace + login → sidebar shows `🤖 <User>'s Assistant`.
3. `DELETE FROM agent WHERE ...` + restart desktop → 6 agents re-materialize.
4. New DM → Assistant → `你好` → reply via WS within ~5s.
5. `@Assistant 写首诗` in a channel → reply via WS.
6. Set `AGENT_LLM_API_KEY=invalid` → restart → `@Assistant` → system_notification `"Agent reply failed: 401 ..."`.
7. Unset `AGENT_LLM_API_KEY` → restart → `@Assistant` → system_notification `"Agent is not configured: missing API key."`.
8. Move `python3` out of PATH (e.g. `PATH=/tmp make start`) → `@Assistant` → system_notification `"Agent runtime unavailable: python3 not installed on server."`.

All 8 must pass.

- [ ] **Step 5: Create PR or merge locally**

Use superpowers:finishing-a-development-branch once all 8 manual checks pass. Preferred: local merge to `main` (same pattern as sub-project #1).

---

## Notes for the implementer

- **Database migrations**: not required for this sub-project. The `agent.cloud_llm_config` column already exists; we're only changing the JSON shape stored inside it. Existing rows with old shape need operator action (DELETE to re-trigger EnsurePersonalAgent) — not a migration.
- **Metadata column on message**: Task 4 calls out the check. If `Metadata` doesn't exist on `CreateMessageParams`, running `make sqlc` after updating `server/pkg/db/queries/message.sql` to include `metadata` in the INSERT is in scope.
- **Rate limiting & history context**: Keep both existing behaviors (3-consecutive cap + 20-message context). They are valid in the new runner-based flow too and weren't deliberately spec'd away.
- **`llmclient` package**: Do NOT delete it. Other callers may exist; removing it is out of scope. We only stop `auto_reply.go` from importing it.
- **Per-task commits**: Every task ends with a commit. Never batch.
- **Worktree path**: Controller will create `.claude/worktrees/personal-agent` before Task 1. If it's missing, controller sets it up — subagent STOPs and reports BLOCKED.
