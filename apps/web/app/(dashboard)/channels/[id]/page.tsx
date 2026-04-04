"use client";

import { useEffect, useRef, useState } from "react";
import { useParams } from "next/navigation";
import { useChannelStore } from "@/features/channels/store";
import { MessageList } from "@/features/messaging/components/message-list";
import { MessageInput } from "@/features/messaging/components/message-input";
import { api } from "@/shared/api";
import type { Message } from "@/shared/types/messaging";
import { toast } from "sonner";

export default function ChannelDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { currentChannel, members, fetchChannel, fetchMembers, leaveChannel } = useChannelStore();
  const [messages, setMessages] = useState<Message[]>([]);
  const [showMembers, setShowMembers] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (!id) return;
    fetchChannel(id);
    fetchMembers(id);
  }, [id, fetchChannel, fetchMembers]);

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
    return () => { if (pollRef.current) clearInterval(pollRef.current); };
  }, [id]);

  async function handleSend(content: string) {
    try {
      const msg = await api.sendMessage({ channel_id: id, content });
      setMessages((prev) => [...prev, msg]);
    } catch {
      toast.error("Failed to send message");
    }
  }

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
              <p className="text-sm text-muted-foreground">{currentChannel.description}</p>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setShowMembers(!showMembers)}
              className="px-3 py-1 text-sm border rounded-md hover:bg-muted/50"
            >
              {members.length} member{members.length !== 1 ? "s" : ""}
            </button>
            <button
              onClick={() => id && leaveChannel(id)}
              className="px-3 py-1 text-sm border rounded-md hover:bg-muted/50 text-red-600"
            >
              Leave
            </button>
          </div>
        </div>

        {/* Messages */}
        <MessageList messages={messages} />
        <MessageInput onSend={handleSend} placeholder="Message channel..." />
      </div>

      {/* Members sidebar */}
      {showMembers && (
        <div className="w-60 border-l flex flex-col">
          <div className="p-4 border-b">
            <h3 className="font-medium text-sm">Members</h3>
          </div>
          <div className="flex-1 overflow-auto p-2">
            {members.map((m) => (
              <div key={`${m.member_id}-${m.member_type}`} className="p-2 text-sm">
                <span className="font-medium">{m.member_id.slice(0, 12)}</span>
                <span className="text-xs text-muted-foreground ml-1">({m.member_type})</span>
              </div>
            ))}
            {members.length === 0 && (
              <div className="text-sm text-muted-foreground p-2 text-center">No members</div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
