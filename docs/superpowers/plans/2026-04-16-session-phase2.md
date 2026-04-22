# Session Phase 2 — Medium Complexity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement 3 medium-complexity Session features: @offline agent push notification, sub-reply-slots 30s tracking, and thread promotion to channel.

**Architecture:** All 3 items are independent. P2-1 and P2-2 extend existing MediationService/AutoReplyService. P2-3 adds a new endpoint. Backend raw SQL (sqlc is broken). No desktop UI changes in this phase (backend-only for P2-1/P2-2; P2-3 adds one button to desktop thread panel if it exists, otherwise API-only).

**Tech Stack:** Go 1.26, PostgreSQL, TypeScript.

**Spec:** [docs/superpowers/specs/2026-04-16-session-features-completion-design.md](../specs/2026-04-16-session-features-completion-design.md) §Phase 2

**Working directory:** Dedicated worktree `.claude/worktrees/session-phase2` on branch `feat/session-phase2`.

---

## Task 1: @Offline Agent Push Notification

**Files:**
- Modify: `server/internal/service/auto_reply.go`

When an agent is @mentioned but offline, send a DM to the agent's owner.

- [ ] **Step 1: Add offline check to replyAsMentionedAgent**

In `server/internal/service/auto_reply.go`, in `replyAsMentionedAgent`, after looking up the agent and BEFORE calling `s.Runner.Run(...)`, add:

```go
// Check if agent is offline — notify owner instead of running LLM.
if agent.OnlineStatus == "offline" {
	ownerID := util.UUIDToString(agent.OwnerID)
	if ownerID != "" {
		notifContent := fmt.Sprintf("Your agent %s was @mentioned in a conversation but is offline. Please bring it online or respond manually.", agentName)
		_, _ = s.Queries.CreateMessage(ctx, db.CreateMessageParams{
			WorkspaceID:   util.ParseUUID(workspaceID),
			SenderID:      agent.ID,
			SenderType:    "agent",
			RecipientID:   agent.OwnerID,
			RecipientType: util.StrToText("member"),
			Content:       notifContent,
			ContentType:   "text",
			Type:          "system_notification",
		})
		slog.Info("auto-reply: agent offline, notified owner", "agent", agentName, "owner", ownerID)
	}
	return nil
}
```

Also add the same check in `ReplyToDM` — if agent offline, notify owner:

```go
// After agent lookup, before config check:
if agent.OnlineStatus == "offline" {
	// ... same notification logic ...
	return
}
```

- [ ] **Step 2: Build**

```bash
cd server && go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add server/internal/service/auto_reply.go
git commit -m "feat(server): notify owner when offline agent is @mentioned"
```

---

## Task 2: Sub-reply-slots 30s Tracking

**Files:**
- Create: `server/migrations/046_reply_slots.up.sql`
- Create: `server/migrations/046_reply_slots.down.sql`
- Modify: `server/internal/service/mediation.go`

- [ ] **Step 1: Create migration**

`server/migrations/046_reply_slots.up.sql`:
```sql
CREATE TABLE IF NOT EXISTS reply_slot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id UUID NOT NULL REFERENCES message(id) ON DELETE CASCADE,
    channel_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    slot_index INTEGER NOT NULL DEFAULT 0,
    content_summary TEXT,
    assigned_agent_id UUID REFERENCES agent(id),
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '30 seconds',
    replied_at TIMESTAMPTZ,
    reply_message_id UUID REFERENCES message(id)
);

CREATE INDEX IF NOT EXISTS idx_reply_slot_message ON reply_slot(message_id);
CREATE INDEX IF NOT EXISTS idx_reply_slot_status ON reply_slot(workspace_id, status) WHERE status = 'pending';
```

`server/migrations/046_reply_slots.down.sql`:
```sql
DROP TABLE IF EXISTS reply_slot;
```

- [ ] **Step 2: Run migration**

```bash
cd server && go run ./cmd/migrate up
```

- [ ] **Step 3: Add slot creation to MediationService**

In `server/internal/service/mediation.go`, find the `handleMessageCreated` method (or equivalent — the function that processes `message:created` events). After the existing response-check logic, add slot creation:

```go
// Create a reply slot for owner messages that need responses.
if msg.SenderType == "member" && msg.ChannelID.Valid {
	_, err := h.DB.Exec(ctx, `
		INSERT INTO reply_slot (message_id, channel_id, workspace_id, slot_index, content_summary, status, expires_at)
		VALUES ($1, $2, $3, 0, $4, 'pending', NOW() + INTERVAL '30 seconds')
	`, msg.ID, msg.ChannelID, msg.WorkspaceID, truncate(msg.Content, 100))
	if err != nil {
		slog.Debug("reply-slot: insert failed", "error", err)
	}
}
```

Add a helper:
```go
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
```

- [ ] **Step 4: Add slot expiry checker**

Add a background goroutine in MediationService.Start() that checks for expired slots every 10 seconds:

```go
go func() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkExpiredSlots(ctx)
		}
	}
}()
```

```go
func (s *MediationService) checkExpiredSlots(ctx context.Context) {
	rows, err := s.DB.Query(ctx, `
		SELECT rs.id, rs.message_id, rs.workspace_id, rs.channel_id, rs.content_summary
		FROM reply_slot rs
		WHERE rs.status = 'pending' AND rs.expires_at < NOW()
		LIMIT 20
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var slotID, msgID, wsID, chID pgtype.UUID
		var summary pgtype.Text
		if err := rows.Scan(&slotID, &msgID, &wsID, &chID, &summary); err != nil {
			continue
		}

		// Mark as escalated.
		_, _ = s.DB.Exec(ctx, `UPDATE reply_slot SET status = 'escalated' WHERE id = $1`, slotID)

		// Post system notification in the channel.
		content := "A message has not been replied to within 30 seconds."
		if summary.Valid && summary.String != "" {
			content = fmt.Sprintf("Unreplied message: \"%s\" — please respond.", summary.String)
		}
		_, _ = s.Queries.CreateMessage(ctx, db.CreateMessageParams{
			WorkspaceID: wsID,
			SenderID:    s.SystemAgentID(ctx, wsID),
			SenderType:  "agent",
			ChannelID:   chID,
			Content:     content,
			ContentType: "text",
			Type:        "system_notification",
		})
	}
}
```

Note: `s.SystemAgentID(ctx, wsID)` needs to look up the system agent for the workspace. Add a helper:
```go
func (s *MediationService) SystemAgentID(ctx context.Context, wsID pgtype.UUID) pgtype.UUID {
	agent, err := s.Queries.GetSystemAgent(ctx, wsID)
	if err != nil {
		return pgtype.UUID{} // zero UUID as fallback
	}
	return agent.ID
}
```

Also: when an agent replies to a channel, mark any pending slots for that channel as 'replied':

In the message:created handler, if sender_type is "agent":
```go
if msg.SenderType == "agent" && msg.ChannelID.Valid {
	_, _ = h.DB.Exec(ctx, `
		UPDATE reply_slot SET status = 'replied', replied_at = NOW(), reply_message_id = $1
		WHERE channel_id = $2 AND status = 'pending'
	`, msg.ID, msg.ChannelID)
}
```

- [ ] **Step 5: Ensure MediationService has DB pool**

Check if MediationService struct has a `DB *pgxpool.Pool` field. If not, add it and wire it in `router.go` / `main.go`.

- [ ] **Step 6: Build**

```bash
cd server && go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add server/migrations/046_reply_slots.up.sql server/migrations/046_reply_slots.down.sql \
        server/internal/service/mediation.go
git commit -m "feat(server): add reply_slot table + 30s expiry tracking in MediationService"
```

---

## Task 3: Thread Promotion to Channel

**Files:**
- Modify: `server/internal/handler/message.go` (or create `thread.go`)
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Add PromoteThread handler**

In `server/internal/handler/message.go` (or a new `server/internal/handler/thread.go`), add:

```go
// POST /api/threads/{threadID}/promote
func (h *Handler) PromoteThread(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	threadID := chi.URLParam(r, "threadID")

	var req struct {
		ChannelName string `json:"channel_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChannelName == "" {
		writeError(w, http.StatusBadRequest, "channel_name is required")
		return
	}

	// 1. Get all thread messages.
	threadMessages, err := h.Queries.ListThreadMessages(r.Context(), db.ListThreadMessagesParams{
		ParentID: parseUUID(threadID),
		Limit:    1000,
		Offset:   0,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "thread not found or empty")
		return
	}

	// 2. Create new channel.
	newCh, err := h.Queries.CreateChannel(r.Context(), db.CreateChannelParams{
		WorkspaceID:   parseUUID(workspaceID),
		Name:          req.ChannelName,
		Description:   pgtype.Text{String: "Promoted from thread", Valid: true},
		CreatedBy:     parseUUID(userID),
		CreatedByType: "member",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}

	// 3. Copy messages to new channel.
	for _, msg := range threadMessages {
		_, _ = h.DB.Exec(r.Context(), `
			INSERT INTO message (workspace_id, sender_id, sender_type, channel_id, content, content_type, type, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, msg.WorkspaceID, msg.SenderID, msg.SenderType, newCh.ID,
			msg.Content, msg.ContentType, msg.Type, msg.CreatedAt)
	}

	// 4. Post link in original channel.
	// Get the thread's parent message to find the original channel.
	var origChannelID pgtype.UUID
	_ = h.DB.QueryRow(r.Context(),
		`SELECT channel_id FROM message WHERE id = $1`, parseUUID(threadID),
	).Scan(&origChannelID)

	if origChannelID.Valid {
		_, _ = h.DB.Exec(r.Context(), `
			INSERT INTO message (workspace_id, sender_id, sender_type, channel_id, content, content_type, type)
			VALUES ($1, $2, 'member', $3, $4, 'text', 'system_notification')
		`, parseUUID(workspaceID), parseUUID(userID), origChannelID,
			fmt.Sprintf("Thread promoted to channel #%s", req.ChannelName))
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"channel": channelToResponse(newCh),
		"copied_messages": len(threadMessages),
	})
}
```

Check that `channelToResponse` exists in the handler package (it should be in channel.go). If not, return the raw `newCh` struct.

- [ ] **Step 2: Register route**

In `server/cmd/server/router.go`, add inside the protected routes:

```go
r.Post("/api/threads/{threadID}/promote", h.PromoteThread)
```

- [ ] **Step 3: Build**

```bash
cd server && go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add server/internal/handler/message.go server/cmd/server/router.go
git commit -m "feat(server): add POST /api/threads/{threadID}/promote endpoint"
```

---

## Task 4: Final Verification

- [ ] **Step 1: Full build + test**

```bash
cd server && go build ./... && DATABASE_URL="postgres://myteam:myteam@localhost:5432/myteam?sslmode=disable" go test ./...
pnpm --filter @myteam/desktop typecheck && pnpm --filter @myteam/desktop test
```

- [ ] **Step 2: Commit + merge**

Use superpowers:finishing-a-development-branch to merge into main.
