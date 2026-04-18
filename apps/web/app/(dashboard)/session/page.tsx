"use client";

import { useEffect, useRef, useState, useCallback, useMemo } from "react";
import { useSearchParams } from "next/navigation";
import { useMessagingStore } from "@/features/messaging/store";
import { useChannelStore } from "@/features/channels/store";
import { useInboxStore } from "@/features/inbox";
import { MessageList } from "@/features/messaging/components/message-list";
import { MessageInput } from "@/features/messaging/components/message-input";
import { ThreadPanel } from "@/features/messaging/components/thread-panel";
import { GenerateProjectButton } from "@/features/messaging/components/generate-project-button";
import { useMessageSelectionStore } from "@/features/messaging/stores/selection-store";
import { api } from "@/shared/api";
import type { Conversation } from "@/shared/types/messaging";
import type { Message } from "@/shared/types/messaging";
import type { Channel } from "@/shared/types/messaging";
import type { InboxItem, Agent } from "@/shared/types";
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
  Plus,
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
}) {
  const q = searchQuery.toLowerCase()
  const filteredConvs = q ? conversations.filter(c => (c.peer_name || "").toLowerCase().includes(q)) : conversations
  const filteredChannels = q ? channels.filter(c => c.name.toLowerCase().includes(q)) : channels

  return (
    <div className="w-60 shrink-0 border-r border-border flex flex-col bg-card h-full">
      {/* Search + Create */}
      <div className="px-3 pt-3 pb-2 space-y-2">
        <div className="relative">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
          <input
            value={searchQuery}
            onChange={e => onSearchChange(e.target.value)}
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

      {/* DMs section */}
      <div className="px-3 pt-2 pb-1">
        <h3 className="text-xs font-semibold text-muted-foreground/60 uppercase tracking-wider flex items-center gap-1.5">
          <MessageCircle className="h-3.5 w-3.5" />
          私聊
        </h3>
      </div>
      <div className="flex-none overflow-auto max-h-[40%] px-1">
        {filteredConvs.length === 0 ? (
          <div className="px-3 py-2 text-xs text-muted-foreground">
            {q ? "无匹配" : "暂无对话"}
          </div>
        ) : (
          filteredConvs.map((conv) => (
            <button
              key={conv.peer_id}
              onClick={() => onSelectDm(conv)}
              className={`w-full text-left px-3 py-1.5 rounded-md text-sm transition-colors ${
                selectedType === "dm" && selectedId === conv.peer_id
                  ? "bg-muted text-foreground"
                  : "hover:bg-accent text-secondary-foreground"
              }`}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="truncate">
                  {conv.peer_name || conv.peer_id.slice(0, 12)}
                </span>
                {(conv.unread_count ?? 0) > 0 && (
                  <span className="shrink-0 text-[10px] bg-primary text-white rounded-full px-1.5 py-0.5 leading-none">
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
          ))
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
        {filteredChannels.length === 0 ? (
          <div className="px-3 py-2 text-xs text-muted-foreground">
            {q ? "无匹配" : "暂无频道"}
          </div>
        ) : (
          filteredChannels.map((ch) => (
            <button
              key={ch.id}
              onClick={() => onSelectChannel(ch)}
              className={`w-full text-left px-3 py-1.5 rounded-md text-sm transition-colors ${
                selectedType === "channel" && selectedId === ch.id
                  ? "bg-muted text-foreground"
                  : "hover:bg-accent text-secondary-foreground"
              }`}
            >
              <span className="truncate">
                # {ch.name}
              </span>
            </button>
          ))
        )}
      </div>
    </div>
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
  // before the user has exchanged any messages with it.
  const [personalAgent, setPersonalAgent] = useState<Agent | null>(null);

  const channels = useChannelStore((s) => s.channels);
  const currentChannel = useChannelStore((s) => s.currentChannel);
  const channelMembers = useChannelStore((s) => s.members);
  const fetchChannels = useChannelStore((s) => s.fetch);
  const fetchChannel = useChannelStore((s) => s.fetchChannel);
  const fetchMembers = useChannelStore((s) => s.fetchMembers);

  const inboxItems = useInboxStore((s) => s.dedupedItems());
  const inboxLoading = useInboxStore((s) => s.loading);
  const inboxUnread = useInboxStore((s) => s.unreadCount());

  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Initial data load
  useEffect(() => {
    fetchConversations();
    fetchChannels();
    // Personal agent is auto-provisioned on the server; if the call fails we
    // simply skip the synthetic DM row rather than blocking the page.
    api
      .getPersonalAgent()
      .then((agent) => setPersonalAgent(agent))
      .catch(() => setPersonalAgent(null));
  }, [fetchConversations, fetchChannels]);

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

  // Selection scope follows the active channel/dm. Switching conversations
  // clears whatever was selected so we never leak picks across rooms.
  useEffect(() => {
    setSelectionScope(selectedType === "channel" ? selectedId : null);
    if (selectedType !== "channel") {
      // Selection mode is channel-only for now. Hide the checkboxes so the
      // DM view stays clean.
      setSelectionEnabled(false);
    }
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
      fetchDmMessages({ recipient_id: selectedId });
      pollRef.current = setInterval(() => {
        fetchDmMessages({ recipient_id: selectedId });
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
      loadChannelMessages();
      pollRef.current = setInterval(loadChannelMessages, 3000);
    }

    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [selectedId, selectedType, fetchDmMessages, fetchChannel, fetchMembers]);

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

  // Thread state
  const [activeThreadId, setActiveThreadId] = useState<string | null>(null);

  // Search + Create state
  const [sidebarSearch, setSidebarSearch] = useState("");
  const [showCreateChannel, setShowCreateChannel] = useState(false);
  const [newChannelName, setNewChannelName] = useState("");
  const [creatingChannel, setCreatingChannel] = useState(false);

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
  const headerName =
    selectedType === "dm"
      ? sidebarConversations.find((c) => c.peer_id === selectedId)?.peer_name ||
        selectedId?.slice(0, 12) ||
        ""
      : currentChannel?.name
        ? `# ${currentChannel.name}`
        : "";
  const headerSub =
    selectedType === "channel" && channelMembers.length > 0
      ? `${channelMembers.length} 位成员`
      : selectedType === "dm"
        ? sidebarConversations.find((c) => c.peer_id === selectedId)?.peer_type ?? ""
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
              </div>
              <div className="flex items-center gap-2">
                {selectedType === "channel" && (
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
                      title={selectionEnabled ? "Exit selection mode" : "Select messages for project"}
                      className={`flex items-center gap-1 px-2 h-7 rounded-md text-[12px] transition-colors ${
                        selectionEnabled
                          ? "bg-accent text-foreground"
                          : "text-muted-foreground hover:text-foreground hover:bg-accent"
                      }`}
                    >
                      <CheckSquare className="h-3.5 w-3.5" />
                      {selectionEnabled ? `Selecting (${selectionCount})` : "Select"}
                    </button>
                    {selectedId && (
                      <GenerateProjectButton
                        channelId={selectedId}
                        channelName={currentChannel?.name ?? "channel"}
                      />
                    )}
                  </>
                )}
                <button
                  onClick={() => setShowInbox(!showInbox)}
                  className="relative p-1.5 rounded-md hover:bg-accent text-muted-foreground hover:text-foreground transition-colors"
                  title="通知"
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
                  onOpenThread={selectedType === "channel" ? (msgId) => setActiveThreadId(msgId) : undefined}
                  selectionEnabled={selectionEnabled && selectedType === "channel"}
                />
              </div>
              {activeThreadId && selectedType === "channel" && selectedId && (
                <ThreadPanel
                  threadId={activeThreadId}
                  channelId={selectedId}
                  onClose={() => setActiveThreadId(null)}
                />
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
    </div>
  );
}
