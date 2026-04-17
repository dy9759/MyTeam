"use client";

import { create } from "zustand";
import { api } from "@/shared/api";
import type { Thread, ThreadContextItem } from "@/shared/types";

type ThreadState = {
  threadsByChannel: Record<string, Thread[]>;
  contextItemsByThread: Record<string, ThreadContextItem[]>;
  loading: Record<string, boolean>;
};

type ThreadActions = {
  loadThread: (threadID: string) => Promise<Thread>;
  loadContextItems: (threadID: string) => Promise<void>;
  addContextItem: (threadID: string, item: ThreadContextItem) => void;
  removeContextItem: (threadID: string, itemID: string) => void;
};

export const useThreadStore = create<ThreadState & ThreadActions>((set) => ({
  threadsByChannel: {},
  contextItemsByThread: {},
  loading: {},

  async loadThread(threadID) {
    const t = await api.getThread(threadID);
    // Push it into threadsByChannel under its channel_id.
    set((s) => {
      const list = s.threadsByChannel[t.channel_id] ?? [];
      const merged = list.filter((x) => x.id !== t.id).concat(t);
      return {
        threadsByChannel: { ...s.threadsByChannel, [t.channel_id]: merged },
      };
    });
    return t;
  },

  async loadContextItems(threadID) {
    set((s) => ({ loading: { ...s.loading, [threadID]: true } }));
    try {
      const items = await api.listThreadContextItems(threadID);
      set((s) => ({
        contextItemsByThread: { ...s.contextItemsByThread, [threadID]: items },
        loading: { ...s.loading, [threadID]: false },
      }));
    } catch (e) {
      set((s) => ({ loading: { ...s.loading, [threadID]: false } }));
      throw e;
    }
  },

  addContextItem(threadID, item) {
    set((s) => {
      const list = s.contextItemsByThread[threadID] ?? [];
      return {
        contextItemsByThread: {
          ...s.contextItemsByThread,
          [threadID]: [...list, item],
        },
      };
    });
  },

  removeContextItem(threadID, itemID) {
    set((s) => {
      const list = (s.contextItemsByThread[threadID] ?? []).filter(
        (i) => i.id !== itemID,
      );
      return {
        contextItemsByThread: {
          ...s.contextItemsByThread,
          [threadID]: list,
        },
      };
    });
  },
}));
