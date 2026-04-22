# Account Module Phase 1 — Additive Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish the additive foundation for the Account refactor — code-level Provider Registry, extended Runtime/Agent/Message schemas with new columns, and frontend type/API parity. ZERO breaking changes; old columns and old code paths keep working.

**Architecture:**
- **Provider** lives in code (`server/pkg/provider/registry.go`), exposed via `GET /api/providers`. Not a DB table.
- **Runtime (`agent_runtime`)** gains `concurrency_limit`, `current_load`, `lease_expires_at`, `last_heartbeat_at`; status CHECK expanded to include `degraded`. Old `runtime_mode` and `last_seen_at` columns preserved during transition.
- **Agent (`agent`)** gains `scope` (mirrors `page_scope` during transition) and `owner_type`. Old fields untouched.
- **Message (`message`)** gains `effective_actor_id/type` + `real_operator_id/type`. Old `is_impersonated` preserved.
- **Frontend** gets new types and a Provider list page; existing UI keeps using legacy fields.

**Tech Stack:** Go 1.26 (Chi router, sqlc, pgx), PostgreSQL (pgvector/pg17), Next.js 16 App Router, TypeScript, Zustand.

**Reference PRD:** `/Users/chauncey2025/Documents/Obsidian Vault/2026-04-16-account-restructure-prd.md`

---

## Architecture & File Structure

### New files
| File | Responsibility |
|---|---|
| `server/pkg/provider/registry.go` | Code-level Provider registry (claude/codex/opencode/cloud_llm) with `List()` / `Get()` / `Validate()` |
| `server/pkg/provider/registry_test.go` | Unit tests for the registry |
| `server/internal/handler/provider.go` | `GET /api/providers` handler returning serialized registry entries |
| `server/migrations/048_account_phase1.up.sql` | Combined migration: runtime extensions + agent.scope/owner_type + message audit columns |
| `server/migrations/048_account_phase1.down.sql` | Reversible rollback |
| `apps/web/shared/types/provider.ts` | `Provider`, `ProviderKind` TS types |
| `apps/web/features/runtimes/components/provider-list.tsx` | Provider list UI fragment |

### Modified files
| File | Why |
|---|---|
| `server/pkg/db/queries/runtime.sql` | sqlc queries for new Runtime columns |
| `server/pkg/db/queries/agent.sql` | sqlc queries for new Agent columns (scope, owner_type) |
| `server/pkg/db/queries/messages.sql` | sqlc queries reading/writing new audit columns |
| `server/internal/handler/runtime.go` | Surface new fields in JSON; accept defaults during create/register |
| `server/internal/handler/agent.go` | Surface new fields; sync `scope` from `page_scope` on read |
| `server/internal/handler/message.go` | Default audit columns from existing sender info |
| `server/cmd/server/router.go` | Mount `GET /api/providers` |
| `apps/web/shared/types/agent.ts` | Extend `RuntimeDevice` and `Agent` types with new optional fields |
| `apps/web/shared/types/messaging.ts` | Add audit fields to `Message` type |
| `apps/web/shared/api/client.ts` (or `apps/web/shared/api/index.ts`) | Add `listProviders()` method |

---

### Task 1: Create Provider Registry (Backend)

**Files:**
- Create: `server/pkg/provider/registry.go`
- Test: `server/pkg/provider/registry_test.go`

- [ ] **Step 1: Write the failing test**

Create `server/pkg/provider/registry_test.go`:

```go
package provider

import (
	"testing"
)

func TestRegistryHasFourProviders(t *testing.T) {
	all := List()
	if len(all) != 4 {
		t.Fatalf("expected 4 providers, got %d", len(all))
	}
	want := map[string]bool{"claude": false, "codex": false, "opencode": false, "cloud_llm": false}
	for _, p := range all {
		want[p.Key] = true
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("missing provider %q", k)
		}
	}
}

func TestGetReturnsSpec(t *testing.T) {
	spec, ok := Get("claude")
	if !ok {
		t.Fatal("expected claude to exist")
	}
	if spec.Kind != KindLocalCLI {
		t.Errorf("expected LocalCLI, got %v", spec.Kind)
	}
	if spec.Executable != "claude" {
		t.Errorf("expected executable 'claude', got %q", spec.Executable)
	}
}

func TestGetReturnsFalseForUnknown(t *testing.T) {
	if _, ok := Get("not-a-provider"); ok {
		t.Fatal("expected unknown provider to return ok=false")
	}
}

func TestValidateAcceptsKnownProvider(t *testing.T) {
	if err := Validate("codex"); err != nil {
		t.Errorf("expected codex to validate, got %v", err)
	}
}

func TestValidateRejectsUnknownProvider(t *testing.T) {
	if err := Validate("legacy_local"); err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestCloudLLMHasNoExecutable(t *testing.T) {
	spec, _ := Get("cloud_llm")
	if spec.Kind != KindCloudAPI {
		t.Errorf("expected CloudAPI kind, got %v", spec.Kind)
	}
	if spec.Executable != "" {
		t.Errorf("expected empty executable, got %q", spec.Executable)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691/server
go test ./pkg/provider/...
```

Expected: build error (`package provider has no Go files` or similar).

- [ ] **Step 3: Write the registry implementation**

Create `server/pkg/provider/registry.go`:

```go
// Package provider declares the static set of execution providers the
// platform knows about. Adding a provider requires a code change because
// every provider needs a corresponding Backend implementation and Daemon
// detection logic.
package provider

import "fmt"

type Kind string

const (
	KindLocalCLI Kind = "local_cli"
	KindCloudAPI Kind = "cloud_api"
)

type Spec struct {
	Key             string   `json:"key"`
	DisplayName     string   `json:"display_name"`
	Kind            Kind     `json:"kind"`
	Executable      string   `json:"executable,omitempty"`
	SupportedModels []string `json:"supported_models,omitempty"`
	DefaultModel    string   `json:"default_model,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
}

var registry = map[string]Spec{
	"claude": {
		Key:             "claude",
		DisplayName:     "Claude Code",
		Kind:            KindLocalCLI,
		Executable:      "claude",
		SupportedModels: []string{"claude-opus-4-7", "claude-sonnet-4-6", "claude-haiku-4-5"},
		DefaultModel:    "claude-sonnet-4-6",
		Capabilities:    []string{"code", "tools", "mcp"},
	},
	"codex": {
		Key:             "codex",
		DisplayName:     "Codex",
		Kind:            KindLocalCLI,
		Executable:      "codex",
		SupportedModels: []string{"gpt-5.4"},
		DefaultModel:    "gpt-5.4",
		Capabilities:    []string{"code", "tools"},
	},
	"opencode": {
		Key:          "opencode",
		DisplayName:  "OpenCode",
		Kind:         KindLocalCLI,
		Executable:   "opencode",
		Capabilities: []string{"code"},
	},
	"cloud_llm": {
		Key:          "cloud_llm",
		DisplayName:  "Cloud LLM",
		Kind:         KindCloudAPI,
		Capabilities: []string{"chat", "tools"},
	},
}

// List returns all registered providers in deterministic order.
func List() []Spec {
	keys := []string{"claude", "codex", "opencode", "cloud_llm"}
	out := make([]Spec, 0, len(keys))
	for _, k := range keys {
		out = append(out, registry[k])
	}
	return out
}

// Get returns a single provider spec by key.
func Get(key string) (Spec, bool) {
	s, ok := registry[key]
	return s, ok
}

// Validate returns an error if the key is not a registered provider.
func Validate(key string) error {
	if _, ok := registry[key]; !ok {
		return fmt.Errorf("provider %q is not registered", key)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691/server
go test ./pkg/provider/...
```

Expected: `ok  myteam/server/pkg/provider`. (Module path may differ — confirm by reading `server/go.mod` first.)

- [ ] **Step 5: Commit**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
git add server/pkg/provider/
git commit -m "feat(provider): add code-level provider registry"
```

---

### Task 2: Migration — Account Phase 1 schema

**Files:**
- Create: `server/migrations/048_account_phase1.up.sql`
- Create: `server/migrations/048_account_phase1.down.sql`

- [ ] **Step 1: Write the up migration**

Create `server/migrations/048_account_phase1.up.sql`:

```sql
-- Account Phase 1: additive schema changes only.
-- Old columns are preserved so that existing code keeps working.

-- ===== Runtime extensions =====
ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS concurrency_limit  INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS current_load       INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS lease_expires_at   TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_heartbeat_at  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS mode               TEXT;

-- Backfill new columns from legacy fields without dropping the originals.
UPDATE agent_runtime SET last_heartbeat_at = last_seen_at WHERE last_heartbeat_at IS NULL;
UPDATE agent_runtime SET mode = runtime_mode WHERE mode IS NULL;

-- Expand status enum to include 'degraded'.
ALTER TABLE agent_runtime DROP CONSTRAINT IF EXISTS agent_runtime_status_check;
ALTER TABLE agent_runtime
    ADD CONSTRAINT agent_runtime_status_check
    CHECK (status IN ('online', 'offline', 'degraded'));

-- New CHECK on mode (allows NULL during transition; enforced after backfill below).
ALTER TABLE agent_runtime
    ADD CONSTRAINT agent_runtime_mode_check
    CHECK (mode IS NULL OR mode IN ('local', 'cloud'));

CREATE INDEX IF NOT EXISTS idx_agent_runtime_lease
    ON agent_runtime(lease_expires_at)
    WHERE lease_expires_at IS NOT NULL;

-- ===== Agent extensions =====
ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS scope       TEXT,
    ADD COLUMN IF NOT EXISTS owner_type  TEXT;

-- Mirror page_scope into scope so reads can move first.
UPDATE agent SET scope = page_scope WHERE scope IS NULL AND page_scope IS NOT NULL;

-- Backfill owner_type from current data.
UPDATE agent SET owner_type = 'organization'
    WHERE owner_type IS NULL AND (agent_type = 'system_agent' OR agent_type = 'page_system_agent' OR is_system = TRUE);
UPDATE agent SET owner_type = 'user'
    WHERE owner_type IS NULL AND owner_id IS NOT NULL;

ALTER TABLE agent
    ADD CONSTRAINT agent_owner_type_check
    CHECK (owner_type IS NULL OR owner_type IN ('user', 'organization'));

ALTER TABLE agent
    ADD CONSTRAINT agent_scope_values_check
    CHECK (scope IS NULL OR scope IN ('account', 'session', 'conversation', 'project', 'file'));

-- ===== Message audit columns =====
ALTER TABLE message
    ADD COLUMN IF NOT EXISTS effective_actor_id   UUID,
    ADD COLUMN IF NOT EXISTS effective_actor_type TEXT,
    ADD COLUMN IF NOT EXISTS real_operator_id     UUID,
    ADD COLUMN IF NOT EXISTS real_operator_type   TEXT;

-- Backfill from existing sender_id / sender_type for non-impersonated messages.
UPDATE message
SET effective_actor_id = sender_id,
    effective_actor_type = sender_type,
    real_operator_id = sender_id,
    real_operator_type = sender_type
WHERE effective_actor_id IS NULL;

ALTER TABLE message
    ADD CONSTRAINT message_effective_actor_type_check
    CHECK (effective_actor_type IS NULL OR effective_actor_type IN ('member', 'agent', 'system')),
    ADD CONSTRAINT message_real_operator_type_check
    CHECK (real_operator_type IS NULL OR real_operator_type IN ('member', 'agent', 'system'));
```

- [ ] **Step 2: Write the down migration**

Create `server/migrations/048_account_phase1.down.sql`:

```sql
-- Reverse Account Phase 1 additive changes.

ALTER TABLE message
    DROP CONSTRAINT IF EXISTS message_effective_actor_type_check,
    DROP CONSTRAINT IF EXISTS message_real_operator_type_check,
    DROP COLUMN IF EXISTS effective_actor_id,
    DROP COLUMN IF EXISTS effective_actor_type,
    DROP COLUMN IF EXISTS real_operator_id,
    DROP COLUMN IF EXISTS real_operator_type;

ALTER TABLE agent
    DROP CONSTRAINT IF EXISTS agent_owner_type_check,
    DROP CONSTRAINT IF EXISTS agent_scope_values_check,
    DROP COLUMN IF EXISTS scope,
    DROP COLUMN IF EXISTS owner_type;

DROP INDEX IF EXISTS idx_agent_runtime_lease;
ALTER TABLE agent_runtime
    DROP CONSTRAINT IF EXISTS agent_runtime_mode_check;
ALTER TABLE agent_runtime DROP CONSTRAINT IF EXISTS agent_runtime_status_check;
ALTER TABLE agent_runtime
    ADD CONSTRAINT agent_runtime_status_check
    CHECK (status IN ('online', 'offline'));
ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS concurrency_limit,
    DROP COLUMN IF EXISTS current_load,
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS last_heartbeat_at,
    DROP COLUMN IF EXISTS mode;
```

- [ ] **Step 3: Apply the migration**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make migrate-up
```

Expected: migration `048_account_phase1` applied without error. If a worktree-specific DB is in use, run `make migrate-up` after sourcing `.env.worktree` per project conventions.

- [ ] **Step 4: Verify schema**

```bash
psql "$DATABASE_URL" -c "\d agent_runtime" | grep -E 'concurrency_limit|current_load|lease_expires_at|last_heartbeat_at|mode'
psql "$DATABASE_URL" -c "\d agent" | grep -E 'scope|owner_type'
psql "$DATABASE_URL" -c "\d message" | grep -E 'effective_actor|real_operator'
```

Expected: all new columns appear.

- [ ] **Step 5: Commit**

```bash
git add server/migrations/048_account_phase1.up.sql server/migrations/048_account_phase1.down.sql
git commit -m "feat(db): add account phase 1 additive columns"
```

---

### Task 3: sqlc queries for new columns

**Files:**
- Modify: `server/pkg/db/queries/runtime.sql`
- Modify: `server/pkg/db/queries/agent.sql`
- Modify: `server/pkg/db/queries/messages.sql`

- [ ] **Step 1: Read existing query files first**

```bash
cat server/pkg/db/queries/runtime.sql
cat server/pkg/db/queries/agent.sql
cat server/pkg/db/queries/messages.sql
```

The goal is to (a) include the new columns in `SELECT *`-equivalent queries and (b) add new named queries that use them.

- [ ] **Step 2: Append new queries to `runtime.sql`**

Add at the end of `server/pkg/db/queries/runtime.sql`:

```sql
-- name: UpdateRuntimeHeartbeat :exec
UPDATE agent_runtime
SET last_heartbeat_at = now(),
    last_seen_at      = now(),
    status            = COALESCE(sqlc.narg('status'), status),
    updated_at        = now()
WHERE id = $1;

-- name: SetRuntimeLoad :exec
UPDATE agent_runtime
SET current_load = $2,
    updated_at   = now()
WHERE id = $1;

-- name: AcquireRuntimeLease :exec
UPDATE agent_runtime
SET lease_expires_at = $2,
    updated_at       = now()
WHERE id = $1;
```

- [ ] **Step 3: Append new queries to `agent.sql`**

Add at the end of `server/pkg/db/queries/agent.sql`:

```sql
-- name: SetAgentScope :exec
UPDATE agent
SET scope      = sqlc.narg('scope'),
    page_scope = sqlc.narg('page_scope'),
    updated_at = now()
WHERE id = $1;

-- name: SetAgentOwnerType :exec
UPDATE agent
SET owner_type = $2,
    updated_at = now()
WHERE id = $1;
```

- [ ] **Step 4: Append new queries to `messages.sql`**

Add at the end of `server/pkg/db/queries/messages.sql`:

```sql
-- name: InsertMessageWithAudit :one
INSERT INTO message (
    id, workspace_id, channel_id, thread_id, session_id,
    sender_id, sender_type, body, metadata,
    is_impersonated,
    effective_actor_id, effective_actor_type,
    real_operator_id, real_operator_type,
    created_at
) VALUES (
    gen_random_uuid(), $1, $2, $3, $4,
    $5, $6, $7, $8,
    $9,
    $10, $11,
    $12, $13,
    now()
)
RETURNING *;
```

If `messages.sql` already has an `InsertMessage` query, leave it alone — `InsertMessageWithAudit` is the new path callers should migrate to over time.

- [ ] **Step 5: Regenerate sqlc**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make sqlc
```

Expected: `server/pkg/db/generated/` updated, no errors.

- [ ] **Step 6: Verify Go compiles**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691/server
go build ./...
```

Expected: clean build. If existing handler code references field names that changed because sqlc inferred a different shape, fix those references inline before continuing.

- [ ] **Step 7: Commit**

```bash
git add server/pkg/db/queries/ server/pkg/db/generated/
git commit -m "feat(sqlc): queries for runtime/agent/message phase 1 columns"
```

---

### Task 4: Provider HTTP endpoint

**Files:**
- Create: `server/internal/handler/provider.go`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Write the failing test**

Create `server/internal/handler/provider_test.go`:

```go
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListProvidersReturnsRegistry(t *testing.T) {
	h := &ProviderHandler{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	h.List(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body.String())
	}

	var got []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Errorf("expected 4 providers, got %d", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691/server
go test ./internal/handler/ -run TestListProvidersReturnsRegistry
```

Expected: build error referencing `ProviderHandler`.

- [ ] **Step 3: Implement the handler**

Create `server/internal/handler/provider.go`:

```go
package handler

import (
	"encoding/json"
	"net/http"

	"<MODULE>/pkg/provider"
)

// ProviderHandler exposes the static Provider registry.
// Replace <MODULE> above with the module path declared in server/go.mod.
type ProviderHandler struct{}

func NewProviderHandler() *ProviderHandler {
	return &ProviderHandler{}
}

func (h *ProviderHandler) List(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(provider.List())
}
```

Confirm the module path by reading `server/go.mod` and substituting it in for `<MODULE>` (likely `github.com/...`).

- [ ] **Step 4: Mount the route in `router.go`**

In `server/cmd/server/router.go`, find the section where public (non-authenticated) routes are registered or where other read-only catalog endpoints live, and add:

```go
providerHandler := handler.NewProviderHandler()
r.Get("/api/providers", providerHandler.List)
```

If the project gates everything behind auth, mount it under the protected group instead — the registry is public information but matching project convention is more important.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/handler/ -run TestListProvidersReturnsRegistry
go build ./...
```

Expected: PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/provider.go server/internal/handler/provider_test.go server/cmd/server/router.go
git commit -m "feat(api): GET /api/providers exposes registry"
```

---

### Task 5: Runtime handler — surface new fields

**Files:**
- Modify: `server/internal/handler/runtime.go` (or whichever handler file owns `agent_runtime` HTTP routes)

- [ ] **Step 1: Locate the runtime read/write paths**

```bash
grep -rn "agent_runtime\|RuntimeDevice\|GetRuntime\|ListRuntimes" server/internal/handler/
```

Identify the handler functions that serialize Runtime rows to JSON.

- [ ] **Step 2: Extend the JSON response struct**

Find the response struct (likely in the same file). Add new fields, mirroring the new SQL columns:

```go
type runtimeResponse struct {
	// ...existing fields stay unchanged...
	Mode             *string    `json:"mode,omitempty"`
	ConcurrencyLimit int32      `json:"concurrency_limit"`
	CurrentLoad      int32      `json:"current_load"`
	LeaseExpiresAt   *time.Time `json:"lease_expires_at,omitempty"`
	LastHeartbeatAt  *time.Time `json:"last_heartbeat_at,omitempty"`
}
```

In the conversion code, populate these from the sqlc row.

- [ ] **Step 3: Default new fields on Daemon registration**

In the runtime registration handler (the one Daemons call to announce themselves), if `concurrency_limit` is missing from the request body, default it to `1`. If `mode` is missing, fall back to `runtime_mode`. Old Daemons that don't send the new fields keep working.

- [ ] **Step 4: Build and run existing tests**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691/server
go build ./...
go test ./internal/handler/...
```

Expected: clean build; existing tests pass.

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/
git commit -m "feat(runtime): surface phase 1 fields in HTTP responses"
```

---

### Task 6: Frontend — Provider type + API method

**Files:**
- Create: `apps/web/shared/types/provider.ts`
- Modify: `apps/web/shared/types/index.ts` (re-export the new type)
- Modify: `apps/web/shared/api/index.ts` (or wherever `api` singleton's methods are defined)

- [ ] **Step 1: Write the type**

Create `apps/web/shared/types/provider.ts`:

```typescript
export type ProviderKind = "local_cli" | "cloud_api";

export interface Provider {
  key: string;
  display_name: string;
  kind: ProviderKind;
  executable?: string;
  supported_models?: string[];
  default_model?: string;
  capabilities?: string[];
}
```

- [ ] **Step 2: Re-export from the types barrel**

Open `apps/web/shared/types/index.ts` and add:

```typescript
export * from "./provider";
```

(If the file uses individual named re-exports rather than `export *`, follow that pattern instead.)

- [ ] **Step 3: Add API method**

Find `apps/web/shared/api/` — likely an `ApiClient` class. Add:

```typescript
async listProviders(): Promise<Provider[]> {
  return this.get<Provider[]>("/api/providers");
}
```

Import `Provider` from `@/shared/types`.

- [ ] **Step 4: Typecheck**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
pnpm typecheck
```

Expected: no new errors.

- [ ] **Step 5: Commit**

```bash
git add apps/web/shared/types/ apps/web/shared/api/
git commit -m "feat(web): Provider type and listProviders() API"
```

---

### Task 7: Frontend — extend Agent and Runtime types

**Files:**
- Modify: `apps/web/shared/types/agent.ts`
- Modify: `apps/web/shared/types/messaging.ts`

- [ ] **Step 1: Extend `RuntimeDevice` and `Agent`**

In `apps/web/shared/types/agent.ts`:

Replace the `RuntimeDevice` interface (lines 29–47 in the current file) with:

```typescript
export interface RuntimeDevice {
  id: string;
  workspace_id: string;
  daemon_id: string | null;
  name: string;
  runtime_mode: AgentRuntimeMode; // legacy, kept during transition
  mode?: AgentRuntimeMode;        // new canonical field
  provider: string;
  status: "online" | "offline" | "degraded";
  device_info: string;
  metadata: Record<string, unknown>;
  last_seen_at: string | null;     // legacy
  last_heartbeat_at?: string | null;
  concurrency_limit?: number;
  current_load?: number;
  lease_expires_at?: string | null;
  created_at: string;
  updated_at: string;
  server_host?: string;
  working_dir?: string;
  capabilities?: string[];
  readiness?: string;
  last_heartbeat?: string;
}
```

Then in the `Agent` interface, add:

```typescript
scope?: PageAgentScope | "conversation" | null;
owner_type?: "user" | "organization";
```

Right after `page_scope` is fine.

- [ ] **Step 2: Extend `Message` type**

Open `apps/web/shared/types/messaging.ts`. Find the `Message` interface and add:

```typescript
effective_actor_id?: string | null;
effective_actor_type?: "member" | "agent" | "system" | null;
real_operator_id?: string | null;
real_operator_type?: "member" | "agent" | "system" | null;
```

- [ ] **Step 3: Typecheck**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
pnpm typecheck
```

Expected: no errors. Existing components keep working because all new fields are optional.

- [ ] **Step 4: Commit**

```bash
git add apps/web/shared/types/agent.ts apps/web/shared/types/messaging.ts
git commit -m "feat(web): extend Agent/Runtime/Message types for phase 1"
```

---

### Task 8: Frontend — Provider list UI fragment

**Files:**
- Create: `apps/web/features/runtimes/components/provider-list.tsx`

This task adds a small reusable component. It does NOT yet wire into a page — page integration is left for Plan 2 once the cleanup happens.

- [ ] **Step 1: Check whether `features/runtimes/` exists**

```bash
ls apps/web/features/ | grep -i runtime
```

If `features/runtimes/` does not exist, create the directory:

```bash
mkdir -p apps/web/features/runtimes/components
```

- [ ] **Step 2: Write the component**

Create `apps/web/features/runtimes/components/provider-list.tsx`:

```tsx
"use client";

import { useEffect, useState } from "react";
import { api } from "@/shared/api";
import type { Provider } from "@/shared/types/provider";

export function ProviderList() {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.listProviders()
      .then(setProviders)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : "Failed"));
  }, []);

  if (error) {
    return <p className="text-destructive text-sm">{error}</p>;
  }
  if (providers.length === 0) {
    return <p className="text-muted-foreground text-sm">Loading providers…</p>;
  }
  return (
    <ul className="divide-y rounded-md border">
      {providers.map((p) => (
        <li key={p.key} className="flex items-center justify-between p-3">
          <div>
            <div className="font-medium">{p.display_name}</div>
            <div className="text-muted-foreground text-xs">
              {p.kind === "local_cli" ? `CLI: ${p.executable}` : "Cloud API"}
            </div>
          </div>
          {p.default_model && (
            <span className="text-muted-foreground text-xs">{p.default_model}</span>
          )}
        </li>
      ))}
    </ul>
  );
}
```

- [ ] **Step 3: Typecheck**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
pnpm typecheck
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add apps/web/features/runtimes/
git commit -m "feat(web): ProviderList component (unwired)"
```

---

### Task 9: End-to-end verification

- [ ] **Step 1: Full backend build + tests**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make test
```

Expected: all Go tests pass.

- [ ] **Step 2: Frontend typecheck + unit tests**

```bash
pnpm typecheck
pnpm test
```

Expected: PASS.

- [ ] **Step 3: Smoke-test the live API**

In one terminal:
```bash
make dev
```

In another:
```bash
curl -s http://localhost:8080/api/providers | jq
```

Expected: a JSON array of 4 providers (claude / codex / opencode / cloud_llm). If the route is gated by auth, fetch a session token first.

- [ ] **Step 4: Confirm migration is recorded**

```bash
psql "$DATABASE_URL" -c "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 3;"
```

Expected: `048` is at the top.

- [ ] **Step 5: Final commit (if anything was tweaked)**

If steps 1-4 surfaced fixes, commit them now with a message describing what was tightened.

---

## Self-Review Checklist

Before declaring this plan complete:

- [ ] Provider Registry has 4 entries, exposed via `GET /api/providers`, frontend can call it.
- [ ] `agent_runtime` table has `concurrency_limit`, `current_load`, `lease_expires_at`, `last_heartbeat_at`, `mode`, plus `degraded` in status CHECK.
- [ ] `agent` table has `scope` and `owner_type` populated by backfill.
- [ ] `message` table has `effective_actor_*` and `real_operator_*` populated for existing rows.
- [ ] `pnpm typecheck`, `pnpm test`, `make test` all green.
- [ ] No legacy column was dropped or renamed (those changes belong in Plan 2).
- [ ] Daemon registration still works without sending the new fields.

---

## Out of Scope (deferred to Plan 2)

- Dropping `is_system`, `page_scope`, `runtime_config`, `cloud_llm_config`, `capabilities`, `tools`, `triggers`, `system_config`, `agent_metadata`, `accessible_files_scope`, `allowed_channels_scope` from `agent`.
- Merging `online_status + workload_status` into a single `status` field with 7 values.
- Renaming `last_seen_at → last_heartbeat_at` (drop the legacy column).
- Renaming `runtime_mode → mode` (drop the legacy column).
- RBAC handler guards (`requireAgentOwner`, `requireAdminOrAbove`).
- `page_system_agent` data migration to `system_agent + scope` and CHECK update.
- Frontend removal of `page_system_agent` references and unifying the `AgentType` union.
