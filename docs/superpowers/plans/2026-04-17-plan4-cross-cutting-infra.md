# Cross-cutting Infrastructure — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Land the cross-cutting infrastructure that the three main PRDs (Project / Account / Session) all depend on — error code catalog, secret encryption, audit log rewrite, inbox extension, workspace quota, MCP tool skeleton, Prometheus metrics, and the consolidated event catalog.

**Architecture:**
- Migration 051 — combined additive + rewrite migration: extend `activity_log` (rename `action → event_type`, add 12 new columns, 4 indexes); extend `inbox_item` (add 8 columns + new index); extend `personal_access_token` (add `scopes`); add `workspace_secret` table; add `workspace_quota` table; add cost columns to `agent_task_queue` (forward-compat for `execution`).
- New Go packages: `server/internal/errcode/` (canonical error code catalog), `server/pkg/crypto/` (AES-256-GCM secret encryption), `server/internal/mcp/` (17-tool skeleton with dispatcher and permission layer), `server/internal/metrics/` (Prometheus registry).
- New protocol file: `server/pkg/protocol/event_catalog.go` (single enumeration of every WS / bus event).
- Service-layer additions: `service/activity_log.go` (transactional write helper), `service/quota.go` (claim-time guard).
- Frontend: extend `inbox.ts` and `activity.ts` types; add minimal Workspace Secret management page.

**Tech Stack:** Go 1.26.1 (Chi router, sqlc, pgx, prometheus/client_golang), PostgreSQL pgvector/pg17, Next.js 16 App Router, TypeScript, Zustand.

**Reference PRD:** `/Users/chauncey2025/Documents/Obsidian Vault/2026-04-17-cross-cutting-prd.md` §2-§14 (full scope).

---

## File Structure

### New
| File | Responsibility |
|---|---|
| `server/internal/errcode/errcode.go` | `Code` struct, ~30 catalog entries from PRD §13.2, `Write()` helper for standard JSON error format |
| `server/internal/errcode/errcode_test.go` | Unit tests for catalog + `Write()` |
| `server/pkg/crypto/secretbox.go` | `Encrypt(plaintext, key) []byte` and `Decrypt(ciphertext, key) ([]byte, error)` using AES-256-GCM |
| `server/pkg/crypto/secretbox_test.go` | Round-trip + tamper tests |
| `server/migrations/051_cross_cutting_infra.up.sql` | Combined: activity_log rewrite + inbox_item extension + workspace_secret + PAT scopes + workspace_quota + agent_task_queue cost cols |
| `server/migrations/051_cross_cutting_infra.down.sql` | Reversible (best-effort) |
| `server/pkg/db/queries/workspace_secret.sql` | sqlc queries: Create/Get/List/Delete/Rotate |
| `server/pkg/db/queries/workspace_quota.sql` | sqlc queries: Get/Upsert/AddUsage/ResetMonth |
| `server/pkg/protocol/event_catalog.go` | `EventCatalog` map enumerating every event from PRD §2.2 with descriptions |
| `server/internal/handler/workspace_secret.go` | CRUD endpoints (owner/admin only) |
| `server/internal/handler/activity_log.go` | `GET /api/activity-log` query endpoint with filters |
| `server/internal/service/activity_log.go` | Transactional `Write(ctx, tx, ActivityEntry) error` helper |
| `server/internal/service/quota.go` | `CheckClaim(ctx, workspaceID) error` returning `errcode.QUOTA_EXCEEDED` / `QUOTA_CONCURRENT_LIMIT` |
| `server/internal/mcp/tool.go` | `Tool` interface + `Registry` + `Exec(ctx, name, args)` dispatcher |
| `server/internal/mcp/auth.go` | Permission check helpers (`workspace_id + user_id + agent_id` from ctx) |
| `server/internal/mcp/tools/get_issue.go` ... (17 files) | One file per tool: `get_issue`, `list_issue_comments`, `create_comment`, `update_issue_status`, `list_assigned_projects`, `get_project`, `search_project_context`, `list_project_files`, `download_attachment`, `upload_artifact`, `complete_task`, `request_approval`, `read_file`, `apply_patch`, `create_pr`, `checkout_repo`, `local_file_read` |
| `server/internal/metrics/metrics.go` | Prometheus collectors (8 initial metrics from PRD §14.1) + `Handler()` |
| `apps/web/features/workspace/components/workspace-secrets.tsx` | Minimal owner/admin secret list + add form |
| `apps/web/app/(dashboard)/settings/secrets/page.tsx` | Page route mounting the component |

### Modified
| File | Why |
|---|---|
| `server/pkg/db/queries/activity.sql` | Replace `CreateActivity` with `event_type` field; add `ListActivitiesByProject/ByTask/ByEventType/ByActor` |
| `server/pkg/db/queries/inbox.sql` | Update `CreateInboxItem` with new fields; add `ResolveInboxItem`, `ListUnresolvedInbox` |
| `server/pkg/db/queries/personal_access_token.sql` | Update `CreatePersonalAccessToken` to accept `scopes` |
| `server/internal/handler/inbox.go` | Add `POST /api/inbox/{id}/resolve`, ensure `mark-all-read` exists, surface new fields in JSON |
| `server/internal/handler/activity.go` | Adjust to new `event_type` column on read |
| `server/internal/handler/handler.go` | Wire new handlers (`WorkspaceSecret`, `ActivityLog`, `MCP`, quota) into `Handler` struct + constructor |
| `server/internal/service/cloud_executor.go` | Call `quota.CheckClaim` before transitioning to `claimed`; on failure write `QUOTA_EXCEEDED` |
| `server/cmd/server/router.go` | Mount: `/api/workspace-secrets`, `/api/activity-log`, `/api/mcp/{tool}`, `/metrics` |
| `apps/web/shared/types/inbox.ts` | Expand `InboxItemType` enum; add `plan_id`, `task_id`, `slot_id`, `thread_id`, `channel_id`, `action_required`, `severity` (3-value), `resolved_at`, `resolution`, `resolution_by` |
| `apps/web/shared/types/activity.ts` | Add `event_type`, `effective_actor_*`, `real_operator_*`, all `related_*` UUID fields, `payload`, `retention_class` |
| `apps/web/shared/types/index.ts` | Re-export `WorkspaceSecret` type |
| `apps/web/shared/api/index.ts` (or `client.ts`) | Add `listSecrets`, `createSecret`, `deleteSecret`, `resolveInboxItem`, `listActivityLog` |

---

### Task 1: Error Code Catalog

**Files:**
- Create: `server/internal/errcode/errcode.go`
- Create: `server/internal/errcode/errcode_test.go`

- [ ] **Step 1: Write the failing test**

Create `server/internal/errcode/errcode_test.go`:

```go
package errcode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogHasCoreEntries(t *testing.T) {
	must := []Code{
		AuthUnauthorized, AuthForbidden,
		ProjectNotFound, PlanNotApproved, DAGCycle,
		QuotaExceeded, QuotaConcurrentLimit,
		MCPToolNotAvailable, MCPPermissionDenied,
		ExecutionLeaseExpired,
	}
	for _, c := range must {
		if c.Code == "" {
			t.Errorf("uninitialized code: %+v", c)
		}
		if c.HTTPStatus == 0 {
			t.Errorf("missing HTTP status for %s", c.Code)
		}
	}
}

func TestWriteEmitsStandardEnvelope(t *testing.T) {
	rr := httptest.NewRecorder()
	Write(rr, DAGCycle, map[string]any{"cycle_path": []string{"a", "b", "a"}})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rr.Code)
	}
	var body struct {
		Error struct {
			Code      string                 `json:"code"`
			Message   string                 `json:"message"`
			Retriable bool                   `json:"retriable"`
			Details   map[string]any         `json:"details,omitempty"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "DAG_CYCLE" {
		t.Errorf("expected DAG_CYCLE, got %q", body.Error.Code)
	}
	if body.Error.Retriable {
		t.Error("DAG_CYCLE should not be retriable")
	}
	if _, ok := body.Error.Details["cycle_path"]; !ok {
		t.Error("details missing cycle_path")
	}
}

func TestWriteWithNilDetails(t *testing.T) {
	rr := httptest.NewRecorder()
	Write(rr, AuthUnauthorized, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rr.Code)
	}
}
```

- [ ] **Step 2: Run test (fails — package missing)**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691/server
go test ./internal/errcode/...
```

Expected: build error.

- [ ] **Step 3: Implement `errcode.go`**

```go
// Package errcode is the canonical catalog of platform-wide error codes.
// PRD: docs/superpowers/plans/2026-04-17-plan4-cross-cutting-infra.md (§13).
package errcode

import (
	"encoding/json"
	"net/http"
)

// Code is one entry in the catalog.
type Code struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
	Retriable  bool   `json:"retriable"`
}

// ----- Auth -----
var (
	AuthUnauthorized = Code{Code: "AUTH_UNAUTHORIZED", Message: "authentication required", HTTPStatus: 401, Retriable: false}
	AuthForbidden    = Code{Code: "AUTH_FORBIDDEN", Message: "permission denied", HTTPStatus: 403, Retriable: false}
)

// ----- Project / Plan / Run -----
var (
	ProjectNotFound    = Code{Code: "PROJECT_NOT_FOUND", Message: "project not found", HTTPStatus: 404, Retriable: false}
	PlanNotApproved    = Code{Code: "PLAN_NOT_APPROVED", Message: "plan must be approved before run", HTTPStatus: 409, Retriable: false}
	PlanHasActiveRun   = Code{Code: "PLAN_HAS_ACTIVE_RUN", Message: "plan has an active run", HTTPStatus: 409, Retriable: false}
	PlanGenMalformed   = Code{Code: "PLAN_GEN_MALFORMED", Message: "plan generator returned malformed output", HTTPStatus: 500, Retriable: true}
	PlanGenTimeout     = Code{Code: "PLAN_GEN_TIMEOUT", Message: "plan generator timed out", HTTPStatus: 504, Retriable: true}
)

// ----- DAG -----
var (
	DAGCycle       = Code{Code: "DAG_CYCLE", Message: "circular dependency detected", HTTPStatus: 400, Retriable: false}
	DAGUnknownTask = Code{Code: "DAG_UNKNOWN_TASK", Message: "depends_on references unknown task", HTTPStatus: 400, Retriable: false}
	DAGSelfRef     = Code{Code: "DAG_SELF_REF", Message: "task cannot depend on itself", HTTPStatus: 400, Retriable: false}
)

// ----- Task / Slot -----
var (
	TaskNotSchedulable    = Code{Code: "TASK_NOT_SCHEDULABLE", Message: "task cannot be scheduled", HTTPStatus: 409, Retriable: false}
	SlotNotReady          = Code{Code: "SLOT_NOT_READY", Message: "slot not in ready state", HTTPStatus: 409, Retriable: false}
	SlotAlreadySubmitted  = Code{Code: "SLOT_ALREADY_SUBMITTED", Message: "slot already submitted", HTTPStatus: 409, Retriable: false}
)

// ----- Execution / Agent / Runtime -----
var (
	ExecutionLeaseExpired = Code{Code: "EXECUTION_LEASE_EXPIRED", Message: "execution lease has expired", HTTPStatus: 410, Retriable: false}
	AgentNotAvailable     = Code{Code: "AGENT_NOT_AVAILABLE", Message: "agent is not available", HTTPStatus: 503, Retriable: true}
	RuntimeOffline        = Code{Code: "RUNTIME_OFFLINE", Message: "runtime is offline", HTTPStatus: 503, Retriable: true}
	RuntimeOverloaded     = Code{Code: "RUNTIME_OVERLOADED", Message: "runtime at concurrency limit", HTTPStatus: 429, Retriable: true}
)

// ----- Artifact / Review -----
var (
	ArtifactInvalid      = Code{Code: "ARTIFACT_INVALID", Message: "artifact validation failed", HTTPStatus: 400, Retriable: false}
	ReviewAlreadyDecided = Code{Code: "REVIEW_ALREADY_DECIDED", Message: "review has already been decided", HTTPStatus: 409, Retriable: false}
)

// ----- MCP -----
var (
	MCPToolNotAvailable = Code{Code: "MCP_TOOL_NOT_AVAILABLE", Message: "tool not available in this runtime", HTTPStatus: 404, Retriable: false}
	MCPPermissionDenied = Code{Code: "MCP_PERMISSION_DENIED", Message: "agent lacks permission for this tool", HTTPStatus: 403, Retriable: false}
)

// ----- Quota -----
var (
	QuotaExceeded         = Code{Code: "QUOTA_EXCEEDED", Message: "monthly quota exceeded", HTTPStatus: 429, Retriable: false}
	QuotaConcurrentLimit  = Code{Code: "QUOTA_CONCURRENT_LIMIT", Message: "concurrent execution limit reached", HTTPStatus: 429, Retriable: true}
)

// ----- Impersonation -----
var (
	ImpersonationNotOwnAgent = Code{Code: "IMPERSONATION_NOT_OWN_AGENT", Message: "cannot impersonate an agent you do not own", HTTPStatus: 403, Retriable: false}
	ImpersonationExpired     = Code{Code: "IMPERSONATION_EXPIRED", Message: "impersonation session expired", HTTPStatus: 410, Retriable: false}
)

// envelope is the on-the-wire shape (PRD §13.3).
type envelope struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retriable bool   `json:"retriable"`
		Details   any    `json:"details,omitempty"`
	} `json:"error"`
}

// Write emits the standard error envelope and HTTP status code.
func Write(w http.ResponseWriter, c Code, details any) {
	var env envelope
	env.Error.Code = c.Code
	env.Error.Message = c.Message
	env.Error.Retriable = c.Retriable
	env.Error.Details = details

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(c.HTTPStatus)
	_ = json.NewEncoder(w).Encode(env)
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/errcode/...
```

Expected: PASS.

- [ ] **Step 5: Migrate two example call sites**

Pick two simple endpoints to demonstrate the pattern. Edit `server/internal/handler/channel_merge.go`:

```go
// before
writeError(w, http.StatusForbidden, "only a channel founder can initiate merge")
// after
errcode.Write(w, errcode.AuthForbidden, map[string]any{"hint": "only a channel founder can initiate merge"})
```

Add `import "github.com/MyAIOSHub/MyTeam/server/internal/errcode"`. Migrate one more handler at most — the rest is out of scope for this plan.

- [ ] **Step 6: Commit**

```bash
git add server/internal/errcode/ server/internal/handler/channel_merge.go
git commit -m "feat(errcode): canonical error catalog + Write() helper"
```

---

### Task 2: Crypto secretbox helpers

**Files:**
- Create: `server/pkg/crypto/secretbox.go`
- Create: `server/pkg/crypto/secretbox_test.go`

- [ ] **Step 1: Write the failing test**

```go
package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func makeKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestRoundTrip(t *testing.T) {
	key := makeKey(t)
	plaintext := []byte("super-secret-anthropic-api-key")
	ct := Encrypt(plaintext, key)
	if bytes.Equal(ct, plaintext) {
		t.Fatal("ciphertext equals plaintext")
	}
	pt, err := Decrypt(ct, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Errorf("round-trip failed: got %q", pt)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	ct := Encrypt([]byte("data"), makeKey(t))
	if _, err := Decrypt(ct, makeKey(t)); err == nil {
		t.Error("expected error decrypting with wrong key")
	}
}

func TestDecryptTampered(t *testing.T) {
	key := makeKey(t)
	ct := Encrypt([]byte("data"), key)
	ct[len(ct)-1] ^= 0xFF
	if _, err := Decrypt(ct, key); err == nil {
		t.Error("expected error on tampered ciphertext")
	}
}

func TestRequiresKeyLength(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on bad key length")
		}
	}()
	Encrypt([]byte("data"), []byte("short"))
}
```

- [ ] **Step 2: Implement**

```go
// Package crypto provides AES-256-GCM symmetric encryption for workspace
// secrets (anthropic_api_key, github_token, etc).
//
// The master key is read from MYTEAM_SECRET_KEY at startup (must be 32 bytes
// hex-encoded). Each Encrypt() call uses a fresh random nonce; the nonce is
// prepended to the ciphertext so Decrypt() can recover it.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// Encrypt seals plaintext under key (must be 32 bytes for AES-256).
// Output layout: nonce (12B) || ciphertext || GCM tag (16B).
func Encrypt(plaintext, key []byte) []byte {
	if len(key) != 32 {
		panic(fmt.Sprintf("crypto: key must be 32 bytes, got %d", len(key)))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err) // unreachable for AES-256
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil)
}

// Decrypt opens ciphertext sealed by Encrypt.
func Decrypt(ciphertext, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("crypto: ciphertext too short")
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./pkg/crypto/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add server/pkg/crypto/
git commit -m "feat(crypto): AES-256-GCM secretbox helpers for workspace secrets"
```

---

### Task 3: Migration 051 — combined schema changes

**Files:**
- Create: `server/migrations/051_cross_cutting_infra.up.sql`
- Create: `server/migrations/051_cross_cutting_infra.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- Cross-cutting infrastructure (Plan 4):
--   1. activity_log rewrite (rename action -> event_type, add 12 cols, 4 indexes)
--   2. inbox_item extension (8 new cols + new partial index)
--   3. personal_access_token.scopes
--   4. workspace_secret table
--   5. workspace_quota table
--   6. agent_task_queue cost columns (forward-compat for execution table)

-- ===== 1. activity_log rewrite =====
-- Rename existing action column to event_type and widen the table.
ALTER TABLE activity_log RENAME COLUMN action TO event_type;

ALTER TABLE activity_log
    ADD COLUMN IF NOT EXISTS effective_actor_id   UUID,
    ADD COLUMN IF NOT EXISTS effective_actor_type TEXT,
    ADD COLUMN IF NOT EXISTS real_operator_id     UUID,
    ADD COLUMN IF NOT EXISTS real_operator_type   TEXT,
    ADD COLUMN IF NOT EXISTS related_project_id   UUID,
    ADD COLUMN IF NOT EXISTS related_plan_id      UUID,
    ADD COLUMN IF NOT EXISTS related_task_id      UUID,
    ADD COLUMN IF NOT EXISTS related_slot_id      UUID,
    ADD COLUMN IF NOT EXISTS related_execution_id UUID,
    ADD COLUMN IF NOT EXISTS related_channel_id   UUID,
    ADD COLUMN IF NOT EXISTS related_thread_id    UUID,
    ADD COLUMN IF NOT EXISTS related_agent_id     UUID,
    ADD COLUMN IF NOT EXISTS related_runtime_id   UUID,
    ADD COLUMN IF NOT EXISTS payload              JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS retention_class      TEXT NOT NULL DEFAULT 'permanent';

-- Backfill: pull existing `details` into payload for rows where payload empty.
UPDATE activity_log
   SET payload = COALESCE(details, '{}'::jsonb)
 WHERE payload = '{}'::jsonb AND details IS NOT NULL;

-- Drop the old narrow index on issue_id (we use related_task_id going forward).
DROP INDEX IF EXISTS idx_activity_log_issue;

-- Indexes from PRD §3.1
CREATE INDEX IF NOT EXISTS idx_activity_log_workspace_time
    ON activity_log(workspace_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_activity_log_project
    ON activity_log(related_project_id, created_at DESC)
    WHERE related_project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_activity_log_task
    ON activity_log(related_task_id, created_at DESC)
    WHERE related_task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_activity_log_event_type
    ON activity_log(workspace_id, event_type, created_at DESC);

-- ===== 2. inbox_item extension =====
ALTER TABLE inbox_item
    ADD COLUMN IF NOT EXISTS plan_id          UUID,
    ADD COLUMN IF NOT EXISTS task_id          UUID,
    ADD COLUMN IF NOT EXISTS slot_id          UUID,
    ADD COLUMN IF NOT EXISTS thread_id        UUID,
    ADD COLUMN IF NOT EXISTS channel_id       UUID,
    ADD COLUMN IF NOT EXISTS action_required  BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS resolved_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS resolution       TEXT,
    ADD COLUMN IF NOT EXISTS resolution_by    UUID;

-- New severity enum (3 values per PRD §8.1 — info/warning/critical).
-- Existing severity column already has CHECK action_required/attention/info; widen.
ALTER TABLE inbox_item DROP CONSTRAINT IF EXISTS inbox_item_severity_check;
ALTER TABLE inbox_item ADD CONSTRAINT inbox_item_severity_check
    CHECK (severity IN ('action_required', 'attention', 'info', 'warning', 'critical'));

ALTER TABLE inbox_item ADD CONSTRAINT inbox_item_resolution_check
    CHECK (resolution IS NULL OR resolution IN ('approved', 'rejected', 'dismissed', 'auto_resolved'));

CREATE INDEX IF NOT EXISTS idx_inbox_item_unresolved
    ON inbox_item(recipient_id, created_at DESC)
    WHERE resolved_at IS NULL;

-- ===== 3. personal_access_token.scopes =====
ALTER TABLE personal_access_token
    ADD COLUMN IF NOT EXISTS scopes TEXT[] NOT NULL DEFAULT '{}';

-- ===== 4. workspace_secret =====
CREATE TABLE IF NOT EXISTS workspace_secret (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    key             TEXT NOT NULL,
    value_encrypted BYTEA NOT NULL,
    created_by      UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at      TIMESTAMPTZ,
    UNIQUE (workspace_id, key)
);

-- ===== 5. workspace_quota =====
CREATE TABLE IF NOT EXISTS workspace_quota (
    workspace_id              UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    max_monthly_usd           NUMERIC(10,2) NOT NULL DEFAULT 100.00,
    max_concurrent_cloud_exec INTEGER NOT NULL DEFAULT 10,
    max_monthly_plan_gen      INTEGER NOT NULL DEFAULT 200,
    current_monthly_usd       NUMERIC(10,2) NOT NULL DEFAULT 0,
    current_month             DATE NOT NULL DEFAULT date_trunc('month', now())::date,
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ===== 6. agent_task_queue cost columns =====
-- Note: PRD §12.1 talks about an `execution` table arriving in Plan 5.
-- For forward compat, add the same columns to agent_task_queue (current task table).
-- Plan 5 will add them to execution as well; queries here read from agent_task_queue.
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS cost_input_tokens  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cost_output_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cost_usd           NUMERIC(10,4) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cost_provider      TEXT;
```

- [ ] **Step 2: Write the down migration**

```sql
-- Reverse Plan 4 cross-cutting changes.

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS cost_input_tokens,
    DROP COLUMN IF EXISTS cost_output_tokens,
    DROP COLUMN IF EXISTS cost_usd,
    DROP COLUMN IF EXISTS cost_provider;

DROP TABLE IF EXISTS workspace_quota;
DROP TABLE IF EXISTS workspace_secret;

ALTER TABLE personal_access_token DROP COLUMN IF EXISTS scopes;

DROP INDEX IF EXISTS idx_inbox_item_unresolved;
ALTER TABLE inbox_item DROP CONSTRAINT IF EXISTS inbox_item_resolution_check;
ALTER TABLE inbox_item DROP CONSTRAINT IF EXISTS inbox_item_severity_check;
ALTER TABLE inbox_item ADD CONSTRAINT inbox_item_severity_check
    CHECK (severity IN ('action_required', 'attention', 'info'));
ALTER TABLE inbox_item
    DROP COLUMN IF EXISTS resolution_by,
    DROP COLUMN IF EXISTS resolution,
    DROP COLUMN IF EXISTS resolved_at,
    DROP COLUMN IF EXISTS action_required,
    DROP COLUMN IF EXISTS channel_id,
    DROP COLUMN IF EXISTS thread_id,
    DROP COLUMN IF EXISTS slot_id,
    DROP COLUMN IF EXISTS task_id,
    DROP COLUMN IF EXISTS plan_id;

DROP INDEX IF EXISTS idx_activity_log_event_type;
DROP INDEX IF EXISTS idx_activity_log_task;
DROP INDEX IF EXISTS idx_activity_log_project;
DROP INDEX IF EXISTS idx_activity_log_workspace_time;
CREATE INDEX IF NOT EXISTS idx_activity_log_issue ON activity_log(issue_id);

ALTER TABLE activity_log
    DROP COLUMN IF EXISTS retention_class,
    DROP COLUMN IF EXISTS payload,
    DROP COLUMN IF EXISTS related_runtime_id,
    DROP COLUMN IF EXISTS related_agent_id,
    DROP COLUMN IF EXISTS related_thread_id,
    DROP COLUMN IF EXISTS related_channel_id,
    DROP COLUMN IF EXISTS related_execution_id,
    DROP COLUMN IF EXISTS related_slot_id,
    DROP COLUMN IF EXISTS related_task_id,
    DROP COLUMN IF EXISTS related_plan_id,
    DROP COLUMN IF EXISTS related_project_id,
    DROP COLUMN IF EXISTS real_operator_type,
    DROP COLUMN IF EXISTS real_operator_id,
    DROP COLUMN IF EXISTS effective_actor_type,
    DROP COLUMN IF EXISTS effective_actor_id;

ALTER TABLE activity_log RENAME COLUMN event_type TO action;
```

- [ ] **Step 3: Apply + verify**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make migrate-up
psql "$DATABASE_URL" -c "\d activity_log" | grep -E 'event_type|related_|payload|retention_class'
psql "$DATABASE_URL" -c "\d inbox_item" | grep -E 'plan_id|task_id|action_required|resolved_at'
psql "$DATABASE_URL" -c "\d workspace_secret"
psql "$DATABASE_URL" -c "\d workspace_quota"
psql "$DATABASE_URL" -c "\d personal_access_token" | grep scopes
psql "$DATABASE_URL" -c "\d agent_task_queue" | grep -E 'cost_'
```

Expected: every column present.

- [ ] **Step 4: Commit**

```bash
git add server/migrations/051_cross_cutting_infra.up.sql server/migrations/051_cross_cutting_infra.down.sql
git commit -m "feat(db): cross-cutting infra schema (activity_log/inbox/secrets/quota/cost)"
```

---

### Task 4: sqlc queries for new tables

**Files:**
- Create: `server/pkg/db/queries/workspace_secret.sql`
- Create: `server/pkg/db/queries/workspace_quota.sql`
- Modify: `server/pkg/db/queries/activity.sql`
- Modify: `server/pkg/db/queries/inbox.sql`
- Modify: `server/pkg/db/queries/personal_access_token.sql`

- [ ] **Step 1: workspace_secret.sql**

```sql
-- name: CreateWorkspaceSecret :one
INSERT INTO workspace_secret (workspace_id, key, value_encrypted, created_by)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetWorkspaceSecret :one
SELECT * FROM workspace_secret
WHERE workspace_id = $1 AND key = $2;

-- name: ListWorkspaceSecrets :many
-- Returns metadata only — value_encrypted is excluded so the API surface is
-- safe to render in admin UI without further filtering.
SELECT id, workspace_id, key, created_by, created_at, rotated_at
FROM workspace_secret
WHERE workspace_id = $1
ORDER BY key ASC;

-- name: DeleteWorkspaceSecret :exec
DELETE FROM workspace_secret
WHERE workspace_id = $1 AND key = $2;

-- name: RotateWorkspaceSecret :one
UPDATE workspace_secret
SET value_encrypted = $3,
    rotated_at      = now()
WHERE workspace_id = $1 AND key = $2
RETURNING *;
```

- [ ] **Step 2: workspace_quota.sql**

```sql
-- name: GetWorkspaceQuota :one
SELECT * FROM workspace_quota
WHERE workspace_id = $1;

-- name: UpsertWorkspaceQuota :one
INSERT INTO workspace_quota (workspace_id, max_monthly_usd, max_concurrent_cloud_exec, max_monthly_plan_gen)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id) DO UPDATE
SET max_monthly_usd           = EXCLUDED.max_monthly_usd,
    max_concurrent_cloud_exec = EXCLUDED.max_concurrent_cloud_exec,
    max_monthly_plan_gen      = EXCLUDED.max_monthly_plan_gen,
    updated_at                = now()
RETURNING *;

-- name: AddWorkspaceUsage :exec
UPDATE workspace_quota
SET current_monthly_usd = current_monthly_usd + $2,
    updated_at          = now()
WHERE workspace_id = $1;

-- name: ResetWorkspaceMonthlyUsage :exec
-- Lazy reset: called when current_month differs from the current calendar month.
UPDATE workspace_quota
SET current_monthly_usd = 0,
    current_month       = date_trunc('month', now())::date,
    updated_at          = now()
WHERE workspace_id = $1
  AND current_month  < date_trunc('month', now())::date;

-- name: CountActiveCloudExecutions :one
-- Used by the claim-time guard. Counts agent_task_queue rows in 'dispatched'
-- or 'running' state for cloud-mode agents in the workspace.
SELECT count(*)::int AS active
FROM agent_task_queue q
JOIN agent a ON a.id = q.agent_id
WHERE a.workspace_id = $1
  AND a.runtime_mode = 'cloud'
  AND q.status IN ('dispatched', 'running');
```

- [ ] **Step 3: Replace activity.sql**

```sql
-- name: ListActivities :many
-- Legacy issue-scoped list (kept for issue timeline UI).
SELECT * FROM activity_log
WHERE issue_id = $1
ORDER BY created_at ASC
LIMIT $2 OFFSET $3;

-- name: ListActivitiesByWorkspace :many
SELECT * FROM activity_log
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListActivitiesByProject :many
SELECT * FROM activity_log
WHERE workspace_id = $1 AND related_project_id = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: ListActivitiesByTask :many
SELECT * FROM activity_log
WHERE workspace_id = $1 AND related_task_id = $2
ORDER BY created_at ASC
LIMIT $3 OFFSET $4;

-- name: ListActivitiesByEventType :many
-- Pattern uses LIKE so callers can filter by prefix (e.g. "slot:%").
SELECT * FROM activity_log
WHERE workspace_id = $1 AND event_type LIKE $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: ListActivitiesByActor :many
SELECT * FROM activity_log
WHERE workspace_id = $1 AND actor_id = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CreateActivity :one
INSERT INTO activity_log (
    workspace_id, event_type,
    actor_id, actor_type,
    effective_actor_id, effective_actor_type,
    real_operator_id, real_operator_type,
    related_project_id, related_plan_id, related_task_id, related_slot_id,
    related_execution_id, related_channel_id, related_thread_id,
    related_agent_id, related_runtime_id,
    issue_id, payload, retention_class
) VALUES (
    $1, $2,
    $3, $4,
    $5, $6,
    $7, $8,
    $9, $10, $11, $12,
    $13, $14, $15,
    $16, $17,
    $18, $19, $20
)
RETURNING *;
```

- [ ] **Step 4: Append to inbox.sql**

Update `CreateInboxItem` to include the new columns and add resolve queries:

```sql
-- name: CreateInboxItemFull :one
-- New canonical insert for Plan 4. Existing CreateInboxItem stays for legacy callers.
INSERT INTO inbox_item (
    workspace_id, recipient_type, recipient_id,
    type, severity, title, body,
    actor_type, actor_id, details,
    issue_id, plan_id, task_id, slot_id, thread_id, channel_id,
    action_required
) VALUES (
    $1, $2, $3,
    $4, $5, $6, $7,
    $8, $9, $10,
    $11, $12, $13, $14, $15, $16,
    $17
)
RETURNING *;

-- name: ResolveInboxItem :one
UPDATE inbox_item
SET resolved_at   = now(),
    resolution    = $2,
    resolution_by = $3
WHERE id = $1
RETURNING *;

-- name: ListUnresolvedInbox :many
SELECT i.*, iss.status AS issue_status
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
WHERE i.workspace_id = $1
  AND i.recipient_type = $2
  AND i.recipient_id = $3
  AND i.archived = FALSE
  AND i.resolved_at IS NULL
ORDER BY i.created_at DESC;
```

- [ ] **Step 5: Update personal_access_token.sql**

Replace `CreatePersonalAccessToken`:

```sql
-- name: CreatePersonalAccessToken :one
INSERT INTO personal_access_token (user_id, name, token_hash, token_prefix, expires_at, scopes)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;
```

- [ ] **Step 6: Regenerate sqlc**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make sqlc
```

Expected: clean. If the existing call-sites for `CreateActivity` / `CreatePersonalAccessToken` break, fix them by passing the new arg (default to empty UUIDs / empty `[]string{}`).

- [ ] **Step 7: Build**

```bash
cd server && go build ./...
```

Address any callers — likely the existing `handler/activity.go` `CreateActivity` call needs the new fields. Pass `pgtype.UUID{}` (Valid=false) for the `related_*` fields and `[]byte("{}")` for payload until the rewrite in Task 6.

- [ ] **Step 8: Commit**

```bash
git add server/pkg/db/queries/ server/pkg/db/generated/ server/internal/handler/
git commit -m "feat(sqlc): queries for activity_log/inbox/secret/quota/PAT scopes"
```

---

### Task 5: Workspace secret CRUD API

**Files:**
- Create: `server/internal/handler/workspace_secret.go`
- Modify: `server/internal/handler/handler.go` (add `MasterKey []byte` field)
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Read master key at startup**

In `server/cmd/server/main.go` (or wherever the `Handler` is constructed), decode `MYTEAM_SECRET_KEY`:

```go
import "encoding/hex"
keyHex := strings.TrimSpace(os.Getenv("MYTEAM_SECRET_KEY"))
if keyHex == "" {
    slog.Warn("MYTEAM_SECRET_KEY not set; workspace_secret API will refuse encryption requests")
}
masterKey, err := hex.DecodeString(keyHex)
if err != nil || (len(masterKey) != 0 && len(masterKey) != 32) {
    return fmt.Errorf("MYTEAM_SECRET_KEY must be empty or 32 bytes hex (got %d bytes)", len(masterKey))
}
```

Pass `masterKey` into `handler.New(...)` and add a corresponding field to the `Handler` struct in `handler.go`.

- [ ] **Step 2: Implement the handler**

```go
// server/internal/handler/workspace_secret.go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
	"github.com/MyAIOSHub/MyTeam/server/pkg/crypto"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

type secretRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type secretResponse struct {
	ID         string  `json:"id"`
	Key        string  `json:"key"`
	MaskedHint string  `json:"masked_hint"`     // last 4 chars only
	CreatedAt  string  `json:"created_at"`
	RotatedAt  *string `json:"rotated_at,omitempty"`
}

// CreateWorkspaceSecret — POST /api/workspaces/{id}/secrets
// Owner / Admin only.
func (h *Handler) CreateWorkspaceSecret(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsID := parseUUID(chi.URLParam(r, "id"))
	if len(h.MasterKey) != 32 {
		errcode.Write(w, errcode.AuthForbidden, map[string]any{"hint": "secret encryption disabled"})
		return
	}

	var req secretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" || req.Value == "" {
		errcode.Write(w, errcode.Code{Code: "INVALID_BODY", Message: "key and value required", HTTPStatus: 400}, nil)
		return
	}

	ct := crypto.Encrypt([]byte(req.Value), h.MasterKey)
	row, err := h.Queries.CreateWorkspaceSecret(r.Context(), db.CreateWorkspaceSecretParams{
		WorkspaceID:    wsID,
		Key:            req.Key,
		ValueEncrypted: ct,
		CreatedBy:      userID,
	})
	if err != nil {
		errcode.Write(w, errcode.Code{Code: "SECRET_WRITE_FAILED", Message: err.Error(), HTTPStatus: 500}, nil)
		return
	}
	writeJSON(w, http.StatusCreated, toSecretResponse(row, req.Value))
}

// ListWorkspaceSecrets — GET /api/workspaces/{id}/secrets
func (h *Handler) ListWorkspaceSecrets(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsID := parseUUID(chi.URLParam(r, "id"))
	rows, err := h.Queries.ListWorkspaceSecrets(r.Context(), wsID)
	if err != nil {
		errcode.Write(w, errcode.Code{Code: "SECRET_LIST_FAILED", Message: err.Error(), HTTPStatus: 500}, nil)
		return
	}
	out := make([]secretResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSecretResponse(row, ""))
	}
	writeJSON(w, http.StatusOK, out)
}

// DeleteWorkspaceSecret — DELETE /api/workspaces/{id}/secrets/{key}
func (h *Handler) DeleteWorkspaceSecret(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsID := parseUUID(chi.URLParam(r, "id"))
	key := chi.URLParam(r, "key")
	if err := h.Queries.DeleteWorkspaceSecret(r.Context(), db.DeleteWorkspaceSecretParams{WorkspaceID: wsID, Key: key}); err != nil {
		errcode.Write(w, errcode.Code{Code: "SECRET_DELETE_FAILED", Message: err.Error(), HTTPStatus: 500}, nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// toSecretResponse masks the value to a 4-char tail hint when raw is provided
// (only on Create — listing never returns the cleartext).
func toSecretResponse(row any, raw string) secretResponse {
	// Adapter — actual struct depends on sqlc-generated row shapes. Implement
	// case-by-case using row's ID/Key/CreatedAt/RotatedAt fields.
	// (Pseudo: out := secretResponse{...})
	return secretResponse{} // TODO: fill from row
}
```

The `toSecretResponse` helper has two callers (single row from Create, list row from List) — implement two type assertions or one method per row type, whichever sqlc emits.

- [ ] **Step 3: Mount routes**

In `server/cmd/server/router.go` inside the workspace `Admin-level access` group:

```go
r.Get("/secrets", h.ListWorkspaceSecrets)
r.Post("/secrets", h.CreateWorkspaceSecret)
r.Delete("/secrets/{key}", h.DeleteWorkspaceSecret)
```

- [ ] **Step 4: Build + smoke-test**

```bash
cd server && go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/workspace_secret.go server/internal/handler/handler.go server/cmd/server/
git commit -m "feat(api): workspace_secret CRUD with AES-256-GCM"
```

---

### Task 6: Activity log service + query API

**Files:**
- Create: `server/internal/service/activity_log.go`
- Create: `server/internal/handler/activity_log.go`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Write the service helper**

```go
// server/internal/service/activity_log.go
package service

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// ActivityEntry is the input shape for service.WriteActivity.
type ActivityEntry struct {
	WorkspaceID        uuid.UUID
	EventType          string

	ActorID            uuid.UUID
	ActorType          string

	EffectiveActorID   uuid.UUID
	EffectiveActorType string

	RealOperatorID     uuid.UUID
	RealOperatorType   string

	ProjectID, PlanID, TaskID, SlotID, ExecutionID uuid.UUID
	ChannelID, ThreadID, AgentID, RuntimeID, IssueID uuid.UUID

	Payload         map[string]any
	RetentionClass  string // "permanent" by default
}

// ActivityLogger is implemented by *db.Queries.
type ActivityLogger interface {
	CreateActivity(ctx context.Context, arg db.CreateActivityParams) (db.ActivityLog, error)
}

// WriteActivity persists one audit row. Caller passes *db.Queries OR the
// transactional Queries handle (sqlc allows queries.WithTx(tx)).
func WriteActivity(ctx context.Context, q ActivityLogger, e ActivityEntry) error {
	payload, _ := json.Marshal(e.Payload)
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	if e.RetentionClass == "" {
		e.RetentionClass = "permanent"
	}

	_, err := q.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID:        e.WorkspaceID,
		EventType:          e.EventType,
		ActorID:            uuidValue(e.ActorID),
		ActorType:          textValue(e.ActorType),
		EffectiveActorID:   uuidValue(e.EffectiveActorID),
		EffectiveActorType: textValue(e.EffectiveActorType),
		RealOperatorID:     uuidValue(e.RealOperatorID),
		RealOperatorType:   textValue(e.RealOperatorType),
		RelatedProjectID:   uuidValue(e.ProjectID),
		RelatedPlanID:      uuidValue(e.PlanID),
		RelatedTaskID:      uuidValue(e.TaskID),
		RelatedSlotID:      uuidValue(e.SlotID),
		RelatedExecutionID: uuidValue(e.ExecutionID),
		RelatedChannelID:   uuidValue(e.ChannelID),
		RelatedThreadID:    uuidValue(e.ThreadID),
		RelatedAgentID:     uuidValue(e.AgentID),
		RelatedRuntimeID:   uuidValue(e.RuntimeID),
		IssueID:            uuidValue(e.IssueID),
		Payload:            payload,
		RetentionClass:     e.RetentionClass,
	})
	return err
}

func uuidValue(u uuid.UUID) pgtype.UUID {
	if u == uuid.Nil {
		return pgtype.UUID{Valid: false}
	}
	return pgtype.UUID{Bytes: u, Valid: true}
}
func textValue(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}
```

Adjust the field types to match what sqlc actually generates (it may emit `pgtype.UUID` or `uuid.NullUUID` — read `server/pkg/db/generated/models.go` after `make sqlc`).

- [ ] **Step 2: Write the query handler**

```go
// server/internal/handler/activity_log.go
package handler

import (
	"net/http"
	"strconv"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// QueryActivityLog — GET /api/activity-log
// Filters (mutually exclusive priority order): task_id, project_id, event_type, actor_id.
// Falls back to workspace listing if none supplied.
func (h *Handler) QueryActivityLog(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsID := parseUUID(resolveWorkspaceID(r))

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	switch {
	case r.URL.Query().Get("task_id") != "":
		taskID := parseUUID(r.URL.Query().Get("task_id"))
		rows, err := h.Queries.ListActivitiesByTask(r.Context(), db.ListActivitiesByTaskParams{
			WorkspaceID: wsID, RelatedTaskID: uuidValue(taskID),
			Limit: int32(limit), Offset: int32(offset),
		})
		writeListOrErr(w, rows, err)
	case r.URL.Query().Get("project_id") != "":
		projID := parseUUID(r.URL.Query().Get("project_id"))
		rows, err := h.Queries.ListActivitiesByProject(r.Context(), db.ListActivitiesByProjectParams{
			WorkspaceID: wsID, RelatedProjectID: uuidValue(projID),
			Limit: int32(limit), Offset: int32(offset),
		})
		writeListOrErr(w, rows, err)
	case r.URL.Query().Get("event_type") != "":
		rows, err := h.Queries.ListActivitiesByEventType(r.Context(), db.ListActivitiesByEventTypeParams{
			WorkspaceID: wsID, EventType: r.URL.Query().Get("event_type"),
			Limit: int32(limit), Offset: int32(offset),
		})
		writeListOrErr(w, rows, err)
	case r.URL.Query().Get("actor_id") != "":
		actorID := parseUUID(r.URL.Query().Get("actor_id"))
		rows, err := h.Queries.ListActivitiesByActor(r.Context(), db.ListActivitiesByActorParams{
			WorkspaceID: wsID, ActorID: uuidValue(actorID),
			Limit: int32(limit), Offset: int32(offset),
		})
		writeListOrErr(w, rows, err)
	default:
		rows, err := h.Queries.ListActivitiesByWorkspace(r.Context(), db.ListActivitiesByWorkspaceParams{
			WorkspaceID: wsID, Limit: int32(limit), Offset: int32(offset),
		})
		writeListOrErr(w, rows, err)
	}
}

func writeListOrErr[T any](w http.ResponseWriter, rows []T, err error) {
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}
```

The `uuidValue` helper from Task 6 step 1 should be reused; export it (`UUIDValue`) or copy.

- [ ] **Step 3: Mount the route**

In the protected workspace member group of `router.go`:

```go
r.Get("/api/activity-log", h.QueryActivityLog)
```

- [ ] **Step 4: Tests + commit**

```bash
cd server && go build ./... && go test ./internal/service/...
git add server/internal/service/activity_log.go server/internal/handler/activity_log.go server/cmd/server/router.go
git commit -m "feat(audit): activity_log write helper + query API"
```

---

### Task 7: Inbox extended types + resolve API

**Files:**
- Modify: `server/internal/handler/inbox.go`
- Modify: `apps/web/shared/types/inbox.ts`
- Modify: `apps/web/shared/types/activity.ts`
- Modify: `apps/web/shared/api/index.ts` (or `client.ts`)

- [ ] **Step 1: Backend handler additions**

In `server/internal/handler/inbox.go` add three handlers (mark-all-read may already exist — check first):

```go
// ResolveInbox — POST /api/inbox/{id}/resolve
type resolveRequest struct {
	Resolution string `json:"resolution"` // approved/rejected/dismissed/auto_resolved
}
func (h *Handler) ResolveInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id := parseUUID(chi.URLParam(r, "id"))
	var req resolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errcode.Write(w, errcode.Code{Code: "INVALID_BODY", Message: err.Error(), HTTPStatus: 400}, nil)
		return
	}
	row, err := h.Queries.ResolveInboxItem(r.Context(), db.ResolveInboxItemParams{
		ID: id, Resolution: pgtype.Text{String: req.Resolution, Valid: true},
		ResolutionBy: pgtype.UUID{Bytes: userID, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// ListUnresolvedInbox — GET /api/inbox?unresolved=true
// Wire from existing ListInbox by branching on the query param.
```

Mount routes in `router.go`:

```go
r.Post("/api/inbox/{id}/resolve", h.ResolveInbox)
// confirm /api/inbox/{id}/read and /api/inbox/mark-all-read already exist; if not, add them.
```

- [ ] **Step 2: Frontend `inbox.ts` rewrite**

Replace the file contents:

```typescript
import type { IssueStatus } from "./issue";

// PRD §8.1 — 3-value severity (info/warning/critical) PLUS legacy values
// kept until the inbox UI is rewritten in a later plan.
export type InboxSeverity =
  | "action_required"
  | "attention"
  | "info"
  | "warning"
  | "critical";

// PRD §8.2 — full type enum.
export type InboxItemType =
  // Project chain
  | "human_input_needed"
  | "review_needed"
  | "task_attention_needed"
  | "plan_approval_needed"
  | "run_completed"
  | "run_failed"
  // Session / channel chain
  | "reply_slow"
  | "mention"
  | "dm_received"
  // Account chain
  | "impersonation_expiring"
  | "agent_suspended"
  | "runtime_offline"
  // System
  | "system_announcement"
  // Legacy types still emitted by the existing handlers
  | "issue_assigned"
  | "unassigned"
  | "assignee_changed"
  | "status_changed"
  | "priority_changed"
  | "due_date_changed"
  | "new_comment"
  | "mentioned"
  | "review_requested"
  | "task_completed"
  | "task_failed"
  | "agent_blocked"
  | "agent_completed"
  | "reaction_added";

export type InboxResolution =
  | "approved"
  | "rejected"
  | "dismissed"
  | "auto_resolved";

export interface InboxItem {
  id: string;
  workspace_id: string;
  recipient_type: "member" | "agent";
  recipient_id: string;
  actor_type: "member" | "agent" | "system" | null;
  actor_id: string | null;
  type: InboxItemType;
  severity: InboxSeverity;
  // Related entities (Plan 4 additions)
  issue_id: string | null;
  plan_id: string | null;
  task_id: string | null;
  slot_id: string | null;
  thread_id: string | null;
  channel_id: string | null;
  // Body
  title: string;
  body: string | null;
  details: Record<string, string> | null;
  issue_status: IssueStatus | null;
  // Lifecycle
  read: boolean;
  archived: boolean;
  action_required: boolean;
  resolved_at: string | null;
  resolution: InboxResolution | null;
  resolution_by: string | null;
  created_at: string;
}
```

- [ ] **Step 3: Frontend `activity.ts` rewrite**

```typescript
import type { Reaction } from "./comment";
import type { Attachment } from "./attachment";

// PRD §3 — full activity_log row shape.
export interface ActivityLog {
  id: string;
  workspace_id: string;
  event_type: string;
  actor_id: string | null;
  actor_type: "member" | "agent" | "system" | null;
  effective_actor_id: string | null;
  effective_actor_type: "member" | "agent" | "system" | null;
  real_operator_id: string | null;
  real_operator_type: "member" | "agent" | "system" | null;
  related_project_id: string | null;
  related_plan_id: string | null;
  related_task_id: string | null;
  related_slot_id: string | null;
  related_execution_id: string | null;
  related_channel_id: string | null;
  related_thread_id: string | null;
  related_agent_id: string | null;
  related_runtime_id: string | null;
  issue_id: string | null;
  payload: Record<string, unknown>;
  retention_class: "permanent" | "long" | "short";
  created_at: string;
}

// TimelineEntry is kept for the existing issue timeline UI — keep both
// the legacy shape (`action`) and the new shape (`event_type`) so the
// component can render either source.
export interface TimelineEntry {
  type: "activity" | "comment";
  id: string;
  actor_type: string;
  actor_id: string;
  created_at: string;
  // Activity (legacy)
  action?: string;
  // Activity (Plan 4)
  event_type?: string;
  details?: Record<string, unknown>;
  payload?: Record<string, unknown>;
  // Comment fields
  content?: string;
  parent_id?: string | null;
  updated_at?: string;
  comment_type?: string;
  reactions?: Reaction[];
  attachments?: Attachment[];
}
```

- [ ] **Step 4: API methods**

In `apps/web/shared/api/` (the `ApiClient` class):

```typescript
async resolveInboxItem(id: string, resolution: InboxResolution): Promise<InboxItem> {
  return this.post<InboxItem>(`/api/inbox/${id}/resolve`, { resolution });
}

async listActivityLog(params: {
  task_id?: string;
  project_id?: string;
  event_type?: string;
  actor_id?: string;
  limit?: number;
}): Promise<ActivityLog[]> {
  const q = new URLSearchParams(params as Record<string, string>);
  return this.get<ActivityLog[]>(`/api/activity-log?${q.toString()}`);
}
```

- [ ] **Step 5: Typecheck + commit**

```bash
pnpm typecheck
cd server && go build ./...
git add apps/web/shared/types/inbox.ts apps/web/shared/types/activity.ts apps/web/shared/api/ server/internal/handler/inbox.go server/cmd/server/router.go
git commit -m "feat(inbox,activity): extended types + resolve/query APIs"
```

---

### Task 8: Workspace quota enforcement at claim time

**Files:**
- Create: `server/internal/service/quota.go`
- Modify: `server/internal/service/cloud_executor.go`

- [ ] **Step 1: Implement the guard**

```go
// server/internal/service/quota.go
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// QuotaService enforces workspace_quota at claim time.
type QuotaService struct {
	Queries *db.Queries
}

func NewQuotaService(q *db.Queries) *QuotaService {
	return &QuotaService{Queries: q}
}

// CheckClaim is called immediately before transitioning an Execution to claimed.
// Returns errcode.QuotaExceeded or errcode.QuotaConcurrentLimit (wrapped) on
// failure; nil if the claim is permitted.
func (s *QuotaService) CheckClaim(ctx context.Context, workspaceID uuid.UUID) error {
	// Lazy monthly reset — cheap, idempotent.
	_ = s.Queries.ResetWorkspaceMonthlyUsage(ctx, workspaceID)

	quota, err := s.Queries.GetWorkspaceQuota(ctx, workspaceID)
	if err != nil {
		// No quota row = unlimited (workspace has not opted in to enforcement).
		// Caller should still call UpsertWorkspaceQuota at workspace creation.
		return nil
	}

	if quota.CurrentMonthlyUsd.Valid && quota.MaxMonthlyUsd.Valid {
		used := quota.CurrentMonthlyUsd.Int.Int64()
		max := quota.MaxMonthlyUsd.Int.Int64()
		if used >= max {
			return WrappedCode{Code: errcode.QuotaExceeded, Detail: fmt.Sprintf("usd %d / %d", used, max)}
		}
	}

	active, err := s.Queries.CountActiveCloudExecutions(ctx, workspaceID)
	if err == nil && active >= quota.MaxConcurrentCloudExec {
		return WrappedCode{Code: errcode.QuotaConcurrentLimit, Detail: fmt.Sprintf("active %d / %d", active, quota.MaxConcurrentCloudExec)}
	}

	_ = time.Now // keep import
	return nil
}

// WrappedCode wraps an errcode.Code so the cloud executor can translate it
// into both an inbox notification and a writeError(...) call.
type WrappedCode struct {
	Code   errcode.Code
	Detail string
}

func (w WrappedCode) Error() string { return fmt.Sprintf("%s: %s", w.Code.Code, w.Detail) }

// AsCode unwraps a WrappedCode if present.
func AsCode(err error) (errcode.Code, bool) {
	var wc WrappedCode
	if errors.As(err, &wc) {
		return wc.Code, true
	}
	return errcode.Code{}, false
}
```

(Field names like `MaxMonthlyUsd` may be `pgtype.Numeric` — adapt after `make sqlc`.)

- [ ] **Step 2: Call from CloudExecutorService**

Edit `server/internal/service/cloud_executor.go`. Add `Quota *QuotaService` to the struct + constructor; before transitioning to `claimed`:

```go
if err := s.Quota.CheckClaim(ctx, agentRow.WorkspaceID); err != nil {
    code, _ := AsCode(err)
    slog.Warn("[cloud-executor] quota check failed",
        "task_id", task.ID, "workspace_id", agentRow.WorkspaceID,
        "code", code.Code, "err", err)
    // Mark task failed with QUOTA_EXCEEDED; the scheduler will surface it via inbox.
    _ = s.Queries.MarkAgentTaskFailed(ctx, db.MarkAgentTaskFailedParams{
        ID:    task.ID,
        Error: pgtype.Text{String: code.Code, Valid: true},
    })
    return
}
```

(Use whatever the existing "fail task" sqlc method is named in the generated package — search for `MarkAgentTaskFailed` or similar.)

- [ ] **Step 3: Wire in router**

In `router.go` where `cloudExecutor` is constructed:

```go
quotaSvc := service.NewQuotaService(queries)
cloudExecutor := service.NewCloudExecutorService(queries, hub, bus, h.TaskService)
cloudExecutor.Quota = quotaSvc
cloudExecutor.Start(context.Background())
```

- [ ] **Step 4: Build + commit**

```bash
cd server && go build ./...
git add server/internal/service/quota.go server/internal/service/cloud_executor.go server/cmd/server/router.go
git commit -m "feat(quota): enforce workspace_quota at cloud-executor claim"
```

---

### Task 9: MCP tools skeleton (17 tools + dispatcher)

**Files:**
- Create: `server/internal/mcp/tool.go`
- Create: `server/internal/mcp/auth.go`
- Create: `server/internal/mcp/registry.go`
- Create: `server/internal/mcp/tools/<name>.go` — 17 files
- Create: `server/internal/handler/mcp.go`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Tool interface + dispatcher**

```go
// server/internal/mcp/tool.go
package mcp

import (
	"context"
	"encoding/json"
)

// Tool is one MCP tool exposed to Claude Agent SDK / myteam CLI.
// Each tool is a thin wrapper over an existing handler / query.
type Tool interface {
	Name() string
	// Schema is the JSON Schema (PRD §7.2) describing the tool's input.
	Schema() json.RawMessage
	// Exec runs the tool. ctx carries workspace_id, user_id, agent_id.
	Exec(ctx context.Context, args json.RawMessage) (any, error)
}
```

```go
// server/internal/mcp/registry.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

func (r *Registry) Register(t Tool) { r.tools[t.Name()] = t }

func (r *Registry) List() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// Exec dispatches a single tool call.
func (r *Registry) Exec(ctx context.Context, name string, args json.RawMessage) (any, error) {
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("%s: unknown tool %q", errcode.MCPToolNotAvailable.Code, name)
	}
	return t.Exec(ctx, args)
}
```

```go
// server/internal/mcp/auth.go
package mcp

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

type ctxKey int

const (
	keyWorkspaceID ctxKey = iota
	keyUserID
	keyAgentID
	keyRuntimeMode // "local" | "cloud"
)

// Caller carries authn/authz context for a tool invocation.
type Caller struct {
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	AgentID     uuid.UUID
	RuntimeMode string
}

func WithCaller(ctx context.Context, c Caller) context.Context {
	ctx = context.WithValue(ctx, keyWorkspaceID, c.WorkspaceID)
	ctx = context.WithValue(ctx, keyUserID, c.UserID)
	ctx = context.WithValue(ctx, keyAgentID, c.AgentID)
	ctx = context.WithValue(ctx, keyRuntimeMode, c.RuntimeMode)
	return ctx
}

func CallerFrom(ctx context.Context) (Caller, error) {
	ws, ok1 := ctx.Value(keyWorkspaceID).(uuid.UUID)
	user, ok2 := ctx.Value(keyUserID).(uuid.UUID)
	agent, ok3 := ctx.Value(keyAgentID).(uuid.UUID)
	mode, _ := ctx.Value(keyRuntimeMode).(string)
	if !ok1 || !ok2 || !ok3 {
		return Caller{}, errors.New("mcp: caller missing workspace/user/agent")
	}
	return Caller{WorkspaceID: ws, UserID: user, AgentID: agent, RuntimeMode: mode}, nil
}
```

- [ ] **Step 2: Generate the 17 tool files**

Each tool follows the same pattern. Example for `get_issue`:

```go
// server/internal/mcp/tools/get_issue.go
package tools

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

type GetIssue struct{ Q *db.Queries }

func (GetIssue) Name() string { return "get_issue" }

func (GetIssue) Schema() json.RawMessage {
	return json.RawMessage(`{
	  "type": "object",
	  "required": ["issue_id"],
	  "properties": { "issue_id": { "type": "string", "format": "uuid" } }
	}`)
}

func (t GetIssue) Exec(ctx context.Context, args json.RawMessage) (any, error) {
	var in struct{ IssueID string `json:"issue_id"` }
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	caller, err := mcp.CallerFrom(ctx)
	if err != nil {
		return nil, err
	}
	_ = caller // TODO: workspace_id check on issue
	id, err := uuid.Parse(in.IssueID)
	if err != nil {
		return nil, err
	}
	return t.Q.GetIssue(ctx, id)
}
```

Create the remaining 16 stubs with the SAME structure:

| File | Tool | Required input | Notes |
|---|---|---|---|
| `tools/list_issue_comments.go` | `list_issue_comments` | issue_id, limit, offset | calls `Q.ListComments` |
| `tools/create_comment.go` | `create_comment` | issue_id, body | calls `Q.CreateComment` |
| `tools/update_issue_status.go` | `update_issue_status` | issue_id, status | calls `Q.UpdateIssueStatus` |
| `tools/list_assigned_projects.go` | `list_assigned_projects` | status (optional) | calls `Q.ListProjectsByAssignee` |
| `tools/get_project.go` | `get_project` | project_id | calls `Q.GetProject` |
| `tools/search_project_context.go` | `search_project_context` | project_id, query | TODO: backed by future search service; return `[]` for now |
| `tools/list_project_files.go` | `list_project_files` | project_id, path_prefix | calls `Q.ListFileIndex` |
| `tools/download_attachment.go` | `download_attachment` | attachment_id | returns metadata + signed URL via existing attachment handler |
| `tools/upload_artifact.go` | `upload_artifact` | task_id, slot_id, execution_id, content | TODO until artifact table lands; return `not_implemented` |
| `tools/complete_task.go` | `complete_task` | task_id, result | calls `Q.MarkAgentTaskCompleted` |
| `tools/request_approval.go` | `request_approval` | task_id, slot_id, context | creates an `inbox_item` of type `review_needed` |
| `tools/read_file.go` | `read_file` | project_id, file_index_id OR path | calls `Q.GetFileIndex` |
| `tools/apply_patch.go` | `apply_patch` | project_id, patch | TODO: `not_implemented` |
| `tools/create_pr.go` | `create_pr` | project_id, branch, title, body | TODO: `not_implemented` |
| `tools/checkout_repo.go` | `checkout_repo` | project_id | local-only; in cloud return `errcode.MCPToolNotAvailable` |
| `tools/local_file_read.go` | `local_file_read` | path | local-only; same |

For `checkout_repo` and `local_file_read`, add this guard at the top of `Exec`:

```go
caller, err := mcp.CallerFrom(ctx)
if err != nil {
    return nil, err
}
if caller.RuntimeMode == "cloud" {
    return nil, fmt.Errorf("%s: not available in cloud runtime", errcode.MCPToolNotAvailable.Code)
}
```

- [ ] **Step 3: HTTP handler**

```go
// server/internal/handler/mcp.go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
	"github.com/MyAIOSHub/MyTeam/server/internal/mcp"
)

// CallMCP — POST /api/mcp/{tool}
// Body is the raw input args; auth headers identify caller (workspace/user/agent).
func (h *Handler) CallMCP(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsID := parseUUID(resolveWorkspaceID(r))
	agentID := uuid.Nil
	if a := r.Header.Get("X-Agent-ID"); a != "" {
		agentID = parseUUID(a)
	}

	mode := r.Header.Get("X-Runtime-Mode") // "local" or "cloud"
	ctx := mcp.WithCaller(r.Context(), mcp.Caller{
		WorkspaceID: wsID, UserID: userID, AgentID: agentID, RuntimeMode: mode,
	})

	body, _ := json.RawMessage{}, error(nil)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body = json.RawMessage("{}")
	}

	out, err := h.MCPRegistry.Exec(ctx, chi.URLParam(r, "tool"), body)
	if err != nil {
		errcode.Write(w, errcode.MCPToolNotAvailable, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ListMCPTools — GET /api/mcp
func (h *Handler) ListMCPTools(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		Name   string          `json:"name"`
		Schema json.RawMessage `json:"input_schema"`
	}
	out := make([]entry, 0)
	for _, t := range h.MCPRegistry.List() {
		out = append(out, entry{Name: t.Name(), Schema: t.Schema()})
	}
	writeJSON(w, http.StatusOK, out)
}
```

Add `MCPRegistry *mcp.Registry` to `Handler` struct; populate in `handler.New()` by registering every tool from step 2.

- [ ] **Step 3: Mount routes**

```go
r.Get("/api/mcp", h.ListMCPTools)
r.Post("/api/mcp/{tool}", h.CallMCP)
```

- [ ] **Step 4: Build + commit**

```bash
cd server && go build ./...
git add server/internal/mcp/ server/internal/handler/mcp.go server/internal/handler/handler.go server/cmd/server/router.go
git commit -m "feat(mcp): 17-tool skeleton + dispatcher + permission layer"
```

---

### Task 10: Prometheus metrics

**Files:**
- Create: `server/internal/metrics/metrics.go`
- Modify: `server/cmd/server/router.go`
- Modify: `server/go.mod` (add `github.com/prometheus/client_golang`)

- [ ] **Step 1: Add dependency**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691/server
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/promhttp
go mod tidy
```

- [ ] **Step 2: Define collectors**

```go
// server/internal/metrics/metrics.go
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PRD §14.1 — first 8 metrics.
var (
	ExecutionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "execution_duration_seconds",
			Help:    "Wall-clock duration of a single Execution.",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 12),
		},
		[]string{"runtime_mode", "provider", "status"},
	)
	ExecutionCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "execution_count_total", Help: "Total Executions."},
		[]string{"status"},
	)
	SchedulerQueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "scheduler_queue_depth", Help: "Tasks waiting to be scheduled."},
		[]string{"priority"},
	)
	RuntimeLoadRatio = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "runtime_load_ratio", Help: "current_load / concurrency_limit."},
		[]string{"runtime_id"},
	)
	PlanGenDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "plan_gen_duration_seconds",
			Help:    "PlanGenerator wall-clock duration.",
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 10),
		},
		[]string{"status"},
	)
	PlanGenTokenUsage = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "plan_gen_token_usage",
			Help:    "Tokens consumed per Plan generation.",
			Buckets: prometheus.LinearBuckets(1000, 5000, 10),
		},
		[]string{"direction"}, // "input" / "output"
	)
	WSConnectedClients = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "ws_connected_clients", Help: "Active WebSocket clients."},
		[]string{"workspace_id"},
	)
	WSEventPublished = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "ws_event_published_total", Help: "WebSocket events published."},
		[]string{"event_type"},
	)
)

// MustRegister attaches every collector to the default registry.
func MustRegister() {
	prometheus.MustRegister(
		ExecutionDuration,
		ExecutionCount,
		SchedulerQueueDepth,
		RuntimeLoadRatio,
		PlanGenDuration,
		PlanGenTokenUsage,
		WSConnectedClients,
		WSEventPublished,
	)
}

// Handler returns the /metrics HTTP handler.
func Handler() http.Handler { return promhttp.Handler() }
```

- [ ] **Step 3: Wire into router**

In `server/cmd/server/router.go` near the top of `NewRouter`:

```go
metrics.MustRegister()
```

And register the route at the public top level (alongside `/health`):

```go
r.Method(http.MethodGet, "/metrics", metrics.Handler())
```

- [ ] **Step 4: Verify**

```bash
cd server && go build ./...
make dev &
sleep 3
curl -s http://localhost:8080/metrics | head -40
kill %1
```

Expected: Prometheus exposition format with `execution_duration_seconds`, etc.

- [ ] **Step 5: Commit**

```bash
git add server/internal/metrics/ server/go.mod server/go.sum server/cmd/server/router.go
git commit -m "feat(metrics): Prometheus collectors + /metrics endpoint"
```

---

### Task 11: Event catalog file

**Files:**
- Create: `server/pkg/protocol/event_catalog.go`

- [ ] **Step 1: Write the file**

```go
package protocol

// EventCatalog enumerates every WebSocket / event-bus event the platform
// emits, with a human-readable description. It is the single source of
// truth referenced by docs and downstream code generators.
//
// Adding an event:
//   1. Add the constant to events.go.
//   2. Add an EventDef entry below.
//   3. Update PRD §2.2.

type EventDef struct {
	Type        string   // matches the const in events.go
	Domain      string   // "project" | "account" | "session" | "inbox"
	Description string
	// Subscribers is informational — actual routing lives in services.
	Subscribers []string // e.g. {"frontend", "scheduler"}
}

var EventCatalog = map[string]EventDef{
	// ----- Project domain -----
	EventProjectCreated:         {Type: EventProjectCreated, Domain: "project", Description: "Project created", Subscribers: []string{"frontend", "system_agent"}},
	EventProjectStatusChanged:   {Type: EventProjectStatusChanged, Domain: "project", Description: "Project.status changed", Subscribers: []string{"frontend"}},
	"plan:created":              {Type: "plan:created", Domain: "project", Description: "Plan created", Subscribers: []string{"frontend"}},
	"plan:approval_changed":     {Type: "plan:approval_changed", Domain: "project", Description: "Plan approval transition", Subscribers: []string{"frontend", "scheduler"}},
	EventRunStarted:             {Type: EventRunStarted, Domain: "project", Description: "ProjectRun started", Subscribers: []string{"frontend"}},
	"run:completed":             {Type: "run:completed", Domain: "project", Description: "Run finished successfully", Subscribers: []string{"frontend"}},
	"run:failed":                {Type: "run:failed", Domain: "project", Description: "Run failed", Subscribers: []string{"frontend"}},
	"run:cancelled":             {Type: "run:cancelled", Domain: "project", Description: "Run cancelled", Subscribers: []string{"frontend"}},
	"task:status_changed":       {Type: "task:status_changed", Domain: "project", Description: "Task status transition", Subscribers: []string{"frontend", "scheduler"}},
	"task:agent_assigned":       {Type: "task:agent_assigned", Domain: "project", Description: "Agent assigned to task", Subscribers: []string{"frontend"}},
	"slot:activated":            {Type: "slot:activated", Domain: "project", Description: "Slot waiting -> ready", Subscribers: []string{"frontend", "inbox"}},
	"slot:submitted":            {Type: "slot:submitted", Domain: "project", Description: "Slot submitted by participant", Subscribers: []string{"scheduler", "frontend"}},
	"slot:decision":             {Type: "slot:decision", Domain: "project", Description: "Slot decision recorded", Subscribers: []string{"scheduler", "frontend"}},
	"execution:claimed":         {Type: "execution:claimed", Domain: "project", Description: "Execution claimed by runtime", Subscribers: []string{"frontend"}},
	"execution:started":         {Type: "execution:started", Domain: "project", Description: "Execution running", Subscribers: []string{"frontend"}},
	"execution:completed":       {Type: "execution:completed", Domain: "project", Description: "Execution completed", Subscribers: []string{"scheduler", "frontend"}},
	"execution:failed":          {Type: "execution:failed", Domain: "project", Description: "Execution failed", Subscribers: []string{"scheduler", "frontend"}},
	"execution:progress":        {Type: "execution:progress", Domain: "project", Description: "Execution progress stream", Subscribers: []string{"frontend"}},
	"artifact:created":          {Type: "artifact:created", Domain: "project", Description: "Artifact created", Subscribers: []string{"frontend"}},
	"review:submitted":          {Type: "review:submitted", Domain: "project", Description: "Review created", Subscribers: []string{"frontend", "scheduler"}},

	// ----- Account domain -----
	EventAgentCreated:                  {Type: EventAgentCreated, Domain: "account", Description: "Agent created", Subscribers: []string{"frontend"}},
	EventAgentStatus:                   {Type: EventAgentStatus, Domain: "account", Description: "Agent status changed", Subscribers: []string{"frontend", "scheduler"}},
	"agent:identity_card_updated":      {Type: "agent:identity_card_updated", Domain: "account", Description: "Identity card edited", Subscribers: []string{"frontend"}},
	"runtime:online":                   {Type: "runtime:online", Domain: "account", Description: "Runtime online", Subscribers: []string{"frontend", "scheduler"}},
	"runtime:offline":                  {Type: "runtime:offline", Domain: "account", Description: "Runtime offline", Subscribers: []string{"frontend", "scheduler"}},
	"runtime:degraded":                 {Type: "runtime:degraded", Domain: "account", Description: "Runtime degraded", Subscribers: []string{"frontend", "scheduler"}},
	"impersonation:started":            {Type: "impersonation:started", Domain: "account", Description: "Impersonation session started", Subscribers: []string{"activity_log"}},
	"impersonation:ended":              {Type: "impersonation:ended", Domain: "account", Description: "Impersonation session ended", Subscribers: []string{"activity_log"}},

	// ----- Session / Channel domain -----
	"channel:created":                  {Type: "channel:created", Domain: "session", Description: "Channel created", Subscribers: []string{"frontend"}},
	"channel:member_added":             {Type: "channel:member_added", Domain: "session", Description: "Channel member added", Subscribers: []string{"frontend"}},
	"channel:member_removed":           {Type: "channel:member_removed", Domain: "session", Description: "Channel member removed", Subscribers: []string{"frontend"}},
	"thread:created":                   {Type: "thread:created", Domain: "session", Description: "Thread created", Subscribers: []string{"frontend"}},
	"thread:status_changed":            {Type: "thread:status_changed", Domain: "session", Description: "Thread status changed", Subscribers: []string{"frontend"}},
	"message:created":                  {Type: "message:created", Domain: "session", Description: "Message sent", Subscribers: []string{"frontend", "mediation"}},
	"message:updated":                  {Type: "message:updated", Domain: "session", Description: "Message updated", Subscribers: []string{"frontend"}},
	"message:deleted":                  {Type: "message:deleted", Domain: "session", Description: "Message deleted", Subscribers: []string{"frontend"}},
	"thread_context_item:created":      {Type: "thread_context_item:created", Domain: "session", Description: "Context item created", Subscribers: []string{"frontend"}},
	"thread_context_item:deleted":      {Type: "thread_context_item:deleted", Domain: "session", Description: "Context item deleted", Subscribers: []string{"frontend"}},

	// ----- Inbox domain -----
	EventInboxNew:        {Type: EventInboxNew, Domain: "inbox", Description: "InboxItem created", Subscribers: []string{"frontend"}},
	EventInboxRead:       {Type: EventInboxRead, Domain: "inbox", Description: "InboxItem marked read", Subscribers: []string{"frontend"}},
	"inbox:item_resolved": {Type: "inbox:item_resolved", Domain: "inbox", Description: "InboxItem resolved", Subscribers: []string{"frontend"}},
}
```

- [ ] **Step 2: Build + commit**

```bash
cd server && go build ./...
git add server/pkg/protocol/event_catalog.go
git commit -m "feat(protocol): event catalog enumeration (single source of truth)"
```

---

### Task 12: Frontend — Workspace Secret management UI

**Files:**
- Create: `apps/web/features/workspace/components/workspace-secrets.tsx`
- Create: `apps/web/app/(dashboard)/settings/secrets/page.tsx`
- Modify: `apps/web/shared/types/index.ts` (re-export `WorkspaceSecret`)

- [ ] **Step 1: Type**

In `apps/web/shared/types/workspace.ts` (or a new `workspace-secret.ts` if cleaner) add:

```typescript
export interface WorkspaceSecret {
  id: string;
  key: string;
  masked_hint: string; // last 4 chars only
  created_at: string;
  rotated_at: string | null;
}
```

Re-export from `apps/web/shared/types/index.ts`.

- [ ] **Step 2: API methods**

```typescript
async listSecrets(workspaceId: string): Promise<WorkspaceSecret[]> {
  return this.get<WorkspaceSecret[]>(`/api/workspaces/${workspaceId}/secrets`);
}
async createSecret(workspaceId: string, key: string, value: string): Promise<WorkspaceSecret> {
  return this.post<WorkspaceSecret>(`/api/workspaces/${workspaceId}/secrets`, { key, value });
}
async deleteSecret(workspaceId: string, key: string): Promise<void> {
  return this.delete<void>(`/api/workspaces/${workspaceId}/secrets/${encodeURIComponent(key)}`);
}
```

- [ ] **Step 3: Component**

```tsx
"use client";

import { useEffect, useState } from "react";
import { api } from "@/shared/api";
import { useWorkspaceStore } from "@/features/workspace";
import type { WorkspaceSecret } from "@/shared/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export function WorkspaceSecrets() {
  const workspaceId = useWorkspaceStore((s) => s.workspaceId);
  const [items, setItems] = useState<WorkspaceSecret[]>([]);
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!workspaceId) return;
    api.listSecrets(workspaceId).then(setItems).catch((e: Error) => setError(e.message));
  }, [workspaceId]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!workspaceId || !key || !value) return;
    try {
      const created = await api.createSecret(workspaceId, key, value);
      setItems((prev) => [...prev, created]);
      setKey("");
      setValue("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed");
    }
  };

  const remove = async (k: string) => {
    if (!workspaceId) return;
    await api.deleteSecret(workspaceId, k);
    setItems((prev) => prev.filter((it) => it.key !== k));
  };

  if (!workspaceId) return null;

  return (
    <div className="space-y-4">
      <h2 className="text-lg font-medium">Workspace secrets</h2>
      {error && <p className="text-destructive text-sm">{error}</p>}
      <ul className="divide-y rounded-md border">
        {items.map((s) => (
          <li key={s.id} className="flex items-center justify-between p-3">
            <div>
              <div className="font-mono text-sm">{s.key}</div>
              <div className="text-muted-foreground text-xs">…{s.masked_hint}</div>
            </div>
            <Button variant="ghost" size="sm" onClick={() => remove(s.key)}>
              Remove
            </Button>
          </li>
        ))}
        {items.length === 0 && (
          <li className="text-muted-foreground p-3 text-sm">No secrets yet.</li>
        )}
      </ul>
      <form onSubmit={submit} className="flex gap-2">
        <Input placeholder="key (e.g. anthropic_api_key)" value={key} onChange={(e) => setKey(e.target.value)} />
        <Input placeholder="value" type="password" value={value} onChange={(e) => setValue(e.target.value)} />
        <Button type="submit" disabled={!key || !value}>Add</Button>
      </form>
    </div>
  );
}
```

- [ ] **Step 4: Page route**

```tsx
// apps/web/app/(dashboard)/settings/secrets/page.tsx
import { WorkspaceSecrets } from "@/features/workspace/components/workspace-secrets";

export default function SecretsPage() {
  return (
    <main className="mx-auto max-w-3xl p-6">
      <WorkspaceSecrets />
    </main>
  );
}
```

- [ ] **Step 5: Typecheck + commit**

```bash
pnpm typecheck
git add apps/web/features/workspace/components/workspace-secrets.tsx apps/web/app/\(dashboard\)/settings/ apps/web/shared/types/ apps/web/shared/api/
git commit -m "feat(web): workspace secret management page"
```

---

### Task 13: Final verification

- [ ] **Step 1: Full backend tests**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make test
```

- [ ] **Step 2: Frontend typecheck + tests**

```bash
pnpm typecheck
pnpm test
```

- [ ] **Step 3: Live smoke**

```bash
make dev &
SERVER=$!
sleep 5
curl -s http://localhost:8080/health
curl -s http://localhost:8080/metrics | grep execution_count_total
# With a JWT in $TOKEN and a workspace in $WS:
# curl -s -H "Authorization: Bearer $TOKEN" -H "X-Workspace-ID: $WS" http://localhost:8080/api/mcp
# curl -s -H "Authorization: Bearer $TOKEN" -H "X-Workspace-ID: $WS" "http://localhost:8080/api/activity-log?limit=5"
kill $SERVER
```

- [ ] **Step 4: Confirm migrations**

```bash
psql "$DATABASE_URL" -c "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 3;"
```

Expected: `051` at top.

- [ ] **Step 5: Commit any tweaks**

If steps 1-4 surfaced fixes, commit them now.

---

## Self-Review Checklist

- [ ] `errcode` package compiles, has 25+ codes, `Write()` emits PRD §13.3 envelope.
- [ ] `pkg/crypto.Encrypt/Decrypt` round-trips and rejects tampered ciphertext.
- [ ] Migration 051 applied: activity_log has `event_type` + 12 related cols + 4 indexes; inbox_item has 8 new cols + unresolved index; `workspace_secret`, `workspace_quota` tables exist; `personal_access_token.scopes` exists; `agent_task_queue` has 4 cost cols.
- [ ] sqlc regenerated; `go build ./...` clean.
- [ ] `POST /api/workspaces/{id}/secrets` round-trips an encrypted value.
- [ ] `GET /api/activity-log` accepts `task_id`, `project_id`, `event_type`, `actor_id` filters.
- [ ] `POST /api/inbox/{id}/resolve` updates `resolved_at` + `resolution`.
- [ ] CloudExecutorService rejects claim with `QUOTA_EXCEEDED` when current ≥ max.
- [ ] `server/internal/mcp/` has 17 tool files + dispatcher; `GET /api/mcp` lists all.
- [ ] `checkout_repo` and `local_file_read` return `MCP_TOOL_NOT_AVAILABLE` when caller mode == "cloud".
- [ ] `/metrics` endpoint serves Prometheus exposition with all 8 collectors.
- [ ] `protocol.EventCatalog` enumerates every event from PRD §2.2.
- [ ] Frontend `inbox.ts` and `activity.ts` updated; secret management page typechecks.
- [ ] `make test`, `pnpm typecheck`, `pnpm test` all green (modulo pre-existing failures).

---

## Out of Scope (deferred to later plans)

- **Full migration of every `http.Error` / `writeError` call** to `errcode.Write` — only 1-2 example migrations land here. Bulk migration is a separate refactor commit.
- **Master key rotation logic** — `rotated_at` column exists; an admin CLI to re-encrypt all rows is Post-MVP per PRD §11.4.
- **Per-Project / per-User cost attribution** — PRD §12.5 marks this Post-MVP; this plan adds workspace-level enforcement only.
- **Real implementation of `upload_artifact`, `apply_patch`, `create_pr`, `search_project_context`** — skeletons only; bodies depend on Plan 5 (Project execution engine) and a future search service.
- **Cron-style monthly quota reset** — implemented as lazy reset on each `CheckClaim`; a true cron is unnecessary for MVP.
- **MCP tool input-schema validation** — schemas are returned but not enforced server-side; SDK clients are expected to validate before calling.
- **Activity log retention enforcement** — `retention_class` column exists; the cleanup job is a future task.
- **Existing `inbox_item` write paths migrating to `CreateInboxItemFull`** — kept as `CreateInboxItem` legacy for now; new code should use the full version.
- **Removing the original `details` JSONB on activity_log** — Plan 4 adds `payload` alongside; the existing column stays for the issue timeline UI until that UI moves to `payload`.
