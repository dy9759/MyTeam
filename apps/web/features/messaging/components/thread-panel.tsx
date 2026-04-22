"use client";

import { useEffect, useRef, useState } from "react";
import { api } from "@/shared/api";
import type { Message, Thread } from "@/shared/types/messaging";
import { MessageList } from "./message-list";
import { MessageInput } from "./message-input";
import { X, MessageSquare } from "lucide-react";
import { toast } from "sonner";

interface ThreadPanelProps {
  threadId: string;
  channelId: string;
  onClose: () => void;
}

export function ThreadPanel({ threadId, channelId, onClose }: ThreadPanelProps) {
  const [thread, setThread] = useState<Thread | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [loading, setLoading] = useState(true);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    async function loadThread() {
      try {
        const t = await api.getThread(threadId);
        setThread(t);
      } catch {
        // Thread info may not be available yet
      }
    }
    loadThread();
  }, [threadId]);

  useEffect(() => {
    if (pollRef.current) clearInterval(pollRef.current);

    async function loadMessages() {
      try {
        const res = await api.getThreadMessages(threadId);
        const list = Array.isArray(res) ? res : Array.isArray(res?.messages) ? res.messages : [];
        setMessages(list);
        setLoading(false);
      } catch {
        setLoading(false);
      }
    }

    loadMessages();
    pollRef.current = setInterval(loadMessages, 3000);
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [threadId]);

  async function handleSend(content: string) {
    try {
      const msg = await api.sendThreadMessage(threadId, content);
      setMessages((prev) => [...prev, msg]);
    } catch {
      toast.error("回复失败");
    }
  }

  return (
    <div className="w-80 border-l border-border flex flex-col h-full bg-card">
      {/* Header */}
      <div className="px-4 py-3 border-b border-border flex items-center justify-between shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <MessageSquare className="h-4 w-4 text-primary shrink-0" />
          <div className="min-w-0">
            <h3 className="font-medium text-[14px] text-foreground truncate">
              {thread?.title ?? "讨论串"}
            </h3>
            <span className="text-[12px] text-muted-foreground">
              {thread?.reply_count ?? messages.length} 条回复
            </span>
          </div>
        </div>
        <button
          onClick={onClose}
          className="p-1 rounded-[4px] hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors"
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      {/* Messages */}
      {loading ? (
        <div className="flex-1 flex items-center justify-center text-[13px] text-muted-foreground">
          加载中...
        </div>
      ) : (
        <MessageList messages={messages} collapseThreads={false} />
      )}

      {/* Input */}
      <MessageInput onSend={handleSend} placeholder="回复讨论串..." />
    </div>
  );
}
