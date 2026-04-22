"use client";

import { useEffect, useRef, useState, useCallback, useMemo } from "react";
import { useSearchParams } from "next/navigation";
import { useMessagingStore } from "@/features/messaging/store";
import { useChannelStore } from "@/features/channels/store";
import { useInboxStore } from "@/features/inbox";
import { useWorkspaceStore } from "@/features/workspace";
import { useAuthStore } from "@/features/auth";
import { MessageList } from "@/features/messaging/components/message-list";
import { MessageInput } from "@/features/messaging/components/message-input";
import { ThreadPanel } from "@/features/messaging/components/thread-panel";
import { MeetingPanel } from "@/features/messaging/components/meeting-panel";
import { FileViewerPanel } from "@/features/messaging/components/file-viewer-panel";
import { ChannelSearchPanel } from "@/features/messaging/components/channel-search-panel";
import { ChannelFilesPanel } from "@/features/messaging/components/channel-files-panel";
import { useFileViewerStore } from "@/features/messaging/stores/file-viewer-store";
import { GenerateProjectButton } from "@/features/messaging/components/generate-project-button";
import { PromoteToChannelButton } from "@/features/messaging/components/promote-to-channel-button";
import { InviteChannelMemberDialog } from "@/features/messaging/components/invite-channel-member-dialog";
import { useConversationArchiveStore } from "@/features/messaging/stores/archive-store";
import { useTypingIndicator } from "@/features/messaging/hooks/use-typing-indicator";
import { useWSEvent } from "@/features/realtime";
import { useMessageSelectionStore } from "@/features/messaging/stores/selection-store";
import { api } from "@/shared/api";
import type { Conversation } from "@/shared/types/messaging";
import type { Message } from "@/shared/types/messaging";
import type { Channel } from "@/shared/types/messaging";
import type { InboxItem, ChannelMeeting } from "@/shared/types";
import { toast } from "sonner";
import {
  Hash,
  MessageCircle,
  Bell,
  BellDot,
  CheckSquare,
  Inbox,
  Users,
  Archive,
  Search,
  FolderOpen,
  Plus,
  UserPlus,
  Mic,
} from "lucide-react";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type SelectionType = "dm" | "channel";

function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "刚刚";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  return `${days}d`;
}

// ---------------------------------------------------------------------------
// Left Sidebar
// ---------------------------------------------------------------------------

function SessionSidebar({
  conversations,
  channels,
  selectedId,
  selectedType,
  onSelectDm,
  onSelectChannel,
  onCreateChannel,
  searchQuery,
  onSearchChange,
  isDMArchived,
  onArchiveDM,
  onUnarchiveDM,
  onArchiveChannel,
  onUnarchiveChannel,
}: {
  conversations: Conversation[];
  channels: Channel[];
  selectedId: string | null;
  selectedType: SelectionType;
  onSelectDm: (conv: Conversation) => void;
  onSelectChannel: (ch: Channel) => void;
  onCreateChannel: () => void;
  searchQuery: string;
  onSearchChange: (q: string) => void;
  isDMArchived: (peerId: string, peerType: "member" | "agent") => boolean;
  onArchiveDM: (peerId: string, peerType: "member" | "agent") => void;
  onUnarchiveDM: (peerId: string, peerType: "member" | "agent") => void;
  onArchiveChannel: (id: string) => void;
  onUnarchiveChannel: (id: string) => void;
}) {
  const [showArchived, setShowArchived] = useState(false);
  const q = searchQuery.toLowerCase();

  const activeConvs: Conversation[] = [];
  const archivedConvs: Conversation[] = [];
  for (const c of conversations) {
    if (q && !(c.peer_name || "").toLowerCase().includes(q)) continue;
    if (isDMArchived(c.peer_id, (c.peer_type as "member" | "agent") ?? "member")) {
      archivedConvs.push(c);
    } else {
      activeConvs.push(c);
    }
  }

  const activeChannels: Channel[] = [];
  const archivedChannels: Channel[] = [];
  for (const ch of channels) {
    if (q && !ch.name.toLowerCase().includes(q)) continue;
    if (ch.archived_at) {
      archivedChannels.push(ch);
    } else {
      activeChannels.push(ch);
    }
  }

  const archivedCount = archivedConvs.length + archivedChannels.length;

  return (
    <div className="w-60 shrink-0 border-r border-border flex flex-col bg-card h-full">
      {/* Search + Create */}
      <div className="px-3 pt-3 pb-2 space-y-2">
        <div className="relative">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
          <input
            value={searchQuery}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder="搜索会话..."
            className="w-full pl-8 pr-3 py-1.5 bg-secondary border border-border rounded-[6px] text-[13px] text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </div>
        <button
          onClick={onCreateChannel}
          className="w-full flex items-center justify-center gap-1.5 px-3 py-1.5 bg-primary text-primary-foreground rounded-[6px] text-[12px] font-medium hover:opacity-90 transition-opacity"
        >
          <Plus className="h-3.5 w-3.5" />
          创建频道
        </button>
      </div>

      {/* Chats section */}
      <div className="px-3 pt-2 pb-1">
        <h3 className="text-xs font-semibold text-muted-foreground/60 uppercase tracking-wider flex items-center gap-1.5">
          <MessageCircle className="h-3.5 w-3.5" />
          聊天
        </h3>
      </div>
      <div className="flex-none overflow-auto max-h-[40%] px-1">
        {activeConvs.length === 0 ? (
          <div className="px-3 py-2 text-xs text-muted-foreground">
            {q ? "无匹配" : "暂无对话"}
          </div>
        ) : (
          activeConvs.map((conv) => {
            const peerType = (conv.peer_type as "member" | "agent") ?? "member";
            const isSelected = selectedType === "dm" && selectedId === conv.peer_id;
            return (
              <div
                key={conv.peer_id}
                className={`group relative flex items-start gap-1 px-3 py-1.5 rounded-md transition-colors ${
                  isSelected
                    ? "bg-muted text-foreground"
                    : "hover:bg-accent text-secondary-foreground"
                }`}
              >
                <button onClick={() => onSelectDm(conv)} className="flex-1 min-w-0 text-left text-sm">
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate">
                      {conv.peer_name || conv.peer_id.slice(0, 12)}
                    </span>
                    {(conv.unread_count ?? 0) > 0 && (
                      <span className="shrink-0 text-[10px] bg-primary text-primary-foreground rounded-full px-1.5 py-0.5 leading-none">
                        {conv.unread_count}
                      </span>
                    )}
                  </div>
                  {conv.last_message && (
                    <div className="text-xs text-muted-foreground truncate mt-0.5">
                      {conv.last_message.content}
                    </div>
                  )}
                </button>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    onArchiveDM(conv.peer_id, peerType);
                  }}
                  className="opacity-0 group-hover:opacity-100 shrink-0 p-1 rounded hover:bg-background text-muted-foreground hover:text-foreground transition-opacity"
                  title="归档"
                  aria-label="归档"
                >
                  <Archive className="h-3 w-3" />
                </button>
              </div>
            );
          })
        )}
      </div>

      {/* Channels section */}
      <div className="px-3 pt-4 pb-1">
        <h3 className="text-xs font-semibold text-muted-foreground/60 uppercase tracking-wider flex items-center gap-1.5">
          <Hash className="h-3.5 w-3.5" />
          频道
        </h3>
      </div>
      <div className="flex-1 overflow-auto px-1 min-h-0">
        {activeChannels.length === 0 ? (
          <div className="px-3 py-2 text-xs text-muted-foreground">
            {q ? "无匹配" : "暂无频道"}
          </div>
        ) : (
          activeChannels.map((ch) => {
            const isSelected = selectedType === "channel" && selectedId === ch.id;
            return (
              <div
                key={ch.id}
                className={`group relative flex items-center gap-1 px-3 py-1.5 rounded-md transition-colors ${
                  isSelected
                    ? "bg-muted text-foreground"
                    : "hover:bg-accent text-secondary-foreground"
                }`}
              >
                <button onClick={() => onSelectChannel(ch)} className="flex-1 min-w-0 text-left text-sm">
                  <span className="truncate block"># {ch.name}</span>
                </button>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    onArchiveChannel(ch.id);
                  }}
                  className="opacity-0 group-hover:opacity-100 shrink-0 p-1 rounded hover:bg-background text-muted-foreground hover:text-foreground transition-opacity"
                  title="归档"
                  aria-label="归档"
                >
                  <Archive className="h-3 w-3" />
                </button>
              </div>
            );
          })
        )}
      </div>

      {/* Archived section — collapsed by default */}
      {archivedCount > 0 && (
        <div className="border-t border-border shrink-0">
          <button
            type="button"
            onClick={() => setShowArchived((v) => !v)}
            className="w-full flex items-center justify-between px-3 py-2 text-xs font-semibold text-muted-foreground/60 uppercase tracking-wider hover:text-foreground transition-colors"
          >
            <span className="flex items-center gap-1.5">
              <Archive className="h-3.5 w-3.5" />
              已归档 ({archivedCount})
            </span>
            <span>{showArchived ? "▾" : "▸"}</span>
          </button>
          {showArchived && (
            <div className="max-h-64 overflow-auto px-1 pb-2">
              {archivedConvs.map((conv) => {
                const peerType = (conv.peer_type as "member" | "agent") ?? "member";
                return (
                  <div
                    key={`a-dm-${conv.peer_id}`}
                    className="group flex items-center gap-1 px-3 py-1.5 rounded-md hover:bg-accent"
                  >
                    <button onClick={() => onSelectDm(conv)} className="flex-1 min-w-0 text-left text-sm text-muted-foreground">
                      <span className="truncate block">{conv.peer_name || conv.peer_id.slice(0, 12)}</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => onUnarchiveDM(conv.peer_id, peerType)}
                      className="opacity-0 group-hover:opacity-100 shrink-0 p-1 rounded hover:bg-background text-muted-foreground hover:text-foreground transition-opacity"
                      title="恢复"
                    >
                      ↺
                    </button>
                  </div>
                );
              })}
              {archivedChannels.map((ch) => (
                <div
                  key={`a-ch-${ch.id}`}
                  className="group flex items-center gap-1 px-3 py-1.5 rounded-md hover:bg-accent"
                >
                  <button onClick={() => onSelectChannel(ch)} className="flex-1 min-w-0 text-left text-sm text-muted-foreground">
                    <span className="truncate block"># {ch.name}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => onUnarchiveChannel(ch.id)}
                    className="opacity-0 group-hover:opacity-100 shrink-0 p-1 rounded hover:bg-background text-muted-foreground hover:text-foreground transition-opacity"
                    title="恢复"
                  >
                    ↺
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Agent status pill
// ---------------------------------------------------------------------------

function AgentStatusPill({
  status,
  isTyping,
}: {
  status?: string;
  isTyping: boolean;
}) {
  let label: string;
  let dotClass: string;
  let pulse = false;
  if (isTyping) {
    label = "输入中…";
    dotClass = "bg-primary";
    pulse = true;
  } else if (status === "online") {
    label = "在线";
    dotClass = "bg-green-500";
  } else if (status === "idle" || status === "") {
    label = "空闲";
    dotClass = "bg-primary/70";
  } else if (status === "busy") {
    label = "忙碌";
    dotClass = "bg-amber-500";
  } else {
    label = "离线";
    dotClass = "bg-muted-foreground/60";
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-border/70 bg-secondary/60 px-2 py-0.5 text-[11px] text-muted-foreground">
      <span
        className={`h-1.5 w-1.5 rounded-full ${dotClass} ${pulse ? "animate-pulse" : ""}`}
      />
      <span>{label}</span>
    </span>
  );
}

// ---------------------------------------------------------------------------
// Inbox Panel (right)
// ---------------------------------------------------------------------------

function InboxPanel({
  items,
  loading,
  onClose,
}: {
  items: InboxItem[];
  loading: boolean;
  onClose: () => void;
}) {
  const handleArchive = async (id: string) => {
    try {
      await api.archiveInbox(id);
      useInboxStore.getState().archive(id);
    } catch {
      toast.error("归档失败");
    }
  };

  return (
    <div className="w-72 shrink-0 border-l border-border flex flex-col h-full bg-popover">
      <div className="flex items-center justify-between px-4 h-12 border-b border-border shrink-0">
        <h3 className="text-sm font-semibold text-foreground">通知</h3>
        <button
          onClick={onClose}
          className="text-xs text-muted-foreground hover:text-foreground"
        >
          关闭
        </button>
      </div>
      <div className="flex-1 overflow-auto min-h-0">
        {loading ? (
          <div className="p-4 text-sm text-muted-foreground text-center">
            加载中...
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
            <Inbox className="mb-2 h-6 w-6 text-muted-foreground/60" />
            <p className="text-xs">暂无通知</p>
          </div>
        ) : (
          items.map((item) => (
            <div
              key={item.id}
              className="flex items-start gap-2 px-3 py-2.5 border-b border-border hover:bg-accent transition-colors group"
            >
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">
                  {!item.read && (
                    <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-primary" />
                  )}
                  <span
                    className={`text-sm truncate ${!item.read ? "font-medium text-foreground" : "text-muted-foreground"}`}
                  >
                    {item.title}
                  </span>
                </div>
                {item.body && (
                  <p className="text-xs text-muted-foreground truncate mt-0.5">
                    {item.body}
                  </p>
                )}
                <span className="text-[10px] text-muted-foreground/60">
                  {timeAgo(item.created_at)}
                </span>
              </div>
              <button
                onClick={() => handleArchive(item.id)}
                className="hidden group-hover:block shrink-0 text-muted-foreground hover:text-foreground p-0.5"
                aria-label="归档通知"
              >
                <Archive className="h-3 w-3" />
              </button>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main page
// ---------------------------------------------------------------------------

export default function SessionPage() {
  const searchParams = useSearchParams();
  const urlId = searchParams.get("id");
  const urlType = searchParams.get("type") as SelectionType | null;

  // Selection state
  const [selectedId, setSelectedId] = useState<string | null>(urlId);
  const [selectedType, setSelectedType] = useState<SelectionType>(
    urlType === "channel" ? "channel" : "dm",
  );
  const [showInbox, setShowInbox] = useState(false);
  // Whether checkboxes are visible on every message. The store remembers
  // which ids are selected; this just toggles whether the UI shows them.
  const [selectionEnabled, setSelectionEnabled] = useState(false);
  const setSelectionScope = useMessageSelectionStore((s) => s.setScope);
  const clearSelection = useMessageSelectionStore((s) => s.clear);
  const setAllSelection = useMessageSelectionStore((s) => s.setAll);
  const selectionCount = useMessageSelectionStore((s) => s.selectedIds.size);

  // Channel messages (local state like channel detail page)
  const [channelMessages, setChannelMessages] = useState<Message[]>([]);

  // Stores
  const conversations = useMessagingStore((s) => s.conversations);
  const dmMessages = useMessagingStore((s) => s.currentMessages);
  const dmLoading = useMessagingStore((s) => s.loading);
  const fetchConversations = useMessagingStore((s) => s.loadConversations);
  const fetchDmMessages = useMessagingStore((s) => s.loadMessages);
  const sendDmMessage = useMessagingStore((s) => s.sendMessage);

  // Personal agent — always shown in DM list as online + interactable, even
  // before the user has exchanged any messages with it. Derived from the
  // workspace agents store so realtime `agent:status_changed` events update
  // the sidebar (see use-realtime-sync's agent → refreshAgents handler).
  const currentUserId = useAuthStore((s) => s.user?.id);
  const personalAgent = useWorkspaceStore((s) =>
    s.agents.find(
      (a) => a.agent_type === "personal_agent" && a.owner_id === currentUserId,
    ) ?? null,
  );

  const channels = useChannelStore((s) => s.channels);
  const currentChannel = useChannelStore((s) => s.currentChannel);
  const channelMembers = useChannelStore((s) => s.members);
  const fetchChannels = useChannelStore((s) => s.fetch);
  const fetchChannel = useChannelStore((s) => s.fetchChannel);
  const fetchMembers = useChannelStore((s) => s.fetchMembers);
  const archiveChannelAction = useChannelStore((s) => s.archiveChannel);
  const unarchiveChannelAction = useChannelStore((s) => s.unarchiveChannel);

  // Archive state for DM conversations (per-user, backed by dm_conversation_state).
  const archivedKeys = useConversationArchiveStore((s) => s.archivedKeys);
  const fetchArchivedDMs = useConversationArchiveStore((s) => s.fetch);
  const archiveDMAction = useConversationArchiveStore((s) => s.archive);
  const unarchiveDMAction = useConversationArchiveStore((s) => s.unarchive);
  const isDMArchived = useCallback(
    (peerId: string, peerType: "member" | "agent") =>
      archivedKeys.has(`${peerType}:${peerId}`),
    [archivedKeys],
  );

  const inboxItems = useInboxStore((s) => s.dedupedItems());
  const inboxLoading = useInboxStore((s) => s.loading);
  const inboxUnread = useInboxStore((s) => s.unreadCount());

  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Initial data load
  useEffect(() => {
    fetchConversations();
    fetchChannels();
    fetchArchivedDMs();
  }, [fetchConversations, fetchChannels, fetchArchivedDMs]);

  // DM list shown in the sidebar = real conversations + the personal agent
  // (if it isn't already present from prior messages).
  const sidebarConversations = useMemo<Conversation[]>(() => {
    if (!personalAgent) return conversations;
    if (conversations.some((c) => c.peer_id === personalAgent.id)) {
      return conversations;
    }
    const synthetic: Conversation = {
      peer_id: personalAgent.id,
      peer_type: "agent",
      peer_name: personalAgent.display_name || personalAgent.name,
      unread_count: 0,
    };
    return [synthetic, ...conversations];
  }, [conversations, personalAgent]);

  const selectedConversation = useMemo(
    () =>
      selectedType === "dm"
        ? sidebarConversations.find((c) => c.peer_id === selectedId) ?? null
        : null,
    [selectedId, selectedType, sidebarConversations],
  );

  // Peer agent (when the current conversation is a DM with an agent).
  // Drives the status pill in the header: online / idle / offline / typing.
  const agentsList = useWorkspaceStore((s) => s.agents);
  const peerAgent = useMemo(() => {
    if (selectedType !== "dm" || selectedConversation?.peer_type !== "agent") return null;
    return agentsList.find((a) => a.id === selectedId) ?? null;
  }, [selectedType, selectedConversation, agentsList, selectedId]);

  const dmTyping = useTypingIndicator({
    peerId: selectedType === "dm" ? selectedId ?? undefined : undefined,
  });
  const channelTyping = useTypingIndicator({
    channelId: selectedType === "channel" ? selectedId ?? undefined : undefined,
  });
  const peerIsTyping =
    selectedType === "dm" && selectedId
      ? dmTyping.typingUsers.includes(selectedId)
      : false;

  // Read-receipt debounce set — persists across renders so a rapid
  // poll cycle doesn't retrigger /messages/read for the same ids.
  const readSentRef = useRef<Set<string>>(new Set());

  // Selection scope follows the active channel/dm. Switching conversations
  // clears whatever was selected so we never leak picks across rooms.
  // scopeKey is prefixed with the type to avoid collisions between a
  // channel and a DM peer sharing the same UUID string.
  useEffect(() => {
    const scopeKey = selectedId ? `${selectedType}:${selectedId}` : null;
    setSelectionScope(scopeKey);
  }, [selectedId, selectedType, setSelectionScope]);

  // Sync URL param on mount
  useEffect(() => {
    if (urlId) {
      setSelectedId(urlId);
      if (urlType === "channel") {
        setSelectedType("channel");
      }
    }
  }, [urlId, urlType]);

  // Poll messages for selected conversation/channel
  useEffect(() => {
    if (pollRef.current) clearInterval(pollRef.current);
    if (!selectedId) return;

    if (selectedType === "dm") {
      fetchDmMessages({
        recipient_id: selectedId,
        peer_type: selectedConversation?.peer_type ?? "member",
      });
      pollRef.current = setInterval(() => {
        fetchDmMessages({
          recipient_id: selectedId,
          peer_type: selectedConversation?.peer_type ?? "member",
        });
      }, 3000);
    } else {
      // Channel messages
      fetchChannel(selectedId);
      fetchMembers(selectedId);

      async function loadChannelMessages() {
        try {
          const res = await api.getChannelMessages(selectedId!);
          setChannelMessages(res.messages);
        } catch {
          // silent poll failure
        }
      }
      async function loadChannelThreads() {
        try {
          const ts = await api.listThreads(selectedId!);
          setChannelThreads(
            ts.map((t) => ({ id: t.id, root_message_id: t.root_message_id })),
          );
        } catch {
          // non-fatal; chip count will fall back to naive keying
        }
      }
      loadChannelMessages();
      loadChannelThreads();
      pollRef.current = setInterval(() => {
        loadChannelMessages();
        loadChannelThreads();
      }, 3000);
    }

    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [selectedId, selectedType, selectedConversation, fetchDmMessages, fetchChannel, fetchMembers]);

  // Selection handlers
  const handleSelectDm = useCallback((conv: Conversation) => {
    setSelectedId(conv.peer_id);
    setSelectedType("dm");
  }, []);

  const handleSelectChannel = useCallback((ch: Channel) => {
    setSelectedId(ch.id);
    setSelectedType("channel");
  }, []);

  // Send handlers
  const handleSendDm = useCallback(
    async (content: string, fileInfo?: { file_id: string; file_name: string; file_size: number; file_content_type: string }) => {
      const conv = sidebarConversations.find((c) => c.peer_id === selectedId);
      if (!conv) return;
      await sendDmMessage({
        recipient_id: conv.peer_id,
        recipient_type: conv.peer_type,
        content,
        ...(fileInfo ? { file_id: fileInfo.file_id, file_name: fileInfo.file_name } : {}),
      });
      // Refresh the conversation list so the synthetic personal-agent row is
      // replaced by the real conversation (with last_message + unread state)
      // as soon as the first message lands. Cheap to do every send; the
      // sidebar dedup logic handles the synthetic → real transition.
      fetchConversations();
    },
    [sidebarConversations, selectedId, sendDmMessage, fetchConversations],
  );

  const handleSendChannel = useCallback(
    async (content: string, fileInfo?: { file_id: string; file_name: string; file_size: number; file_content_type: string }) => {
      if (!selectedId) return;
      try {
        const msg = await api.sendMessage({
          channel_id: selectedId,
          content,
          ...(fileInfo ? { file_id: fileInfo.file_id, file_name: fileInfo.file_name } : {}),
        });
        setChannelMessages((prev) => [...prev, msg]);
      } catch {
        toast.error("发送消息失败");
      }
    },
    [selectedId],
  );

  // Right-rail panels share a single slot. Modeled as a discriminated union
  // so the panel kind and its payload stay consistent, and the user doesn't
  // end up with a sliver-wide thread squeezed next to a search panel.
  type RightPanel =
    | { kind: "thread"; threadId: string }
    | { kind: "meeting"; initialId: string | null }
    | { kind: "search" }
    | { kind: "files" }
    | null;

  const [rightPanel, setRightPanel] = useState<RightPanel>(null);
  const [channelMeetings, setChannelMeetings] = useState<ChannelMeeting[]>([]);
  const [channelThreads, setChannelThreads] = useState<Array<{ id: string; root_message_id?: string | null }>>([]);
  const activeFile = useFileViewerStore((s) => s.active);
  const closeFileViewer = useFileViewerStore((s) => s.close);

  // File viewer is mutually exclusive with the other right-rail panels.
  // Opening a file (e.g. from ChannelFilesPanel or a message chip) closes
  // whatever right panel was active.
  useEffect(() => {
    if (activeFile) setRightPanel(null);
  }, [activeFile]);

  // Load channel meetings whenever the user flips to a channel. Poll while
  // any meeting is still recording/processing so the inline bubbles tick
  // into their final state without a page refresh.
  useEffect(() => {
    if (selectedType !== "channel" || !selectedId) {
      setChannelMeetings([]);
      return;
    }
    let cancelled = false;
    const load = async () => {
      try {
        const res = await api.listChannelMeetings(selectedId, 50);
        if (!cancelled) setChannelMeetings(res.meetings ?? []);
      } catch {
        // Best effort — meeting list is non-critical.
      }
    };
    load();
    const anyActive = (ms: ChannelMeeting[]) =>
      ms.some((m) => m.status === "recording" || m.status === "processing");
    let timer: ReturnType<typeof setInterval> | null = null;
    const schedule = () => {
      if (timer) clearInterval(timer);
      timer = setInterval(async () => {
        try {
          const res = await api.listChannelMeetings(selectedId, 50);
          if (cancelled) return;
          setChannelMeetings(res.meetings ?? []);
          if (!anyActive(res.meetings ?? []) && timer) {
            clearInterval(timer);
            timer = null;
          }
        } catch {
          /* ignore */
        }
      }, 4000);
    };
    schedule();
    return () => {
      cancelled = true;
      if (timer) clearInterval(timer);
    };
  }, [selectedType, selectedId]);

  const handleOpenMeeting = useCallback(
    (meetingId: string) => {
      useFileViewerStore.getState().close();
      setRightPanel({ kind: "meeting", initialId: meetingId });
    },
    [],
  );

  // Threads have their own UUIDs, distinct from the root message. Opening
  // a thread from a message requires creating the thread row first (or
  // returning the existing one). The backend's CreateThread endpoint is
  // idempotent on root_message_id so repeated calls are safe.
  const openThreadForMessage = useCallback(
    async (msgId: string) => {
      if (!selectedId || selectedType !== "channel") return;
      try {
        const thread = await api.createThread(selectedId, { root_message_id: msgId });
        useFileViewerStore.getState().close();
        setRightPanel({ kind: "thread", threadId: thread.id });
      } catch {
        toast.error("打开讨论串失败");
      }
    },
    [selectedId, selectedType],
  );

  // Inline thread reply (from the expanded chip under a root message).
  // Reuses createThread for idempotency, then posts via the thread
  // endpoint. After success we refresh the channel list so the new
  // reply shows up immediately under the expanded root instead of
  // waiting on the 3s poll.
  const handleInlineThreadReply = useCallback(
    async (rootMessageId: string, content: string) => {
      if (!selectedId || selectedType !== "channel") return;
      try {
        const thread = await api.createThread(selectedId, { root_message_id: rootMessageId });
        await api.sendThreadMessage(thread.id, content);
        const res = await api.getChannelMessages(selectedId);
        setChannelMessages(res.messages);
      } catch {
        toast.error("回复讨论串失败");
      }
    },
    [selectedId, selectedType],
  );

  // Search + Create state
  const [sidebarSearch, setSidebarSearch] = useState("");
  const [showCreateChannel, setShowCreateChannel] = useState(false);
  const [newChannelName, setNewChannelName] = useState("");
  const [creatingChannel, setCreatingChannel] = useState(false);
  const [showInviteDialog, setShowInviteDialog] = useState(false);

  const handleCreateChannel = async () => {
    if (!newChannelName.trim()) { setShowCreateChannel(true); return; }
    setCreatingChannel(true);
    try {
      const ch = await api.createChannel({ name: newChannelName.trim() });
      toast.success(`频道 #${ch.name} 创建成功`);
      setNewChannelName("");
      setShowCreateChannel(false);
      fetchChannels();
      setSelectedId(ch.id);
      setSelectedType("channel");
    } catch (e) {
      toast.error("创建频道失败");
    } finally { setCreatingChannel(false); }
  };

  // Derive current messages and header info
  const messages = selectedType === "dm" ? dmMessages : channelMessages;

  // Multi-select highlight: parent rows whose right-rail panel is
  // currently open stay visually distinct from regular hover. Both
  // the thread panel and the file viewer can be open at once, so
  // we fold both into one Set.
  const highlightedMessageIds = useMemo(() => {
    const s = new Set<string>();
    // New threads carry their own UUID (distinct from root_message_id);
    // legacy resolveOrCreateThread used the root id directly. Resolve
    // via the loaded thread rows so the highlight lands on the parent
    // message in both cases.
    if (rightPanel?.kind === "thread") {
      const t = channelThreads.find((x) => x.id === rightPanel.threadId);
      s.add(t?.root_message_id ?? rightPanel.threadId);
    }
    if (activeFile?.file_id) {
      const msg = messages.find((m) => m.file_id === activeFile.file_id);
      if (msg) s.add(msg.id);
    }
    return s;
  }, [rightPanel, activeFile, messages, channelThreads]);

  // Sender-side read receipt: when the recipient marks a channel
  // message as read, the server publishes message:read; flip the
  // cached channel-message status so MessageList can swap the tick
  // icon. DM messages live in the messaging store which handles the
  // same event internally.
  useWSEvent(
    "message:read",
    useCallback((payload: unknown) => {
      const data = payload as { message_id?: string } | undefined;
      const mid = data?.message_id;
      if (!mid) return;
      setChannelMessages((prev) =>
        prev.map((m) => (m.id === mid && m.status !== "read" ? { ...m, status: "read" } : m)),
      );
    }, []),
  );

  // Auto mark-as-read: when messages load, flip every incoming message
  // whose status is still 'sent' to 'read' on the server. The server
  // broadcasts message:read events so the original sender's ticks
  // update in real time.
  useEffect(() => {
    if (!selectedId || !currentUserId || messages.length === 0) return;
    const toMark: string[] = [];
    for (const m of messages) {
      if (m.sender_id === currentUserId) continue;
      const status = (m as { status?: string }).status;
      if (status === "read") continue;
      if (readSentRef.current.has(m.id)) continue;
      toMark.push(m.id);
    }
    if (toMark.length === 0) return;
    for (const id of toMark) readSentRef.current.add(id);
    void api.markMessagesRead(toMark).catch(() => {
      for (const id of toMark) readSentRef.current.delete(id);
    });
  }, [messages, selectedId, currentUserId]);
  const headerName =
    selectedType === "dm"
      ? selectedConversation?.peer_name ||
        selectedId?.slice(0, 12) ||
        ""
      : currentChannel?.name
        ? `# ${currentChannel.name}`
        : "";
  const headerSub =
    selectedType === "channel" && channelMembers.length > 0
      ? `${channelMembers.length} 位成员`
      : selectedType === "dm"
        ? selectedConversation?.peer_type ?? ""
        : "";

  return (
    <div className="flex h-full">
      {/* Left sidebar */}
      <SessionSidebar
        conversations={sidebarConversations}
        channels={channels}
        selectedId={selectedId}
        selectedType={selectedType}
        onSelectDm={handleSelectDm}
        onSelectChannel={handleSelectChannel}
        onCreateChannel={() => setShowCreateChannel(true)}
        searchQuery={sidebarSearch}
        onSearchChange={setSidebarSearch}
        isDMArchived={isDMArchived}
        onArchiveDM={archiveDMAction}
        onUnarchiveDM={unarchiveDMAction}
        onArchiveChannel={archiveChannelAction}
        onUnarchiveChannel={unarchiveChannelAction}
      />

      {/* Create channel dialog */}
      {showCreateChannel && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-card border border-border rounded-[12px] p-5 w-80 space-y-4 shadow-lg">
            <h3 className="text-[15px] font-semibold text-foreground">创建频道</h3>
            <input
              value={newChannelName}
              onChange={e => setNewChannelName(e.target.value)}
              placeholder="频道名称"
              autoFocus
              className="w-full px-3 py-2 bg-secondary border border-border rounded-[6px] text-[13px] text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
              onKeyDown={e => e.key === "Enter" && handleCreateChannel()}
            />
            <div className="flex gap-2 justify-end">
              <button onClick={() => setShowCreateChannel(false)} className="px-3 py-1.5 text-[13px] text-muted-foreground hover:text-foreground transition-colors">取消</button>
              <button onClick={handleCreateChannel} disabled={creatingChannel || !newChannelName.trim()}
                className="px-4 py-1.5 bg-primary text-primary-foreground rounded-[6px] text-[13px] font-medium disabled:opacity-40 hover:opacity-90 transition-opacity">
                {creatingChannel ? "创建中..." : "创建"}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Center - message area */}
      <div className="flex-1 flex flex-col min-w-0 h-full bg-background">
        {selectedId ? (
          <>
            {/* Header */}
            <div className="flex items-center justify-between px-4 h-12 border-b border-border shrink-0">
              <div className="flex items-center gap-2 min-w-0">
                {selectedType === "channel" ? (
                  <Hash className="h-4 w-4 shrink-0 text-muted-foreground" />
                ) : (
                  <MessageCircle className="h-4 w-4 shrink-0 text-muted-foreground" />
                )}
                <span className="font-medium text-sm truncate text-foreground">
                  {headerName}
                </span>
                {headerSub && (
                  <>
                    <span className="text-muted-foreground/60">|</span>
                    <span className="text-xs text-muted-foreground flex items-center gap-1">
                      <Users className="h-3 w-3" />
                      {headerSub}
                    </span>
                  </>
                )}
                {selectedType === "dm" && peerAgent && (
                  <AgentStatusPill
                    status={peerAgent.status}
                    isTyping={peerIsTyping}
                  />
                )}
              </div>
              <div className="flex items-center gap-2">
                {selectedId && (
                  <>
                    <button
                      type="button"
                      onClick={() => {
                        if (selectionEnabled) {
                          // Turning off selection mode also clears picks so a
                          // re-enable starts fresh.
                          clearSelection();
                        }
                        setSelectionEnabled(!selectionEnabled);
                      }}
                      title={selectionEnabled ? "Exit selection mode" : "Select messages"}
                      className={`flex items-center gap-1 px-2 h-7 rounded-md text-[12px] transition-colors ${
                        selectionEnabled
                          ? "bg-accent text-foreground"
                          : "text-muted-foreground hover:text-foreground hover:bg-accent"
                      }`}
                    >
                      <CheckSquare className="h-3.5 w-3.5" />
                      {selectionEnabled ? `Selecting (${selectionCount})` : "Select"}
                    </button>
                    {selectionEnabled && messages.length > 0 && (
                      <button
                        type="button"
                        onClick={() => {
                          const allIds = messages.map((m) => m.id);
                          const allSelected =
                            selectionCount === allIds.length && allIds.length > 0;
                          if (allSelected) {
                            clearSelection();
                          } else {
                            setAllSelection(allIds);
                          }
                        }}
                        title="全选 / 取消全选当前可见消息"
                        className="flex items-center gap-1 px-2 h-7 rounded-md text-[12px] text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
                      >
                        {selectionCount === messages.length && messages.length > 0
                          ? "取消全选"
                          : "全选"}
                      </button>
                    )}
                    <GenerateProjectButton
                      sourceType={selectedType}
                      sourceId={selectedId}
                      sourceName={
                        selectedType === "channel"
                          ? currentChannel?.name ?? "channel"
                          : selectedConversation?.peer_name ?? "chat"
                      }
                      peerType={
                        selectedType === "dm"
                          ? (selectedConversation?.peer_type as "member" | "agent" | undefined)
                          : undefined
                      }
                    />
                    {selectedType === "dm" && (
                      <PromoteToChannelButton
                        peerId={selectedId}
                        peerType={(selectedConversation?.peer_type as "member" | "agent" | undefined) ?? "member"}
                        peerName={selectedConversation?.peer_name ?? "chat"}
                      />
                    )}
                    {selectedType === "channel" && (
                      <>
                        <button
                          type="button"
                          onClick={() => {
                            useFileViewerStore.getState().close();
                            setRightPanel({ kind: "meeting", initialId: null });
                          }}
                          title="开始会议"
                          className="flex items-center gap-1 px-2 h-7 rounded-md text-[12px] text-primary hover:bg-accent transition-colors"
                        >
                          <Mic className="h-3.5 w-3.5" />
                          开始会议
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            useFileViewerStore.getState().close();
                            setRightPanel(rightPanel?.kind === "search" ? null : { kind: "search" });
                          }}
                          title="搜索聊天记录"
                          className={`flex items-center gap-1 px-2 h-7 rounded-md text-[12px] hover:bg-accent transition-colors ${rightPanel?.kind === "search" ? "text-foreground bg-accent" : "text-muted-foreground hover:text-foreground"}`}
                        >
                          <Search className="h-3.5 w-3.5" />
                          搜索
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            useFileViewerStore.getState().close();
                            setRightPanel(rightPanel?.kind === "files" ? null : { kind: "files" });
                          }}
                          title="查看频道文件"
                          className={`flex items-center gap-1 px-2 h-7 rounded-md text-[12px] hover:bg-accent transition-colors ${rightPanel?.kind === "files" ? "text-foreground bg-accent" : "text-muted-foreground hover:text-foreground"}`}
                        >
                          <FolderOpen className="h-3.5 w-3.5" />
                          文件
                        </button>
                        <button
                          type="button"
                          onClick={() => setShowInviteDialog(true)}
                          title="邀请成员/Agent"
                          className="flex items-center gap-1 px-2 h-7 rounded-md text-[12px] text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
                        >
                          <UserPlus className="h-3.5 w-3.5" />
                          邀请
                        </button>
                      </>
                    )}
                  </>
                )}
                <button
                  onClick={() => setShowInbox(!showInbox)}
                  className="relative p-1.5 rounded-md hover:bg-accent text-muted-foreground hover:text-foreground transition-colors"
                  title="通知"
                  aria-label="通知"
                >
                  {inboxUnread > 0 ? (
                    <BellDot className="h-4 w-4" />
                  ) : (
                    <Bell className="h-4 w-4" />
                  )}
                  {inboxUnread > 0 && (
                    <span className="absolute -top-0.5 -right-0.5 text-[9px] bg-primary text-white rounded-full h-3.5 w-3.5 flex items-center justify-center leading-none">
                      {inboxUnread > 9 ? "9+" : inboxUnread}
                    </span>
                  )}
                </button>
              </div>
            </div>

            {/* Messages + Thread */}
            <div className="flex-1 flex min-h-0">
              <div className="flex-1 flex flex-col min-w-0">
                <MessageList
                  messages={messages}
                  currentUserId={currentUserId}
                  onOpenThread={selectedType === "channel" ? openThreadForMessage : undefined}
                  onReplyInThread={selectedType === "channel" ? handleInlineThreadReply : undefined}
                  threads={selectedType === "channel" ? channelThreads : undefined}
                  highlightedIds={highlightedMessageIds}
                  selectionEnabled={selectionEnabled}
                  meetings={selectedType === "channel" ? channelMeetings : []}
                  onOpenMeeting={selectedType === "channel" ? handleOpenMeeting : undefined}
                  typingUsers={
                    selectedType === "channel"
                      ? channelTyping.typingUsers
                      : peerIsTyping && selectedId
                        ? [selectedId]
                        : []
                  }
                />
              </div>
              {rightPanel?.kind === "thread" && selectedType === "channel" && selectedId && (
                <ThreadPanel
                  threadId={rightPanel.threadId}
                  channelId={selectedId}
                  onClose={() => setRightPanel(null)}
                />
              )}
              {rightPanel?.kind === "meeting" && selectedType === "channel" && selectedId && (
                <MeetingPanel
                  channelId={selectedId}
                  initialMeetingId={rightPanel.initialId}
                  onClose={() => setRightPanel(null)}
                />
              )}
              {rightPanel?.kind === "search" && selectedType === "channel" && (
                <ChannelSearchPanel
                  messages={messages}
                  onClose={() => setRightPanel(null)}
                />
              )}
              {rightPanel?.kind === "files" && selectedType === "channel" && (
                <ChannelFilesPanel
                  messages={messages}
                  onClose={() => setRightPanel(null)}
                />
              )}
              {activeFile && (
                <FileViewerPanel target={activeFile} onClose={closeFileViewer} />
              )}
            </div>

            {/* Input */}
            <MessageInput
              onSend={
                selectedType === "dm" ? handleSendDm : handleSendChannel
              }
              placeholder={
                selectedType === "dm"
                  ? "输入消息..."
                  : `发送消息到 #${currentChannel?.name ?? "频道"}...`
              }
            />
          </>
        ) : (
          <div className="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-2">
            <MessageCircle className="h-10 w-10 text-muted-foreground/60" />
            <p className="text-sm">选择一个对话</p>
          </div>
        )}
      </div>

      {/* Right panel - inbox */}
      {showInbox && (
        <InboxPanel
          items={inboxItems}
          loading={inboxLoading}
          onClose={() => setShowInbox(false)}
        />
      )}

      {/* Invite dialog */}
      {selectedType === "channel" && selectedId && (
        <InviteChannelMemberDialog
          channelId={selectedId}
          channelName={currentChannel?.name ?? "channel"}
          open={showInviteDialog}
          onClose={() => setShowInviteDialog(false)}
        />
      )}
    </div>
  );
}
