# Session MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the smallest end-to-end messaging loop on the desktop client — send, receive (WS), @-mention, agent auto-reply — while lifting shared messaging logic into `packages/client-core` so both web and desktop consume the same factory.

**Architecture:** Factory-based shared logic. `packages/client-core/src/messaging/` holds the Zustand store factory, WebSocket client, mention parser, and types. `apps/web/features/messaging/store.ts` and `apps/desktop/src/features/messaging/store.ts` are thin wrappers that inject their own API client + WS client. Desktop ships new UI components (thick-card style); web UI is unchanged.

**Tech Stack:** TypeScript, React 19, Zustand, Vitest, Testing Library, native `WebSocket` API. Backend (Go + Chi + pgvector) is untouched.

**Spec:** [docs/superpowers/specs/2026-04-15-session-mvp-design.md](../specs/2026-04-15-session-mvp-design.md)

**Working directory:** All commands assume repo root `/Users/chauncey2025/Documents/GitHub/MyTeam` (or the worktree `.claude/worktrees/stupefied-heyrovsky` which shares `.git`). Paths in this plan are relative to repo root.

---

## File structure

### Create

```
packages/client-core/src/messaging/
├── index.ts
├── types.ts
├── ws-client.ts
├── ws-client.test.ts
├── mention-parser.ts
├── mention-parser.test.ts
├── store-factory.ts
└── store-factory.test.ts

apps/desktop/src/features/messaging/
├── store.ts
├── components/
│   ├── mention-picker.tsx
│   ├── mention-picker.test.tsx
│   ├── message-list.tsx
│   ├── message-list.test.tsx
│   ├── message-input.tsx
│   ├── message-input.test.tsx
│   ├── new-dm-dialog.tsx
│   ├── new-dm-dialog.test.tsx
│   ├── new-channel-dialog.tsx
│   └── new-channel-dialog.test.tsx
└── index.ts

apps/desktop/src/components/
└── connection-status-banner.tsx  (new, ~40 LOC)

apps/desktop/vitest.config.ts  (modify to add jsdom)
```

### Modify

- `packages/client-core/src/desktop-api-client.ts` — add `sendMessage`, `createChannel`; tighten `listMessages` return to `{ messages: Message[] }`.
- `packages/client-core/src/index.ts` — export new messaging modules.
- `apps/web/shared/api/ws-client.ts` — re-export from client-core (shim).
- `apps/web/features/messaging/store.ts` — refactor to use `createMessagingStore` factory.
- `apps/desktop/src/lib/desktop-client.ts` — create `desktopWS` + `useDesktopMessagingStore`, connect in `bootstrapDesktopApp`.
- `apps/desktop/src/routes/session-route.tsx` — rewrite as ~60 LOC container.
- `apps/desktop/src/components/desktop-shell.tsx` — mount connection status banner.
- `apps/desktop/package.json` — add `@testing-library/react`, `@testing-library/user-event`, `@testing-library/jest-dom`, `jsdom` to devDependencies.

---

## Task 1: Messaging types module in client-core

**Files:**
- Create: `packages/client-core/src/messaging/types.ts`

Re-export the messaging types that already live in `apps/web/shared/types/messaging.ts` so downstream modules in client-core import from one canonical spot without reaching into `apps/web` directly.

- [ ] **Step 1: Create types re-export**

```ts
// packages/client-core/src/messaging/types.ts
export type {
  Message,
  Conversation,
  Channel,
  ChannelMember,
  Thread,
} from "../../../../apps/web/shared/types/messaging";

export type { WSMessage, WSEventType } from "../../../../apps/web/shared/types/events";
```

- [ ] **Step 2: Typecheck client-core**

Run: `cd packages/client-core && pnpm typecheck`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add packages/client-core/src/messaging/types.ts
git commit -m "feat(client-core): add messaging types re-export module"
```

---

## Task 2: Lift WSClient to client-core (with tests)

**Files:**
- Create: `packages/client-core/src/messaging/ws-client.ts`
- Create: `packages/client-core/src/messaging/ws-client.test.ts`

Lift `apps/web/shared/api/ws-client.ts` into client-core. Keep the behavior identical except: add a `status` subscription so UI can render the reconnect banner, and switch the reconnect strategy to exponential backoff capped at 30s (spec 3.5).

- [ ] **Step 1: Write failing test**

```ts
// packages/client-core/src/messaging/ws-client.test.ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WSClient } from "./ws-client";

class FakeWebSocket {
  static OPEN = 1;
  static CLOSED = 3;
  readyState = 0;
  url: string;
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: ((ev: unknown) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  sent: string[] = [];

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({});
  }
  simulateOpen() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.({});
  }
  simulateMessage(obj: unknown) {
    this.onmessage?.({ data: JSON.stringify(obj) });
  }

  static instances: FakeWebSocket[] = [];
  static reset() {
    FakeWebSocket.instances = [];
  }
}

describe("WSClient", () => {
  beforeEach(() => {
    FakeWebSocket.reset();
    vi.useFakeTimers();
    (globalThis as unknown as { WebSocket: typeof FakeWebSocket }).WebSocket =
      FakeWebSocket;
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("connects with token and workspace in query string", () => {
    const client = new WSClient("ws://api.test/ws", {
      getToken: () => "tok-1",
      getWorkspaceId: () => "ws-1",
    });
    client.connect();
    expect(FakeWebSocket.instances[0].url).toBe(
      "ws://api.test/ws?token=tok-1&workspace_id=ws-1",
    );
  });

  it("updates status through connecting → connected", () => {
    const client = new WSClient("ws://api.test/ws", {
      getToken: () => "t",
      getWorkspaceId: () => "w",
    });
    const seen: string[] = [];
    client.subscribeStatus((s) => seen.push(s));
    client.connect();
    FakeWebSocket.instances[0].simulateOpen();
    expect(seen).toEqual(["disconnected", "connecting", "connected"]);
  });

  it("emits parsed events to onEvent", () => {
    const events: unknown[] = [];
    const client = new WSClient("ws://api.test/ws", {
      getToken: () => "t",
      getWorkspaceId: () => "w",
      onEvent: (msg) => events.push(msg),
    });
    client.connect();
    FakeWebSocket.instances[0].simulateOpen();
    FakeWebSocket.instances[0].simulateMessage({
      type: "message:created",
      payload: { id: "m1" },
    });
    expect(events).toEqual([
      { type: "message:created", payload: { id: "m1" } },
    ]);
  });

  it("reconnects with exponential backoff capped at 30s", () => {
    const client = new WSClient("ws://api.test/ws", {
      getToken: () => "t",
      getWorkspaceId: () => "w",
    });
    client.connect();
    // delays: 1000, 2000, 4000, 8000, 16000, 30000, 30000...
    const expectedDelays = [1000, 2000, 4000, 8000, 16000, 30000, 30000];
    for (const delay of expectedDelays) {
      const socket =
        FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
      socket.simulateOpen();
      socket.close();
      vi.advanceTimersByTime(delay - 1);
      expect(FakeWebSocket.instances.length).toBe(
        expectedDelays.indexOf(delay) + 1,
      );
      vi.advanceTimersByTime(1);
      expect(FakeWebSocket.instances.length).toBe(
        expectedDelays.indexOf(delay) + 2,
      );
    }
  });

  it("re-reads token on reconnect", () => {
    let currentToken = "tok-A";
    const client = new WSClient("ws://api.test/ws", {
      getToken: () => currentToken,
      getWorkspaceId: () => "w",
    });
    client.connect();
    FakeWebSocket.instances[0].simulateOpen();
    currentToken = "tok-B";
    FakeWebSocket.instances[0].close();
    vi.advanceTimersByTime(1000);
    expect(FakeWebSocket.instances[1].url).toContain("token=tok-B");
  });

  it("disconnect cancels pending reconnect", () => {
    const client = new WSClient("ws://api.test/ws", {
      getToken: () => "t",
      getWorkspaceId: () => "w",
    });
    client.connect();
    FakeWebSocket.instances[0].simulateOpen();
    FakeWebSocket.instances[0].close();
    client.disconnect();
    vi.advanceTimersByTime(60000);
    expect(FakeWebSocket.instances.length).toBe(1);
  });
});
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `cd packages/client-core && pnpm test messaging/ws-client`
Expected: FAIL with `Cannot find module './ws-client'`.

- [ ] **Step 3: Implement WSClient**

```ts
// packages/client-core/src/messaging/ws-client.ts
import type { WSMessage, WSEventType } from "./types";

export type WSStatus =
  | "disconnected"
  | "connecting"
  | "connected"
  | "reconnecting";

type EventHandler = (payload: unknown) => void;
type StatusHandler = (status: WSStatus) => void;

export interface WSClientOptions {
  getToken: () => string | null;
  getWorkspaceId: () => string | null;
  onEvent?: (msg: WSMessage) => void;
  logger?: {
    debug?: (msg: string, ...args: unknown[]) => void;
    info?: (msg: string, ...args: unknown[]) => void;
    warn?: (msg: string, ...args: unknown[]) => void;
  };
}

const BACKOFF_STEPS = [1000, 2000, 4000, 8000, 16000, 30000];

export class WSClient {
  private ws: WebSocket | null = null;
  private baseUrl: string;
  private opts: WSClientOptions;
  private handlers = new Map<WSEventType, Set<EventHandler>>();
  private anyHandlers = new Set<(msg: WSMessage) => void>();
  private statusHandlers = new Set<StatusHandler>();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private retryIndex = 0;
  private intentionallyClosed = false;
  private statusValue: WSStatus = "disconnected";

  constructor(url: string, opts: WSClientOptions) {
    this.baseUrl = url;
    this.opts = opts;
  }

  get status(): WSStatus {
    return this.statusValue;
  }

  subscribeStatus(handler: StatusHandler): () => void {
    this.statusHandlers.add(handler);
    handler(this.statusValue);
    return () => this.statusHandlers.delete(handler);
  }

  private setStatus(next: WSStatus) {
    if (this.statusValue === next) return;
    this.statusValue = next;
    for (const h of this.statusHandlers) h(next);
  }

  connect() {
    this.intentionallyClosed = false;
    this.setStatus(this.retryIndex === 0 ? "connecting" : "reconnecting");

    const url = new URL(this.baseUrl);
    const token = this.opts.getToken();
    const workspaceId = this.opts.getWorkspaceId();
    if (token) url.searchParams.set("token", token);
    if (workspaceId) url.searchParams.set("workspace_id", workspaceId);

    this.ws = new WebSocket(url.toString());

    this.ws.onopen = () => {
      this.retryIndex = 0;
      this.setStatus("connected");
      this.opts.logger?.info?.("[ws] connected");
    };

    this.ws.onmessage = (event) => {
      let msg: WSMessage;
      try {
        msg = JSON.parse(event.data as string) as WSMessage;
      } catch {
        this.opts.logger?.warn?.("[ws] bad payload");
        return;
      }
      const handlers = this.handlers.get(msg.type);
      if (handlers) for (const h of handlers) h(msg.payload);
      for (const h of this.anyHandlers) h(msg);
      this.opts.onEvent?.(msg);
    };

    this.ws.onclose = () => {
      this.ws = null;
      if (this.intentionallyClosed) {
        this.setStatus("disconnected");
        return;
      }
      const delay =
        BACKOFF_STEPS[Math.min(this.retryIndex, BACKOFF_STEPS.length - 1)];
      this.retryIndex++;
      this.setStatus("reconnecting");
      this.reconnectTimer = setTimeout(() => this.connect(), delay);
    };

    this.ws.onerror = () => {
      // onclose will follow; no-op here
    };
  }

  disconnect() {
    this.intentionallyClosed = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      const ws = this.ws;
      this.ws = null;
      ws.onclose = null;
      ws.onerror = null;
      ws.close();
    }
    this.retryIndex = 0;
    this.setStatus("disconnected");
  }

  on(event: WSEventType, handler: EventHandler): () => void {
    if (!this.handlers.has(event)) this.handlers.set(event, new Set());
    this.handlers.get(event)!.add(handler);
    return () => this.handlers.get(event)?.delete(handler);
  }

  onAny(handler: (msg: WSMessage) => void): () => void {
    this.anyHandlers.add(handler);
    return () => this.anyHandlers.delete(handler);
  }

  send(message: WSMessage) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message));
    }
  }
}
```

- [ ] **Step 4: Run test — expect PASS**

Run: `cd packages/client-core && pnpm test messaging/ws-client`
Expected: 6 tests pass.

- [ ] **Step 5: Commit**

```bash
git add packages/client-core/src/messaging/ws-client.ts packages/client-core/src/messaging/ws-client.test.ts
git commit -m "feat(client-core): add WSClient with exponential backoff reconnect"
```

---

## Task 3: Mention parser in client-core (with tests)

**Files:**
- Create: `packages/client-core/src/messaging/mention-parser.ts`
- Create: `packages/client-core/src/messaging/mention-parser.test.ts`

- [ ] **Step 1: Write failing test**

```ts
// packages/client-core/src/messaging/mention-parser.test.ts
import { describe, expect, it } from "vitest";
import { detectTrigger, filterCandidates } from "./mention-parser";

describe("detectTrigger", () => {
  it("triggers on @ at start", () => {
    expect(detectTrigger("@", 1)).toEqual({
      triggering: true,
      query: "",
      start: 0,
      end: 1,
    });
  });

  it("triggers on @ after space", () => {
    expect(detectTrigger("hi @", 4)).toEqual({
      triggering: true,
      query: "",
      start: 3,
      end: 4,
    });
  });

  it("captures query after @", () => {
    expect(detectTrigger("@Ali", 4)).toEqual({
      triggering: true,
      query: "Ali",
      start: 0,
      end: 4,
    });
    expect(detectTrigger("hello @Alice ", 12)).toEqual({
      triggering: true,
      query: "Alice",
      start: 6,
      end: 12,
    });
  });

  it("does not trigger inside an email", () => {
    expect(detectTrigger("email foo@bar", 13).triggering).toBe(false);
  });

  it("stops triggering after space", () => {
    expect(detectTrigger("@Alice hi", 9).triggering).toBe(false);
  });

  it("does not trigger on empty text or mid-word", () => {
    expect(detectTrigger("", 0).triggering).toBe(false);
    expect(detectTrigger("foobar", 3).triggering).toBe(false);
  });
});

describe("filterCandidates", () => {
  const list = [
    { name: "Assistant", id: "a" },
    { name: "Alice", id: "b" },
    { name: "Bob", id: "c" },
    { name: "AliceBot", id: "d" },
  ];

  it("returns all when query empty", () => {
    expect(filterCandidates(list, "").map((c) => c.id)).toEqual([
      "a",
      "b",
      "c",
      "d",
    ]);
  });

  it("prefix-matches case-insensitive", () => {
    expect(filterCandidates(list, "al").map((c) => c.id)).toEqual(["b", "d"]);
    expect(filterCandidates(list, "AL").map((c) => c.id)).toEqual(["b", "d"]);
  });

  it("exact-prefix matches before substring matches", () => {
    expect(filterCandidates(list, "ot").map((c) => c.id)).toEqual([]);
    // 'bot' is substring of 'AliceBot' but not a prefix; with pure prefix it's filtered out
  });
});
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `cd packages/client-core && pnpm test messaging/mention-parser`
Expected: FAIL with module not found.

- [ ] **Step 3: Implement parser**

```ts
// packages/client-core/src/messaging/mention-parser.ts
export interface MentionTrigger {
  triggering: boolean;
  query: string;
  start: number; // position of @
  end: number;   // caret position
}

/**
 * Detect whether the caret position in `text` is inside a mention token.
 * A mention token starts with `@` that is preceded by start-of-text or
 * whitespace, and extends until the next whitespace.
 */
export function detectTrigger(text: string, caret: number): MentionTrigger {
  const empty: MentionTrigger = { triggering: false, query: "", start: -1, end: caret };
  if (caret <= 0 || caret > text.length) return empty;

  // Walk backwards from caret to find `@`, stopping at whitespace.
  let i = caret - 1;
  while (i >= 0) {
    const ch = text[i];
    if (ch === "@") break;
    if (/\s/.test(ch)) return empty;
    i--;
  }
  if (i < 0 || text[i] !== "@") return empty;

  // The char before `@` must be start-of-text or whitespace.
  if (i > 0 && !/\s/.test(text[i - 1])) return empty;

  return {
    triggering: true,
    query: text.slice(i + 1, caret),
    start: i,
    end: caret,
  };
}

/**
 * Case-insensitive prefix match. Preserves list order among matches.
 */
export function filterCandidates<T extends { name: string }>(
  list: T[],
  query: string,
): T[] {
  if (!query) return list;
  const q = query.toLowerCase();
  return list.filter((item) => item.name.toLowerCase().startsWith(q));
}
```

- [ ] **Step 4: Run test — expect PASS**

Run: `cd packages/client-core && pnpm test messaging/mention-parser`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add packages/client-core/src/messaging/mention-parser.ts packages/client-core/src/messaging/mention-parser.test.ts
git commit -m "feat(client-core): add mention parser (detectTrigger, filterCandidates)"
```

---

## Task 4: Extend DesktopApiClient with sendMessage + createChannel

**Files:**
- Modify: `packages/client-core/src/desktop-api-client.ts`

Current `listMessages` returns `{ messages: unknown[] }` — tighten it to `{ messages: Message[] }`. Add `sendMessage` and `createChannel` matching the web client.

- [ ] **Step 1: Add imports**

Edit `packages/client-core/src/desktop-api-client.ts`, find the existing import block at the top and extend the type imports:

```ts
import type {
  Agent,
  AgentRuntime,
  Channel,
  Conversation,
  FileIndex,
  Message,  // ← add
  MemberWithUser,
  Project,
  User,
  Workspace,
  WorkspaceAuditEntry,
  WorkspaceSnapshot,
} from "./host";
```

Check whether `host.ts` already re-exports `Message`; if not, add it (see Step 2). If already exported, skip Step 2.

- [ ] **Step 2: Ensure host.ts exports Message**

Check: `grep "^export type.*Message" packages/client-core/src/host.ts`

If no match, edit `packages/client-core/src/host.ts` and add:

```ts
export type { Message } from "../../../apps/web/shared/types/messaging";
```

- [ ] **Step 3: Tighten listMessages and add sendMessage + createChannel**

Find the `listMessages` method around line 187 and replace with:

```ts
  async listMessages(params: {
    channel_id?: string;
    recipient_id?: string;
    session_id?: string;
    limit?: number;
    offset?: number;
  }): Promise<{ messages: Message[] }> {
    const search = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) {
      if (value != null) {
        search.set(key, String(value));
      }
    }
    return this.request(`/api/messages?${search.toString()}`);
  }

  async sendMessage(params: {
    channel_id?: string;
    recipient_id?: string;
    recipient_type?: "member" | "agent";
    session_id?: string;
    content: string;
    content_type?: "text" | "json" | "file";
    file_id?: string;
    file_name?: string;
  }): Promise<Message> {
    return this.request<Message>("/api/messages", {
      method: "POST",
      body: JSON.stringify(params),
    });
  }

  async createChannel(params: {
    name: string;
    description?: string;
    visibility?: "public" | "private" | "invite_code";
  }): Promise<Channel> {
    return this.request<Channel>("/api/channels", {
      method: "POST",
      body: JSON.stringify(params),
    });
  }
```

- [ ] **Step 4: Typecheck**

Run: `cd packages/client-core && pnpm typecheck`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add packages/client-core/src/desktop-api-client.ts packages/client-core/src/host.ts
git commit -m "feat(client-core): add sendMessage + createChannel to DesktopApiClient"
```

---

## Task 5: createMessagingStore factory in client-core (with tests)

**Files:**
- Create: `packages/client-core/src/messaging/store-factory.ts`
- Create: `packages/client-core/src/messaging/store-factory.test.ts`

- [ ] **Step 1: Write failing test**

```ts
// packages/client-core/src/messaging/store-factory.test.ts
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMessagingStore } from "./store-factory";
import type { Message, Conversation, Channel } from "./types";

function makeMessage(over: Partial<Message> = {}): Message {
  return {
    id: "m1",
    workspace_id: "w1",
    sender_id: "u1",
    sender_type: "member",
    content: "hello",
    content_type: "text",
    status: "sent",
    created_at: "2026-04-15T00:00:00Z",
    updated_at: "2026-04-15T00:00:00Z",
    ...over,
  } as Message;
}

function makeApi() {
  return {
    listConversations: vi.fn(async () => ({ conversations: [] as Conversation[] })),
    listChannels: vi.fn(async () => ({ channels: [] as Channel[] })),
    listMessages: vi.fn(async () => ({ messages: [] as Message[] })),
    sendMessage: vi.fn(async (params: { content: string }) =>
      makeMessage({ content: params.content }),
    ),
    createChannel: vi.fn(async (params: { name: string }) => ({
      id: "c1",
      workspace_id: "w1",
      name: params.name,
      created_by: "u1",
      created_by_type: "member",
      created_at: "2026-04-15T00:00:00Z",
    }) as Channel),
  };
}

describe("createMessagingStore", () => {
  let api: ReturnType<typeof makeApi>;
  let onError: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    api = makeApi();
    onError = vi.fn();
  });

  it("loadConversations populates state", async () => {
    const convs: Conversation[] = [
      { peer_id: "p1", peer_type: "agent", peer_name: "A" } as Conversation,
    ];
    api.listConversations.mockResolvedValueOnce({ conversations: convs });
    const useStore = createMessagingStore({ apiClient: api, onError });
    await useStore.getState().loadConversations();
    expect(useStore.getState().conversations).toEqual(convs);
  });

  it("loadMessages replaces currentMessages and sets selection keys", async () => {
    const msgs = [makeMessage({ id: "a" }), makeMessage({ id: "b" })];
    api.listMessages.mockResolvedValueOnce({ messages: msgs });
    const useStore = createMessagingStore({ apiClient: api, onError });
    await useStore.getState().loadMessages({ channel_id: "c1" });
    expect(useStore.getState().currentMessages.map((m) => m.id)).toEqual([
      "a",
      "b",
    ]);
    expect(useStore.getState().currentChannelId).toBe("c1");
  });

  it("sendMessage appends returned message and does NOT optimistically append", async () => {
    const useStore = createMessagingStore({ apiClient: api, onError });
    await useStore.getState().loadMessages({ channel_id: "c1" });
    expect(useStore.getState().currentMessages).toHaveLength(0);
    const p = useStore.getState().sendMessage({
      channel_id: "c1",
      content: "hi",
    });
    // before resolution, no optimistic entry
    expect(useStore.getState().currentMessages).toHaveLength(0);
    await p;
    expect(useStore.getState().currentMessages).toHaveLength(1);
    expect(useStore.getState().currentMessages[0].content).toBe("hi");
  });

  it("handleEvent appends message when channel matches", () => {
    const useStore = createMessagingStore({ apiClient: api, onError });
    useStore.setState({ currentChannelId: "c1" });
    useStore.getState().handleEvent({
      type: "message:created",
      payload: makeMessage({ id: "evt", channel_id: "c1" }),
    });
    expect(useStore.getState().currentMessages).toHaveLength(1);
    expect(useStore.getState().currentMessages[0].id).toBe("evt");
  });

  it("handleEvent ignores message from other channel", () => {
    const useStore = createMessagingStore({ apiClient: api, onError });
    useStore.setState({ currentChannelId: "c1" });
    useStore.getState().handleEvent({
      type: "message:created",
      payload: makeMessage({ id: "evt", channel_id: "c2" }),
    });
    expect(useStore.getState().currentMessages).toHaveLength(0);
  });

  it("handleEvent matches DM by recipient_id", () => {
    const useStore = createMessagingStore({ apiClient: api, onError });
    useStore.setState({ currentPeerId: "p1" });
    useStore.getState().handleEvent({
      type: "message:created",
      payload: makeMessage({ id: "evt", sender_id: "p1" }),
    });
    expect(useStore.getState().currentMessages).toHaveLength(1);
  });

  it("load failures call onError and set loading=false", async () => {
    api.listMessages.mockRejectedValueOnce(new Error("boom"));
    const useStore = createMessagingStore({ apiClient: api, onError });
    await useStore.getState().loadMessages({ channel_id: "c1" });
    expect(onError).toHaveBeenCalledWith("Failed to load messages");
    expect(useStore.getState().loading).toBe(false);
  });
});
```

- [ ] **Step 2: Run — expect FAIL**

Run: `cd packages/client-core && pnpm test messaging/store-factory`
Expected: module not found.

- [ ] **Step 3: Implement store factory**

```ts
// packages/client-core/src/messaging/store-factory.ts
import { create, type StoreApi, type UseBoundStore } from "zustand";
import type { Message, Conversation, Channel, WSMessage } from "./types";

export interface MessagingApiClient {
  listConversations(): Promise<{ conversations: Conversation[] }>;
  listChannels(): Promise<{ channels: Channel[] }>;
  listMessages(params: {
    channel_id?: string;
    recipient_id?: string;
    session_id?: string;
    limit?: number;
    offset?: number;
  }): Promise<{ messages: Message[] }>;
  sendMessage(params: {
    channel_id?: string;
    recipient_id?: string;
    recipient_type?: "member" | "agent";
    session_id?: string;
    content: string;
    content_type?: "text" | "json" | "file";
    file_id?: string;
    file_name?: string;
  }): Promise<Message>;
  createChannel(params: {
    name: string;
    description?: string;
    visibility?: "public" | "private" | "invite_code";
  }): Promise<Channel>;
}

export interface MessagingStoreOptions {
  apiClient: MessagingApiClient;
  onError?: (message: string) => void;
}

export interface MessagingState {
  conversations: Conversation[];
  channels: Channel[];
  currentMessages: Message[];
  currentChannelId: string | null;
  currentPeerId: string | null;
  loading: boolean;
  sending: boolean;
  loadConversations: () => Promise<void>;
  loadChannels: () => Promise<void>;
  loadMessages: (params: {
    channel_id?: string;
    recipient_id?: string;
  }) => Promise<void>;
  sendMessage: (params: {
    channel_id?: string;
    recipient_id?: string;
    recipient_type?: "member" | "agent";
    content: string;
  }) => Promise<void>;
  createChannel: (params: {
    name: string;
    visibility?: "public" | "private" | "invite_code";
  }) => Promise<Channel>;
  handleEvent: (msg: WSMessage) => void;
  clear: () => void;
}

export type MessagingStore = UseBoundStore<StoreApi<MessagingState>>;

export function createMessagingStore(
  options: MessagingStoreOptions,
): MessagingStore {
  const { apiClient, onError } = options;
  const report = (msg: string) => onError?.(msg);

  return create<MessagingState>((set, get) => ({
    conversations: [],
    channels: [],
    currentMessages: [],
    currentChannelId: null,
    currentPeerId: null,
    loading: false,
    sending: false,

    loadConversations: async () => {
      try {
        const res = await apiClient.listConversations();
        set({ conversations: res.conversations });
      } catch {
        report("Failed to load conversations");
      }
    },

    loadChannels: async () => {
      try {
        const res = await apiClient.listChannels();
        set({ channels: res.channels });
      } catch {
        report("Failed to load channels");
      }
    },

    loadMessages: async (params) => {
      set({
        loading: true,
        currentChannelId: params.channel_id ?? null,
        currentPeerId: params.recipient_id ?? null,
      });
      try {
        const res = await apiClient.listMessages({ ...params, limit: 100 });
        set({ currentMessages: res.messages, loading: false });
      } catch {
        report("Failed to load messages");
        set({ loading: false });
      }
    },

    sendMessage: async (params) => {
      set({ sending: true });
      try {
        const msg = await apiClient.sendMessage({
          ...params,
          content_type: "text",
        });
        set((s) => ({
          currentMessages: [...s.currentMessages, msg],
          sending: false,
        }));
      } catch {
        report("Failed to send message");
        set({ sending: false });
      }
    },

    createChannel: async (params) => {
      const ch = await apiClient.createChannel({
        ...params,
        visibility: params.visibility ?? "private",
      });
      set((s) => ({ channels: [...s.channels, ch] }));
      return ch;
    },

    handleEvent: (evt) => {
      if (evt.type !== "message:created") return;
      const msg = evt.payload as Message;
      const { currentChannelId, currentPeerId } = get();
      const isCurrentChannel =
        currentChannelId && msg.channel_id === currentChannelId;
      const isCurrentDM =
        currentPeerId &&
        (msg.sender_id === currentPeerId || msg.recipient_id === currentPeerId);
      if (isCurrentChannel || isCurrentDM) {
        set((s) => {
          if (s.currentMessages.some((m) => m.id === msg.id)) return s;
          return { currentMessages: [...s.currentMessages, msg] };
        });
      }
    },

    clear: () =>
      set({
        conversations: [],
        channels: [],
        currentMessages: [],
        currentChannelId: null,
        currentPeerId: null,
        loading: false,
        sending: false,
      }),
  }));
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `cd packages/client-core && pnpm test messaging/store-factory`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add packages/client-core/src/messaging/store-factory.ts packages/client-core/src/messaging/store-factory.test.ts
git commit -m "feat(client-core): add createMessagingStore factory"
```

---

## Task 6: Messaging barrel index in client-core

**Files:**
- Create: `packages/client-core/src/messaging/index.ts`
- Modify: `packages/client-core/src/index.ts`

- [ ] **Step 1: Barrel index**

```ts
// packages/client-core/src/messaging/index.ts
export type * from "./types";
export {
  WSClient,
  type WSStatus,
  type WSClientOptions,
} from "./ws-client";
export {
  detectTrigger,
  filterCandidates,
  type MentionTrigger,
} from "./mention-parser";
export {
  createMessagingStore,
  type MessagingApiClient,
  type MessagingStoreOptions,
  type MessagingState,
  type MessagingStore,
} from "./store-factory";
```

- [ ] **Step 2: Re-export from top-level**

Append to `packages/client-core/src/index.ts`:

```ts
export * from "./messaging";
```

- [ ] **Step 3: Typecheck**

Run: `cd packages/client-core && pnpm typecheck`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add packages/client-core/src/messaging/index.ts packages/client-core/src/index.ts
git commit -m "feat(client-core): export messaging module barrel"
```

---

## Task 7: Shim web's ws-client.ts to re-export from client-core

**Files:**
- Modify: `apps/web/shared/api/ws-client.ts`

- [ ] **Step 1: Replace web ws-client body**

Replace the entire file contents with:

```ts
// Thin shim — real implementation lives in @myteam/client-core/messaging.
// Kept to preserve existing imports from apps/web/shared/api.
export { WSClient } from "@myteam/client-core";
export type { WSStatus, WSClientOptions } from "@myteam/client-core";
```

Note: the web WSClient constructor signature **changes** — old: `new WSClient(url, { logger? })` + `setAuth(token, wsId)` + auto-reconnect-at-3s; new: `new WSClient(url, { getToken, getWorkspaceId, onEvent?, logger? })`. We need to update every call site.

- [ ] **Step 2: Find and update web WSClient call sites**

Run: `grep -rn "new WSClient\|\.setAuth(" apps/web`
For each call site, update to new signature. Typical pattern:

```ts
// OLD
const ws = new WSClient(url);
ws.setAuth(token, workspaceId);
ws.connect();

// NEW
const ws = new WSClient(url, {
  getToken: () => token,
  getWorkspaceId: () => workspaceId,
});
ws.connect();
```

If a site reads `token`/`workspaceId` from a reactive source, wrap in a closure that reads latest value each call.

- [ ] **Step 3: Verify web typecheck**

Run: `pnpm --filter @multica/web typecheck`
Expected: no errors.

- [ ] **Step 4: Verify web tests**

Run: `pnpm --filter @multica/web test`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add apps/web
git commit -m "refactor(web): re-export WSClient from client-core"
```

---

## Task 8: Refactor web messaging store to use factory

**Files:**
- Modify: `apps/web/features/messaging/store.ts`

- [ ] **Step 1: Replace store body**

```ts
// apps/web/features/messaging/store.ts
"use client";

import { toast } from "sonner";
import { api } from "@/shared/api";
import {
  createMessagingStore,
  type MessagingApiClient,
} from "@myteam/client-core";

// Adapter: map the existing web `api` singleton to the factory's contract.
const apiAdapter: MessagingApiClient = {
  listConversations: () => api.listConversations(),
  listChannels: () => api.listChannels(),
  listMessages: (params) => api.listMessages(params),
  sendMessage: (params) => api.sendMessage(params),
  createChannel: (params) => api.createChannel(params),
};

export const useMessagingStore = createMessagingStore({
  apiClient: apiAdapter,
  onError: (msg) => toast.error(msg),
});
```

**Thread-related methods** (`fetchThreads`, `sendThreadMessage`, `fetchThreadMessages`, `fetchOwnerAgentConversations`) are NOT in the MVP factory. They are used by `thread-panel.tsx` and `agents-tab.tsx`. If any web component imports these from `useMessagingStore`, split them into a sibling store:

- [ ] **Step 2: Find thread store usages**

Run: `grep -rn "fetchThreads\|sendThreadMessage\|fetchThreadMessages\|fetchOwnerAgentConversations\|currentThreadMessages\|threads:" apps/web`

- [ ] **Step 3: Extract thread store if needed**

If step 2 returns matches, create `apps/web/features/messaging/thread-store.ts`:

```ts
"use client";
import { create } from "zustand";
import { toast } from "sonner";
import type { Thread, Message, Conversation } from "@myteam/client-core";
import { api } from "@/shared/api";

interface ThreadState {
  threads: Thread[];
  currentThreadMessages: Message[];
  ownerAgentConversations: Conversation[];
  fetchThreads: (channelId: string) => Promise<void>;
  fetchThreadMessages: (threadId: string) => Promise<void>;
  sendThreadMessage: (threadId: string, content: string) => Promise<void>;
  fetchOwnerAgentConversations: () => Promise<void>;
}

export const useThreadStore = create<ThreadState>((set) => ({
  threads: [],
  currentThreadMessages: [],
  ownerAgentConversations: [],
  fetchThreads: async (channelId) => {
    try {
      const threads = await api.listThreads(channelId);
      set({ threads });
    } catch {
      toast.error("Failed to load threads");
    }
  },
  fetchThreadMessages: async (threadId) => {
    try {
      const res = await api.getThreadMessages(threadId);
      set({ currentThreadMessages: res.messages });
    } catch {
      toast.error("Failed to load thread messages");
    }
  },
  sendThreadMessage: async (threadId, content) => {
    try {
      const msg = await api.sendThreadMessage(threadId, content);
      set((s) => ({ currentThreadMessages: [...s.currentThreadMessages, msg] }));
    } catch {
      toast.error("Failed to send thread message");
    }
  },
  fetchOwnerAgentConversations: async () => {
    try {
      const res = await api.listOwnerAgentConversations();
      set({ ownerAgentConversations: res.conversations });
    } catch {
      toast.error("Failed to load agent conversations");
    }
  },
}));
```

Update every call site from step 2 to import `useThreadStore` for thread/owner-agent methods and `useMessagingStore` only for the MVP-factory methods.

- [ ] **Step 4: Verify web typecheck + tests**

Run:
```
pnpm --filter @multica/web typecheck
pnpm --filter @multica/web test
```
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add apps/web/features/messaging
git commit -m "refactor(web): use createMessagingStore factory + split thread store"
```

---

## Task 9: Configure desktop vitest for jsdom + component testing

**Files:**
- Modify: `apps/desktop/package.json`
- Modify: `apps/desktop/vitest.config.ts`

- [ ] **Step 1: Add test deps**

Run from repo root:
```bash
pnpm --filter @myteam/desktop add -D \
  @testing-library/react@^16.3.2 \
  @testing-library/user-event@^14.6.1 \
  @testing-library/jest-dom@^6.9.1 \
  jsdom@^29.0.1
```

- [ ] **Step 2: Update vitest config**

Replace `apps/desktop/vitest.config.ts` with:

```ts
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./vitest.setup.ts"],
  },
});
```

- [ ] **Step 3: Create setup file**

Create `apps/desktop/vitest.setup.ts`:

```ts
import "@testing-library/jest-dom/vitest";
```

- [ ] **Step 4: Verify**

Run: `pnpm --filter @myteam/desktop test`
Expected: existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add apps/desktop/package.json apps/desktop/vitest.config.ts apps/desktop/vitest.setup.ts pnpm-lock.yaml
git commit -m "chore(desktop): configure vitest for jsdom + Testing Library"
```

---

## Task 10: Wire WS + messaging store into desktop bootstrap

**Files:**
- Modify: `apps/desktop/src/lib/desktop-client.ts`
- Create: `apps/desktop/src/features/messaging/store.ts`
- Create: `apps/desktop/src/features/messaging/index.ts`

- [ ] **Step 1: Create desktop messaging store**

```ts
// apps/desktop/src/features/messaging/store.ts
import { createMessagingStore, type MessagingApiClient } from "@myteam/client-core";
import { desktopApi } from "@/lib/desktop-client";

const apiAdapter: MessagingApiClient = {
  listConversations: () => desktopApi.listConversations(),
  listChannels: () => desktopApi.listChannels(),
  listMessages: (params) => desktopApi.listMessages(params),
  sendMessage: (params) => desktopApi.sendMessage(params),
  createChannel: (params) => desktopApi.createChannel(params),
};

export const useDesktopMessagingStore = createMessagingStore({
  apiClient: apiAdapter,
  onError: (msg) => {
    // MVP: surface via browser alert. Replace with toast component later.
    // eslint-disable-next-line no-console
    console.error("[messaging]", msg);
  },
});
```

- [ ] **Step 2: Barrel index**

```ts
// apps/desktop/src/features/messaging/index.ts
export { useDesktopMessagingStore } from "./store";
```

- [ ] **Step 3: Wire WS into bootstrap**

Edit `apps/desktop/src/lib/desktop-client.ts`. Add to the imports:

```ts
import {
  DesktopApiClient,
  createAuthStore,
  WORKSPACE_STORAGE_KEY,
  createWorkspaceStore,
  WSClient,          // ← add
  type NativeSecrets,
  type SessionStorageLike,
  type WSStatus,      // ← add
} from "@myteam/client-core";
```

Add a ref for WS status + the WS client singleton near the bottom, above `bootstrapDesktopApp`:

```ts
import { create } from "zustand";

export const useWSStatusStore = create<{ status: WSStatus }>(() => ({
  status: "disconnected",
}));

let wsClient: WSClient | null = null;

function ensureWSClient() {
  if (wsClient) return wsClient;
  const wsUrl =
    (import.meta.env.VITE_WS_URL as string | undefined) ??
    apiBaseUrl.replace(/^http/, "ws") + "/ws";
  wsClient = new WSClient(wsUrl, {
    getToken: () => {
      const state = useDesktopAuthStore.getState();
      return (state as unknown as { token?: string | null }).token ?? null;
    },
    getWorkspaceId: () => useDesktopWorkspaceStore.getState().workspace?.id ?? null,
    onEvent: (msg) => {
      // Delegate to messaging store only for messaging events
      if (msg.type === "message:created") {
        void import("@/features/messaging").then(({ useDesktopMessagingStore }) => {
          useDesktopMessagingStore.getState().handleEvent(msg);
        });
      }
    },
  });
  wsClient.subscribeStatus((status) => useWSStatusStore.setState({ status }));
  return wsClient;
}

export function disconnectWS() {
  wsClient?.disconnect();
  wsClient = null;
}
```

Find `bootstrapDesktopApp` and append at the end of the `if (useDesktopAuthStore.getState().user)` block:

```ts
    ensureWSClient().connect();
```

Update `onUnauthorized` inside `new DesktopApiClient(...)` to also disconnect WS:

```ts
export const desktopApi = new DesktopApiClient(apiBaseUrl, {
  async onUnauthorized() {
    disconnectWS();  // ← add
    await window.myteam.auth.clearSession();
    useDesktopWorkspaceStore.getState().clearWorkspace();
    useDesktopAuthStore.setState({
      user: null,
      isLoading: false,
    });
  },
});
```

Note: the `disconnectWS` function is defined later in the file, so move the `new DesktopApiClient(...)` block BELOW the `ensureWSClient`/`disconnectWS` definitions if needed, OR declare `disconnectWS` with `function` (hoisted) earlier.

- [ ] **Step 4: Expose token on the auth store for sync WS access**

Current `packages/client-core/src/auth-store.ts` holds the token only in async `NativeSecrets.getToken()`. The WS client needs a sync read. Add a `token` field to the auth store state.

Edit `packages/client-core/src/auth-store.ts`:

```ts
// In AuthStoreState interface — add:
  token: string | null;

// In the store body — add to the initial state (near `user: null`):
  token: null,

// In `setSession(token, user)` — after `api.setToken(token)`:
  set({ token, user, isLoading: false });  // include token in the set call

// In `initialize` after `const token = await secrets.getToken()` and `api.setToken(token)`:
  set({ token, user, isLoading: false });

// On `onUnauthorized` / logout set:
  set({ token: null, user: null, isLoading: false });
```

Read the current body of [auth-store.ts](packages/client-core/src/auth-store.ts) before editing — keep existing setState patterns, just thread `token` through.

Update the desktop WS plumbing to read from store state (not `as unknown as`):

```ts
getToken: () => useDesktopAuthStore.getState().token,
```

Update `useDesktopAuthStore` consumers if any now rely on the store having no `token` (unlikely; it's additive).

- [ ] **Step 5: Typecheck desktop**

Run: `pnpm --filter @myteam/desktop typecheck`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add apps/desktop/src/features/messaging apps/desktop/src/lib/desktop-client.ts packages/client-core/src/auth-store.ts
git commit -m "feat(desktop): wire WSClient + messaging store into bootstrap"
```

---

## Task 11: Desktop mention-picker component (with test)

**Files:**
- Create: `apps/desktop/src/features/messaging/components/mention-picker.tsx`
- Create: `apps/desktop/src/features/messaging/components/mention-picker.test.tsx`

- [ ] **Step 1: Write failing test**

```tsx
// apps/desktop/src/features/messaging/components/mention-picker.test.tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MentionPicker } from "./mention-picker";

const candidates = [
  { id: "a1", name: "Assistant", kind: "agent" as const },
  { id: "u1", name: "Alice", kind: "owner" as const },
  { id: "a2", name: "Bob", kind: "agent" as const },
];

describe("MentionPicker", () => {
  it("filters candidates by query (prefix, case-insensitive)", () => {
    render(
      <MentionPicker
        candidates={candidates}
        query="al"
        onSelect={() => {}}
        onClose={() => {}}
      />,
    );
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.queryByText("Bob")).not.toBeInTheDocument();
  });

  it("Enter selects active candidate", async () => {
    const onSelect = vi.fn();
    render(
      <MentionPicker
        candidates={candidates}
        query=""
        onSelect={onSelect}
        onClose={() => {}}
      />,
    );
    await userEvent.keyboard("{Enter}");
    expect(onSelect).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Assistant" }),
    );
  });

  it("Escape calls onClose", async () => {
    const onClose = vi.fn();
    render(
      <MentionPicker
        candidates={candidates}
        query=""
        onSelect={() => {}}
        onClose={onClose}
      />,
    );
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();
  });

  it("arrow down moves active, enter picks next item", async () => {
    const onSelect = vi.fn();
    render(
      <MentionPicker
        candidates={candidates}
        query=""
        onSelect={onSelect}
        onClose={() => {}}
      />,
    );
    await userEvent.keyboard("{ArrowDown}{Enter}");
    expect(onSelect).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Alice" }),
    );
  });
});
```

- [ ] **Step 2: Run — expect FAIL**

Run: `pnpm --filter @myteam/desktop test mention-picker`
Expected: module not found.

- [ ] **Step 3: Implement component**

```tsx
// apps/desktop/src/features/messaging/components/mention-picker.tsx
import { useEffect, useState } from "react";
import { filterCandidates } from "@myteam/client-core";

export interface MentionCandidate {
  id: string;
  name: string;
  kind: "agent" | "owner";
}

interface Props {
  candidates: MentionCandidate[];
  query: string;
  onSelect: (candidate: MentionCandidate) => void;
  onClose: () => void;
}

export function MentionPicker({ candidates, query, onSelect, onClose }: Props) {
  const filtered = filterCandidates(candidates, query).slice(0, 8);
  const [active, setActive] = useState(0);

  useEffect(() => {
    setActive(0);
  }, [query]);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActive((i) => (filtered.length === 0 ? 0 : (i + 1) % filtered.length));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setActive((i) =>
          filtered.length === 0 ? 0 : (i - 1 + filtered.length) % filtered.length,
        );
      } else if (e.key === "Enter") {
        if (filtered[active]) {
          e.preventDefault();
          onSelect(filtered[active]);
        }
      } else if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener("keydown", handler, true);
    return () => window.removeEventListener("keydown", handler, true);
  }, [filtered, active, onSelect, onClose]);

  if (filtered.length === 0) return null;

  return (
    <div className="absolute bottom-full left-0 right-0 z-20 mb-2 overflow-hidden rounded-2xl border border-border/70 bg-card/95 shadow-lg backdrop-blur">
      {filtered.map((c, i) => (
        <button
          key={c.id}
          type="button"
          onMouseDown={(e) => {
            e.preventDefault();
            onSelect(c);
          }}
          onMouseEnter={() => setActive(i)}
          className={`flex w-full items-center gap-3 px-4 py-2 text-left text-sm ${
            i === active
              ? "bg-primary text-primary-foreground"
              : "text-foreground hover:bg-white/5"
          }`}
        >
          <span>{c.kind === "agent" ? "🤖" : "👤"}</span>
          <span className="truncate">{c.name}</span>
        </button>
      ))}
    </div>
  );
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `pnpm --filter @myteam/desktop test mention-picker`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add apps/desktop/src/features/messaging/components/mention-picker.tsx apps/desktop/src/features/messaging/components/mention-picker.test.tsx
git commit -m "feat(desktop): add MentionPicker component"
```

---

## Task 12: Desktop message-input component (with test)

**Files:**
- Create: `apps/desktop/src/features/messaging/components/message-input.tsx`
- Create: `apps/desktop/src/features/messaging/components/message-input.test.tsx`

- [ ] **Step 1: Write failing test**

```tsx
// apps/desktop/src/features/messaging/components/message-input.test.tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MessageInput } from "./message-input";

const candidates = [
  { id: "a1", name: "Assistant", kind: "agent" as const },
  { id: "u1", name: "Alice", kind: "owner" as const },
];

describe("MessageInput", () => {
  it("Enter calls onSend and clears", async () => {
    const onSend = vi.fn().mockResolvedValue(undefined);
    render(
      <MessageInput
        placeholder="Say hi"
        candidates={candidates}
        onSend={onSend}
      />,
    );
    const input = screen.getByPlaceholderText("Say hi");
    await userEvent.type(input, "hello{Enter}");
    expect(onSend).toHaveBeenCalledWith("hello");
    expect((input as HTMLTextAreaElement).value).toBe("");
  });

  it("Shift+Enter inserts newline instead of sending", async () => {
    const onSend = vi.fn();
    render(
      <MessageInput
        placeholder="Say hi"
        candidates={candidates}
        onSend={onSend}
      />,
    );
    const input = screen.getByPlaceholderText("Say hi") as HTMLTextAreaElement;
    await userEvent.type(input, "line1{Shift>}{Enter}{/Shift}line2");
    expect(onSend).not.toHaveBeenCalled();
    expect(input.value).toBe("line1\nline2");
  });

  it("typing @ shows mention picker; selecting inserts @Name space", async () => {
    render(
      <MessageInput
        placeholder="Say hi"
        candidates={candidates}
        onSend={vi.fn()}
      />,
    );
    const input = screen.getByPlaceholderText("Say hi") as HTMLTextAreaElement;
    await userEvent.type(input, "@");
    expect(screen.getByText("Assistant")).toBeInTheDocument();
    await userEvent.keyboard("{Enter}");
    expect(input.value).toBe("@Assistant ");
  });

  it("disabled while sending is true", () => {
    render(
      <MessageInput
        placeholder="Say hi"
        candidates={candidates}
        onSend={vi.fn()}
        sending
      />,
    );
    expect(screen.getByPlaceholderText("Say hi")).toBeDisabled();
  });
});
```

- [ ] **Step 2: Run — expect FAIL**

Run: `pnpm --filter @myteam/desktop test message-input`
Expected: module not found.

- [ ] **Step 3: Implement component**

```tsx
// apps/desktop/src/features/messaging/components/message-input.tsx
import { useRef, useState } from "react";
import { detectTrigger } from "@myteam/client-core";
import { MentionPicker, type MentionCandidate } from "./mention-picker";

interface Props {
  placeholder: string;
  candidates: MentionCandidate[];
  onSend: (text: string) => Promise<void> | void;
  sending?: boolean;
}

export function MessageInput({ placeholder, candidates, onSend, sending }: Props) {
  const [value, setValue] = useState("");
  const [caret, setCaret] = useState(0);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerQuery, setPickerQuery] = useState("");
  const [pickerRange, setPickerRange] = useState<{ start: number; end: number }>({
    start: 0,
    end: 0,
  });
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const recomputeTrigger = (text: string, pos: number) => {
    const t = detectTrigger(text, pos);
    setPickerOpen(t.triggering);
    setPickerQuery(t.query);
    if (t.triggering) setPickerRange({ start: t.start, end: t.end });
  };

  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const next = e.target.value;
    const pos = e.target.selectionStart ?? next.length;
    setValue(next);
    setCaret(pos);
    recomputeTrigger(next, pos);
  };

  const handleSelect = (e: React.SyntheticEvent<HTMLTextAreaElement>) => {
    const target = e.currentTarget;
    const pos = target.selectionStart ?? value.length;
    setCaret(pos);
    recomputeTrigger(value, pos);
  };

  const submit = async () => {
    const text = value.trim();
    if (!text || sending) return;
    await onSend(text);
    setValue("");
    setCaret(0);
    setPickerOpen(false);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // Picker consumes Arrow/Enter/Esc via its own window listener when open.
    if (pickerOpen && ["ArrowUp", "ArrowDown", "Enter", "Escape"].includes(e.key)) {
      return;
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void submit();
    }
  };

  const insertMention = (c: MentionCandidate) => {
    const before = value.slice(0, pickerRange.start);
    const after = value.slice(pickerRange.end);
    const inserted = `@${c.name} `;
    const next = before + inserted + after;
    setValue(next);
    setPickerOpen(false);
    // move caret to after the insertion
    const newPos = (before + inserted).length;
    requestAnimationFrame(() => {
      textareaRef.current?.focus();
      textareaRef.current?.setSelectionRange(newPos, newPos);
      setCaret(newPos);
    });
  };

  return (
    <div className="relative">
      {pickerOpen ? (
        <MentionPicker
          candidates={candidates}
          query={pickerQuery}
          onSelect={insertMention}
          onClose={() => setPickerOpen(false)}
        />
      ) : null}
      <textarea
        ref={textareaRef}
        value={value}
        onChange={handleChange}
        onSelect={handleSelect}
        onKeyDown={handleKeyDown}
        disabled={sending}
        placeholder={placeholder}
        rows={3}
        className="w-full resize-none rounded-3xl border border-border/70 bg-background/70 px-4 py-3 text-sm text-foreground outline-none focus:border-primary disabled:opacity-50"
      />
    </div>
  );
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `pnpm --filter @myteam/desktop test message-input`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add apps/desktop/src/features/messaging/components/message-input.tsx apps/desktop/src/features/messaging/components/message-input.test.tsx
git commit -m "feat(desktop): add MessageInput with @ mention support"
```

---

## Task 13: Desktop message-list component (with test)

**Files:**
- Create: `apps/desktop/src/features/messaging/components/message-list.tsx`
- Create: `apps/desktop/src/features/messaging/components/message-list.test.tsx`

- [ ] **Step 1: Write failing test**

```tsx
// apps/desktop/src/features/messaging/components/message-list.test.tsx
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Message } from "@myteam/client-core";
import { MessageList } from "./message-list";

function m(over: Partial<Message>): Message {
  return {
    id: "x",
    workspace_id: "w",
    sender_id: "s",
    sender_type: "member",
    content: "hi",
    content_type: "text",
    status: "sent",
    created_at: "2026-04-15T10:00:00Z",
    updated_at: "2026-04-15T10:00:00Z",
    ...over,
  } as Message;
}

describe("MessageList", () => {
  it("renders owner and agent icons differently", () => {
    render(
      <MessageList
        messages={[
          m({ id: "1", sender_type: "member", content: "hello" }),
          m({ id: "2", sender_type: "agent", content: "world" }),
        ]}
        resolveName={(id, type) => (type === "agent" ? "Bot" : "Alice")}
      />,
    );
    expect(screen.getByText("hello")).toBeInTheDocument();
    expect(screen.getByText("world")).toBeInTheDocument();
    // icon text differs
    expect(screen.getAllByText("👤").length).toBe(1);
    expect(screen.getAllByText("🤖").length).toBe(1);
  });

  it("renders empty state when no messages", () => {
    render(<MessageList messages={[]} resolveName={() => "X"} />);
    expect(screen.getByText(/no messages/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run — expect FAIL**

Run: `pnpm --filter @myteam/desktop test message-list`
Expected: module not found.

- [ ] **Step 3: Implement component**

```tsx
// apps/desktop/src/features/messaging/components/message-list.tsx
import { useEffect, useRef } from "react";
import type { Message } from "@myteam/client-core";

interface Props {
  messages: Message[];
  resolveName: (senderId: string, senderType: "member" | "agent") => string;
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

export function MessageList({ messages, resolveName }: Props) {
  const bottomRef = useRef<HTMLDivElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    if (nearBottom) {
      bottomRef.current?.scrollIntoView({ behavior: "smooth" });
    }
  }, [messages.length]);

  if (messages.length === 0) {
    return (
      <div className="flex h-full items-center justify-center rounded-3xl border border-dashed border-border/70 bg-background/50 px-4 py-10 text-center text-sm text-muted-foreground">
        No messages yet. Say something to get started.
      </div>
    );
  }

  return (
    <div ref={scrollRef} className="flex max-h-full flex-col gap-3 overflow-y-auto">
      {messages.map((msg) => (
        <article
          key={msg.id}
          className="rounded-3xl border border-border/70 bg-background/70 px-4 py-3"
        >
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span>{msg.sender_type === "agent" ? "🤖" : "👤"}</span>
            <span className="font-medium text-foreground">
              {resolveName(msg.sender_id, msg.sender_type)}
            </span>
            <span>{formatTime(msg.created_at)}</span>
          </div>
          <p className="mt-2 whitespace-pre-wrap text-sm leading-6 text-foreground">
            {msg.content}
          </p>
        </article>
      ))}
      <div ref={bottomRef} />
    </div>
  );
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `pnpm --filter @myteam/desktop test message-list`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add apps/desktop/src/features/messaging/components/message-list.tsx apps/desktop/src/features/messaging/components/message-list.test.tsx
git commit -m "feat(desktop): add MessageList component"
```

---

## Task 14: Desktop new-dm-dialog (with test)

**Files:**
- Create: `apps/desktop/src/features/messaging/components/new-dm-dialog.tsx`
- Create: `apps/desktop/src/features/messaging/components/new-dm-dialog.test.tsx`

- [ ] **Step 1: Write failing test**

```tsx
// apps/desktop/src/features/messaging/components/new-dm-dialog.test.tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NewDMDialog } from "./new-dm-dialog";

const items = [
  { id: "a1", name: "Assistant", kind: "agent" as const },
  { id: "u1", name: "Alice", kind: "owner" as const },
];

describe("NewDMDialog", () => {
  it("filters by name", async () => {
    render(
      <NewDMDialog candidates={items} onSelect={() => {}} onClose={() => {}} />,
    );
    await userEvent.type(screen.getByPlaceholderText(/search/i), "ass");
    expect(screen.getByText("Assistant")).toBeInTheDocument();
    expect(screen.queryByText("Alice")).not.toBeInTheDocument();
  });

  it("click invokes onSelect with peer id + type", async () => {
    const onSelect = vi.fn();
    render(
      <NewDMDialog candidates={items} onSelect={onSelect} onClose={() => {}} />,
    );
    await userEvent.click(screen.getByText("Assistant"));
    expect(onSelect).toHaveBeenCalledWith("a1", "agent");
  });
});
```

- [ ] **Step 2: Run — expect FAIL**

Run: `pnpm --filter @myteam/desktop test new-dm-dialog`
Expected: module not found.

- [ ] **Step 3: Implement component**

```tsx
// apps/desktop/src/features/messaging/components/new-dm-dialog.tsx
import { useState } from "react";
import { filterCandidates } from "@myteam/client-core";

export interface DMCandidate {
  id: string;
  name: string;
  kind: "agent" | "owner";
}

interface Props {
  candidates: DMCandidate[];
  onSelect: (peerId: string, peerType: "agent" | "member") => void;
  onClose: () => void;
}

export function NewDMDialog({ candidates, onSelect, onClose }: Props) {
  const [query, setQuery] = useState("");
  const filtered = filterCandidates(candidates, query);
  return (
    <div
      className="fixed inset-0 z-30 flex items-center justify-center bg-black/40"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-[28px] border border-border/70 bg-card/95 p-6 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-medium text-foreground">Start a new DM</h2>
        <input
          autoFocus
          placeholder="Search agents and members"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="mt-4 w-full rounded-2xl border border-border/70 bg-background/70 px-4 py-2 text-sm outline-none focus:border-primary"
        />
        <div className="mt-4 max-h-80 overflow-y-auto">
          {filtered.length === 0 ? (
            <p className="px-4 py-8 text-center text-sm text-muted-foreground">
              No matches.
            </p>
          ) : (
            filtered.map((c) => (
              <button
                key={c.id}
                type="button"
                onClick={() =>
                  onSelect(c.id, c.kind === "agent" ? "agent" : "member")
                }
                className="flex w-full items-center gap-3 rounded-2xl px-4 py-3 text-left text-sm hover:bg-white/5"
              >
                <span>{c.kind === "agent" ? "🤖" : "👤"}</span>
                <span className="truncate text-foreground">{c.name}</span>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `pnpm --filter @myteam/desktop test new-dm-dialog`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add apps/desktop/src/features/messaging/components/new-dm-dialog.tsx apps/desktop/src/features/messaging/components/new-dm-dialog.test.tsx
git commit -m "feat(desktop): add NewDMDialog component"
```

---

## Task 15: Desktop new-channel-dialog (with test)

**Files:**
- Create: `apps/desktop/src/features/messaging/components/new-channel-dialog.tsx`
- Create: `apps/desktop/src/features/messaging/components/new-channel-dialog.test.tsx`

- [ ] **Step 1: Write failing test**

```tsx
// apps/desktop/src/features/messaging/components/new-channel-dialog.test.tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NewChannelDialog } from "./new-channel-dialog";

describe("NewChannelDialog", () => {
  it("submit button disabled when name blank", () => {
    render(<NewChannelDialog onCreate={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByRole("button", { name: /create/i })).toBeDisabled();
  });

  it("submit calls onCreate with name", async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined);
    render(<NewChannelDialog onCreate={onCreate} onClose={vi.fn()} />);
    await userEvent.type(screen.getByPlaceholderText(/channel name/i), "general");
    await userEvent.click(screen.getByRole("button", { name: /create/i }));
    expect(onCreate).toHaveBeenCalledWith("general");
  });
});
```

- [ ] **Step 2: Run — expect FAIL**

Run: `pnpm --filter @myteam/desktop test new-channel-dialog`
Expected: module not found.

- [ ] **Step 3: Implement component**

```tsx
// apps/desktop/src/features/messaging/components/new-channel-dialog.tsx
import { useState } from "react";

interface Props {
  onCreate: (name: string) => Promise<void> | void;
  onClose: () => void;
}

export function NewChannelDialog({ onCreate, onClose }: Props) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (!name.trim() || busy) return;
    setBusy(true);
    try {
      await onCreate(name.trim());
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-30 flex items-center justify-center bg-black/40"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-[28px] border border-border/70 bg-card/95 p-6 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-medium text-foreground">New channel</h2>
        <input
          autoFocus
          placeholder="Channel name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-4 w-full rounded-2xl border border-border/70 bg-background/70 px-4 py-2 text-sm outline-none focus:border-primary"
        />
        <p className="mt-2 text-xs text-muted-foreground">
          Channel will be private. You can invite members later.
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded-2xl border border-border/70 px-4 py-2 text-sm text-foreground"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={!name.trim() || busy}
            className="rounded-2xl bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-50"
          >
            Create
          </button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `pnpm --filter @myteam/desktop test new-channel-dialog`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add apps/desktop/src/features/messaging/components/new-channel-dialog.tsx apps/desktop/src/features/messaging/components/new-channel-dialog.test.tsx
git commit -m "feat(desktop): add NewChannelDialog component"
```

---

## Task 16: Rewrite session-route to wire everything together

**Files:**
- Modify: `apps/desktop/src/routes/session-route.tsx`
- Modify: `apps/desktop/src/features/messaging/index.ts`

- [ ] **Step 1: Extend messaging barrel**

```ts
// apps/desktop/src/features/messaging/index.ts
export { useDesktopMessagingStore } from "./store";
export { MessageInput } from "./components/message-input";
export { MessageList } from "./components/message-list";
export { MentionPicker, type MentionCandidate } from "./components/mention-picker";
export { NewDMDialog, type DMCandidate } from "./components/new-dm-dialog";
export { NewChannelDialog } from "./components/new-channel-dialog";
```

- [ ] **Step 2: Rewrite session-route**

Replace the entire contents of `apps/desktop/src/routes/session-route.tsx`:

```tsx
import { useEffect, useMemo, useState } from "react";
import type { Channel, Conversation } from "@myteam/client-core";
import { RouteShell } from "@/components/route-shell";
import { useDesktopWorkspaceStore } from "@/lib/desktop-client";
import {
  MessageInput,
  MessageList,
  NewChannelDialog,
  NewDMDialog,
  useDesktopMessagingStore,
  type DMCandidate,
} from "@/features/messaging";

type Selection =
  | { kind: "channel"; channel: Channel }
  | { kind: "dm"; conversation: Conversation };

export function SessionRoute() {
  const agents = useDesktopWorkspaceStore((s) => s.agents);
  const members = useDesktopWorkspaceStore((s) => s.members);

  const {
    currentMessages,
    sending,
    loadConversations,
    loadChannels,
    loadMessages,
    sendMessage,
    createChannel,
    channels,
    conversations,
  } = useDesktopMessagingStore();

  const [selection, setSelection] = useState<Selection | null>(null);
  const [showNewDM, setShowNewDM] = useState(false);
  const [showNewChannel, setShowNewChannel] = useState(false);

  const mentionCandidates = useMemo(() => {
    const all = [
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
    return all;
  }, [agents, members]);

  const dmCandidates: DMCandidate[] = mentionCandidates;

  useEffect(() => {
    void loadConversations();
    void loadChannels();
  }, [loadConversations, loadChannels]);

  useEffect(() => {
    if (!selection) return;
    if (selection.kind === "channel") {
      void loadMessages({ channel_id: selection.channel.id });
    } else {
      void loadMessages({ recipient_id: selection.conversation.peer_id });
    }
  }, [selection, loadMessages]);

  const resolveName = (senderId: string, senderType: "member" | "agent") => {
    if (senderType === "agent") {
      return agents.find((a) => a.id === senderId)?.name ?? "Agent";
    }
    return members.find((m) => m.id === senderId)?.name ?? "User";
  };

  const placeholder =
    selection?.kind === "channel"
      ? `Message # ${selection.channel.name}`
      : selection?.kind === "dm"
      ? `Message ${selection.conversation.peer_name ?? selection.conversation.peer_id}`
      : "Select a conversation";

  const handleSend = async (text: string) => {
    if (!selection) return;
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

  return (
    <RouteShell
      eyebrow="Session"
      title="Collaborate with agents and teammates"
      description="Send messages, mention agents, and watch replies stream in real time."
    >
      <div className="grid min-h-[70vh] gap-4 xl:grid-cols-[260px_1fr]">
        <section className="flex flex-col rounded-[28px] border border-border/70 bg-card/85 p-4">
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => setShowNewDM(true)}
              className="flex-1 rounded-2xl bg-primary px-3 py-2 text-xs font-medium text-primary-foreground"
            >
              + New DM
            </button>
            <button
              type="button"
              onClick={() => setShowNewChannel(true)}
              className="flex-1 rounded-2xl border border-border/70 px-3 py-2 text-xs font-medium text-foreground"
            >
              + Channel
            </button>
          </div>
          <p className="mt-5 px-2 text-xs uppercase tracking-[0.2em] text-muted-foreground">
            Channels
          </p>
          <div className="mt-2 space-y-1">
            {channels.map((channel) => (
              <SidebarItem
                key={channel.id}
                active={selection?.kind === "channel" && selection.channel.id === channel.id}
                onClick={() => setSelection({ kind: "channel", channel })}
                title={`# ${channel.name}`}
              />
            ))}
          </div>
          <p className="mt-5 px-2 text-xs uppercase tracking-[0.2em] text-muted-foreground">
            Direct Messages
          </p>
          <div className="mt-2 space-y-1">
            {conversations.map((conversation) => (
              <SidebarItem
                key={`${conversation.peer_type}:${conversation.peer_id}`}
                active={
                  selection?.kind === "dm" &&
                  selection.conversation.peer_id === conversation.peer_id
                }
                onClick={() => setSelection({ kind: "dm", conversation })}
                title={conversation.peer_name ?? conversation.peer_id}
                subtitle={conversation.peer_type}
              />
            ))}
          </div>
        </section>

        <section className="flex flex-col rounded-[28px] border border-border/70 bg-card/85 p-4">
          <div className="border-b border-border/70 px-2 pb-4">
            <h3 className="text-xl font-medium text-foreground">
              {selection?.kind === "channel"
                ? `# ${selection.channel.name}`
                : selection?.kind === "dm"
                ? selection.conversation.peer_name ?? selection.conversation.peer_id
                : "Select a conversation"}
            </h3>
          </div>
          <div className="flex-1 overflow-hidden py-4">
            {selection ? (
              <MessageList messages={currentMessages} resolveName={resolveName} />
            ) : (
              <EmptyPane message="Pick a channel or DM on the left, or start a new one." />
            )}
          </div>
          {selection ? (
            <MessageInput
              placeholder={placeholder}
              candidates={mentionCandidates}
              onSend={handleSend}
              sending={sending}
            />
          ) : null}
        </section>
      </div>

      {showNewDM ? (
        <NewDMDialog
          candidates={dmCandidates}
          onSelect={(peerId, peerType) => {
            setShowNewDM(false);
            const peer = mentionCandidates.find((c) => c.id === peerId);
            setSelection({
              kind: "dm",
              conversation: {
                peer_id: peerId,
                peer_type: peerType,
                peer_name: peer?.name,
                unread_count: 0,
              } as Conversation,
            });
          }}
          onClose={() => setShowNewDM(false)}
        />
      ) : null}
      {showNewChannel ? (
        <NewChannelDialog
          onCreate={async (name) => {
            const ch = await createChannel({ name, visibility: "private" });
            setShowNewChannel(false);
            setSelection({ kind: "channel", channel: ch });
          }}
          onClose={() => setShowNewChannel(false)}
        />
      ) : null}
    </RouteShell>
  );
}

function SidebarItem({
  active,
  onClick,
  title,
  subtitle,
}: {
  active: boolean;
  onClick: () => void;
  title: string;
  subtitle?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`w-full rounded-2xl px-3 py-2 text-left transition ${
        active ? "bg-primary text-primary-foreground" : "hover:bg-white/5"
      }`}
    >
      <p className="truncate text-sm font-medium">{title}</p>
      {subtitle ? (
        <p
          className={`mt-1 text-xs ${
            active ? "text-primary-foreground/70" : "text-muted-foreground"
          }`}
        >
          {subtitle}
        </p>
      ) : null}
    </button>
  );
}

function EmptyPane({ message }: { message: string }) {
  return (
    <div className="flex h-full items-center justify-center rounded-3xl border border-dashed border-border/70 bg-background/50 px-4 py-10 text-center text-sm text-muted-foreground">
      {message}
    </div>
  );
}
```

- [ ] **Step 3: Typecheck**

Run: `pnpm --filter @myteam/desktop typecheck`
Expected: no errors.

Note: `members` and `agents` are both present on the workspace store (verified). Both are typed as `SessionMember[]` / `SessionAgent[]`. If either's `id` or `name` fields differ from what the code uses, fix the field accessor — don't remove members.

- [ ] **Step 4: Commit**

```bash
git add apps/desktop/src/routes/session-route.tsx apps/desktop/src/features/messaging/index.ts
git commit -m "feat(desktop): rewrite SessionRoute with new messaging components"
```

---

## Task 17: Connection status banner in desktop shell

**Files:**
- Create: `apps/desktop/src/components/connection-status-banner.tsx`
- Modify: `apps/desktop/src/components/desktop-shell.tsx`

- [ ] **Step 1: Create banner component**

```tsx
// apps/desktop/src/components/connection-status-banner.tsx
import { useWSStatusStore } from "@/lib/desktop-client";

export function ConnectionStatusBanner() {
  const status = useWSStatusStore((s) => s.status);
  if (status === "connected") return null;

  const text =
    status === "reconnecting"
      ? "连接中断,正在重连..."
      : status === "connecting"
      ? "正在连接服务器..."
      : "连接失败,请检查网络";

  const color =
    status === "reconnecting" || status === "connecting"
      ? "bg-amber-500/20 text-amber-200"
      : "bg-destructive/20 text-destructive";

  return (
    <div
      className={`flex h-8 items-center justify-center text-xs font-medium ${color}`}
    >
      {text}
    </div>
  );
}
```

- [ ] **Step 2: Mount in desktop-shell**

Read the current file first:

```bash
cat apps/desktop/src/components/desktop-shell.tsx
```

Add an import at the top:

```tsx
import { ConnectionStatusBanner } from "./connection-status-banner";
```

Then add `<ConnectionStatusBanner />` just inside the outer layout container, above the `<Outlet />` (or equivalent) element. Example if the file has structure `<div><Nav/><main><Outlet/></main></div>`:

```tsx
<div>
  <ConnectionStatusBanner />
  <Nav/>
  <main><Outlet/></main>
</div>
```

- [ ] **Step 3: Typecheck**

Run: `pnpm --filter @myteam/desktop typecheck`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add apps/desktop/src/components/connection-status-banner.tsx apps/desktop/src/components/desktop-shell.tsx
git commit -m "feat(desktop): add WebSocket connection status banner"
```

---

## Task 18: Final verification

- [ ] **Step 1: Full typecheck**

Run from repo root:
```bash
pnpm --filter @myteam/client-core typecheck
pnpm --filter @multica/web typecheck
pnpm --filter @myteam/desktop typecheck
```
Expected: all three green.

- [ ] **Step 2: Full unit tests**

```bash
pnpm --filter @myteam/client-core test
pnpm --filter @multica/web test
pnpm --filter @myteam/desktop test
```
Expected: all three green.

- [ ] **Step 3: Go tests unchanged**

```bash
cd server && go test ./...
```
Expected: green (no new tests, no regression).

- [ ] **Step 4: Manual acceptance**

Ensure backend + web + desktop are all running:
```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam
make start &             # backend :8080 + web :3000
pnpm --filter @myteam/desktop dev &   # desktop Electron
```

Walk through the 6-point acceptance list from the spec:

1. Login to the desktop app. Sidebar shows `+ New DM` and `+ Channel` at the top.
2. Click `+ New DM` → the picker lists your `Assistant` agent. Click it → DM opens (empty state).
3. Type `@Assistant hello`. MentionPicker should appear after typing `@`; select Assistant; press Enter. Your message appears immediately.
4. Within a few seconds the Assistant's reply appears in the list without reloading.
5. Open the same DM in a browser tab (web client, same account). Send a message from desktop → it appears in the web tab without refresh. Send from web → it appears on desktop without refresh.
6. Stop the backend (`make stop` or kill server). The desktop top banner turns amber (`reconnecting`). Wait, restart backend; banner disappears within ~30s.

All 6 must pass.

- [ ] **Step 5: Final commit / PR**

If anything needed tweaking during manual acceptance, commit fixes. Then create a PR:

```bash
git log --oneline main..HEAD
gh pr create --title "feat: Session MVP (desktop messaging + client-core lift)" \
  --body "$(cat <<'EOF'
## Summary
- Lift messaging logic (store factory, WSClient, mention parser, types) into \`packages/client-core\`
- Rewrite desktop Session route on top of shared factory with new UI: send, receive, @ mention, DM/channel creation
- Add WebSocket connection status banner on desktop shell
- Web behavior unchanged; store now uses the factory

## Test plan
- [x] packages/client-core tests (mention-parser, ws-client, store-factory)
- [x] apps/desktop component tests (message-input, message-list, mention-picker, dialogs)
- [x] Manual acceptance list 1–6 per spec
- [x] Web tests unchanged and green

Spec: docs/superpowers/specs/2026-04-15-session-mvp-design.md
Plan: docs/superpowers/plans/2026-04-15-session-mvp.md

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Notes for the implementer

- **Token plumbing gotcha (Task 10 Step 4):** If `auth-store` does not expose the JWT synchronously, you must extend it. This is the highest-risk step in the plan because it may touch files outside the immediate desktop tree. Budget extra attention there.
- **Web store adapter (Task 8):** Tread carefully around thread/owner-agent methods. If splitting into `useThreadStore` breaks more than a couple of files, consider extending the factory with optional thread methods instead — but only after confirming the call-site count is high.
- **Session route TypeScript (Task 16 Step 3):** `members` on the workspace store may not exist yet. Falling back to `agents` only is acceptable for MVP; real `members` arrive with Account sub-project.
- **Always commit per task.** Every task ends with a commit; never batch across tasks.
- **Run tests per task.** Don't skip the "run — expect PASS" step; it catches regressions early.
