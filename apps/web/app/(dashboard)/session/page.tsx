"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import { useSearchParams } from "next/navigation";
import { useMessagingStore } from "@/features/messaging/store";
import { useChannelStore } from "@/features/channels/store";
import { useInboxStore } from "@/features/inbox";
import { MessageList } from "@/features/messaging/components/message-list";
import { MessageInput } from "@/features/messaging/components/message-input";
import { api } from "@/shared/api";
import type { Conversation } from "@/shared/types/messaging";
import type { Message } from "@/shared/types/messaging";
import type { Channel } from "@/shared/types/messaging";
import type { InboxItem } from "@/shared/types";
import { toast } from "sonner";
import {
  Hash,
  MessageCircle,
  Bell,
  BellDot,
  Inbox,
  Users,
  Archive,
} from "lucide-react";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type SelectionType = "dm" | "channel";

function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "just now";
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
}: {
  conversations: Conversation[];
  channels: Channel[];
  selectedId: string | null;
  selectedType: SelectionType;
  onSelectDm: (conv: Conversation) => void;
  onSelectChannel: (ch: Channel) => void;
}) {
  return (
    <div className="w-60 shrink-0 border-r flex flex-col bg-muted/30 h-full">
      {/* DMs section */}
      <div className="px-3 pt-4 pb-1">
        <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
          <MessageCircle className="h-3.5 w-3.5" />
          私聊
        </h3>
      </div>
      <div className="flex-none overflow-auto max-h-[40%] px-1">
        {conversations.length === 0 ? (
          <div className="px-3 py-2 text-xs text-muted-foreground">
            No conversations
          </div>
        ) : (
          conversations.map((conv) => (
            <button
              key={conv.peer_id}
              onClick={() => onSelectDm(conv)}
              className={`w-full text-left px-3 py-1.5 rounded-md text-sm transition-colors ${
                selectedType === "dm" && selectedId === conv.peer_id
                  ? "bg-accent text-accent-foreground"
                  : "hover:bg-accent/50 text-foreground"
              }`}
            >
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
          ))
        )}
      </div>

      {/* Channels section */}
      <div className="px-3 pt-4 pb-1">
        <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
          <Hash className="h-3.5 w-3.5" />
          频道
        </h3>
      </div>
      <div className="flex-1 overflow-auto px-1 min-h-0">
        {channels.length === 0 ? (
          <div className="px-3 py-2 text-xs text-muted-foreground">
            No channels
          </div>
        ) : (
          channels.map((ch) => (
            <button
              key={ch.id}
              onClick={() => onSelectChannel(ch)}
              className={`w-full text-left px-3 py-1.5 rounded-md text-sm transition-colors ${
                selectedType === "channel" && selectedId === ch.id
                  ? "bg-accent text-accent-foreground"
                  : "hover:bg-accent/50 text-foreground"
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
      toast.error("Failed to archive");
    }
  };

  return (
    <div className="w-72 shrink-0 border-l flex flex-col h-full bg-muted/30">
      <div className="flex items-center justify-between px-4 h-12 border-b shrink-0">
        <h3 className="text-sm font-semibold">通知</h3>
        <button
          onClick={onClose}
          className="text-xs text-muted-foreground hover:text-foreground"
        >
          Close
        </button>
      </div>
      <div className="flex-1 overflow-auto min-h-0">
        {loading ? (
          <div className="p-4 text-sm text-muted-foreground text-center">
            Loading...
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
            <Inbox className="mb-2 h-6 w-6 text-muted-foreground/50" />
            <p className="text-xs">No notifications</p>
          </div>
        ) : (
          items.map((item) => (
            <div
              key={item.id}
              className="flex items-start gap-2 px-3 py-2.5 border-b hover:bg-accent/50 transition-colors group"
            >
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">
                  {!item.read && (
                    <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-primary" />
                  )}
                  <span
                    className={`text-sm truncate ${!item.read ? "font-medium" : "text-muted-foreground"}`}
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

  // Channel messages (local state like channel detail page)
  const [channelMessages, setChannelMessages] = useState<Message[]>([]);

  // Stores
  const conversations = useMessagingStore((s) => s.conversations);
  const dmMessages = useMessagingStore((s) => s.currentMessages);
  const dmLoading = useMessagingStore((s) => s.loading);
  const fetchConversations = useMessagingStore((s) => s.fetch);
  const fetchDmMessages = useMessagingStore((s) => s.fetchMessages);
  const sendDmMessage = useMessagingStore((s) => s.sendMessage);

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
  }, [fetchConversations, fetchChannels]);

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
      const conv = conversations.find((c) => c.peer_id === selectedId);
      if (!conv) return;
      await sendDmMessage({
        recipient_id: conv.peer_id,
        recipient_type: conv.peer_type,
        content,
        ...(fileInfo ? { file_id: fileInfo.file_id, file_name: fileInfo.file_name } : {}),
      });
    },
    [conversations, selectedId, sendDmMessage],
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
        toast.error("Failed to send message");
      }
    },
    [selectedId],
  );

  // Derive current messages and header info
  const messages = selectedType === "dm" ? dmMessages : channelMessages;
  const headerName =
    selectedType === "dm"
      ? conversations.find((c) => c.peer_id === selectedId)?.peer_name ||
        selectedId?.slice(0, 12) ||
        ""
      : currentChannel?.name
        ? `# ${currentChannel.name}`
        : "";
  const headerSub =
    selectedType === "channel" && channelMembers.length > 0
      ? `${channelMembers.length} member${channelMembers.length !== 1 ? "s" : ""}`
      : selectedType === "dm"
        ? conversations.find((c) => c.peer_id === selectedId)?.peer_type ?? ""
        : "";

  return (
    <div className="flex h-full">
      {/* Left sidebar */}
      <SessionSidebar
        conversations={conversations}
        channels={channels}
        selectedId={selectedId}
        selectedType={selectedType}
        onSelectDm={handleSelectDm}
        onSelectChannel={handleSelectChannel}
      />

      {/* Center - message area */}
      <div className="flex-1 flex flex-col min-w-0 h-full">
        {selectedId ? (
          <>
            {/* Header */}
            <div className="flex items-center justify-between px-4 h-12 border-b shrink-0">
              <div className="flex items-center gap-2 min-w-0">
                {selectedType === "channel" ? (
                  <Hash className="h-4 w-4 shrink-0 text-muted-foreground" />
                ) : (
                  <MessageCircle className="h-4 w-4 shrink-0 text-muted-foreground" />
                )}
                <span className="font-medium text-sm truncate">
                  {headerName}
                </span>
                {headerSub && (
                  <>
                    <span className="text-muted-foreground">|</span>
                    <span className="text-xs text-muted-foreground flex items-center gap-1">
                      <Users className="h-3 w-3" />
                      {headerSub}
                    </span>
                  </>
                )}
              </div>
              <button
                onClick={() => setShowInbox(!showInbox)}
                className="relative p-1.5 rounded-md hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                title="通知"
              >
                {inboxUnread > 0 ? (
                  <BellDot className="h-4 w-4" />
                ) : (
                  <Bell className="h-4 w-4" />
                )}
                {inboxUnread > 0 && (
                  <span className="absolute -top-0.5 -right-0.5 text-[9px] bg-primary text-primary-foreground rounded-full h-3.5 w-3.5 flex items-center justify-center leading-none">
                    {inboxUnread > 9 ? "9+" : inboxUnread}
                  </span>
                )}
              </button>
            </div>

            {/* Messages */}
            <MessageList messages={messages} />

            {/* Input */}
            <MessageInput
              onSend={
                selectedType === "dm" ? handleSendDm : handleSendChannel
              }
              placeholder={
                selectedType === "dm"
                  ? "Type a message..."
                  : `Message #${currentChannel?.name ?? "channel"}...`
              }
            />
          </>
        ) : (
          <div className="flex-1 flex flex-col items-center justify-center text-muted-foreground gap-2">
            <MessageCircle className="h-10 w-10 text-muted-foreground/30" />
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
