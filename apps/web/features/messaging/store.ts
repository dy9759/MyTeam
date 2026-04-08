"use client";

import { create } from "zustand";
import type { Message, Conversation, Thread } from "@/shared/types/messaging";
import { api } from "@/shared/api";
import { toast } from "sonner";

interface MessagingState {
  conversations: Conversation[];
  currentMessages: Message[];
  loading: boolean;
  threads: Thread[];
  currentThreadMessages: Message[];
  ownerAgentConversations: Conversation[];
  fetch: () => Promise<void>;
  fetchMessages: (params: { channel_id?: string; recipient_id?: string; session_id?: string }) => Promise<void>;
  sendMessage: (params: { channel_id?: string; recipient_id?: string; recipient_type?: string; session_id?: string; content: string; file_id?: string; file_name?: string }) => Promise<void>;
  addMessage: (message: Message) => void;
  fetchThreads: (channelId: string) => Promise<void>;
  fetchThreadMessages: (threadId: string) => Promise<void>;
  sendThreadMessage: (threadId: string, content: string) => Promise<void>;
  fetchOwnerAgentConversations: () => Promise<void>;
}

export const useMessagingStore = create<MessagingState>((set, get) => ({
  conversations: [],
  currentMessages: [],
  loading: false,
  threads: [],
  currentThreadMessages: [],
  ownerAgentConversations: [],

  fetch: async () => {
    set({ loading: true });
    try {
      const res = await api.listConversations();
      set({ conversations: res.conversations, loading: false });
    } catch (err) {
      toast.error("Failed to load conversations");
      set({ loading: false });
    }
  },

  fetchMessages: async (params) => {
    set({ loading: true });
    try {
      const res = await api.listMessages({ ...params, limit: 100 });
      set({ currentMessages: res.messages, loading: false });
    } catch (err) {
      toast.error("Failed to load messages");
      set({ loading: false });
    }
  },

  sendMessage: async (params) => {
    try {
      const msg = await api.sendMessage(params);
      set((s) => ({ currentMessages: [...s.currentMessages, msg] }));
    } catch (err) {
      toast.error("Failed to send message");
    }
  },

  addMessage: (message) =>
    set((s) => ({
      currentMessages: [...s.currentMessages, message],
    })),

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
