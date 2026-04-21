"use client";

import { create } from "zustand";
import type { Channel, ChannelMember } from "@/shared/types/messaging";
import { api } from "@/shared/api";
import { toast } from "sonner";

interface ChannelState {
  channels: Channel[];
  currentChannel: Channel | null;
  members: ChannelMember[];
  loading: boolean;
  fetch: () => Promise<void>;
  fetchChannel: (id: string) => Promise<void>;
  fetchMembers: (id: string) => Promise<void>;
  createChannel: (data: { name: string; description?: string }) => Promise<Channel | null>;
  joinChannel: (id: string) => Promise<void>;
  leaveChannel: (id: string) => Promise<void>;
  setCurrentChannel: (channel: Channel | null) => void;
  upgradeToChannel: (channelId: string, name: string) => Promise<void>;
  archiveChannel: (id: string) => Promise<void>;
  unarchiveChannel: (id: string) => Promise<void>;
}

export const useChannelStore = create<ChannelState>((set) => ({
  channels: [],
  currentChannel: null,
  members: [],
  loading: false,

  fetch: async () => {
    set({ loading: true });
    try {
      const res = await api.listChannels();
      set({ channels: res.channels, loading: false });
    } catch (err) {
      toast.error("Failed to load channels");
      set({ loading: false });
    }
  },

  fetchChannel: async (id) => {
    try {
      const channel = await api.getChannel(id);
      set({ currentChannel: channel });
    } catch (err) {
      toast.error("Failed to load channel");
    }
  },

  fetchMembers: async (id) => {
    try {
      const res = await api.getChannelMembers(id);
      set({ members: res.members });
    } catch (err) {
      toast.error("Failed to load members");
    }
  },

  createChannel: async (data) => {
    try {
      const channel = await api.createChannel(data);
      set((s) => ({ channels: [...s.channels, channel] }));
      return channel;
    } catch (err) {
      toast.error("Failed to create channel");
      return null;
    }
  },

  joinChannel: async (id) => {
    try {
      await api.joinChannel(id);
      toast.success("Joined channel");
    } catch (err) {
      toast.error("Failed to join channel");
    }
  },

  leaveChannel: async (id) => {
    try {
      await api.leaveChannel(id);
      toast.success("Left channel");
    } catch (err) {
      toast.error("Failed to leave channel");
    }
  },

  setCurrentChannel: (channel) => set({ currentChannel: channel }),

  upgradeToChannel: async (channelId, name) => {
    try {
      const channel = await api.upgradeToChannel(channelId, name);
      set((s) => ({
        channels: s.channels.map((c) => (c.id === channelId ? channel : c)),
        currentChannel: s.currentChannel?.id === channelId ? channel : s.currentChannel,
      }));
      toast.success("Upgraded to channel");
    } catch {
      toast.error("Failed to upgrade to channel");
    }
  },

  archiveChannel: async (id) => {
    try {
      await api.archiveChannel(id);
      const now = new Date().toISOString();
      set((s) => ({
        channels: s.channels.map((c) => (c.id === id ? { ...c, archived_at: now } : c)),
        currentChannel:
          s.currentChannel?.id === id ? { ...s.currentChannel, archived_at: now } : s.currentChannel,
      }));
      toast.success("已归档频道");
    } catch {
      toast.error("归档频道失败");
    }
  },

  unarchiveChannel: async (id) => {
    try {
      await api.unarchiveChannel(id);
      set((s) => ({
        channels: s.channels.map((c) => (c.id === id ? { ...c, archived_at: null } : c)),
        currentChannel:
          s.currentChannel?.id === id ? { ...s.currentChannel, archived_at: null } : s.currentChannel,
      }));
      toast.success("已恢复频道");
    } catch {
      toast.error("恢复频道失败");
    }
  },
}));
