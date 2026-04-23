import { create, type StoreApi, type UseBoundStore } from "zustand";
import type { Message, Conversation, Channel, WSMessage } from "./types";

export interface MessagingApiClient {
  listConversations(): Promise<{ conversations: Conversation[] }>;
  listChannels(): Promise<{ channels: Channel[] }>;
  listMessages(params: {
    channel_id?: string;
    recipient_id?: string;
    peer_type?: "member" | "agent";
    thread_id?: string;
    limit?: number;
    offset?: number;
  }): Promise<{ messages: Message[] }>;
  sendMessage(params: {
    channel_id?: string;
    recipient_id?: string;
    recipient_type?: "member" | "agent";
    thread_id?: string;
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
    peer_type?: "member" | "agent";
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
      if (evt.type === "message:read") {
        // Sender-side receipt: the recipient marked this message as
        // read. Flip its status so the UI can swap the sent tick for
        // a colored read tick.
        const payload = evt.payload as { message_id?: string } | undefined;
        const messageId = payload?.message_id;
        if (!messageId) return;
        set((s) => {
          let changed = false;
          const next = s.currentMessages.map((m) => {
            if (m.id !== messageId || m.status === "read") return m;
            changed = true;
            return { ...m, status: "read" as const };
          });
          return changed ? { currentMessages: next } : s;
        });
        return;
      }
      if (evt.type === "message:updated") {
        const payload = evt.payload as Message | { message?: Message } | undefined;
        const msg = payload && "message" in payload && payload.message
          ? payload.message
          : payload as Message | undefined;
        if (!msg?.id) return;
        set((state) => ({
          currentMessages: state.currentMessages.map((existing) =>
            existing.id === msg.id ? { ...existing, ...msg } : existing,
          ),
        }));
        return;
      }

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
