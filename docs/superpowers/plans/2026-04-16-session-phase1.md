# Session Phase 1 — Quick Wins Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close 4 quick-win gaps in the Session page: wire file tab data, add founder transfer API, cross-owner visibility warning, and two UI fixes (typing indicator + DM picker filter).

**Architecture:** All 4 items are independent. Backend changes are in Go handlers with raw SQL (sqlc is broken). Desktop changes are in React components + stores. Each task produces a self-contained commit.

**Tech Stack:** Go 1.26, PostgreSQL, TypeScript, React 19, Zustand, Tailwind CSS.

**Spec:** [docs/superpowers/specs/2026-04-16-session-features-completion-design.md](../specs/2026-04-16-session-features-completion-design.md)

**Working directory:** Dedicated worktree `.claude/worktrees/session-phase1` on branch `feat/session-phase1`.

---

## File structure

### Create
```
server/migrations/045_founder_id.up.sql
server/migrations/045_founder_id.down.sql
```

### Modify
```
server/internal/handler/file_index.go          — wire ListFiles to real DB query
server/internal/handler/channel.go             — add TransferFounder handler
server/cmd/server/router.go                    — register new route
apps/desktop/src/features/messaging/components/message-list.tsx  — typing indicator
apps/desktop/src/features/messaging/components/typing-indicator.tsx  — new component
apps/desktop/src/routes/session-route.tsx       — filter DM picker + cross-owner warning
```

---

## Task 1: Wire ListFiles to real DB data

**Files:**
- Modify: `server/internal/handler/file_index.go`

- [ ] **Step 1: Replace the TODO with a raw SQL query**

In `server/internal/handler/file_index.go`, replace the `ListFiles` function body (lines 66-109). The new implementation uses `h.DB.Query()` with dynamic WHERE clauses based on query params:

```go
func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	_, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	sourceType := r.URL.Query().Get("source_type")
	projectID := r.URL.Query().Get("project_id")
	channelID := r.URL.Query().Get("channel_id")
	ownerID := r.URL.Query().Get("owner_id")

	query := `SELECT id, workspace_id, file_name, file_size, content_type,
	                 source_type, source_id, storage_path, channel_id, project_id,
	                 owner_id, created_at
	          FROM file_index WHERE workspace_id = $1`
	args := []any{parseUUID(workspaceID)}
	argIdx := 2

	if channelID != "" {
		query += fmt.Sprintf(" AND channel_id = $%d", argIdx)
		args = append(args, parseUUID(channelID))
		argIdx++
	}
	if projectID != "" {
		query += fmt.Sprintf(" AND project_id = $%d", argIdx)
		args = append(args, parseUUID(projectID))
		argIdx++
	}
	if sourceType != "" {
		query += fmt.Sprintf(" AND source_type = $%d", argIdx)
		args = append(args, sourceType)
		argIdx++
	}
	if ownerID != "" {
		query += fmt.Sprintf(" AND owner_id = $%d", argIdx)
		args = append(args, parseUUID(ownerID))
		argIdx++
	}
	query += " ORDER BY created_at DESC LIMIT 200"

	rows, err := h.DB.Query(r.Context(), query, args...)
	if err != nil {
		slog.Warn("list files query failed", "error", err)
		writeJSON(w, http.StatusOK, []FileIndexResponse{})
		return
	}
	defer rows.Close()

	var files []FileIndexResponse
	for rows.Next() {
		var f FileIndexResponse
		var id, wsID, sourceID, chID, projID, ownerUUID pgtype.UUID
		var fileSize pgtype.Int8
		var contentType, storagePath pgtype.Text
		if err := rows.Scan(&id, &wsID, &f.FileName, &fileSize,
			&contentType, &f.SourceType, &sourceID, &storagePath,
			&chID, &projID, &ownerUUID, &f.CreatedAt); err != nil {
			continue
		}
		f.ID = uuidToString(id)
		f.WorkspaceID = uuidToString(wsID)
		f.SourceID = uuidToString(sourceID)
		f.ChannelID = uuidToString(chID)
		f.ProjectID = uuidToString(projID)
		f.OwnerID = uuidToString(ownerUUID)
		if fileSize.Valid {
			f.FileSize = fileSize.Int64
		}
		if contentType.Valid {
			f.ContentType = contentType.String
		}
		if storagePath.Valid {
			f.StoragePath = storagePath.String
		}
		files = append(files, f)
	}
	if files == nil {
		files = []FileIndexResponse{}
	}
	writeJSON(w, http.StatusOK, files)
}
```

Ensure `"fmt"` is imported (it should already be).

- [ ] **Step 2: Verify build**

```bash
cd server && go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add server/internal/handler/file_index.go
git commit -m "feat(server): wire ListFiles to real file_index table query"
```

---

## Task 2: Founder transfer migration + handler

**Files:**
- Create: `server/migrations/045_founder_id.up.sql`
- Create: `server/migrations/045_founder_id.down.sql`
- Modify: `server/internal/handler/channel.go`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Create migration**

`server/migrations/045_founder_id.up.sql`:
```sql
ALTER TABLE channel ADD COLUMN IF NOT EXISTS founder_id UUID REFERENCES "user"(id);
UPDATE channel SET founder_id = created_by WHERE founder_id IS NULL;
```

`server/migrations/045_founder_id.down.sql`:
```sql
ALTER TABLE channel DROP COLUMN IF EXISTS founder_id;
```

- [ ] **Step 2: Run migration**

```bash
cd server && go run ./cmd/migrate up
```

- [ ] **Step 3: Add TransferFounder handler**

In `server/internal/handler/channel.go`, add at the end:

```go
// POST /api/channels/{channelID}/transfer-founder
func (h *Handler) TransferFounder(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	channelID := chi.URLParam(r, "channelID")

	var req struct {
		NewFounderID string `json:"new_founder_id"`
	}
	if err := readJSON(r, &req); err != nil || req.NewFounderID == "" {
		writeError(w, http.StatusBadRequest, "new_founder_id is required")
		return
	}

	// Check caller is current founder.
	var currentFounder pgtype.UUID
	err := h.DB.QueryRow(r.Context(),
		`SELECT COALESCE(founder_id, created_by) FROM channel WHERE id = $1`,
		parseUUID(channelID),
	).Scan(&currentFounder)
	if err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if uuidToString(currentFounder) != userID {
		writeError(w, http.StatusForbidden, "only the founder can transfer ownership")
		return
	}

	// Update founder.
	_, err = h.DB.Exec(r.Context(),
		`UPDATE channel SET founder_id = $1 WHERE id = $2`,
		parseUUID(req.NewFounderID), parseUUID(channelID),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to transfer founder")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "transferred"})
}
```

- [ ] **Step 4: Register route**

In `server/cmd/server/router.go`, find the channel routes block (`r.Route("/api/channels", ...)`). Inside the `r.Route("/{channelID}", ...)` sub-router, add:

```go
r.Post("/transfer-founder", h.TransferFounder)
```

- [ ] **Step 5: Build + verify**

```bash
cd server && go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add server/migrations/045_founder_id.up.sql server/migrations/045_founder_id.down.sql \
        server/internal/handler/channel.go server/cmd/server/router.go
git commit -m "feat(server): add founder_id column + TransferFounder endpoint"
```

---

## Task 3: Cross-owner DM visibility warning

**Files:**
- Modify: `apps/desktop/src/routes/session-route.tsx`

- [ ] **Step 1: Add warning banner to DM view**

In `session-route.tsx`, find the message area header (the `<h3>` with the conversation title). Add a warning banner below it when the DM peer is an agent owned by a different user.

After the `<h3>` tag inside the message section header div, add:

```tsx
{selection?.kind === "dm" && (() => {
  const peer = agents.find(a => a.id === selection.conversation.peer_id);
  // Show warning if peer is an agent (cross-owner DMs are visible to both owners)
  if (peer && selection.conversation.peer_type === "agent") {
    return (
      <p className="mt-2 rounded-xl bg-amber-500/10 px-3 py-1.5 text-xs text-amber-300">
        跨 owner 对话对双方 owner 可见
      </p>
    );
  }
  return null;
})()}
```

- [ ] **Step 2: Typecheck**

```bash
pnpm --filter @myteam/desktop typecheck
```

- [ ] **Step 3: Commit**

```bash
git add apps/desktop/src/routes/session-route.tsx
git commit -m "feat(desktop): add cross-owner DM visibility warning banner"
```

---

## Task 4: Typing indicator

**Files:**
- Create: `apps/desktop/src/features/messaging/components/typing-indicator.tsx`
- Modify: `apps/desktop/src/routes/session-route.tsx`
- Modify: `apps/desktop/src/features/messaging/index.ts`

- [ ] **Step 1: Create TypingIndicator component**

`apps/desktop/src/features/messaging/components/typing-indicator.tsx`:

```tsx
interface Props {
  agentName: string;
}

export function TypingIndicator({ agentName }: Props) {
  return (
    <div className="flex items-center gap-2 rounded-3xl border border-border/70 bg-background/70 px-4 py-3 text-sm text-muted-foreground">
      <span className="inline-flex gap-1">
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground [animation-delay:0ms]" />
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground [animation-delay:150ms]" />
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground [animation-delay:300ms]" />
      </span>
      <span>{agentName} is typing...</span>
    </div>
  );
}
```

- [ ] **Step 2: Export from barrel**

Add to `apps/desktop/src/features/messaging/index.ts`:

```ts
export { TypingIndicator } from "./components/typing-indicator";
```

- [ ] **Step 3: Wire into session-route**

In `session-route.tsx`:

Add state:
```tsx
const [typingAgent, setTypingAgent] = useState<string | null>(null);
```

Import `TypingIndicator` from `@/features/messaging`.

Modify `handleSend` to set typing state when sending to an agent:
```tsx
const handleSend = async (text: string) => {
  if (!selection) return;
  if (selection.kind === "dm" && selection.conversation.peer_type === "agent") {
    setTypingAgent(selection.conversation.peer_name ?? "Agent");
  }
  if (selection.kind === "channel") {
    await sendMessage({ channel_id: selection.channel.id, content: text });
  } else {
    await sendMessage({
      recipient_id: selection.conversation.peer_id,
      recipient_type: selection.conversation.peer_type,
      content: text,
    });
  }
};
```

Add an effect to clear typing when a new message arrives from the agent:
```tsx
useEffect(() => {
  if (!typingAgent) return;
  // Clear typing when we receive a message from the agent (last message sender is agent)
  const lastMsg = currentMessages[currentMessages.length - 1];
  if (lastMsg && lastMsg.sender_type === "agent") {
    setTypingAgent(null);
  }
}, [currentMessages, typingAgent]);
```

Render the indicator in the message area, below `<MessageList>` and above `<MessageInput>`:
```tsx
{typingAgent ? <TypingIndicator agentName={typingAgent} /> : null}
```

- [ ] **Step 4: Typecheck + test**

```bash
pnpm --filter @myteam/desktop typecheck
pnpm --filter @myteam/desktop test
```

- [ ] **Step 5: Commit**

```bash
git add apps/desktop/src/features/messaging/components/typing-indicator.tsx \
        apps/desktop/src/features/messaging/index.ts \
        apps/desktop/src/routes/session-route.tsx
git commit -m "feat(desktop): add typing indicator for agent DM replies"
```

---

## Task 5: New DM picker filters out system agents

**Files:**
- Modify: `apps/desktop/src/routes/session-route.tsx`

- [ ] **Step 1: Filter system agents from DM candidates**

In `session-route.tsx`, find where `mentionCandidates` is built from `agents`:

```tsx
const mentionCandidates = useMemo(() => {
  return [
    ...agents.map((a) => ({
      id: a.id,
      name: a.name,
      kind: "agent" as const,
    })),
    ...members.map((m) => ({
      id: m.id,
      name: m.name,
      kind: "owner" as const,
    })),
  ];
}, [agents, members]);
```

Change to filter out system agents:

```tsx
const mentionCandidates = useMemo(() => {
  const personalAgents = agents.filter(
    (a) => a.agent_type !== "system_agent" && a.agent_type !== "page_system_agent"
  );
  return [
    ...personalAgents.map((a) => ({
      id: a.id,
      name: a.name,
      kind: "agent" as const,
    })),
    ...members.map((m) => ({
      id: m.id,
      name: m.name,
      kind: "owner" as const,
    })),
  ];
}, [agents, members]);
```

Check that `agent_type` is available on the `agents` array items. The workspace store returns `SessionAgent` which is `Agent` from `apps/web/shared/types/agent.ts` — it has `agent_type?: AgentType` where `AgentType = "personal_agent" | "system_agent" | "page_system_agent"`.

If the type doesn't include `agent_type`, check via `(a as any).agent_type` or verify it's in the API response.

- [ ] **Step 2: Typecheck**

```bash
pnpm --filter @myteam/desktop typecheck
```

- [ ] **Step 3: Commit**

```bash
git add apps/desktop/src/routes/session-route.tsx
git commit -m "feat(desktop): filter system agents from New DM picker"
```

---

## Task 6: Final verification

- [ ] **Step 1: Full build + test**

```bash
cd server && go build ./... && go test ./...
pnpm --filter @myteam/client-core test
pnpm --filter /web test
pnpm --filter @myteam/desktop test
pnpm --filter @myteam/desktop typecheck
```

- [ ] **Step 2: Manual checks**

1. Open desktop → Session → sidebar DIRECT MESSAGES lists existing DMs ✓
2. Click `+ New DM` → System Agent / page agents NOT shown; only personal agents + members ✓
3. DM dy9759's Assistant → send message → "is typing..." indicator appears → clears when reply arrives ✓
4. DM an agent → header shows "跨 owner 对话对双方 owner 可见" warning ✓
5. Go to a channel → check if files show (may be empty if no files indexed — that's OK, the query works) ✓

- [ ] **Step 3: Commit + merge**

Use superpowers:finishing-a-development-branch to merge `feat/session-phase1` into `main`.
