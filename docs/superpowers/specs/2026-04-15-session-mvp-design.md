# Session MVP — Design Spec

- **Date**: 2026-04-15
- **Scope**: Sub-project #1 of the desktop client migration. Ship the smallest end-to-end messaging loop on the desktop client: send → receive (WS) → @-mention → agent auto-reply.
- **Sub-projects deferred**: thread, merge/split, privacy attributes, impersonation, auto-reply frequency limits, file tab, sub-reply-slots, cross-channel @, offline @ push, typing indicator, unread counts, channel member management.

## 1. Goals & Non-Goals

### Goals

1. User can send and receive text messages in a DM or channel from the desktop client.
2. Messages arrive in real time via WebSocket; no manual reload.
3. User can create a new DM (pick an owner or agent) and a new private channel (name only).
4. User can `@AgentName` inside a message; the backend parses the mention and triggers the agent's auto-reply via DashScope; the reply streams back through WS into the same thread.
5. The web client keeps working with zero visible regression. Its messaging logic is relocated into `packages/client-core` but behavior is unchanged.

### Non-Goals

- Threads, merge/split, privacy toggles, invite codes, member management UI.
- Impersonation indicator, typing indicator, unread counts, read receipts.
- Markdown rendering, file attachments, emoji picker, message editing/deletion.
- Offline @ push notifications, cross-channel mentions.
- WS message backfill after reconnect — user re-selects conversation to refetch.
- Playwright / Electron E2E — covered by manual acceptance list.

## 2. Architecture

### 2.1 Package structure

```
packages/client-core/src/messaging/
├── types.ts             — Message / Conversation / Channel / Thread types
├── ws-client.ts         — lifted from apps/web/shared/api/ws-client.ts, zero web deps
├── store-factory.ts     — createMessagingStore(apiClient, wsClient) → Zustand store
├── mention-parser.ts    — detectTrigger(text, caret) + filterCandidates(list, query)
└── index.ts
```

`packages/client-core` stays free of web/desktop framework coupling. It may depend on `zustand` and `react` (for store hooks) but **must not** import Next.js, `sonner`, `lucide-react`, or anything under `apps/web/shared/*` except by lifting those modules into `client-core` first.

### 2.2 Consumer layout

```
apps/web/features/messaging/
├── store.ts              — thin wrapper: createMessagingStore(api, wsSingleton), exports useMessagingStore
└── components/*          — existing message-input, message-list, thread-panel stay put

apps/desktop/src/features/messaging/
├── store.ts              — thin wrapper: createMessagingStore(desktopApi, desktopWS), exports useDesktopMessagingStore
└── components/
    ├── message-list.tsx     — desktop rounded-3xl thick-card style, ~120 LOC
    ├── message-input.tsx    — textarea + Enter send + @ picker, ~180 LOC
    ├── mention-picker.tsx   — absolute-positioned dropdown, ~60 LOC
    ├── new-dm-dialog.tsx    — picker of owners+agents, ~80 LOC
    └── new-channel-dialog.tsx — name input, visibility=private, ~60 LOC
```

### 2.3 Key principles

- `createMessagingStore` is a **factory**, not a singleton — each consumer owns its own Zustand instance but shares reducer/action code.
- `createWSClient({ url, getToken, onEvent })` is also a factory and returns `{ connect, disconnect, subscribe, status }`. It is **not** a React Provider. Each consumer wires it into its own bootstrap path.
- Error surfacing goes through an `onError(message: string)` callback passed in at factory creation. `client-core` never talks to `sonner` or `alert`.
- `client-core` remains unaware of which store (workspace / auth) the consumer uses. Agent/member lists for the mention picker are read by the picker from the consumer's workspace store, not from `client-core`.

## 3. Data flow

### 3.1 REST — initial load

```
UI mount or selection change
    ↓
store.actions.loadConversations()
store.actions.loadChannels()
store.actions.loadMessages({ channel_id | recipient_id })
    ↓
apiClient: GET /api/messages/conversations
           GET /api/channels
           GET /api/messages?channel_id=... | ?recipient_id=...
    ↓
Go backend → PostgreSQL → rows → store.setState
```

### 3.2 Send with @-mention

```
User types "@Assistant help me ..." + Enter
    ↓
store.actions.sendMessage({ content, channel_id | recipient_id })
    ↓
POST /api/messages { content, channel_id | recipient_id }
    ↓
Backend:
  1. INSERT message
  2. util.ParseMentions(content) → ["Assistant"]
  3. Hub.Broadcast("message:created") → all WS subscribers
  4. AutoReplyService.CheckAndReply(mentions) →
       find agent by name where on_mention=true →
       call DashScope → insert reply message (parent_id=original) →
       Hub.Broadcast("message:created") again
```

### 3.3 WebSocket — receive

```
On desktop login success (in bootstrapDesktopApp):
    wsClient = createWSClient({
      url: env.MYTEAM_SERVER_URL,     // ws://localhost:8080/ws
      getToken: () => authStore.getState().token,
      onEvent: (event) => messagingStore.handleEvent(event),
    })
    wsClient.connect()

On event { type: "message:created", data: Message }:
    if data.channel_id === currentChannelId || data.recipient_id === currentPeerId:
        append to currentMessages
    else:
        // MVP: ignore silently (no unread badge)
```

### 3.4 @-mention autocomplete (client-side)

```
User keystroke in <textarea>
    ↓
mention-parser.detectTrigger(value, caretPos)
    ↓
  { triggering: boolean, query: string, start: number, end: number }
    ↓
If triggering:
  <MentionPicker
    candidates={workspace.agents.concat(workspace.members)}
    query={query}
    onSelect={(name) => replaceRange(start, end, "@" + name + " ")}
  />
```

### 3.5 Reconnect strategy

- WS `close` / `error` → exponential backoff: 1s, 2s, 4s, 8s, 16s, capped at 30s.
- On reconnect success, messaging store does **not** backfill missing messages. The sidebar stays as-is. User may re-click the conversation to trigger `loadMessages` refetch.
- `wsClient.status` is a subscribable value (`"connecting" | "connected" | "reconnecting" | "disconnected"`). A top-of-shell thin banner renders non-modal status.

### 3.6 Failure handling

| Failure | Behavior |
|---|---|
| REST load fails | `store.error` set; UI shows inline banner; user retries via re-select |
| `sendMessage` fails | textarea value preserved (not cleared); user retries with Enter |
| WS receives malformed event | `console.warn`; no throw |
| @-mentioned agent not found | backend no-op; user sees their own message, no reply (acceptable for MVP) |
| `on_mention` disabled on target agent | backend no-op; same as above |

## 4. UI components (desktop)

### 4.1 Sidebar

```
┌─────────────────────────────┐
│ Session                     │
│ ┌──────────┐ ┌───────────┐  │
│ │ + New DM │ │ + Channel │  │
│ └──────────┘ └───────────┘  │
├─────────────────────────────┤
│ CHANNELS                    │
│ # general                   │
│ # project-alpha             │
├─────────────────────────────┤
│ DIRECT MESSAGES             │
│ 🤖 Assistant       ●        │
│ 👤 Alice                    │
└─────────────────────────────┘
```

- `●` = agent `online_status === "online"`. Owners show no status dot in MVP.
- No unread counts. No sort by most recent (keep API-returned order).

### 4.2 Creation dialogs

- `new-dm-dialog.tsx`: search input + scrollable list of `agents ∪ members`. On click calls `store.startConversation(peerId, peerType)` which:
  - If a conversation with that peer already exists locally → select it.
  - Else POST `/api/messages` with an empty placeholder? **No** — the backend creates the conversation row on first message. MVP resolves this by immediately selecting the peer in the UI and letting `loadMessages` return `[]`. The conversation shows up in the sidebar only after the first message lands.

- `new-channel-dialog.tsx`: single name input. Submits `POST /api/channels { name, visibility: "private" }`. On success, appends to channel list and selects the new channel.

### 4.3 Message area

- `message-list.tsx`: renders `store.currentMessages`. Each message is a card with sender icon, name, timestamp, and pre-wrapped text. Auto-scroll to bottom when new message arrives **only if** the user is already near the bottom (within 80px); otherwise no auto-scroll.
- `message-input.tsx`: controlled `<textarea>`. Enter → send, Shift+Enter → newline. Disabled while `store.sending` is true. Placeholder: `Message # channel-name` or `Message Alice`.
- `mention-picker.tsx`: absolute-positioned dropdown above the textarea. Arrow keys move active, Enter selects, Esc closes. Displays up to 8 candidates, scrollable beyond. Icon differentiates agent vs owner. Insert format: `@Name ` (with trailing space).

### 4.4 Connection status banner

Top of `<DesktopShell>`, non-modal, 32px tall:

- `"connected"` → hidden
- `"reconnecting"` → yellow, copy: `连接中断,正在重连...`
- `"disconnected"` → red, copy: `连接失败,请检查网络`

### 4.5 Shell wiring

- `bootstrapDesktopApp()` in [lib/desktop-client.ts](apps/desktop/src/lib/desktop-client.ts): after auth + workspace load, create WS client, attach to messaging store, call `wsClient.connect()`.
- On logout: `wsClient.disconnect()`.
- [routes/session-route.tsx](apps/desktop/src/routes/session-route.tsx) becomes a ~60 LOC container wiring sidebar + message-list + message-input; no longer fetches directly.

## 5. Testing strategy

### 5.1 `packages/client-core/src/messaging/` (Vitest, pure unit)

| File | Coverage |
|---|---|
| `mention-parser.test.ts` | `detectTrigger("hello @", 7)` → triggering; `detectTrigger("@Ali", 4)` → query="Ali"; `detectTrigger("email foo@bar", 13)` → not triggering (word boundary); `filterCandidates` sorts exact-prefix matches first |
| `store-factory.test.ts` | `loadMessages` sets state on success / sets error on fail; `sendMessage` calls apiClient.postMessage and does NOT optimistically append; `handleEvent({type:"message:created"})` appends when channel_id matches currentChannelId; ignores when it doesn't |
| `ws-client.test.ts` | Connect opens socket; reconnect on close with exponential backoff capped at 30s; subscribe receives parsed events; token getter re-invoked on each reconnect |

### 5.2 Web regression

- Existing tests in `apps/web/features/messaging/components/message-list.test.tsx` and `.../hooks/use-typing-indicator.test.ts` must still pass.
- No new web tests. The thin wrapper is covered by existing integration.

### 5.3 Desktop components (Vitest + jsdom + Testing Library)

| File | Coverage |
|---|---|
| `message-input.test.tsx` | Enter calls `onSend(text)` and clears; Shift+Enter inserts newline; typing `@a` shows `<MentionPicker>`; selecting a candidate yields `@Alice ` in textarea; `sending=true` disables the textarea |
| `mention-picker.test.tsx` | filters by query; ↑/↓ cycles active; Enter calls onSelect; Esc calls onClose |
| `message-list.test.tsx` | renders owner vs agent icon correctly; empty state renders empty-pane; autoscroll to bottom when already near bottom |
| `new-dm-dialog.test.tsx` | lists agents + members; search filters by name; click invokes `onSelect(peerId, peerType)` |
| `new-channel-dialog.test.tsx` | submit calls `onCreate(name)`; blank name disables submit |

### 5.4 Backend

- No backend code change in this MVP.
- `make test` must stay green.

### 5.5 Manual acceptance (must pass before merge)

1. Login → sidebar shows `+ New DM` and `+ New Channel`.
2. Click `+ New DM` → picker lists the Assistant agent → create → auto-enter DM.
3. Type `@Assistant hello` → Enter → message appears immediately in the list.
4. Within a few seconds, Assistant's reply arrives via WS (no reload).
5. Open the same DM in a web tab on the same account: send from desktop → appears in web tab without reload; send from web → appears on desktop without reload.
6. Stop the backend → top banner turns red/yellow with reconnect copy; restart → banner clears and messaging resumes.

### 5.6 Signals

- `pnpm test` green across `packages/client-core` and `apps/desktop`.
- `pnpm typecheck` green across all workspaces.
- `make test` green (unchanged).
- Manual acceptance list 1–6 all pass.

## 6. Out-of-scope items flagged for later sub-projects

| Feature | Deferred to |
|---|---|
| Thread UI (panel, open-thread button, reply count badge) | Sub-project #5 |
| Privacy (public / semi-public / invite code) | #5 |
| Merge / split channels | #5 |
| Impersonation indicator + owner-possession | #5 |
| Auto-reply frequency limits and toggle per chat | #5 |
| File tab per channel/chat | #6 |
| Attachments in message input | #6 |
| Cross-owner visibility UI warnings | #5 |
| Unread counts, read receipts | #5 |
| Sub-reply-slots 30s tracking (system agent mediation) | #5 |
| WS backfill on reconnect | #5 |

## 7. Migration ordering (implementation hint for the writing-plans step)

Suggested order for the plan author:

1. Lift types + `ws-client` to `client-core` (no behavior change).
2. Lift + refactor `messaging/store` into `createMessagingStore` factory in `client-core`.
3. Update `apps/web/features/messaging/store.ts` to use the factory — web tests must stay green here.
4. Add `createMessagingStore` consumer in `apps/desktop/src/features/messaging/store.ts`, wired to `desktopApi` and a new `desktopWS` singleton created in `bootstrapDesktopApp`.
5. Build desktop `message-list`, `message-input`, `mention-picker`.
6. Build desktop `new-dm-dialog`, `new-channel-dialog`.
7. Replace the body of `session-route.tsx`.
8. Add connection status banner to `<DesktopShell>`.
9. Run full acceptance list manually.
