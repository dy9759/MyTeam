# Session/Channel Restructure — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Retire the `session` / `session_participant` tables and the `message.session_id` column. Promote `Thread` to an independent first-class object with its own UUID, structured context (`thread_context_item`), and its own REST surface. Migrate every legacy session to a private channel + thread + decomposed context items. Cut the frontend over from `/sessions` to `/session`. Make MediationService the single routing source so `auto_reply_config` no longer triggers replies directly.

**Architecture:**
- **Phase 1 (additive):** `thread` table gains `workspace_id`, `root_message_id`, `issue_id`, `created_by/_type`, `status`, `metadata`, `last_activity_at`; `id` default switches to `gen_random_uuid()`. New tables `thread_context_item` and `session_migration_map`. No drops.
- **Phase 2 (additive):** New Thread API surface (`/api/channels/{id}/threads`, `/api/threads/{id}/...`). Compatibility resolver writes legacy `message.session_id` posts via `session_migration_map`.
- **Phase 3 (DESTRUCTIVE — data migration):** Per-session migration script: 1 private channel + 1 thread + decomposed context items per session, plus `session_migration_map` row. Recompute `reply_count` excluding system messages.
- **Phase 4 (frontend cutover):** Delete `/sessions` route + `features/sessions/store.ts` + `Session`/`SessionParticipant` types. Replace `SessionContextPanel` with `ThreadContextPanel`. Move `RemoteSessionsList` to `features/runtimes/`.
- **Phase 5:** MediationService becomes the single router. `auto_reply_config` becomes input-only. SLA escalation: T+0 fallback, T+300 upgrade, T+600 warning, T+900 critical. Anti-loop: agent→agent ≤ 3 rounds, ≤ 50 replies/24h per thread, no self-reply.
- **Phase 6 (DESTRUCTIVE — drops):** `message.session_id` writes return 400. `DROP TABLE session_participant`, `DROP TABLE session`, `ALTER TABLE message DROP COLUMN session_id`. System Agent CHECK constraint replaces `'session'` with `'conversation'`.

**Tech Stack:** Go 1.26 (Chi router, sqlc, pgx), PostgreSQL (pgvector/pg17), Next.js 16 App Router, TypeScript, Zustand.

**Reference PRD:** `/Users/chauncey2025/Documents/Obsidian Vault/2026-04-17-session-channel-restructure-prd.md`

---

## File Structure

### New files

| File | Responsibility |
|---|---|
| `server/migrations/051_thread_phase1.up.sql` | Thread enhancement + `thread_context_item` + `session_migration_map` |
| `server/migrations/051_thread_phase1.down.sql` | Reverse Phase 1 additive changes |
| `server/migrations/052_session_data_migration.up.sql` | DESTRUCTIVE — per-session decomposition into channel + thread + context items |
| `server/migrations/052_session_data_migration.down.sql` | Best-effort restore via `session_migration_map` |
| `server/migrations/053_session_drop.up.sql` | DESTRUCTIVE — drop `session_participant`, `session`, `message.session_id`; tighten agent scope CHECK |
| `server/migrations/053_session_drop.down.sql` | Re-add columns/tables (data not restored) |
| `server/pkg/db/queries/thread_context.sql` | sqlc queries for `thread_context_item` CRUD |
| `server/internal/handler/thread_context.go` | New Thread API handlers (create/get/messages/context) |
| `server/internal/handler/thread_context_test.go` | Handler unit tests |
| `apps/web/shared/types/thread.ts` | `Thread`, `ThreadContextItem`, `ContextItemType`, `RetentionClass` types |
| `apps/web/features/threads/index.ts` | Barrel export |
| `apps/web/features/threads/store.ts` | Zustand store for threads + context items |
| `apps/web/features/threads/components/thread-context-panel.tsx` | Replacement for `SessionContextPanel` |
| `apps/web/features/runtimes/components/remote-sessions-list.tsx` | Moved from `features/sessions/components/` |

### Modified files

| File | Why |
|---|---|
| `server/pkg/db/queries/threads.sql` | Add new fields to `UpsertThread` / `GetThread`; new `CreateThread`, `UpdateThreadLastActivity`, `RecomputeThreadReplyCount` |
| `server/pkg/db/queries/messages.sql` | Insert path resolves `session_id` via `session_migration_map` (Phase 2 only); drop `session_id` writes (Phase 6) |
| `server/internal/handler/thread.go` | Mount new endpoints; extend response with `workspace_id`, `issue_id`, `status`, `metadata`, `last_activity_at` |
| `server/internal/handler/message.go` | Phase 2: rewrite `session_id` → `thread_id+channel_id`; Phase 6: reject `session_id` (400) |
| `server/internal/handler/session.go` | Phase 6: delete or stub-redirect to `session_migration_map` lookup |
| `server/internal/service/mediation.go` | Single routing source; SLA escalation; anti-loop; consume `auto_reply_config` triggers as input |
| `server/internal/service/auto_reply.go` | Remove direct `Send` path; expose `MatchTriggers()` for MediationService consumption only |
| `server/cmd/server/router.go` | Mount new Thread routes; remove `/api/sessions` group in Phase 6 |
| `apps/web/shared/types/messaging.ts` | Phase 4: remove `Session`, `SessionParticipant`, `Message.session_id` |
| `apps/web/features/sessions/index.ts` | Phase 4: delete entire feature folder |
| `apps/web/app/(dashboard)/sessions/page.tsx` | Phase 4: replace with redirect to `/session` |
| `apps/web/app/(dashboard)/sessions/[id]/page.tsx` | Phase 4: replace with `session_migration_map` redirect |
| `apps/web/shared/api/index.ts` | Add `createThread`, `getThread`, `listThreadMessages`, `addContextItem`, `deleteContextItem`, `listContextItems` |

---

### Task 1: Migration 051 — Thread enhancement + new tables (additive)

**Files:**
- Create: `server/migrations/051_thread_phase1.up.sql`
- Create: `server/migrations/051_thread_phase1.down.sql`

- [ ] **Step 1: Write the up migration**

Create `server/migrations/051_thread_phase1.up.sql`:

```sql
-- Phase 1 (Session/Channel Restructure) — additive only.

-- ===== Thread enhancement =====
ALTER TABLE thread
    ALTER COLUMN id SET DEFAULT gen_random_uuid(),
    ADD COLUMN IF NOT EXISTS workspace_id      UUID REFERENCES workspace(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS root_message_id   UUID REFERENCES message(id)   ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS issue_id          UUID REFERENCES issue(id)     ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS created_by        UUID,
    ADD COLUMN IF NOT EXISTS created_by_type   TEXT NOT NULL DEFAULT 'member',
    ADD COLUMN IF NOT EXISTS status            TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS metadata          JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS last_activity_at  TIMESTAMPTZ;

-- Backfill workspace_id from channel.
UPDATE thread t
SET workspace_id = c.workspace_id
FROM channel c
WHERE t.channel_id = c.id AND t.workspace_id IS NULL;

-- Historical thread.id == root message id; record that fact.
UPDATE thread SET root_message_id = id WHERE root_message_id IS NULL;

-- Seed last_activity_at from existing last_reply_at (NULL for empty threads is acceptable).
UPDATE thread SET last_activity_at = COALESCE(last_reply_at, created_at)
WHERE last_activity_at IS NULL;

ALTER TABLE thread
    ADD CONSTRAINT thread_status_check
    CHECK (status IN ('active', 'archived'));

ALTER TABLE thread
    ADD CONSTRAINT thread_created_by_type_check
    CHECK (created_by_type IN ('member', 'agent', 'system'));

CREATE INDEX IF NOT EXISTS idx_thread_workspace      ON thread(workspace_id);
CREATE INDEX IF NOT EXISTS idx_thread_issue          ON thread(issue_id);
CREATE INDEX IF NOT EXISTS idx_thread_last_activity  ON thread(last_activity_at DESC);

-- ===== thread_context_item =====
CREATE TABLE IF NOT EXISTS thread_context_item (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    thread_id         UUID NOT NULL REFERENCES thread(id)    ON DELETE CASCADE,
    item_type         TEXT NOT NULL CHECK (item_type IN ('decision','file','code_snippet','summary','reference')),
    title             TEXT,
    body              TEXT,
    metadata          JSONB NOT NULL DEFAULT '{}',
    source_message_id UUID REFERENCES message(id) ON DELETE SET NULL,
    retention_class   TEXT NOT NULL DEFAULT 'ttl'
        CHECK (retention_class IN ('permanent','ttl','temp')),
    expires_at        TIMESTAMPTZ,
    created_by        UUID,
    created_by_type   TEXT DEFAULT 'system'
        CHECK (created_by_type IN ('member','agent','system')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_thread_context_item_thread
    ON thread_context_item(thread_id);
CREATE INDEX IF NOT EXISTS idx_thread_context_item_workspace
    ON thread_context_item(workspace_id);
CREATE INDEX IF NOT EXISTS idx_thread_context_item_expires
    ON thread_context_item(expires_at)
    WHERE retention_class = 'ttl' AND expires_at IS NOT NULL;

-- ===== session_migration_map =====
CREATE TABLE IF NOT EXISTS session_migration_map (
    session_id  UUID PRIMARY KEY,
    channel_id  UUID NOT NULL REFERENCES channel(id),
    thread_id   UUID NOT NULL REFERENCES thread(id),
    migrated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_session_migration_map_thread
    ON session_migration_map(thread_id);
```

- [ ] **Step 2: Write the down migration**

Create `server/migrations/051_thread_phase1.down.sql`:

```sql
-- Reverse Phase 1 additive changes.

DROP TABLE IF EXISTS session_migration_map;
DROP TABLE IF EXISTS thread_context_item;

DROP INDEX IF EXISTS idx_thread_workspace;
DROP INDEX IF EXISTS idx_thread_issue;
DROP INDEX IF EXISTS idx_thread_last_activity;

ALTER TABLE thread
    DROP CONSTRAINT IF EXISTS thread_status_check,
    DROP CONSTRAINT IF EXISTS thread_created_by_type_check;

ALTER TABLE thread
    DROP COLUMN IF EXISTS workspace_id,
    DROP COLUMN IF EXISTS root_message_id,
    DROP COLUMN IF EXISTS issue_id,
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS created_by_type,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS metadata,
    DROP COLUMN IF EXISTS last_activity_at;

ALTER TABLE thread ALTER COLUMN id DROP DEFAULT;
```

- [ ] **Step 3: Apply and verify**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make migrate-up
psql "$DATABASE_URL" -c "\d thread"               | grep -E 'workspace_id|root_message_id|issue_id|status|metadata|last_activity_at'
psql "$DATABASE_URL" -c "\d thread_context_item"  | head -20
psql "$DATABASE_URL" -c "\d session_migration_map"
```

Expected: all new columns present; new tables exist with indexes.

- [ ] **Step 4: Commit**

```bash
git add server/migrations/051_thread_phase1.up.sql server/migrations/051_thread_phase1.down.sql
git commit -m "feat(db): thread phase 1 — workspace_id/issue_id/status + context tables"
```

---

### Task 2: sqlc — Thread + ThreadContextItem queries

**Files:**
- Modify: `server/pkg/db/queries/threads.sql`
- Create: `server/pkg/db/queries/thread_context.sql`

- [ ] **Step 1: Update `threads.sql`**

Replace the body of `server/pkg/db/queries/threads.sql` with:

```sql
-- name: CreateThread :one
INSERT INTO thread (
    id, channel_id, workspace_id, title,
    root_message_id, issue_id,
    created_by, created_by_type,
    status, metadata,
    reply_count, last_activity_at, created_at
) VALUES (
    gen_random_uuid(), $1, $2, $3,
    sqlc.narg('root_message_id'), sqlc.narg('issue_id'),
    sqlc.narg('created_by'), COALESCE(sqlc.narg('created_by_type'), 'member'),
    'active', COALESCE(sqlc.narg('metadata'), '{}'::jsonb),
    0, NOW(), NOW()
)
RETURNING *;

-- name: UpsertThread :one
INSERT INTO thread (id, channel_id, title, reply_count, last_reply_at, last_activity_at, created_at)
VALUES ($1, $2, $3, 1, NOW(), NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
    reply_count       = thread.reply_count + 1,
    last_reply_at     = NOW(),
    last_activity_at  = NOW()
RETURNING *;

-- name: GetThread :one
SELECT * FROM thread WHERE id = $1;

-- name: ListThreadsByChannel :many
SELECT * FROM thread
WHERE channel_id = $1 AND status = 'active'
ORDER BY last_activity_at DESC NULLS LAST
LIMIT $2 OFFSET $3;

-- name: IncrementThreadReplyCount :exec
UPDATE thread
SET reply_count       = reply_count + 1,
    last_reply_at     = NOW(),
    last_activity_at  = NOW()
WHERE id = $1;

-- name: UpdateThreadLastActivity :exec
UPDATE thread
SET last_activity_at = NOW()
WHERE id = $1;

-- name: UpdateThreadStatus :exec
UPDATE thread SET status = $2 WHERE id = $1;

-- name: RecomputeThreadReplyCount :exec
-- Phase 3 helper: re-derive reply_count and last_reply_at from member/agent messages only.
UPDATE thread t
SET reply_count   = sub.cnt,
    last_reply_at = sub.last_at
FROM (
    SELECT thread_id,
           COUNT(*)         AS cnt,
           MAX(created_at)  AS last_at
    FROM message
    WHERE thread_id = $1
      AND sender_type IN ('member', 'agent')
    GROUP BY thread_id
) sub
WHERE t.id = $1 AND sub.thread_id = t.id;

-- name: DeleteThread :exec
DELETE FROM thread WHERE id = $1;
```

- [ ] **Step 2: Create `thread_context.sql`**

```sql
-- name: CreateThreadContextItem :one
INSERT INTO thread_context_item (
    id, workspace_id, thread_id,
    item_type, title, body, metadata,
    source_message_id,
    retention_class, expires_at,
    created_by, created_by_type, created_at
) VALUES (
    gen_random_uuid(), $1, $2,
    $3, sqlc.narg('title'), sqlc.narg('body'), COALESCE(sqlc.narg('metadata'), '{}'::jsonb),
    sqlc.narg('source_message_id'),
    COALESCE(sqlc.narg('retention_class'), 'ttl'),
    sqlc.narg('expires_at'),
    sqlc.narg('created_by'), COALESCE(sqlc.narg('created_by_type'), 'system'),
    NOW()
)
RETURNING *;

-- name: GetThreadContextItem :one
SELECT * FROM thread_context_item WHERE id = $1;

-- name: ListThreadContextItems :many
SELECT * FROM thread_context_item
WHERE thread_id = $1
ORDER BY created_at ASC;

-- name: ListExpiredContextItems :many
-- Post-MVP background job feed.
SELECT * FROM thread_context_item
WHERE retention_class = 'ttl'
  AND expires_at IS NOT NULL
  AND expires_at < NOW()
LIMIT $1;

-- name: DeleteThreadContextItem :exec
DELETE FROM thread_context_item WHERE id = $1;
```

- [ ] **Step 3: Regenerate sqlc and build**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make sqlc
cd server && go build ./...
```

Expected: clean build. If existing handlers reference old `UpsertThread` shape, fix them inline (the old positional INSERT now expects 4 columns instead of 3 — adjust call sites in `handler/message.go` if they use it).

- [ ] **Step 4: Commit**

```bash
git add server/pkg/db/queries/threads.sql server/pkg/db/queries/thread_context.sql server/pkg/db/generated/
git commit -m "feat(sqlc): thread + thread_context_item queries"
```

---

### Task 3: New Thread REST API (Phase 2)

**Files:**
- Create: `server/internal/handler/thread_context.go`
- Create: `server/internal/handler/thread_context_test.go`
- Modify: `server/internal/handler/thread.go` (add `CreateThread`, `GetThreadDetail` — only if not already present in current file)
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Read current `thread.go` to know what already exists**

```bash
cat /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691/server/internal/handler/thread.go
```

Identify which of the PRD endpoints already have implementations and which are new.

- [ ] **Step 2: Write the failing test**

Create `server/internal/handler/thread_context_test.go`:

```go
package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateThreadReturnsIndependentUUID(t *testing.T) {
	h, ws, ch := newThreadTestHandler(t)
	body, _ := json.Marshal(map[string]any{
		"title": "Discuss API contract",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/channels/"+ch.String()+"/threads", bytes.NewReader(body))
	req.Header.Set("X-Workspace-ID", ws.String())
	req.Header.Set("X-User-ID", testUserID().String())
	rr := httptest.NewRecorder()
	h.CreateThread(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["id"] == nil || got["id"] == got["root_message_id"] {
		t.Errorf("expected independent thread.id; got %v", got)
	}
	if got["workspace_id"] != ws.String() {
		t.Errorf("workspace_id mismatch: %v", got["workspace_id"])
	}
}

func TestAddContextItemDefaultsRetentionByType(t *testing.T) {
	h, ws, _ := newThreadTestHandler(t)
	threadID := mustCreateThread(t, h, ws)
	body, _ := json.Marshal(map[string]any{
		"item_type": "decision",
		"title":     "Use HTTP/2 for sync",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+threadID.String()+"/context-items", bytes.NewReader(body))
	req.Header.Set("X-Workspace-ID", ws.String())
	rr := httptest.NewRecorder()
	h.CreateThreadContextItem(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["retention_class"] != "permanent" {
		t.Errorf("expected permanent retention for decision; got %v", got["retention_class"])
	}
}
```

The test helpers `newThreadTestHandler`, `testUserID`, `mustCreateThread` follow the existing handler-test pattern in `server/internal/handler/` — copy the shape from `auto_reply_test.go` or `channel_merge_test.go`.

- [ ] **Step 3: Implement the handlers**

Create `server/internal/handler/thread_context.go`:

```go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// CreateThread implements POST /api/channels/{channelID}/threads.
func (h *Handler) CreateThread(w http.ResponseWriter, r *http.Request) {
	channelID, err := uuid.Parse(chi.URLParam(r, "channelID"))
	if err != nil {
		http.Error(w, "invalid channelID", http.StatusBadRequest)
		return
	}
	wsID, err := uuid.Parse(r.Header.Get("X-Workspace-ID"))
	if err != nil {
		http.Error(w, "missing X-Workspace-ID", http.StatusBadRequest)
		return
	}
	userID, _ := uuid.Parse(r.Header.Get("X-User-ID"))

	var body struct {
		Title         string                 `json:"title"`
		RootMessageID *string                `json:"root_message_id,omitempty"`
		IssueID       *string                `json:"issue_id,omitempty"`
		Metadata      map[string]any         `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	params := db.CreateThreadParams{
		ChannelID:     pgUUID(channelID),
		WorkspaceID:   pgUUID(wsID),
		Title:         pgText(body.Title),
		CreatedBy:     pgUUID(userID),
		CreatedByType: pgText("member"),
	}
	if body.RootMessageID != nil {
		if id, err := uuid.Parse(*body.RootMessageID); err == nil {
			params.RootMessageID = pgUUID(id)
		}
	}
	if body.IssueID != nil {
		if id, err := uuid.Parse(*body.IssueID); err == nil {
			params.IssueID = pgUUID(id)
		}
	}
	if body.Metadata != nil {
		raw, _ := json.Marshal(body.Metadata)
		params.Metadata = raw
	}

	row, err := h.Queries.CreateThread(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

// GetThreadDetail implements GET /api/threads/{threadID}.
func (h *Handler) GetThreadDetail(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "threadID"))
	if err != nil {
		http.Error(w, "invalid threadID", http.StatusBadRequest)
		return
	}
	row, err := h.Queries.GetThread(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// CreateThreadContextItem implements POST /api/threads/{threadID}/context-items.
func (h *Handler) CreateThreadContextItem(w http.ResponseWriter, r *http.Request) {
	threadID, err := uuid.Parse(chi.URLParam(r, "threadID"))
	if err != nil {
		http.Error(w, "invalid threadID", http.StatusBadRequest)
		return
	}
	wsID, err := uuid.Parse(r.Header.Get("X-Workspace-ID"))
	if err != nil {
		http.Error(w, "missing X-Workspace-ID", http.StatusBadRequest)
		return
	}

	var body struct {
		ItemType        string         `json:"item_type"`
		Title           string         `json:"title,omitempty"`
		Body            string         `json:"body,omitempty"`
		Metadata        map[string]any `json:"metadata,omitempty"`
		SourceMessageID *string        `json:"source_message_id,omitempty"`
		RetentionClass  *string        `json:"retention_class,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	retention := defaultRetentionClass(body.ItemType, "member")
	if body.RetentionClass != nil {
		retention = *body.RetentionClass
	}

	params := db.CreateThreadContextItemParams{
		WorkspaceID:    pgUUID(wsID),
		ThreadID:       threadID,
		ItemType:       body.ItemType,
		Title:          pgText(body.Title),
		Body:           pgText(body.Body),
		RetentionClass: pgText(retention),
		CreatedByType:  pgText("member"),
	}
	if body.Metadata != nil {
		raw, _ := json.Marshal(body.Metadata)
		params.Metadata = raw
	}
	if body.SourceMessageID != nil {
		if id, err := uuid.Parse(*body.SourceMessageID); err == nil {
			params.SourceMessageID = pgUUID(id)
		}
	}

	row, err := h.Queries.CreateThreadContextItem(r.Context(), params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

// ListThreadContextItems implements GET /api/threads/{threadID}/context.
func (h *Handler) ListThreadContextItems(w http.ResponseWriter, r *http.Request) {
	threadID, err := uuid.Parse(chi.URLParam(r, "threadID"))
	if err != nil {
		http.Error(w, "invalid threadID", http.StatusBadRequest)
		return
	}
	rows, err := h.Queries.ListThreadContextItems(r.Context(), threadID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// DeleteThreadContextItem implements DELETE /api/threads/{threadID}/context-items/{id}.
func (h *Handler) DeleteThreadContextItem(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.Queries.DeleteThreadContextItem(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// defaultRetentionClass maps PRD §3.2 / §7.2 item_type → retention_class.
func defaultRetentionClass(itemType, createdByType string) string {
	switch itemType {
	case "decision":
		return "permanent"
	case "summary":
		if createdByType == "member" {
			return "permanent"
		}
		return "ttl"
	case "file", "reference", "code_snippet":
		return "ttl"
	default:
		return "ttl"
	}
}

// pgUUID / pgText helpers — reuse if already present in handler/utils.go.
func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }
func pgText(s string) pgtype.Text     { return pgtype.Text{String: s, Valid: s != ""} }
```

If `pgUUID` / `pgText` / `writeJSON` already exist elsewhere in `internal/handler/`, drop the duplicates here.

- [ ] **Step 4: Mount routes in `router.go`**

In `server/cmd/server/router.go`, inside the protected route group, add right after the existing `/api/channels` block:

```go
r.Route("/api/channels/{channelID}/threads", func(r chi.Router) {
    r.Post("/", h.CreateThread)
})

r.Route("/api/threads/{threadID}", func(r chi.Router) {
    r.Get("/", h.GetThreadDetail)
    r.Get("/messages", h.ListThreadMessages)            // existing in thread.go
    r.Post("/messages", h.CreateThreadMessage)          // existing or wrap CreateMessage
    r.Get("/context", h.ListThreadContextItems)
    r.Post("/context-items", h.CreateThreadContextItem)
    r.Delete("/context-items/{id}", h.DeleteThreadContextItem)
})
```

If `ListThreadMessages` / `CreateThreadMessage` are not yet in `thread.go`, add thin wrappers over the existing message handlers that scope by `thread_id`.

- [ ] **Step 5: Run tests + build**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691/server
go test ./internal/handler/ -run TestCreateThreadReturnsIndependentUUID
go test ./internal/handler/ -run TestAddContextItemDefaultsRetentionByType
go build ./...
```

Expected: PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/thread_context.go server/internal/handler/thread_context_test.go server/cmd/server/router.go
git commit -m "feat(api): independent Thread API + ThreadContextItem CRUD"
```

---

### Task 4: Message handler — Phase 2 compatibility resolver

**Files:**
- Modify: `server/internal/handler/message.go`

**Goal:** Posts that still send `session_id` are accepted but rewritten to `channel_id + thread_id` via `session_migration_map`. After Task 7 (data migration) every legacy session_id resolves cleanly.

- [ ] **Step 1: Locate the message create path**

```bash
grep -n "session_id\|CreateMessage" /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691/server/internal/handler/message.go
```

- [ ] **Step 2: Insert the resolver before the INSERT**

In `CreateMessage`, before constructing the sqlc params:

```go
if body.SessionID != nil && *body.SessionID != "" {
    sessID, err := uuid.Parse(*body.SessionID)
    if err != nil {
        http.Error(w, "invalid session_id", http.StatusBadRequest)
        return
    }
    mapped, err := h.Queries.GetSessionMigrationByID(r.Context(), sessID)
    if err == nil {
        body.ChannelID = strPtr(mapped.ChannelID.String())
        body.ThreadID  = strPtr(mapped.ThreadID.String())
        if body.Metadata == nil { body.Metadata = map[string]any{} }
        body.Metadata["source_session_id"] = sessID.String()
    }
    // If unmapped, leave session_id on the row for now — Phase 6 rejects it.
}
```

- [ ] **Step 3: Add the lookup query**

Append to `server/pkg/db/queries/messages.sql`:

```sql
-- name: GetSessionMigrationByID :one
SELECT * FROM session_migration_map WHERE session_id = $1;
```

Run `make sqlc` and adjust the import.

- [ ] **Step 4: Increment counters helper**

In `server/internal/handler/message.go`, after the message INSERT, replace the existing `IncrementThreadReplyCount` call with:

```go
func (h *Handler) incrementThreadCounters(ctx context.Context, threadID uuid.UUID, senderType string) {
    _ = h.Queries.UpdateThreadLastActivity(ctx, threadID)
    if senderType == "member" || senderType == "agent" {
        _ = h.Queries.IncrementThreadReplyCount(ctx, threadID)
    }
}
```

Call it after every successful message insert that has a non-nil `thread_id`.

- [ ] **Step 5: Build + targeted tests**

```bash
cd server
go build ./...
go test ./internal/handler/ -run Message
```

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/message.go server/pkg/db/queries/messages.sql server/pkg/db/generated/
git commit -m "feat(message): session_id compatibility resolver + thread counter semantics"
```

---

### Task 5: Frontend — Thread types, store, ContextPanel

**Files:**
- Create: `apps/web/shared/types/thread.ts`
- Modify: `apps/web/shared/types/index.ts`
- Create: `apps/web/features/threads/index.ts`
- Create: `apps/web/features/threads/store.ts`
- Create: `apps/web/features/threads/components/thread-context-panel.tsx`
- Modify: `apps/web/shared/api/index.ts`

- [ ] **Step 1: Type definitions**

Create `apps/web/shared/types/thread.ts`:

```typescript
export type ContextItemType =
  | "decision"
  | "file"
  | "code_snippet"
  | "summary"
  | "reference";

export type RetentionClass = "permanent" | "ttl" | "temp";

export type ThreadStatus = "active" | "archived";

export interface Thread {
  id: string;
  workspace_id: string;
  channel_id: string;
  root_message_id: string | null;
  issue_id: string | null;
  title: string | null;
  status: ThreadStatus;
  metadata: Record<string, unknown>;
  created_by: string | null;
  created_by_type: "member" | "agent" | "system";
  reply_count: number;
  last_reply_at: string | null;
  last_activity_at: string | null;
  created_at: string;
}

export interface ThreadContextItem {
  id: string;
  workspace_id: string;
  thread_id: string;
  item_type: ContextItemType;
  title: string | null;
  body: string | null;
  metadata: Record<string, unknown>;
  source_message_id: string | null;
  retention_class: RetentionClass;
  expires_at: string | null;
  created_by: string | null;
  created_by_type: "member" | "agent" | "system";
  created_at: string;
}
```

Add to `apps/web/shared/types/index.ts`:

```typescript
export * from "./thread";
```

- [ ] **Step 2: API client methods**

In `apps/web/shared/api/index.ts` (or wherever the `ApiClient` class lives), add:

```typescript
async createThread(channelID: string, body: { title?: string; root_message_id?: string; issue_id?: string; metadata?: Record<string, unknown> }): Promise<Thread> {
  return this.post<Thread>(`/api/channels/${channelID}/threads`, body);
}
async getThread(threadID: string): Promise<Thread> {
  return this.get<Thread>(`/api/threads/${threadID}`);
}
async listThreadMessages(threadID: string): Promise<Message[]> {
  return this.get<Message[]>(`/api/threads/${threadID}/messages`);
}
async listThreadContextItems(threadID: string): Promise<ThreadContextItem[]> {
  return this.get<ThreadContextItem[]>(`/api/threads/${threadID}/context`);
}
async addContextItem(threadID: string, body: { item_type: ContextItemType; title?: string; body?: string; metadata?: Record<string, unknown>; source_message_id?: string; retention_class?: RetentionClass }): Promise<ThreadContextItem> {
  return this.post<ThreadContextItem>(`/api/threads/${threadID}/context-items`, body);
}
async deleteContextItem(threadID: string, itemID: string): Promise<void> {
  return this.delete<void>(`/api/threads/${threadID}/context-items/${itemID}`);
}
```

Import `Thread`, `ThreadContextItem`, `ContextItemType`, `RetentionClass` from `@/shared/types`.

- [ ] **Step 3: Threads zustand store**

Create `apps/web/features/threads/store.ts`:

```typescript
import { create } from "zustand";
import { api } from "@/shared/api";
import type { Thread, ThreadContextItem } from "@/shared/types";

interface ThreadsState {
  byID: Record<string, Thread>;
  contextByThread: Record<string, ThreadContextItem[]>;
  loadThread: (id: string) => Promise<void>;
  loadContext: (id: string) => Promise<void>;
  addContextItem: (id: string, item: ThreadContextItem) => void;
  removeContextItem: (threadID: string, itemID: string) => void;
}

export const useThreadStore = create<ThreadsState>((set) => ({
  byID: {},
  contextByThread: {},

  loadThread: async (id) => {
    const t = await api.getThread(id);
    set((s) => ({ byID: { ...s.byID, [id]: t } }));
  },

  loadContext: async (id) => {
    const items = await api.listThreadContextItems(id);
    set((s) => ({ contextByThread: { ...s.contextByThread, [id]: items } }));
  },

  addContextItem: (id, item) =>
    set((s) => ({
      contextByThread: {
        ...s.contextByThread,
        [id]: [...(s.contextByThread[id] ?? []), item],
      },
    })),

  removeContextItem: (threadID, itemID) =>
    set((s) => ({
      contextByThread: {
        ...s.contextByThread,
        [threadID]: (s.contextByThread[threadID] ?? []).filter((i) => i.id !== itemID),
      },
    })),
}));
```

Create `apps/web/features/threads/index.ts`:

```typescript
export { useThreadStore } from "./store";
export { ThreadContextPanel } from "./components/thread-context-panel";
```

- [ ] **Step 4: ThreadContextPanel component**

Create `apps/web/features/threads/components/thread-context-panel.tsx`:

```tsx
"use client";

import { useEffect } from "react";
import { useThreadStore } from "../store";
import type { ThreadContextItem } from "@/shared/types";

interface Props {
  threadID: string;
}

export function ThreadContextPanel({ threadID }: Props) {
  const items = useThreadStore((s) => s.contextByThread[threadID] ?? []);
  const loadContext = useThreadStore((s) => s.loadContext);

  useEffect(() => {
    void loadContext(threadID);
  }, [threadID, loadContext]);

  const grouped = items.reduce<Record<string, ThreadContextItem[]>>((acc, item) => {
    (acc[item.item_type] ??= []).push(item);
    return acc;
  }, {});

  return (
    <aside className="border-l flex flex-col gap-4 p-4 w-72">
      <h3 className="font-semibold text-sm">Thread Context</h3>
      {Object.entries(grouped).map(([type, list]) => (
        <section key={type}>
          <h4 className="text-muted-foreground text-xs uppercase">{type}</h4>
          <ul className="mt-1 space-y-2">
            {list.map((item) => (
              <li key={item.id} className="rounded-md border p-2 text-sm">
                {item.title && <div className="font-medium">{item.title}</div>}
                {item.body && <div className="text-muted-foreground text-xs">{item.body}</div>}
              </li>
            ))}
          </ul>
        </section>
      ))}
      {items.length === 0 && (
        <p className="text-muted-foreground text-xs">No context yet.</p>
      )}
    </aside>
  );
}
```

- [ ] **Step 5: Typecheck**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
pnpm typecheck
```

Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add apps/web/shared/types/thread.ts apps/web/shared/types/index.ts apps/web/shared/api/index.ts apps/web/features/threads/
git commit -m "feat(web): Thread types, store, and ThreadContextPanel"
```

---

### Task 6: MediationService — single routing source (Phase 5)

**Files:**
- Modify: `server/internal/service/mediation.go`
- Modify: `server/internal/service/auto_reply.go`

**Goal:** Move the actual `Send` call out of `auto_reply.go`. `MatchTriggers()` returns matched configurations to MediationService, which decides whether to route. Add SLA escalation timers and anti-loop counters.

- [ ] **Step 1: Refactor `auto_reply.go`**

```bash
grep -n "func.*Send\|func.*Trigger\|func.*Match" /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691/server/internal/service/auto_reply.go
```

Rename or split: any function that previously fired a reply directly should now `return matchedAgents` and let MediationService make the decision. Specifically:

- Keep `MatchTriggers(ctx, msg) []*db.Agent` — returns candidates from `auto_reply_config`.
- DELETE any `SendReply`, `TriggerReply`, or similar dispatch from `auto_reply.go`.

- [ ] **Step 2: Extend `mediation.go` routing logic**

In `MediationService.handleMessageCreated`, replace the existing channel-only heuristic with the priority chain from PRD §6 Phase 5:

```go
func (s *MediationService) routeReply(ctx context.Context, msg *db.Message) *db.Agent {
    // 1. @mention → direct
    if mentions := parseMentionsFromContent(msg.Content); len(mentions) > 0 {
        if a := s.resolveMentionedAgent(ctx, mentions[0], msg.WorkspaceID.Bytes); a != nil {
            return a
        }
    }
    // 2. Plan thread → Plan agent
    if msg.ThreadID.Valid {
        if a := s.resolvePlanThreadAgent(ctx, msg.ThreadID.Bytes); a != nil {
            return a
        }
        // 3. Issue thread → Issue assignee agent
        if a := s.resolveIssueThreadAgent(ctx, msg.ThreadID.Bytes); a != nil {
            return a
        }
    }
    // 4. Capability match (consume auto_reply triggers as candidate signal)
    candidates := s.AutoReply.MatchTriggers(ctx, msg)
    if len(candidates) > 0 {
        return candidates[0]
    }
    return nil
}
```

- [ ] **Step 3: Anti-loop counters**

In `mediation.go`, before assigning a responder:

```go
// Reject self-reply.
if responder.ID == msg.SenderID.Bytes {
    return
}
// Reject agent→agent over 3 rounds.
if msg.SenderType == "agent" {
    rounds, _ := s.Queries.CountConsecutiveAgentReplies(ctx, msg.ThreadID.Bytes)
    if rounds >= 3 { return }
}
// Reject if thread > 50 replies in last 24h.
recent, _ := s.Queries.CountThreadRepliesSince(ctx, db.CountThreadRepliesSinceParams{
    ThreadID: msg.ThreadID.Bytes,
    Since:    time.Now().Add(-24 * time.Hour),
})
if recent >= 50 { return }
```

Add the two new sqlc queries to `messages.sql`:

```sql
-- name: CountConsecutiveAgentReplies :one
SELECT COUNT(*) FROM (
    SELECT sender_type FROM message
    WHERE thread_id = $1
    ORDER BY created_at DESC
    LIMIT 4
) tail
WHERE sender_type = 'agent';

-- name: CountThreadRepliesSince :one
SELECT COUNT(*) FROM message
WHERE thread_id = $1 AND created_at >= $2 AND sender_type IN ('member', 'agent');
```

- [ ] **Step 4: SLA escalation**

In the existing periodic loop (look for `checkExpiredSlots`), expand into four buckets:

```go
const (
    slaFallbackUpgradeAt = 300 * time.Second  // T+300
    slaWarningAt         = 600 * time.Second  // T+600
    slaCriticalAt        = 900 * time.Second  // T+900
)

func (s *MediationService) tickSLA(ctx context.Context) {
    now := time.Now()
    rows, _ := s.Queries.ListPendingReplySlots(ctx)
    for _, slot := range rows {
        age := now.Sub(slot.CreatedAt.Time)
        switch {
        case age >= slaCriticalAt:
            s.escalateInbox(ctx, slot, "critical")
        case age >= slaWarningAt:
            s.escalateInbox(ctx, slot, "warning")
        case age >= slaFallbackUpgradeAt:
            s.upgradeFallback(ctx, slot)
        }
    }
}
```

`escalateInbox` writes an `inbox_item` row with `severity` and `thread_id` references; `upgradeFallback` reroutes to a different responder (next candidate from `routeReply`).

- [ ] **Step 5: Tests**

```bash
cd server
go test ./internal/service/ -run Mediation
go test ./internal/service/ -run AutoReply
```

Update `auto_reply_test.go` if it asserted that `auto_reply.go` calls `Send` directly — now the assertion should be that `MatchTriggers` returns the right candidate set.

- [ ] **Step 6: Commit**

```bash
git add server/internal/service/mediation.go server/internal/service/auto_reply.go server/internal/service/auto_reply_test.go server/pkg/db/queries/messages.sql server/pkg/db/generated/
git commit -m "feat(mediation): single routing source + SLA escalation + anti-loop"
```

---

### Task 7: Migration 052 — DESTRUCTIVE per-session data migration

**Files:**
- Create: `server/migrations/052_session_data_migration.up.sql`
- Create: `server/migrations/052_session_data_migration.down.sql`

**WARNING — DESTRUCTIVE:** This migration creates new channels/threads/context items based on every existing session row. Apply it AFTER Tasks 1-6 land and the staging DB has been backed up. Idempotent guards (`session_migration_map` check) ensure re-runs do nothing.

- [ ] **Step 1: Write the up migration**

Create `server/migrations/052_session_data_migration.up.sql`:

```sql
-- Phase 3 (DESTRUCTIVE): per-session decomposition.
-- Idempotent: a session that already appears in session_migration_map is skipped.

-- ===== 1. New private channel + thread per session =====
WITH new_channels AS (
    INSERT INTO channel (id, workspace_id, name, visibility, conversation_type, created_at, updated_at)
    SELECT
        gen_random_uuid(),
        s.workspace_id,
        'session-' || left(s.id::text, 8) || '-' || regexp_replace(lower(coalesce(s.title, 'untitled')), '[^a-z0-9]+', '-', 'g'),
        'private',
        'channel',
        s.created_at,
        s.updated_at
    FROM session s
    LEFT JOIN session_migration_map m ON m.session_id = s.id
    WHERE m.session_id IS NULL
    RETURNING id, workspace_id, name
),
session_pairs AS (
    SELECT s.id AS session_id, s.workspace_id, s.title, s.issue_id, s.status, s.max_turns, s.current_turn, s.context, s.created_at,
           c.id AS channel_id
    FROM session s
    JOIN new_channels c ON c.workspace_id = s.workspace_id
        AND c.name LIKE 'session-' || left(s.id::text, 8) || '%'
    LEFT JOIN session_migration_map m ON m.session_id = s.id
    WHERE m.session_id IS NULL
),
new_threads AS (
    INSERT INTO thread (id, channel_id, workspace_id, title, issue_id, status, metadata, reply_count, last_activity_at, created_at, created_by_type)
    SELECT
        gen_random_uuid(),
        sp.channel_id,
        sp.workspace_id,
        sp.title,
        sp.issue_id,
        CASE WHEN sp.status = 'active' THEN 'active' ELSE 'archived' END,
        jsonb_build_object('source_session_id', sp.session_id, 'max_turns', sp.max_turns, 'current_turn', sp.current_turn),
        0,
        sp.created_at,
        sp.created_at,
        'member'
    FROM session_pairs sp
    RETURNING id, channel_id, (metadata->>'source_session_id')::uuid AS session_id
)
INSERT INTO session_migration_map (session_id, channel_id, thread_id, migrated_at)
SELECT session_id, channel_id, id, NOW() FROM new_threads;

-- ===== 2. Migrate session_participant → channel_member =====
INSERT INTO channel_member (channel_id, member_id, member_type, role, joined_at)
SELECT m.channel_id, sp.participant_id, sp.participant_type, sp.role, sp.joined_at
FROM session_participant sp
JOIN session_migration_map m ON m.session_id = sp.session_id
ON CONFLICT DO NOTHING;

-- ===== 3. Reroute messages =====
UPDATE message msg
SET channel_id = m.channel_id,
    thread_id  = m.thread_id,
    metadata   = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('source_session_id', msg.session_id::text)
FROM session_migration_map m
WHERE msg.session_id = m.session_id
  AND (msg.channel_id IS NULL OR msg.thread_id IS NULL);

-- ===== 4. Decompose session.context JSONB into thread_context_item rows =====

-- 4a. summary (single)
INSERT INTO thread_context_item (workspace_id, thread_id, item_type, title, body, retention_class, created_by_type)
SELECT s.workspace_id, m.thread_id, 'summary', NULL, s.context->>'summary', 'permanent', 'member'
FROM session s
JOIN session_migration_map m ON m.session_id = s.id
WHERE s.context ? 'summary' AND length(coalesce(s.context->>'summary', '')) > 0;

-- 4b. decisions[]
INSERT INTO thread_context_item (workspace_id, thread_id, item_type, title, body, retention_class, created_by_type)
SELECT s.workspace_id,
       m.thread_id,
       'decision',
       d.elem->>'decision',
       coalesce(d.elem->>'by', '') || ' @ ' || coalesce(d.elem->>'at', ''),
       'permanent',
       'member'
FROM session s
JOIN session_migration_map m ON m.session_id = s.id
CROSS JOIN LATERAL jsonb_array_elements(s.context->'decisions') AS d(elem)
WHERE s.context ? 'decisions' AND jsonb_typeof(s.context->'decisions') = 'array';

-- 4c. files[]
INSERT INTO thread_context_item (workspace_id, thread_id, item_type, title, metadata, retention_class, created_by_type)
SELECT s.workspace_id, m.thread_id, 'file', f.elem->>'name', f.elem, 'ttl', 'member'
FROM session s
JOIN session_migration_map m ON m.session_id = s.id
CROSS JOIN LATERAL jsonb_array_elements(s.context->'files') AS f(elem)
WHERE s.context ? 'files' AND jsonb_typeof(s.context->'files') = 'array';

-- 4d. code_snippets[]
INSERT INTO thread_context_item (workspace_id, thread_id, item_type, title, body, metadata, retention_class, created_by_type)
SELECT s.workspace_id,
       m.thread_id,
       'code_snippet',
       s.context->'topic'->>'title',
       cs.elem->>'code',
       jsonb_build_object('language', cs.elem->>'language'),
       'ttl',
       'member'
FROM session s
JOIN session_migration_map m ON m.session_id = s.id
CROSS JOIN LATERAL jsonb_array_elements(s.context->'code_snippets') AS cs(elem)
WHERE s.context ? 'code_snippets' AND jsonb_typeof(s.context->'code_snippets') = 'array';

-- ===== 5. Recompute thread reply_count / last_reply_at (skip system messages) =====
UPDATE thread t
SET reply_count = sub.cnt,
    last_reply_at = sub.last_at,
    last_activity_at = COALESCE(sub.last_at, t.last_activity_at)
FROM (
    SELECT thread_id,
           COUNT(*)        AS cnt,
           MAX(created_at) AS last_at
    FROM message
    WHERE thread_id IS NOT NULL
      AND sender_type IN ('member', 'agent')
    GROUP BY thread_id
) sub
WHERE t.id = sub.thread_id;
```

- [ ] **Step 2: Write the down migration**

Create `server/migrations/052_session_data_migration.down.sql`:

```sql
-- Best-effort rollback. Restores message.session_id but does NOT delete the
-- channels / threads / context items the migration created — that would risk
-- losing user data appended after the migration.

UPDATE message msg
SET channel_id = NULL,
    thread_id  = NULL,
    session_id = m.session_id
FROM session_migration_map m
WHERE msg.thread_id = m.thread_id
  AND (msg.metadata->>'source_session_id')::uuid = m.session_id;

DELETE FROM session_migration_map;
```

- [ ] **Step 3: Apply on a fresh seeded DB and verify**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make migrate-up
psql "$DATABASE_URL" -c "SELECT COUNT(*) AS sessions FROM session;"
psql "$DATABASE_URL" -c "SELECT COUNT(*) AS mapped   FROM session_migration_map;"
psql "$DATABASE_URL" -c "SELECT COUNT(*) AS rerouted FROM message WHERE thread_id IS NOT NULL AND metadata ? 'source_session_id';"
psql "$DATABASE_URL" -c "SELECT item_type, COUNT(*) FROM thread_context_item GROUP BY item_type;"
```

Expected: `sessions == mapped`; `rerouted >= sum(messages with session_id)`; context counts roughly match the JSON shapes in `session.context`.

- [ ] **Step 4: Commit**

```bash
git add server/migrations/052_session_data_migration.up.sql server/migrations/052_session_data_migration.down.sql
git commit -m "feat(db): DESTRUCTIVE — per-session migration to channel/thread/context_item"
```

---

### Task 8: Frontend cutover (Phase 4)

**Files:**
- Delete: `apps/web/app/(dashboard)/sessions/page.tsx` → replace with redirect
- Delete: `apps/web/app/(dashboard)/sessions/[id]/page.tsx` → replace with `session_migration_map` redirect
- Delete: `apps/web/features/sessions/` (entire folder)
- Modify: `apps/web/shared/types/messaging.ts` (remove `Session`, `SessionParticipant`, `Message.session_id`)
- Move: `apps/web/features/sessions/components/remote-sessions-list.tsx` → `apps/web/features/runtimes/components/remote-sessions-list.tsx`
- Wire: `ThreadContextPanel` into `apps/web/app/(dashboard)/session/[id]/page.tsx` (replaces `SessionContextPanel`)

- [ ] **Step 1: Replace `/sessions/page.tsx` with redirect**

```tsx
// apps/web/app/(dashboard)/sessions/page.tsx
import { redirect } from "next/navigation";

export default function LegacySessionsRedirect() {
  redirect("/session");
}
```

- [ ] **Step 2: Replace `/sessions/[id]/page.tsx` with migration-map redirect**

```tsx
// apps/web/app/(dashboard)/sessions/[id]/page.tsx
import { redirect } from "next/navigation";
import { api } from "@/shared/api";

export default async function LegacySessionRedirect({ params }: { params: { id: string } }) {
  try {
    const mapping = await api.getSessionMigration(params.id);
    redirect(`/session/${mapping.thread_id}`);
  } catch {
    redirect("/session");
  }
}
```

Add the lookup endpoint to the API client:

```typescript
async getSessionMigration(sessionID: string): Promise<{ session_id: string; channel_id: string; thread_id: string }> {
  return this.get(`/api/session-migrations/${sessionID}`);
}
```

And the matching backend handler (`server/internal/handler/session.go` — replace `GetSession` body with):

```go
func (h *Handler) GetSessionMigration(w http.ResponseWriter, r *http.Request) {
    id, err := uuid.Parse(chi.URLParam(r, "sessionID"))
    if err != nil { http.Error(w, "invalid id", http.StatusBadRequest); return }
    row, err := h.Queries.GetSessionMigrationByID(r.Context(), id)
    if err != nil { http.Error(w, "not found", http.StatusNotFound); return }
    writeJSON(w, http.StatusOK, row)
}
```

Mount in `router.go`:

```go
r.Get("/api/session-migrations/{sessionID}", h.GetSessionMigration)
```

- [ ] **Step 3: Move `RemoteSessionsList`**

```bash
mkdir -p apps/web/features/runtimes/components
git mv apps/web/features/sessions/components/remote-sessions-list.tsx apps/web/features/runtimes/components/remote-sessions-list.tsx
```

Update its imports if they reference `../store`. Update `apps/web/features/runtimes/index.ts` (create if missing):

```typescript
export { RemoteSessionsList } from "./components/remote-sessions-list";
```

Update every consumer:

```bash
grep -rn "features/sessions" apps/web/ --include="*.ts" --include="*.tsx"
```

Replace each `from "@/features/sessions"` import of `RemoteSessionsList` with `from "@/features/runtimes"`.

- [ ] **Step 4: Delete `features/sessions/` and replace `SessionContextPanel`**

In `apps/web/app/(dashboard)/session/[id]/page.tsx`, replace any `<SessionContextPanel ... />` with `<ThreadContextPanel threadID={threadID} />` (import from `@/features/threads`).

Then delete the folder:

```bash
rm -rf apps/web/features/sessions/
```

- [ ] **Step 5: Strip session types from `messaging.ts`**

Open `apps/web/shared/types/messaging.ts`, delete:
- `interface Session` (line ~57)
- `interface SessionParticipant` (line ~78)
- `Message.session_id` field (line 9)

Add a `thread_id?: string | null` to `Message` if it isn't already there.

- [ ] **Step 6: Typecheck and fix fallout**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
pnpm typecheck
```

For every error, replace `session_id` reads with `thread_id`. Replace `useSessionStore` reads with `useThreadStore`.

- [ ] **Step 7: Commit**

```bash
git add apps/web/app/\(dashboard\)/sessions/ apps/web/app/\(dashboard\)/session/\[id\]/ apps/web/features/runtimes/ apps/web/shared/types/messaging.ts apps/web/shared/api/index.ts server/internal/handler/session.go server/cmd/server/router.go
git rm -r apps/web/features/sessions/
git commit -m "feat(web): cutover from /sessions to /session + ThreadContextPanel"
```

---

### Task 9: Migration 053 — DESTRUCTIVE drops + System Agent scope rename

**Files:**
- Create: `server/migrations/053_session_drop.up.sql`
- Create: `server/migrations/053_session_drop.down.sql`
- Modify: `server/internal/handler/message.go` (reject `session_id` writes with 400)
- Modify: `server/cmd/server/router.go` (remove the entire `/api/sessions` route group)

**WARNING — DESTRUCTIVE:** Do NOT apply until Task 7 has run on the target DB and `session_migration_map` is fully populated. After this migration, raw `session_id` is gone permanently.

- [ ] **Step 1: Write the up migration**

Create `server/migrations/053_session_drop.up.sql`:

```sql
-- Phase 6 (DESTRUCTIVE): drop session* tables and message.session_id.
-- Pre-condition: every row in `session` has a row in `session_migration_map`.
-- This statement aborts the migration if not.
DO $$
DECLARE missing INT;
BEGIN
    SELECT COUNT(*) INTO missing
    FROM session s
    LEFT JOIN session_migration_map m ON m.session_id = s.id
    WHERE m.session_id IS NULL;
    IF missing > 0 THEN
        RAISE EXCEPTION 'Refusing to drop session table: % session(s) not in session_migration_map', missing;
    END IF;
END $$;

-- Drop indexes referencing session_id.
DROP INDEX IF EXISTS idx_message_session;

-- Drop legacy session* tables.
DROP TABLE IF EXISTS session_participant;
DROP TABLE IF EXISTS session;

-- Drop message.session_id column.
ALTER TABLE message DROP COLUMN IF EXISTS session_id;

-- ===== System Agent scope rename: 'session' → 'conversation' =====
UPDATE agent SET scope = 'conversation' WHERE scope = 'session';

ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_scope_values_check;
ALTER TABLE agent ADD CONSTRAINT agent_scope_values_check
    CHECK (scope IS NULL OR scope IN ('account', 'conversation', 'project', 'file'));
```

- [ ] **Step 2: Write the down migration**

Create `server/migrations/053_session_drop.down.sql`:

```sql
-- Best-effort rollback. Tables are recreated empty (data is gone).

CREATE TABLE IF NOT EXISTS session (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    creator_id UUID NOT NULL,
    creator_type TEXT NOT NULL DEFAULT 'member',
    status TEXT NOT NULL DEFAULT 'active',
    max_turns INTEGER NOT NULL DEFAULT 0,
    current_turn INTEGER NOT NULL DEFAULT 0,
    context JSONB,
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS session_participant (
    session_id UUID NOT NULL REFERENCES session(id) ON DELETE CASCADE,
    participant_id UUID NOT NULL,
    participant_type TEXT NOT NULL DEFAULT 'member',
    role TEXT NOT NULL DEFAULT 'participant',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (session_id, participant_id, participant_type)
);

ALTER TABLE message ADD COLUMN IF NOT EXISTS session_id UUID;
CREATE INDEX IF NOT EXISTS idx_message_session ON message(session_id, created_at);

UPDATE agent SET scope = 'session' WHERE scope = 'conversation';
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_scope_values_check;
ALTER TABLE agent ADD CONSTRAINT agent_scope_values_check
    CHECK (scope IS NULL OR scope IN ('account', 'session', 'conversation', 'project', 'file'));
```

- [ ] **Step 3: Reject `session_id` writes in message handler**

In `server/internal/handler/message.go`, in `CreateMessage`:

```go
if body.SessionID != nil && *body.SessionID != "" {
    http.Error(w, "session_id is no longer supported; use thread_id", http.StatusBadRequest)
    return
}
```

Remove the Phase 2 `GetSessionMigrationByID` resolver — it's now handled at the route layer for legacy bookmarks.

- [ ] **Step 4: Remove `/api/sessions` from router**

In `server/cmd/server/router.go`, delete the entire `r.Route("/api/sessions", ...)` block (lines 360-373 in current file). Keep `/api/session-migrations/{sessionID}` (added in Task 8) for redirect support.

Delete the now-orphaned handlers from `server/internal/handler/session.go`:
- `CreateSession`, `ListSessions`, `GetSession`, `UpdateSession`, `JoinSession`,
- `ListSessionMessages`, `SessionSummary`, `StartAutoDiscussion`, `StopAutoDiscussion`, `ShareSessionContext`.

Keep `GetSessionMigration` (added in Task 8).

- [ ] **Step 5: Drop sqlc queries that reference `session` table**

```bash
rm /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691/server/pkg/db/queries/sessions.sql
make sqlc
cd server && go build ./...
```

Fix any remaining `_ = h.Queries.CreateSession(...)` or similar references inline.

- [ ] **Step 6: Apply migration**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make migrate-up
psql "$DATABASE_URL" -c "\d message" | grep session_id    # expected: empty output
psql "$DATABASE_URL" -c "\dt session*"                    # expected: only session_migration_map
psql "$DATABASE_URL" -c "SELECT DISTINCT scope FROM agent WHERE scope IS NOT NULL;"
```

Expected: no `session_id` column; only `session_migration_map` from the session* family; no `'session'` scope.

- [ ] **Step 7: Commit**

```bash
git add server/migrations/053_session_drop.up.sql server/migrations/053_session_drop.down.sql server/internal/handler/message.go server/internal/handler/session.go server/cmd/server/router.go server/pkg/db/queries/ server/pkg/db/generated/
git commit -m "feat(db): DESTRUCTIVE — drop session tables, message.session_id, scope=conversation"
```

---

### Task 10: End-to-end verification

- [ ] **Step 1: Full backend tests**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make test
```

Expected: all Go tests pass. Update fixtures in any test that previously created a `session` row.

- [ ] **Step 2: Frontend typecheck + tests**

```bash
pnpm typecheck
pnpm test
```

Expected: no `Session` / `SessionParticipant` / `session_id` references remain.

- [ ] **Step 3: Verify no DEPRECATED markers linger**

```bash
grep -rn "DEPRECATED.*session\|DEPRECATED.*migrating" server/ apps/web/
```

Expected: no matches. Remove any that turn up.

- [ ] **Step 4: Smoke-test live API**

```bash
make dev &
SERVER=$!
sleep 5
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login -d '{"email":"test@test"}' | jq -r .token)
curl -s -X POST http://localhost:8080/api/messages -H "Authorization: Bearer $TOKEN" -d '{"session_id":"00000000-0000-0000-0000-000000000000","content":"hi"}' | jq
kill $SERVER
```

Expected: 400 with `session_id is no longer supported; use thread_id`.

- [ ] **Step 5: Round-trip migrations**

```bash
make migrate-down  # 053 down
make migrate-down  # 052 down (best-effort)
make migrate-down  # 051 down
make migrate-up
make migrate-up
make migrate-up
```

Expected: all six steps succeed.

- [ ] **Step 6: Final verification commit**

If steps 1-5 surfaced fixes, commit them now. Otherwise:

```bash
git log --oneline -10   # confirm the plan's commits
```

---

## Self-Review Checklist

- [ ] Migration 051 added `workspace_id`, `root_message_id`, `issue_id`, `created_by`, `created_by_type`, `status`, `metadata`, `last_activity_at` to `thread`; created `thread_context_item` and `session_migration_map`.
- [ ] `thread.id` default is now `gen_random_uuid()`; existing rows have `root_message_id = id`.
- [ ] `thread_context_item.retention_class` defaults follow PRD §3.2 (decision=permanent, summary=ttl/permanent based on creator, file/code_snippet/reference=ttl).
- [ ] New endpoints exist: `POST /api/channels/{channelID}/threads`, `GET /api/threads/{threadID}`, `GET /api/threads/{threadID}/messages`, `POST /api/threads/{threadID}/messages`, `GET /api/threads/{threadID}/context`, `POST /api/threads/{threadID}/context-items`, `DELETE /api/threads/{threadID}/context-items/{id}`.
- [ ] Phase 2 message handler resolves legacy `session_id` via `session_migration_map` and stamps `metadata.source_session_id`.
- [ ] `incrementThreadCounters` updates `last_activity_at` always; updates `reply_count`/`last_reply_at` only for `member`/`agent` senders.
- [ ] Migration 052 created exactly one channel + thread per session, decomposed `session.context` into `thread_context_item` rows, and recomputed `reply_count` excluding system messages.
- [ ] Frontend `/sessions` routes redirect; `features/sessions/` is gone; `RemoteSessionsList` lives in `features/runtimes/`; `Session`/`SessionParticipant`/`Message.session_id` types removed.
- [ ] MediationService is the single router. `auto_reply.go` only exposes `MatchTriggers()`; SLA buckets at T+300/T+600/T+900; anti-loop guards (≤ 3 agent rounds, ≤ 50 replies/24h, no self-reply).
- [ ] Migration 053 dropped `session_participant`, `session`, and `message.session_id`; agent scope CHECK now allows `conversation` instead of `session`; `POST /api/messages` with `session_id` returns 400.
- [ ] `make test`, `pnpm typecheck`, `pnpm test` all green.
- [ ] No "DEPRECATED" comments remain referencing the session table.

---

## Out of Scope (Post-MVP)

- Background TTL cleanup job for `thread_context_item` (`expires_at < NOW() AND retention_class = 'ttl'`).
- Periodic auto-summary regeneration with old summary replacement.
- FileIndex-linked cleanup for `file` / `reference` context items.
- Removing `session_migration_map` after a deprecation window.
- Thread-level ACL (Threads currently inherit Channel membership).
- Channel merge/split for legacy session-derived channels (deferred per PRD §10).
- Cross-workspace Agent sharing for `scope='conversation'` System Agent.
- Advanced capability-matching scoring in MediationService — this plan ships simple rule-priority routing only.
