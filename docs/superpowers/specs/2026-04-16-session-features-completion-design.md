# Session Features Completion — Design Spec

- **Date**: 2026-04-16
- **Scope**: Fill the 7 feature gaps identified in the Session audit. Organized in 3 phases by complexity and dependency.
- **Depends on**: Sub-project #1 (Session MVP) and #2 (Personal Agent + Claude SDK) both merged to main.

## Feature Overview

| ID | Feature | Phase | Status Before |
|---|---|---|---|
| P1-1 | File tab (wire real data) | 1 | ⚠️ handler returns empty |
| P1-2 | Founder transfer | 1 | ❌ no code |
| P1-3 | Cross-owner visibility warning UI | 1 | ❌ no UI |
| P1-4 | UI fixes: typing indicator + New DM filter | 1 | ❌ no code |
| P2-1 | @offline agent push notification | 2 | ❌ no code |
| P2-2 | Sub-reply-slots 30s tracking | 2 | ❌ no code |
| P2-3 | Thread → Channel promotion | 2 | ❌ no code |
| P3-1 | Channel merge | 3 | ❌ no code |
| P3-2 | Channel split | 3 | ❌ no code |

---

## Phase 1: Quick Wins

### P1-1: File Tab (Wire Real Data)

**Goal**: Each channel/chat shows a "Files" sub-view listing files shared in that conversation.

**Backend**:
- `server/internal/handler/file_index.go` currently returns `[]` for all queries. Wire it to the `file_index` table which already has `channel_id`, `workspace_id` columns.
- Need sqlc query: `ListFilesByChannel(workspace_id, channel_id)` → returns file_name, content_type, file_size, source_type, created_at.
- If sqlc is broken (pre-existing project.sql errors), write raw SQL in the handler (same pattern as ListConversations fix).

**Desktop**:
- Add a "Files" tab or toggle to the channel/DM detail view.
- Call `GET /api/files?channel_id=...` and render a simple list.

**Not in scope**: Upload UI, dedup, agent-generated file tracking.

---

### P1-2: Founder Transfer

**Goal**: Channel creator (founder) can transfer founder role to another member. After transfer, original founder can leave.

**Backend**:
- Add `founder_id` column to `channel` table (initially = `created_by`). Migration: `ALTER TABLE channel ADD COLUMN founder_id UUID REFERENCES "user"(id)`. Backfill: `UPDATE channel SET founder_id = created_by`.
- New endpoint: `POST /api/channels/{id}/transfer-founder` body: `{ "new_founder_id": "..." }`. Only current founder can call.
- Handler: validate caller is founder, update `founder_id`, respond 200.

**Desktop**:
- In channel settings (future) or via a context menu, show "Transfer Founder" option visible only to founder.
- MVP: just the API; desktop UI deferred to when channel settings page exists.

---

### P1-3: Cross-Owner Visibility Warning UI

**Goal**: When a user opens a DM between their agent and another owner's agent, display a prominent banner: "All cross-owner conversations are visible to both owners."

**Desktop**:
- In the DM message area header, check: is the peer an agent owned by a different owner?
- If yes, render a yellow info bar below the title: "跨 owner 对话对双方 owner 可见"
- Data needed: peer's `owner_id` (already in agent data from workspace store).

**Backend**: No changes needed — agent ownership is already in the agent row.

---

### P1-4: UI Fixes

#### P1-4a: Typing Indicator

**Goal**: After user sends a message to an agent, show "dy9759's Assistant is typing..." until the reply arrives.

**Desktop**:
- When `sendMessage` succeeds in the store AND the recipient is an agent, set `typingAgentName` state.
- Clear it when a WS `message:created` event arrives from that agent.
- Render a small animated indicator below the last message.

**Backend**: No changes — the typing state is purely client-side (agent reply latency is the "typing" period).

#### P1-4b: New DM Picker Filter

**Goal**: The `+ New DM` dialog should NOT show system agents (System Agent, page agents). Only personal agents and members.

**Desktop**:
- In `session-route.tsx` where `mentionCandidates` / `dmCandidates` are built from `agents`, filter: `agents.filter(a => !a.is_system)`.
- Requires `is_system` field on the agent data from workspace store. Check if `SessionAgent` type includes it.

---

## Phase 2: Medium Complexity

### P2-1: @Offline Agent Push Notification

**Goal**: When a user @mentions an agent that is offline, system creates a DM to the agent's owner: "Your agent X was mentioned in #channel but is offline. Please bring it online."

**Backend**:
- In `AutoReplyService.CheckAndReply` or `MediationService.checkNeedsResponse`, after looking up the mentioned agent:
  - Check `agent.online_status`. If `"offline"`:
    - Find the agent's `owner_id`.
    - Create a system DM from "System Agent" to the owner: `"Your agent {name} was @mentioned in #{channel} but is offline. Please bring it online or respond manually."`
    - Set `reply_status = "awaiting_owner"` on the original message (new field or use `reply_expected`).
  - After 30s with no response, mark as `unresolved`.

**Desktop**: No specific UI — the owner sees the DM in their inbox.

---

### P2-2: Sub-Reply-Slots 30s Tracking

**Goal**: System agent splits each message into semantic "reply slots", each tracked independently with a 30s window.

**Backend**:
- New table: `reply_slot`
  ```sql
  CREATE TABLE reply_slot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id UUID NOT NULL REFERENCES message(id) ON DELETE CASCADE,
    channel_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    slot_index INTEGER NOT NULL,
    content_summary TEXT,
    assigned_agent_id UUID REFERENCES agent(id),
    status TEXT NOT NULL DEFAULT 'pending',  -- pending/replied/escalated/expired
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    replied_at TIMESTAMPTZ,
    reply_message_id UUID REFERENCES message(id)
  );
  ```
- `MediationService`: on `message:created` from an owner:
  1. Call LLM (system agent) to split message into N semantic slots.
  2. Insert N `reply_slot` rows, each with `expires_at = NOW() + 30s`.
  3. For each slot, assign an agent based on relevance (existing responder assignment logic).
  4. Background timer checks expired slots → escalate (reassign or notify owner).
- Semantic splitting can use a simple heuristic (split by sentence/paragraph) or LLM call.

**Desktop**: Show slot tracking UI in a future iteration (MVP: backend only).

---

### P2-3: Thread Promotion to Channel

**Goal**: Any thread can be "upgraded" to a standalone channel. Messages are copied; original channel keeps a link.

**Backend**:
- New endpoint: `POST /api/threads/{threadId}/promote` body: `{ "channel_name": "..." }`
- Handler:
  1. Get all messages in the thread.
  2. Create a new channel (name from body, founder = caller).
  3. Copy messages to the new channel (preserve sender, timestamp, content; new message IDs).
  4. Add a system message in the original channel: "Thread promoted to #new-channel".
  5. Return the new channel.

**Desktop**: Thread panel gets a "Promote to Channel" button.

---

## Phase 3: High Complexity

### P3-1: Channel Merge

**Goal**: Two channels (or a channel + a DM) can be merged into one. Requires all founders' consent.

**Backend**:
- New table: `merge_request`
  ```sql
  CREATE TABLE merge_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_channel_id UUID NOT NULL REFERENCES channel(id),
    target_channel_id UUID NOT NULL REFERENCES channel(id),
    initiated_by UUID NOT NULL REFERENCES "user"(id),
    status TEXT NOT NULL DEFAULT 'pending',  -- pending/approved/rejected/completed
    approvals JSONB DEFAULT '[]',  -- [{founder_id, approved_at}]
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
  );
  ```
- Flow:
  1. `POST /api/channels/{targetId}/merge-request` body: `{ "source_channel_id": "..." }`
  2. System checks: caller is founder of source OR target.
  3. Creates `merge_request`. Notifies all founders of both channels.
  4. Each founder approves via `POST /api/merge-requests/{id}/approve`.
  5. When all founders approve: execute merge.
- Merge execution:
  1. Copy all messages from source into target, interleaving by `created_at`.
  2. Move all members from source to target.
  3. Preserve independent threads (they keep their thread_id).
  4. Set merged channel founder = merge initiator.
  5. Archive source channel (don't delete — keep for audit).
  6. System message in target: "Channel #source merged into #target."

**Desktop**: "Merge" option in channel settings → picker for target → approval flow UI.

---

### P3-2: Channel Split

**Goal**: Any member can split a channel by selecting members to move to a new channel/DM.

**Backend**:
- New endpoint: `POST /api/channels/{id}/split` body: `{ "member_ids": [...], "name": "new-channel" }`
- Handler:
  1. Create new channel (name from body, founder = caller).
  2. Add selected members to new channel.
  3. Do NOT copy historical messages (split starts fresh).
  4. System message in original: "Members X, Y split into #new-channel."
  5. System message in new channel: "Split from #original by @initiator."

**Desktop**: "Split" option → member picker → confirm.

---

## Out of Scope (deferred)

- Per-channel auto-reply toggle UI (backend supports it, UI deferred)
- Impersonation pause auto-reply logic (impersonation works, pause is nuanced)
- `agent_structured` / `mention_reply` message types (no clear use case yet)
- File upload in DM (sub-project #6 File page scope)
- Cross-owner DM read API (data queryable, API deferred until Account rework)

## Migration Ordering

Phase 1 → Phase 2 → Phase 3. Within each phase, items are independent and can be parallelized via subagents.

P1-2 (founder transfer) should land before P3-1 (merge) since merge requires founder consent.
