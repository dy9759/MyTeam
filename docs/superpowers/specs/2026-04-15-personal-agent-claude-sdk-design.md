# Personal Agent + Claude Agent SDK Kernel — Design Spec

- **Date**: 2026-04-15
- **Sub-project**: #2 of the desktop migration. Unblocks Session MVP (#1) verification and introduces Claude Agent SDK as the reply runtime.
- **Depends on**: Session MVP (merge commit `75445e5c` on main).

## 1. Goals & Non-Goals

### Goals

1. When a user opens the desktop client, a personal agent named `<User>'s Assistant` is automatically created in their workspace and appears in the DM sidebar.
2. When the user sends `@Assistant <prompt>` (or DMs the assistant), the backend invokes Claude Agent SDK (Python subprocess) with configured LLM (default: DashScope / Bailian via OpenAI-compatible mode) and returns the reply as a chat message.
3. The existing DashScope HTTP-direct path in `auto_reply.go` is fully replaced; one kernel, not two.
4. Configuration lives in server env vars; on agent creation, a snapshot is stored in `agent.cloud_llm_config`.
5. Failure states (missing API key, missing `python3`, SDK import error, LLM 401/429, timeout) surface as `system_notification` messages in the conversation so the user can see what went wrong.

### Non-Goals

- Account identity card UI (deferred to a later sub-project).
- Subagents, tool use, skill registry (deferred — the current agent is single-turn, text-only reply).
- Streaming token-by-token to the WS (MVP waits for `done` event, sends one message).
- Multi-turn conversation state (each reply is `max_turns=1`).
- Process pool / long-running Python workers (per-reply spawn; optimize later if cold start bites).
- Docker image updates and CI installation of Python/SDK (dev-only for MVP; follow-ups tracked).
- Per-user or per-agent API keys.
- `Runner → DashScope HTTP` fallback.
- Prompt sanitization beyond the built-in system prompt constraint.

## 2. Architecture

### 2.1 High-level data flow

```
Desktop opens
    ↓
GET /api/system-agent                             (existing endpoint)
    ↓
Go handler.GetOrCreateSystemAgent
    ├── EnsurePersonalAgent(workspace, owner)
    │       └── create agent row with cloud_llm_config = server env snapshot
    ├── EnsurePageAgents(...)
    └── return {agent, page_agents}
    ↓
Desktop sidebar shows Assistant; user types "@Assistant hello"
    ↓
POST /api/messages → backend inserts + broadcasts + ParseMentions
    ↓
AutoReplyService.CheckAndReply(mentions=["Assistant"])   (rewritten)
    ↓
ClaudeRunner.Run(ctx, prompt, agentConfig)
    ├── spawn python3 server/agent_runner/claude_reply.py
    │       env = {OPENAI_BASE_URL, OPENAI_API_KEY, OPENAI_MODEL,
    │              AGENT_SYSTEM_PROMPT}
    │       stdin = prompt text
    │       stdout = NDJSON; final {"type":"done","text":"..."}
    ├── collect reply text
    ├── insert agent's reply message
    └── Hub.Broadcast("message:created")
    ↓
Desktop receives WS event → store.handleEvent → message appears
```

### 2.2 New and modified files

```
server/
├── agent_runner/                       — new package
│   ├── runner.go                       — Go side: spawn, pipe, parse, interface
│   ├── runner_test.go                  — unit tests using bash fake script
│   ├── claude_reply.py                 — Python entry, ~50 LOC
│   ├── claude_reply_test.py            — lightweight integration test (skipif SDK missing)
│   ├── requirements.txt                — claude-agent-sdk>=0.1.0
│   └── testdata/
│       └── fake_runner.sh              — deterministic fake Python for runner_test.go
├── internal/service/
│   ├── auto_reply.go                   — REWRITE replyAsMentionedAgent to use AgentRunner
│   ├── auto_reply_test.go              — new unit tests (no existing file)
│   ├── personal_agent.go               — read AGENT_* env vars, snapshot into cloud_llm_config
│   └── personal_agent_test.go          — new unit tests (no existing file)
└── cmd/server/main.go                  — wire agent_runner.NewRunner() into AutoReplyService

packages/client-core/src/desktop-api-client.ts   — add getOrCreateSystemAgent()
apps/desktop/src/lib/desktop-client.ts           — call getOrCreateSystemAgent in bootstrap

.env.example / .env                              — add AGENT_KERNEL / AGENT_LLM_BASE_URL /
                                                   AGENT_LLM_API_KEY / AGENT_LLM_MODEL
Makefile                                         — new target: setup-agent-runner
README.md                                        — Python prereqs note
```

### 2.3 Key architectural decisions

- **Runtime**: Python subprocess per @-reply. Not Node. Not a long-running service. Matches the SaySo reference (`nightly-test0327/main/services/agent-pipeline/pipeline-manager.ts`) pattern. Go spawns, pipes stdin, reads NDJSON stdout, waits for exit.
- **Kernel**: Single Claude Agent SDK client, `max_turns=1`, no subagents, no tools. Bailian (OpenAI-compatible) is the default backend via `OPENAI_*` env vars; Anthropic native supported via `ANTHROPIC_*` when `AGENT_KERNEL=anthropic`.
- **Config scope**: Server-global env var → snapshotted to `agent.cloud_llm_config` on `EnsurePersonalAgent`. Per-agent overrides are allowed (JSONB), but the MVP doesn't provide any UI to set them.
- **Replacement, not coexistence**: `auto_reply.go` no longer calls `llmclient.Client.Chat()` directly for mention replies. The `llmclient` package itself stays (other callers may exist), but `AutoReplyService` is the one relevant consumer and it's changing.
- **Trigger**: Desktop calls `GET /api/system-agent` fire-and-forget on bootstrap after workspace.bootstrap completes. The endpoint already exists (`handler/system_agent.go`) and already handles both system-agent creation and personal-agent provisioning.

## 3. Component specifications

### 3.1 Python — `server/agent_runner/claude_reply.py`

~50 LOC. Reads prompt from stdin, calls `ClaudeSDKClient`, emits NDJSON events to stdout, exits with code 0 on success.

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
  exit 0 = success; non-zero = failure (stderr carries detail)
"""
import asyncio, json, os, sys

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
        "You are a helpful AI assistant on the MyTeam platform. Reply concisely."
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

### 3.2 Go — `server/agent_runner/runner.go`

Defines `AgentRunner` interface (so `AutoReplyService` can mock) and a `Runner` struct that implements it by spawning `claude_reply.py`.

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

// Config carries the per-invocation LLM config from agent.cloud_llm_config.
type Config struct {
    Kernel       string // "openai_compat" (default) or "anthropic"
    BaseURL      string
    APIKey       string
    Model        string
    SystemPrompt string // optional
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

func NewRunner() *Runner {
    scriptPath := os.Getenv("AGENT_RUNNER_SCRIPT_PATH")
    if scriptPath == "" {
        _, thisFile, _, _ := runtime.Caller(0)
        scriptPath = filepath.Join(filepath.Dir(thisFile), "claude_reply.py")
    }
    return &Runner{
        PythonPath: envOr("AGENT_RUNNER_PYTHON", "python3"),
        ScriptPath: scriptPath,
        Timeout:    60 * time.Second,
    }
}

func envOr(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}

type Event struct {
    Type    string `json:"type"`
    Text    string `json:"text,omitempty"`
    Message string `json:"message,omitempty"`
}

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
    default: // "openai_compat" or empty
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
        return "", fmt.Errorf("spawn python: %w", err)
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
            return "", fmt.Errorf("python runner failed: %s", lastErrMsg)
        }
        return "", fmt.Errorf("python exited non-zero: %w", waitErr)
    }
    if lastDone == "" {
        return "", errors.New("runner finished with no reply text")
    }
    return lastDone, nil
}
```

### 3.3 `EnsurePersonalAgent` changes — `server/internal/service/personal_agent.go`

Remove hardcoded DashScope defaults. Read from env:

- `AGENT_KERNEL` (default: `"openai_compat"`)
- `AGENT_LLM_BASE_URL`
- `AGENT_LLM_API_KEY`
- `AGENT_LLM_MODEL`
- `AGENT_SYSTEM_PROMPT` (default: empty → runner uses its built-in)

Snapshot into `cloud_llm_config` JSONB:

```json
{
  "kernel": "openai_compat",
  "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
  "api_key": "sk-...",
  "model": "qwen-plus",
  "system_prompt": ""
}
```

Agent is always created, even if any env field is empty. Log a `Warn` if `api_key` is empty (admin misconfiguration).

### 3.4 `AutoReplyService.replyAsMentionedAgent` rewrite — `server/internal/service/auto_reply.go`

Replace the current `llmclient.Client.Chat()` call with:

```go
// Parse agent.cloud_llm_config into agent_runner.Config.
cfg, err := parseAgentConfig(agent.CloudLlmConfig)
if err != nil || cfg.APIKey == "" {
    postSystemNotification(..., "Agent is not configured: missing API key.")
    return nil
}

// Call runner.
text, err := s.Runner.Run(ctx, buildPrompt(trigger), cfg)
if err != nil {
    postSystemNotification(..., "Agent reply failed: "+redactKey(err.Error()))
    return nil
}

// Insert agent's reply message.
reply, err := s.Queries.CreateMessage(ctx, db.CreateMessageParams{
    // ... sender = agent, channel/recipient = trigger's, content = text, ...
})
// ... broadcast via Hub
```

`AutoReplyService` struct gains a field:

```go
type AutoReplyService struct {
    Queries *db.Queries
    Hub     *realtime.Hub
    Runner  agent_runner.AgentRunner   // NEW
}
```

`postSystemNotification` inserts a message with `sender_id = agent.id`, `sender_type = "agent"`, `content_type = "text"`, `content = text`, and sets `metadata = {"system_notification": true}` so UIs can render it distinctly later. Broadcasts normally through the Hub. The existing `Message.content_type` enum (`"text" | "json" | "file"`) is not extended in this sub-project.

### 3.5 Desktop client — `getOrCreateSystemAgent` + bootstrap

`packages/client-core/src/desktop-api-client.ts`:

```ts
async getOrCreateSystemAgent(): Promise<{ id: string; name: string; agent_type: string }> {
    return this.request("/api/system-agent");
}
```

`apps/desktop/src/lib/desktop-client.ts` — inside `bootstrapDesktopApp`, after `useDesktopWorkspaceStore.getState().bootstrap(storedWorkspaceId)` and before `ensureWSClient().connect()`:

```ts
void desktopApi.getOrCreateSystemAgent().catch((err) => {
    console.warn("[bootstrap] ensure system agent failed:", err);
});
```

Fire-and-forget. The sidebar's agent list refreshes on next workspace poll or on-mount reload (existing behavior).

## 4. Configuration

### 4.1 Environment variables

Added to `.env.example` and documented in `README.md`:

```bash
# Agent LLM kernel (personal agent reply runtime)
AGENT_KERNEL=openai_compat                             # "openai_compat" | "anthropic"
AGENT_LLM_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
AGENT_LLM_API_KEY=sk-...                               # Bailian key or Anthropic key
AGENT_LLM_MODEL=qwen-plus                              # e.g. qwen-plus / qwen-max / claude-sonnet-4-5
AGENT_SYSTEM_PROMPT=                                   # optional; overrides Python default
AGENT_RUNNER_PYTHON=python3                            # optional override
AGENT_RUNNER_SCRIPT_PATH=                              # optional override for production
```

Never include `AGENT_LLM_API_KEY` in sample values in `.env.example` — leave it blank so devs set their own.

### 4.2 Python runtime

- `server/agent_runner/requirements.txt` pins `claude-agent-sdk>=0.1.0`.
- `Makefile`: new target `setup-agent-runner` that runs `pip install -r server/agent_runner/requirements.txt` (with `python3 -m pip` for safety).
- `make setup` calls `setup-agent-runner` as part of first-time setup; prints a clear message if `python3` not found.
- Production deployment (Docker) is **out of MVP scope** — a follow-up ticket covers adding Python to the Dockerfile.

### 4.3 Script discoverability

In dev, `runtime.Caller(0)` in `NewRunner` resolves `claude_reply.py` relative to the Go source. In binary deployment, operators set `AGENT_RUNNER_SCRIPT_PATH` explicitly. MVP does not bundle the Python file into the Go binary.

## 5. Error handling

Each failure surface produces a visible `system_notification` message in the conversation so the user understands what happened:

| Failure | Detection | User-visible message |
|---|---|---|
| `cloud_llm_config.api_key` empty | Pre-check in `replyAsMentionedAgent` | `"Agent is not configured: missing API key."` |
| `python3` not on PATH | `cmd.Start()` returns `exec: "python3"` error | `"Agent runtime unavailable: python3 not installed on server."` |
| `claude_agent_sdk` not installed | Python exits 2 with `{"type":"error","message":"claude-agent-sdk not installed"}` | `"Agent runtime unavailable: claude-agent-sdk not installed."` |
| LLM 401/429/5xx/timeout | SDK raises; Python exits 1 with error event | `"Agent reply failed: <SDK error message>"` (api keys redacted) |
| 60s timeout | `ctx` DeadlineExceeded; process killed | `"Agent reply timed out after 60s."` |
| Python emits `done` with empty text | Runner returns `errors.New("runner finished with no reply text")` | `"Agent returned empty reply."` |
| Non-JSON stdout noise | Runner silently skips `json.Unmarshal` errors | (no user-visible impact) |

API-key redaction: in any error string or log, matches against `/sk-[a-zA-Z0-9\-]{8,}/` and replaces with `sk-***`.

## 6. Testing

### 6.1 Go unit tests — `server/agent_runner/runner_test.go`

Uses `testdata/fake_runner.sh` (bash script) instead of real Python. Bash is universally available on CI.

Cases:

| Test | Fake behavior | Expected |
|---|---|---|
| `TestRun_Success` | Emit `{"type":"done","text":"echo: <prompt>"}`, exit 0 | Returns `"echo: hello"`, nil error |
| `TestRun_EmitsError` | Emit `{"type":"error","message":"missing sdk"}`, exit 2 | Error contains `"missing sdk"` |
| `TestRun_NoDoneEvent` | Emit status only, exit 0 | Error `"runner finished with no reply text"` |
| `TestRun_Timeout` | `sleep 10` and no output | `Runner.Timeout = 100ms` → error `"timed out"` |
| `TestRun_EnvInjection_OpenAI` | Emit `{"type":"done","text":"$OPENAI_API_KEY/$OPENAI_MODEL"}` | Config flows into `OPENAI_*` env |
| `TestRun_EnvInjection_Anthropic` | Same as above for `ANTHROPIC_*` | Config flows into `ANTHROPIC_*` when `Kernel="anthropic"` |
| `TestRun_MixedNoisyStdout` | Emit a non-JSON line + a `done` line | Non-JSON ignored, `done` text returned |

### 6.2 Python integration test — `server/agent_runner/claude_reply_test.py`

Uses `pytest.importorskip("claude_agent_sdk")` to skip when SDK is not installed. Single shallow test: subprocess runs, exits with a JSON event on stdout. Does not call the real LLM.

Not run in CI by default; a manual `make test-agent-runner` target covers it.

### 6.3 AutoReplyService unit tests — `server/internal/service/auto_reply_test.go` (new)

Uses a mock `AgentRunner` (interface, not struct) injected into `AutoReplyService`.

Cases:

| Test | Coverage |
|---|---|
| `TestReplyAsMentionedAgent_DispatchesToRunner` | Config parsed from agent.cloud_llm_config; runner called with correct Config; reply written to DB and broadcast |
| `TestReplyAsMentionedAgent_NoAPIKey` | Config key empty → system_notification posted; runner not called |
| `TestReplyAsMentionedAgent_RunnerError` | Mock runner returns error → system_notification contains the error message (redacted) |
| `TestReplyAsMentionedAgent_AgentNotFound` | Mention name doesn't match any agent → silent skip (no notification) |
| `TestReplyAsMentionedAgent_OnMentionDisabled` | Agent has `on_mention: false` → silent skip (matches existing semantics) |

### 6.4 EnsurePersonalAgent unit tests — `server/internal/service/personal_agent_test.go` (new)

Uses test database (same pattern as other handler tests).

Cases:

| Test | Coverage |
|---|---|
| `TestEnsurePersonalAgent_SnapshotsServerEnv` | Env set → `cloud_llm_config` matches env fields |
| `TestEnsurePersonalAgent_EmptyEnvAllowed` | Env unset → agent created with empty fields; warn log emitted |
| `TestEnsurePersonalAgent_Idempotent` | Second call returns existing agent, does not create duplicate |
| `TestEnsurePersonalAgent_SwitchesKernelBasedOnEnv` | `AGENT_KERNEL=anthropic` → snapshot reflects that |

### 6.5 Desktop / client-core tests

| Test | Coverage |
|---|---|
| `DesktopApiClient.getOrCreateSystemAgent` | fetch mock; GET /api/system-agent; returns parsed body |
| `bootstrapDesktopApp` calls `getOrCreateSystemAgent` after workspace bootstrap | Verify call sequence; failure does not block |

### 6.6 Manual acceptance (must pass before merge)

1. Set `.env` with real Bailian key → restart backend → server logs no warns.
2. Fresh workspace opens desktop → login → sidebar shows `🤖 <User>'s Assistant`.
3. `DELETE FROM agent WHERE ...` then restart desktop → 6 agents re-materialize (1 system + 4 page + 1 personal).
4. New DM → Assistant → type `你好` → reply arrives within ~5s via WS.
5. `@Assistant 写首诗` in a channel → reply streams into the channel via WS.
6. Set `AGENT_LLM_API_KEY=invalid` → `@Assistant` → `system_notification`: `"Agent reply failed: 401 ..."`.
7. Unset `AGENT_LLM_API_KEY` → `@Assistant` → `system_notification`: `"Agent is not configured: missing API key."`
8. Shadow `python3` out of PATH → `@Assistant` → `system_notification`: `"Agent runtime unavailable: python3 not installed on server."`

### 6.7 Acceptance signals

- `make test` green (Go tests incl. new auto_reply / personal_agent / agent_runner suites).
- `pnpm --filter @myteam/client-core test` and `pnpm --filter @myteam/desktop test` green.
- Manual acceptance list 1–8 all pass.
- Python integration test (`make test-agent-runner`) green **on a developer machine with the SDK installed** — not gated in CI.

## 7. Out-of-scope / deferred follow-ups

| Item | Reason to defer |
|---|---|
| Process pool / daemon Python worker | Per-reply cold start is acceptable for MVP; revisit if metrics show >2s p50 |
| Docker / CI Python + SDK installation | Dev-only for MVP; infra ticket |
| Streaming tokens via WS (vs `done` batch) | Single message per reply matches current WS event shape; optimization |
| Subagents / tool use / skill registry | Belongs to sub-project #3 (Project execution) |
| Multi-turn conversation state | Requires session plumbing beyond MVP |
| Per-user / per-agent API keys | Requires Settings UI; sub-project pending |
| Fallback to DashScope HTTP direct | Out of kernel-swap scope; revisit if Claude SDK stability becomes an issue |
| Claude → DashScope fallback on error | Same as above |
| Hardened prompt sanitization | Low value at `max_turns=1` with no tools; revisit when tools land |

## 8. Migration ordering (hint for the plan author)

Suggested task decomposition order for the implementation plan:

1. Add `server/agent_runner/` package (Python script + Go Runner + testdata/fake_runner.sh + runner_test.go).
2. Refactor `AutoReplyService`: introduce `AgentRunner` interface dependency; rewrite `replyAsMentionedAgent` to use it.
3. Rewrite `EnsurePersonalAgent` to snapshot env vars into `cloud_llm_config`.
4. Add `postSystemNotification` helper in auto_reply.go.
5. Add `auto_reply_test.go` and `personal_agent_test.go`.
6. Wire `agent_runner.NewRunner()` into `AutoReplyService` in `cmd/server/main.go`.
7. Add `getOrCreateSystemAgent` method to `DesktopApiClient`.
8. Call it from `bootstrapDesktopApp`; add client-core test.
9. Update `.env.example`, `Makefile` (`setup-agent-runner`), `README.md`.
10. Full verification: `make test`, `pnpm test`, manual acceptance list.
