"use client";

import { create } from "zustand";
import type { Session } from "@/shared/types/messaging";
import type { Message } from "@/shared/types/messaging";
import { api } from "@/shared/api";
import { toast } from "sonner";

interface SessionState {
  sessions: Session[];
  currentSession: Session | null;
  sessionMessages: Message[];
  loading: boolean;
  fetch: () => Promise<void>;
  fetchSession: (id: string) => Promise<void>;
  fetchSessionMessages: (id: string) => Promise<void>;
  createSession: (data: { title: string; issue_id?: string; max_turns?: number; context?: any; participants?: Array<{id: string; type: string}> }) => Promise<Session | null>;
  joinSession: (id: string) => Promise<void>;
  updateSession: (id: string, data: { status?: string; context?: any }) => Promise<void>;
  setSessions: (sessions: Session[]) => void;
  setCurrentSession: (session: Session | null) => void;
}

export const useSessionStore = create<SessionState>((set) => ({
  sessions: [],
  currentSession: null,
  sessionMessages: [],
  loading: false,

  fetch: async () => {
    set({ loading: true });
    try {
      const res = await api.listSessions();
      set({ sessions: res.sessions, loading: false });
    } catch (err) {
      toast.error("Failed to load sessions");
      set({ loading: false });
    }
  },

  fetchSession: async (id) => {
    try {
      const session = await api.getSession(id);
      set({ currentSession: session });
    } catch (err) {
      toast.error("Failed to load session");
    }
  },

  fetchSessionMessages: async (id) => {
    try {
      const res = await api.getSessionMessages(id);
      set({ sessionMessages: res.messages });
    } catch (err) {
      toast.error("Failed to load session messages");
    }
  },

  createSession: async (data) => {
    try {
      const session = await api.createSession(data);
      set((s) => ({ sessions: [...s.sessions, session] }));
      return session;
    } catch (err) {
      toast.error("Failed to create session");
      return null;
    }
  },

  joinSession: async (id) => {
    try {
      await api.joinSession(id);
      toast.success("Joined session");
    } catch (err) {
      toast.error("Failed to join session");
    }
  },

  updateSession: async (id, data) => {
    try {
      const updated = await api.updateSession(id, data);
      set((s) => ({
        sessions: s.sessions.map((sess) => sess.id === id ? { ...sess, ...updated } : sess),
        currentSession: s.currentSession?.id === id ? { ...s.currentSession, ...updated } : s.currentSession,
      }));
    } catch (err) {
      toast.error("Failed to update session");
    }
  },

  setSessions: (sessions) => set({ sessions }),
  setCurrentSession: (session) => set({ currentSession: session }),
}));
