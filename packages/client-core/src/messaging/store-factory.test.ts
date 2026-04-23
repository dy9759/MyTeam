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

  it("handleEvent merges streamed message updates", () => {
    const useStore = createMessagingStore({ apiClient: api, onError });
    useStore.setState({
      currentMessages: [
        makeMessage({
          id: "m1",
          content: "hello",
          metadata: { streaming: true },
        }),
      ],
    });

    useStore.getState().handleEvent({
      type: "message:updated",
      payload: {
        message: makeMessage({
          id: "m1",
          content: "hello world",
          metadata: { streaming: false, source: "local_agent" },
          updated_at: "2026-04-15T00:00:01Z",
        }),
      },
    });

    expect(useStore.getState().currentMessages[0]).toMatchObject({
      id: "m1",
      content: "hello world",
      metadata: { streaming: false, source: "local_agent" },
      updated_at: "2026-04-15T00:00:01Z",
    });
  });

  it("load failures call onError and set loading=false", async () => {
    api.listMessages.mockRejectedValueOnce(new Error("boom"));
    const useStore = createMessagingStore({ apiClient: api, onError });
    await useStore.getState().loadMessages({ channel_id: "c1" });
    expect(onError).toHaveBeenCalledWith("Failed to load messages");
    expect(useStore.getState().loading).toBe(false);
  });
});
