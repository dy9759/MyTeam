# Account Module Phase 2 — Slim Agent + RBAC Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Finish the Account refactor — collapse `system_agent + page_system_agent` into one type, merge `online_status + workload_status → status`, drop redundant Agent/Runtime columns, add RBAC permission guards.

**Architecture:**
- Migration 049: data migration (page_system_agent → system_agent + scope; status merge; provider value normalization).
- Migration 050: drop deprecated columns (`is_system`, `page_scope`, `runtime_config`, `cloud_llm_config`, `capabilities`, `tools`, `triggers`, `system_config`, `agent_metadata`, `accessible_files_scope`, `allowed_channels_scope`, `online_status`, `workload_status`, `last_seen_at`, `runtime_mode`).
- New helper: `server/internal/auth/rbac.go` with `requireAgentOwner / requireAdminOrAbove / requireOwner`.
- Handlers: apply guards on Agent/SystemAgent/Runtime/IdentityCard endpoints.
- Frontend: collapse `AgentType` union, drop `page_scope` references, switch reads to `scope`, status union to single 7-value enum.

**Tech Stack:** Same as Plan 1 — Go 1.26, PostgreSQL, Next.js 16, TypeScript.

**Reference PRD:** `/Users/chauncey2025/Documents/Obsidian Vault/2026-04-16-account-restructure-prd.md` §10.1 problems #1, #4, #5, #6 + §11 Phase 4.

---

## File Structure

### New
| File | Responsibility |
|---|---|
| `server/migrations/049_account_phase2_data.up.sql` | Data migration: backfill scope/status/provider before drops |
| `server/migrations/049_account_phase2_data.down.sql` | Reversible |
| `server/migrations/050_account_phase2_drop.up.sql` | Drop deprecated columns + add new constraints |
| `server/migrations/050_account_phase2_drop.down.sql` | Re-add columns (best-effort restore) |
| `server/internal/auth/rbac.go` | `RequireAgentOwner / RequireAdminOrAbove / RequireOwner` helpers |
| `server/internal/auth/rbac_test.go` | Unit tests |

### Modified
| File | Why |
|---|---|
| `server/pkg/db/queries/agent.sql` | Update existing queries to drop dead columns; add status-merge logic |
| `server/pkg/db/queries/runtime.sql` | Stop reading dropped columns; switch to `mode`/`last_heartbeat_at` |
| `server/internal/handler/agent.go` | Apply RBAC guards; map old `AgentType + scope` → unified surface; merge status writes |
| `server/internal/handler/runtime.go` | Apply guards on register/remove |
| `server/internal/handler/identity_card.go` (or wherever IdentityCard endpoint lives) | Apply guards |
| `server/internal/handler/daemon.go` | Drop legacy `runtime_mode`/`runtime_config` reads if present |
| `server/cmd/server/router.go` | Wire new helpers if not already in scope |
| `apps/web/shared/types/agent.ts` | Drop legacy fields, collapse `AgentType`, single `AgentStatus` |
| `apps/web/features/workspace/` | Replace `page_system_agent` checks with `agent_type === 'system_agent' && scope === 'xxx'` |

---

### Task 1: Data migration (049) — backfill before drops

**Files:**
- Create: `server/migrations/049_account_phase2_data.up.sql`
- Create: `server/migrations/049_account_phase2_data.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- Account Phase 2 - data migration. No column drops yet.

-- ===== Agent type collapse =====
-- page_system_agent rows: convert to system_agent + scope (was page_scope)
UPDATE agent
SET agent_type = 'system_agent',
    scope      = COALESCE(scope, page_scope)
WHERE agent_type = 'page_system_agent';

-- Existing system_agent (is_system=TRUE) rows: scope must remain NULL (global orchestrator)
UPDATE agent
SET scope = NULL
WHERE agent_type = 'system_agent' AND is_system = TRUE AND scope IS NOT NULL;

-- ===== Status merge: online_status + workload_status -> status =====
-- 7 values: offline | online | idle | busy | blocked | degraded | suspended
-- Priority: workload_status takes precedence when not 'idle' (it has the work signal);
-- otherwise online_status decides offline/online/idle.
UPDATE agent SET status =
    CASE
      WHEN workload_status = 'suspended'                                    THEN 'suspended'
      WHEN workload_status = 'blocked'                                      THEN 'blocked'
      WHEN workload_status = 'degraded'                                     THEN 'degraded'
      WHEN workload_status = 'busy'                                         THEN 'busy'
      WHEN online_status = 'offline'                                        THEN 'offline'
      WHEN online_status = 'online' AND workload_status IN ('idle', NULL)   THEN 'idle'
      ELSE 'online'
    END
WHERE status IS NULL OR status NOT IN
    ('offline','online','idle','busy','blocked','degraded','suspended');

-- ===== Owner type backfill (idempotent — Plan 1 already did most of this) =====
UPDATE agent SET owner_type = 'organization'
    WHERE owner_type IS NULL AND agent_type = 'system_agent';
UPDATE agent SET owner_type = 'user'
    WHERE owner_type IS NULL AND owner_id IS NOT NULL;

-- ===== Runtime: mode column from runtime_mode (idempotent) =====
UPDATE agent_runtime SET mode = runtime_mode WHERE mode IS NULL;
ALTER TABLE agent_runtime ALTER COLUMN mode SET NOT NULL;
ALTER TABLE agent_runtime ALTER COLUMN mode SET DEFAULT 'local';

-- ===== Runtime: last_heartbeat_at from last_seen_at (idempotent) =====
UPDATE agent_runtime SET last_heartbeat_at = last_seen_at WHERE last_heartbeat_at IS NULL;

-- ===== Provider value normalization (PRD §10.1 #2) =====
UPDATE agent_runtime SET provider = 'cloud_llm' WHERE provider = 'multica_agent';
UPDATE agent_runtime SET provider = 'claude'    WHERE provider = 'legacy_local';
-- Anything else non-standard: keep value, surface in concerns; humans verify post-deploy.
```

- [ ] **Step 2: Write the down migration**

```sql
-- Reverse data migration is mostly impossible (we cannot recover the original
-- workload_status values from a single status). Best-effort:
ALTER TABLE agent_runtime ALTER COLUMN mode DROP NOT NULL;
ALTER TABLE agent_runtime ALTER COLUMN mode DROP DEFAULT;
-- agent.status, agent.scope, owner_type backfills remain — they're harmless.
```

- [ ] **Step 3: Apply + verify (against worktree DB)**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make migrate-up
psql "$DATABASE_URL" -c "SELECT DISTINCT agent_type FROM agent;"
psql "$DATABASE_URL" -c "SELECT DISTINCT status FROM agent WHERE status IS NOT NULL;"
psql "$DATABASE_URL" -c "SELECT DISTINCT provider FROM agent_runtime;"
```

Expected: agent_type ⊂ {personal_agent, system_agent}; status ⊂ 7 enum; provider ⊂ {claude, codex, opencode, cloud_llm}.

- [ ] **Step 4: Commit**

```bash
git add server/migrations/049_account_phase2_data.up.sql server/migrations/049_account_phase2_data.down.sql
git commit -m "feat(db): account phase 2 data migration (status merge, type collapse, provider normalize)"
```

---

### Task 2: Drop migration (050) — destructive; runs after callers stop reading legacy columns

**Files:**
- Create: `server/migrations/050_account_phase2_drop.up.sql`
- Create: `server/migrations/050_account_phase2_drop.down.sql`

**WARNING:** This is destructive. Apply ONLY after Tasks 3 & 4 (queries + handlers updated to stop reading the dropped columns) and Task 7 (frontend updated). The plan executes Task 2 LAST among DB tasks for this reason.

DO NOT actually apply this migration during Task 2 — just write the file. Apply at Task 8.

- [ ] **Step 1: Write the up migration**

```sql
-- Account Phase 2 - drop deprecated columns and tighten constraints.

-- ===== Agent table cleanup =====
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_owner_type_check;
ALTER TABLE agent ADD CONSTRAINT agent_owner_type_check
    CHECK (owner_type IN ('user', 'organization'));
ALTER TABLE agent ALTER COLUMN owner_type SET NOT NULL;

ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_type_owner_match;
ALTER TABLE agent ADD CONSTRAINT agent_type_owner_match CHECK (
  (agent_type = 'personal_agent' AND owner_type = 'user' AND owner_id IS NOT NULL)
  OR
  (agent_type = 'system_agent' AND owner_type = 'organization' AND owner_id IS NULL)
);

-- Status NOT NULL after data migration filled it.
ALTER TABLE agent ALTER COLUMN status SET NOT NULL;
ALTER TABLE agent ADD CONSTRAINT agent_status_check
    CHECK (status IN ('offline','online','idle','busy','blocked','degraded','suspended'));

-- Drop legacy columns.
ALTER TABLE agent
    DROP COLUMN IF EXISTS is_system,
    DROP COLUMN IF EXISTS page_scope,
    DROP COLUMN IF EXISTS runtime_mode,
    DROP COLUMN IF EXISTS runtime_config,
    DROP COLUMN IF EXISTS cloud_llm_config,
    DROP COLUMN IF EXISTS capabilities,
    DROP COLUMN IF EXISTS tools,
    DROP COLUMN IF EXISTS triggers,
    DROP COLUMN IF EXISTS system_config,
    DROP COLUMN IF EXISTS agent_metadata,
    DROP COLUMN IF EXISTS accessible_files_scope,
    DROP COLUMN IF EXISTS allowed_channels_scope,
    DROP COLUMN IF EXISTS online_status,
    DROP COLUMN IF EXISTS workload_status;

-- agent_type CHECK enum tightened to two values.
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_agent_type_check;
ALTER TABLE agent ADD CONSTRAINT agent_agent_type_check
    CHECK (agent_type IN ('personal_agent', 'system_agent'));

-- New uniqueness constraints for system_agent + scope (replaces old is_system unique).
DROP INDEX IF EXISTS uq_workspace_system_agent;
CREATE UNIQUE INDEX uq_workspace_global_system_agent
    ON agent(workspace_id)
    WHERE agent_type = 'system_agent' AND scope IS NULL;
CREATE UNIQUE INDEX uq_workspace_scoped_system_agent
    ON agent(workspace_id, scope)
    WHERE agent_type = 'system_agent' AND scope IS NOT NULL;

-- ===== Runtime table cleanup =====
ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS runtime_mode,
    DROP COLUMN IF EXISTS last_seen_at;

-- last_heartbeat_at SET NOT NULL only if all rows backfilled (probably not — keep nullable for now).
```

- [ ] **Step 2: Write the down migration**

Best-effort restore (data lost; columns nullable):

```sql
ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS is_system BOOLEAN DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS page_scope TEXT,
    ADD COLUMN IF NOT EXISTS runtime_mode TEXT,
    ADD COLUMN IF NOT EXISTS runtime_config JSONB,
    ADD COLUMN IF NOT EXISTS cloud_llm_config JSONB,
    ADD COLUMN IF NOT EXISTS capabilities TEXT[],
    ADD COLUMN IF NOT EXISTS tools JSONB,
    ADD COLUMN IF NOT EXISTS triggers JSONB,
    ADD COLUMN IF NOT EXISTS system_config JSONB,
    ADD COLUMN IF NOT EXISTS agent_metadata JSONB,
    ADD COLUMN IF NOT EXISTS accessible_files_scope JSONB,
    ADD COLUMN IF NOT EXISTS allowed_channels_scope JSONB,
    ADD COLUMN IF NOT EXISTS online_status TEXT,
    ADD COLUMN IF NOT EXISTS workload_status TEXT;

ALTER TABLE agent ALTER COLUMN status DROP NOT NULL;
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_status_check;
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_type_owner_match;

ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS runtime_mode TEXT,
    ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;

DROP INDEX IF EXISTS uq_workspace_global_system_agent;
DROP INDEX IF EXISTS uq_workspace_scoped_system_agent;
```

- [ ] **Step 3: Commit (DO NOT apply yet)**

```bash
git add server/migrations/050_account_phase2_drop.up.sql server/migrations/050_account_phase2_drop.down.sql
git commit -m "feat(db): account phase 2 drop migration (NOT YET APPLIED)

Applied at end of Phase 2 plan after callers updated."
```

---

### Task 3: Update sqlc queries to stop reading legacy columns

**Files:**
- Modify: `server/pkg/db/queries/agent.sql`
- Modify: `server/pkg/db/queries/runtime.sql`

- [ ] **Step 1: Audit all SELECT/INSERT/UPDATE queries**

```bash
grep -n 'is_system\|page_scope\|runtime_mode\|runtime_config\|cloud_llm_config\|online_status\|workload_status\|capabilities\|tools\|triggers\|system_config\|agent_metadata\|accessible_files_scope\|allowed_channels_scope\|last_seen_at' server/pkg/db/queries/agent.sql server/pkg/db/queries/runtime.sql
```

For each match, decide:
- INSERT/UPDATE: remove the column from the query.
- SELECT: replace `SELECT *` with explicit column list excluding dropped columns; or remove the named field.

Mass replacements:
- `runtime_mode` reads → `mode`
- `last_seen_at` reads → `last_heartbeat_at`
- `online_status`, `workload_status` reads → `status`
- `is_system` reads → `agent_type = 'system_agent'`
- `page_scope` reads → `scope`

Note: `cloud_llm_config` data should have been migrated to `runtime.metadata` in a prior step; if any queries still expect it, you must move that data first.

- [ ] **Step 2: Regenerate sqlc**

```bash
make sqlc
go build ./...
```

Expected: build is broken in handler files that reference dropped fields. THAT'S OK — Task 4 fixes those. Note all the broken file:line locations.

- [ ] **Step 3: Commit (queries only; build still broken)**

```bash
git add server/pkg/db/queries/ server/pkg/db/generated/
git commit -m "refactor(sqlc): remove legacy agent/runtime columns from queries

Build will be broken until handler updates land in next commit."
```

---

### Task 4: Update handlers to use new fields

**Files:**
- Modify: `server/internal/handler/agent.go`
- Modify: `server/internal/handler/runtime.go`
- Modify: `server/internal/handler/daemon.go`

- [ ] **Step 1: Use the build errors as a punch list**

```bash
cd server
go build ./internal/handler/... 2>&1 | tee /tmp/build-errors.txt
```

For each error:
- "undefined: row.IsSystem" → replace with `row.AgentType == "system_agent"`
- "undefined: row.PageScope" → replace with `row.Scope`
- "undefined: row.OnlineStatus" → replace with `row.Status`
- "undefined: row.WorkloadStatus" → replace with `row.Status`
- "undefined: row.RuntimeMode" → replace with `row.Mode`
- "undefined: row.LastSeenAt" → replace with `row.LastHeartbeatAt`
- "undefined: row.Capabilities/Tools/Triggers/CloudLLMConfig" → DELETE the surface; update any JSON response struct to drop the field.

Update the `AgentRuntimeResponse` struct in `server/internal/handler/runtime.go`:
- DROP `RuntimeMode` from the struct (was legacy alias). Keep `Mode` as the canonical field.
- DROP `LastSeenAt`. Keep `LastHeartbeatAt` (added in Plan 1) as canonical.
- Keep all Plan 1 additions.

Update the agent JSON response struct similarly:
- DROP `OnlineStatus`, `WorkloadStatus`, `IsSystem`, `PageScope`, `Capabilities`, `Tools`, `Triggers`, `CloudLLMConfig`, `RuntimeMode`, `RuntimeConfig`.
- Keep `AgentType`, `Status`, `Scope`, `OwnerType`, `IdentityCard`.

- [ ] **Step 2: Build clean**

```bash
go build ./...
go test ./internal/handler/...
```

Expected: build clean. Tests may break — fix assertions to use new field names.

- [ ] **Step 3: Commit**

```bash
git add server/internal/handler/
git commit -m "refactor(agent,runtime): use unified status/mode/scope fields"
```

---

### Task 5: RBAC helpers

**Files:**
- Create: `server/internal/auth/rbac.go`
- Create: `server/internal/auth/rbac_test.go`

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeMemberLookup struct {
	role string
	err  error
}

func (f fakeMemberLookup) GetMemberRole(_ context.Context, _ uuid.UUID, _ uuid.UUID) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.role, nil
}

type fakeAgentLookup struct {
	ownerID uuid.UUID
	err     error
}

func (f fakeAgentLookup) GetAgentOwnerID(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
	if f.err != nil {
		return uuid.Nil, f.err
	}
	return f.ownerID, nil
}

func TestRequireAdminOrAbove_AllowsAdmin(t *testing.T) {
	g := Guards{Member: fakeMemberLookup{role: "admin"}}
	if err := g.RequireAdminOrAbove(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestRequireAdminOrAbove_RejectsMember(t *testing.T) {
	g := Guards{Member: fakeMemberLookup{role: "member"}}
	err := g.RequireAdminOrAbove(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestRequireOwner_OnlyOwner(t *testing.T) {
	g := Guards{Member: fakeMemberLookup{role: "admin"}}
	if !errors.Is(g.RequireOwner(context.Background(), uuid.New(), uuid.New()), ErrForbidden) {
		t.Error("expected admin to be rejected by RequireOwner")
	}
	g.Member = fakeMemberLookup{role: "owner"}
	if err := g.RequireOwner(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Errorf("expected owner allowed, got %v", err)
	}
}

func TestRequireAgentOwner_AllowsOwner(t *testing.T) {
	user := uuid.New()
	agent := uuid.New()
	g := Guards{Agent: fakeAgentLookup{ownerID: user}}
	if err := g.RequireAgentOwner(context.Background(), agent, user); err != nil {
		t.Errorf("expected owner allowed, got %v", err)
	}
}

func TestRequireAgentOwner_RejectsOther(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()
	g := Guards{Agent: fakeAgentLookup{ownerID: owner}}
	err := g.RequireAgentOwner(context.Background(), uuid.New(), other)
	if !errors.Is(err, ErrForbidden) {
		t.Error("expected non-owner to be forbidden")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd server
go test ./internal/auth/... -run Require
```

Expected: build error.

- [ ] **Step 3: Implement**

```go
// server/internal/auth/rbac.go
package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var ErrForbidden = errors.New("forbidden")

type MemberLookup interface {
	GetMemberRole(ctx context.Context, workspaceID, userID uuid.UUID) (string, error)
}

type AgentLookup interface {
	GetAgentOwnerID(ctx context.Context, agentID uuid.UUID) (uuid.UUID, error)
}

type Guards struct {
	Member MemberLookup
	Agent  AgentLookup
}

// RequireAdminOrAbove allows owner and admin roles.
func (g Guards) RequireAdminOrAbove(ctx context.Context, workspaceID, userID uuid.UUID) error {
	role, err := g.Member.GetMemberRole(ctx, workspaceID, userID)
	if err != nil {
		return err
	}
	if role == "owner" || role == "admin" {
		return nil
	}
	return ErrForbidden
}

// RequireOwner allows only the workspace owner role.
func (g Guards) RequireOwner(ctx context.Context, workspaceID, userID uuid.UUID) error {
	role, err := g.Member.GetMemberRole(ctx, workspaceID, userID)
	if err != nil {
		return err
	}
	if role == "owner" {
		return nil
	}
	return ErrForbidden
}

// RequireAgentOwner allows only the agent's Owner.
func (g Guards) RequireAgentOwner(ctx context.Context, agentID, userID uuid.UUID) error {
	ownerID, err := g.Agent.GetAgentOwnerID(ctx, agentID)
	if err != nil {
		return err
	}
	if ownerID == userID {
		return nil
	}
	return ErrForbidden
}
```

- [ ] **Step 4: Adapter to sqlc**

Add a small file `server/internal/auth/rbac_adapter.go` that wraps `*db.Queries` to satisfy `MemberLookup` and `AgentLookup`:

```go
package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

type sqlcMember struct{ q *generated.Queries }

func (s sqlcMember) GetMemberRole(ctx context.Context, ws, user uuid.UUID) (string, error) {
	row, err := s.q.GetMember(ctx, generated.GetMemberParams{WorkspaceID: ws, UserID: user})
	if err != nil {
		return "", err
	}
	return row.Role, nil
}

type sqlcAgent struct{ q *generated.Queries }

func (s sqlcAgent) GetAgentOwnerID(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	row, err := s.q.GetAgent(ctx, id)
	if err != nil {
		return uuid.Nil, err
	}
	if !row.OwnerID.Valid {
		return uuid.Nil, nil
	}
	return row.OwnerID.UUID, nil // adjust to your actual nullable type
}

// NewGuards builds Guards backed by sqlc.
func NewGuards(q *generated.Queries) Guards {
	return Guards{Member: sqlcMember{q: q}, Agent: sqlcAgent{q: q}}
}
```

Adapt `OwnerID` field access to whatever the generated struct uses (probably `pgtype.UUID` or `sql.NullString`).

- [ ] **Step 5: Run tests**

```bash
go test ./internal/auth/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/auth/rbac.go server/internal/auth/rbac_test.go server/internal/auth/rbac_adapter.go
git commit -m "feat(auth): RBAC guards (RequireAgentOwner / RequireAdminOrAbove / RequireOwner)"
```

---

### Task 6: Apply guards to handlers

**Files:**
- Modify: `server/internal/handler/agent.go`
- Modify: `server/internal/handler/runtime.go`
- Modify: `server/internal/handler/identity_card.go` (if exists; otherwise the relevant agent endpoint)
- Modify: `server/internal/handler/handler.go` (constructor wiring)

- [ ] **Step 1: Wire `Guards` into the handler struct**

In whichever struct(s) hold the agent/runtime/identity_card handlers, add a `Guards auth.Guards` field. Wire it via the constructor.

- [ ] **Step 2: Apply guards per PRD §8.9 table**

| Endpoint | Guard |
|---|---|
| `PATCH /api/agents/{id}` | `RequireAgentOwner` |
| `PATCH /api/agents/{id}/identity-card` | `RequireAgentOwner` OR `RequireAdminOrAbove` |
| `DELETE /api/agents/{id}` | `RequireAgentOwner` OR `RequireAdminOrAbove` |
| `PATCH /api/agents/{id}/status` (suspended) | `RequireAgentOwner` OR `RequireAdminOrAbove` |
| `POST /api/system-agents` | `RequireAdminOrAbove` |
| `PATCH /api/system-agents/{id}` | `RequireAdminOrAbove` |
| `DELETE /api/system-agents/{id}` | `RequireOwner` |
| `POST /api/runtimes` (manual register) | `RequireAdminOrAbove` |
| `DELETE /api/runtimes/{id}` | `RequireAdminOrAbove` |

For each handler function, insert at the top:

```go
if err := h.Guards.RequireAgentOwner(r.Context(), agentID, userID); err != nil {
    http.Error(w, "forbidden", http.StatusForbidden)
    return
}
```

(or the appropriate guard).

- [ ] **Step 3: Tests**

```bash
go test ./internal/handler/...
```

Expected: PASS. If existing tests didn't supply a member with the right role, they may now 403 — update fixtures.

- [ ] **Step 4: Commit**

```bash
git add server/internal/handler/
git commit -m "feat(rbac): guard agent/system-agent/runtime endpoints"
```

---

### Task 7: Frontend — collapse types and remove legacy fields

**Files:**
- Modify: `apps/web/shared/types/agent.ts`
- Modify: `apps/web/features/workspace/` (any component referencing `page_system_agent` / `page_scope` / `online_status` / `workload_status`)

- [ ] **Step 1: Update `agent.ts` type unions**

Replace at top of `apps/web/shared/types/agent.ts`:

```typescript
// AgentType — collapsed to two values per Account PRD §6.2
export type AgentType = "personal_agent" | "system_agent";

// AgentStatus — single 7-value enum (PRD §3.4)
export type AgentStatus =
  | "offline"
  | "online"
  | "idle"
  | "busy"
  | "blocked"
  | "degraded"
  | "suspended";

// AgentScope — System Agent's functional scope
export type AgentScope = "account" | "conversation" | "project" | "file" | null;
```

Remove/edit the now-deleted unions:
- DELETE `AgentOnlineStatus`, `AgentWorkloadStatus`.
- DELETE `PageAgentScope` (or alias to `AgentScope`).

In the `Agent` interface, REMOVE these fields (they were in Plan 1 as optional; now drop):
- `runtime_mode`
- `runtime_config`
- `cloud_llm_config`
- `online_status`
- `workload_status`
- `page_scope`
- `tools` (move to identity_card if used)
- `triggers`

Make REQUIRED:
- `agent_type: AgentType`
- `status: AgentStatus`
- `scope: AgentScope`
- `owner_type: "user" | "organization"`

In the `RuntimeDevice` interface, REMOVE:
- `runtime_mode` (legacy)
- `last_seen_at` (legacy)

Make REQUIRED:
- `mode: AgentRuntimeMode`
- `last_heartbeat_at: string | null`

- [ ] **Step 2: Search-and-replace remaining call sites**

```bash
cd apps/web
grep -rn "page_system_agent\|page_scope\|online_status\|workload_status\|runtime_mode\b" --include="*.ts" --include="*.tsx"
```

For each match:
- `agent_type === 'page_system_agent'` → `agent_type === 'system_agent' && agent.scope !== null`
- `page_scope` reads → `scope`
- `online_status === 'offline' || workload_status === 'busy'` → `status === 'offline' || status === 'busy'`
- `runtime_mode` reads → `mode`

- [ ] **Step 3: Typecheck (if `node_modules` is available)**

```bash
pnpm typecheck
```

If `tsc: command not found`, eye-check the diff. Do `pnpm install` if you have user permission.

- [ ] **Step 4: Commit**

```bash
git add apps/web/shared/types/agent.ts apps/web/features/
git commit -m "feat(web): collapse AgentType, drop legacy fields"
```

---

### Task 8: Apply drop migration + final verification

- [ ] **Step 1: Apply migration 050**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make migrate-up
```

Expected: 050 applies cleanly. Old columns gone. New CHECK constraints in place.

- [ ] **Step 2: Round-trip test**

```bash
make migrate-down  # rolls back 050 (best-effort restore)
make migrate-up    # re-applies 050
```

Expected: both succeed.

- [ ] **Step 3: Final build + tests**

```bash
make test
```

If pre-existing test failures (TestAgentCRUD, realtime hub_test, auto_reply) still surface, note them as pre-existing.

- [ ] **Step 4: Live API smoke test**

```bash
make dev &
SERVER=$!
sleep 5
# Get a token; create a personal agent; verify response shape has no legacy fields.
curl -s http://localhost:8080/api/health
kill $SERVER
```

- [ ] **Step 5: Commit any final fixes**

If steps 1-4 surfaced issues, commit them here.

---

## Self-Review Checklist

- [ ] Migration 049 successfully backfilled status/scope/mode/owner_type.
- [ ] Migration 050 dropped all 14 legacy agent columns + 2 runtime legacy columns.
- [ ] sqlc queries no longer reference dropped columns.
- [ ] Handlers use new field names; build clean.
- [ ] RBAC guards applied to 9 endpoints listed in §8.9.
- [ ] Frontend types collapsed; no `page_system_agent` / `page_scope` / `online_status` / `workload_status` / `runtime_mode` references remain in `apps/web/`.
- [ ] `make sqlc && go build ./... && go test ./internal/handler/...` all pass.
- [ ] No new test failures beyond known pre-existing ones.

---

## Out of Scope (for later plans)

- `cloud_llm_config` data migration to `runtime.metadata` — needs investigation; if no row uses it today, just drop. Otherwise add explicit migration step to 049.
- Runtime mode write path through `UpsertAgentRuntime` — Plan 5 territory once the new sqlc query lands.
- Removing `auto_reply_config` triggers — Plan 4 (MediationService consolidation).
- Multi-workspace cross-Owner Agent sharing.
