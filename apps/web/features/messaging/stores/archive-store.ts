"use client";

import { create } from "zustand";
import { toast } from "sonner";
import { api } from "@/shared/api";

// Per-user archive state for DM conversations. Channels archive
// workspace-wide via the channel store; this store only holds the local
// user's personal archive list, backed by dm_conversation_state on the
// server.
//
// We key by `${peer_type}:${peer_id}` so member/agent UUIDs don't
// collide. Sidebar uses isArchived() to split DMs into active/archived
// segments.
interface ArchiveState {
  archivedKeys: Set<string>;
  loading: boolean;
  fetch: () => Promise<void>;
  archive: (peerId: string, peerType: "member" | "agent") => Promise<void>;
  unarchive: (peerId: string, peerType: "member" | "agent") => Promise<void>;
  isArchived: (peerId: string, peerType: "member" | "agent") => boolean;
}

function key(peerId: string, peerType: "member" | "agent") {
  return `${peerType}:${peerId}`;
}

export const useConversationArchiveStore = create<ArchiveState>((set, get) => ({
  archivedKeys: new Set<string>(),
  loading: false,

  fetch: async () => {
    set({ loading: true });
    try {
      const res = await api.listArchivedDMPeers();
      const next = new Set<string>();
      for (const p of res.archived) {
        next.add(key(p.peer_id, p.peer_type));
      }
      set({ archivedKeys: next, loading: false });
    } catch {
      // Silent — archive is non-critical UX state. The sidebar stays on
      // last known snapshot rather than falling into an error banner.
      set({ loading: false });
    }
  },

  archive: async (peerId, peerType) => {
    try {
      await api.archiveDMConversation({ peer_id: peerId, peer_type: peerType, archived: true });
      const next = new Set(get().archivedKeys);
      next.add(key(peerId, peerType));
      set({ archivedKeys: next });
      toast.success("已归档聊天");
    } catch {
      toast.error("归档失败");
    }
  },

  unarchive: async (peerId, peerType) => {
    try {
      await api.archiveDMConversation({ peer_id: peerId, peer_type: peerType, archived: false });
      const next = new Set(get().archivedKeys);
      next.delete(key(peerId, peerType));
      set({ archivedKeys: next });
      toast.success("已恢复聊天");
    } catch {
      toast.error("恢复失败");
    }
  },

  isArchived: (peerId, peerType) => get().archivedKeys.has(key(peerId, peerType)),
}));
