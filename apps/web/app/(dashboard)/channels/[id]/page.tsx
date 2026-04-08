"use client";

import { useEffect, useRef, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { useChannelStore } from "@/features/channels/store";
import { useMessagingStore } from "@/features/messaging/store";
import { MessageList } from "@/features/messaging/components/message-list";
import { MessageInput } from "@/features/messaging/components/message-input";
import { ThreadPanel } from "@/features/messaging/components/thread-panel";
import { ImpersonationIndicator } from "@/features/messaging/components/impersonation-indicator";
import { api } from "@/shared/api";
import type { Message } from "@/shared/types/messaging";
import { toast } from "sonner";
import { MessageSquareText, FolderGit2 } from "lucide-react";

export default function ChannelDetailPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const { currentChannel, members, fetchChannel, fetchMembers, leaveChannel } = useChannelStore();
  const { fetchThreads, threads } = useMessagingStore();
  const [messages, setMessages] = useState<Message[]>([]);
  const [showMembers, setShowMembers] = useState(false);
  const [activeTab, setActiveTab] = useState<"messages" | "files">("messages");
  const [activeThreadId, setActiveThreadId] = useState<string | null>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (!id) return;
    fetchChannel(id);
    fetchMembers(id);
    fetchThreads(id);
  }, [id, fetchChannel, fetchMembers, fetchThreads]);

  useEffect(() => {
    if (pollRef.current) clearInterval(pollRef.current);
    if (!id) return;

    async function loadMessages() {
      try {
        const res = await api.getChannelMessages(id);
        setMessages(res.messages);
      } catch {
        // silently fail on poll
      }
    }

    loadMessages();
    pollRef.current = setInterval(loadMessages, 3000);
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [id]);

  async function handleSend(content: string) {
    try {
      const msg = await api.sendMessage({ channel_id: id, content });
      setMessages((prev) => [...prev, msg]);
    } catch {
      toast.error("Failed to send message");
    }
  }

  function handleMessageClick(msg: Message) {
    if (msg.reply_count && msg.reply_count > 0) {
      setActiveThreadId(msg.id);
    }
  }

  function handleReplyInThread(msgId: string) {
    setActiveThreadId(msgId);
  }

  const fileMessages = messages.filter(
    (m) => m.file_name || m.content_type === "file",
  );

  // Build a set of message IDs that have threads
  const threadMessageIds = new Set(threads.map((t) => t.id));

  return (
    <div className="flex h-full">
      <div className="flex-1 flex flex-col">
        {/* Header */}
        <div className="p-4 border-b flex items-center justify-between">
          <div>
            <h2 className="font-semibold text-lg">
              #{currentChannel?.name || "..."}
            </h2>
            {currentChannel?.description && (
              <p className="text-sm text-muted-foreground">
                {currentChannel.description}
              </p>
            )}
          </div>
          <div className="flex items-center gap-2">
            <div className="flex gap-2 mr-2">
              <button
                onClick={() => setActiveTab("messages")}
                className={`px-3 py-1 text-sm rounded ${
                  activeTab === "messages"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted"
                }`}
              >
                Messages
              </button>
              <button
                onClick={() => setActiveTab("files")}
                className={`px-3 py-1 text-sm rounded ${
                  activeTab === "files"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted"
                }`}
              >
                Files
              </button>
            </div>
            <button
              onClick={() => {
                if (!id || !currentChannel) return;
                const params = new URLSearchParams();
                params.set("from_channel", id);
                params.set("channel_name", currentChannel.name ?? "");
                router.push(`/projects?${params.toString()}`);
              }}
              className="px-3 py-1 text-sm border rounded-md hover:bg-muted/50 flex items-center gap-1"
              title="Create Project from This Channel"
            >
              <FolderGit2 className="h-3.5 w-3.5" />
              Create Project
            </button>
            <button
              onClick={() => setShowMembers(!showMembers)}
              className="px-3 py-1 text-sm border rounded-md hover:bg-muted/50"
            >
              {members.length} member{members.length !== 1 ? "s" : ""}
            </button>
            <button
              onClick={() => id && leaveChannel(id)}
              className="px-3 py-1 text-sm border rounded-md hover:bg-muted/50 text-destructive"
            >
              Leave
            </button>
          </div>
        </div>

        {/* Content */}
        {activeTab === "files" ? (
          <div className="p-4 space-y-2 flex-1 overflow-auto">
            {fileMessages.map((m) => (
              <div
                key={m.id}
                className="flex items-center gap-3 p-3 border rounded-lg"
              >
                <div className="flex-1">
                  <div className="font-medium">{m.file_name ?? "File"}</div>
                  <div className="text-xs text-muted-foreground">
                    Shared by {m.sender_id?.slice(0, 12)} &middot;{" "}
                    {new Date(m.created_at).toLocaleString()}
                  </div>
                </div>
              </div>
            ))}
            {fileMessages.length === 0 && (
              <div className="text-center text-muted-foreground py-8">
                No files shared in this channel yet
              </div>
            )}
          </div>
        ) : (
          <>
            {/* Enhanced message list with thread support */}
            <div className="flex-1 overflow-auto p-4 space-y-3">
              {messages.map((msg) => (
                <div
                  key={msg.id}
                  className={`flex ${msg.sender_id === undefined ? "justify-start" : "justify-start"}`}
                >
                  <div className="max-w-[70%] px-4 py-2 rounded-lg text-sm bg-muted">
                    <div className="text-xs opacity-70 mb-1 flex items-center gap-1">
                      {msg.sender_id.slice(0, 12)}
                      {msg.is_impersonated && <ImpersonationIndicator />}
                      {" \u00B7 "}
                      {new Date(msg.created_at).toLocaleTimeString()}
                    </div>
                    <div>{msg.content}</div>
                    {msg.file_name && (
                      <div className="mt-1 flex items-center gap-1 text-xs">
                        {msg.file_name}
                      </div>
                    )}
                    {/* Thread indicator */}
                    <div className="flex items-center gap-2 mt-1">
                      {(msg.reply_count ?? 0) > 0 || threadMessageIds.has(msg.id) ? (
                        <button
                          onClick={() => handleMessageClick(msg)}
                          className="text-xs text-primary hover:underline flex items-center gap-1"
                        >
                          <MessageSquareText className="h-3 w-3" />
                          {msg.reply_count ?? threads.find((t) => t.id === msg.id)?.reply_count ?? 0}{" "}
                          replies
                        </button>
                      ) : (
                        <button
                          onClick={() => handleReplyInThread(msg.id)}
                          className="text-xs text-muted-foreground hover:text-foreground opacity-0 group-hover:opacity-100 transition-opacity flex items-center gap-1"
                          title="Reply in thread"
                        >
                          <MessageSquareText className="h-3 w-3" />
                          Reply
                        </button>
                      )}
                    </div>
                  </div>
                </div>
              ))}
              {messages.length === 0 && (
                <div className="text-center text-muted-foreground mt-8">
                  No messages yet
                </div>
              )}
            </div>
            <MessageInput onSend={handleSend} placeholder="Message channel..." />
          </>
        )}
      </div>

      {/* Thread panel */}
      {activeThreadId && id && (
        <ThreadPanel
          threadId={activeThreadId}
          channelId={id}
          onClose={() => setActiveThreadId(null)}
        />
      )}

      {/* Members sidebar */}
      {showMembers && (
        <div className="w-60 border-l flex flex-col">
          <div className="p-4 border-b">
            <h3 className="font-medium text-sm">Members</h3>
          </div>
          <div className="flex-1 overflow-auto p-2">
            {members.map((m) => (
              <div
                key={`${m.member_id}-${m.member_type}`}
                className="p-2 text-sm"
              >
                <span className="font-medium">{m.member_id.slice(0, 12)}</span>
                <span className="text-xs text-muted-foreground ml-1">
                  ({m.member_type})
                </span>
              </div>
            ))}
            {members.length === 0 && (
              <div className="text-sm text-muted-foreground p-2 text-center">
                No members
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
