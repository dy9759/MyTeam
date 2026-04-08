"use client";

import { useEffect, useRef, useState } from "react";
import { api } from "@/shared/api";
import type { Message, Thread } from "@/shared/types/messaging";
import { MessageList } from "./message-list";
import { MessageInput } from "./message-input";
import { Button } from "@/components/ui/button";
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
        setMessages(res.messages);
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
      toast.error("Failed to send reply");
    }
  }

  return (
    <div className="w-80 border-l flex flex-col h-full bg-background">
      {/* Header */}
      <div className="p-4 border-b flex items-center justify-between shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <MessageSquare className="h-4 w-4 text-muted-foreground shrink-0" />
          <div className="min-w-0">
            <h3 className="font-medium text-sm truncate">
              {thread?.title ?? "Thread"}
            </h3>
            <span className="text-xs text-muted-foreground">
              {thread?.reply_count ?? messages.length} replies
            </span>
          </div>
        </div>
        <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" onClick={onClose}>
          <X className="h-4 w-4" />
        </Button>
      </div>

      {/* Messages */}
      {loading ? (
        <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
          Loading...
        </div>
      ) : (
        <MessageList messages={messages} />
      )}

      {/* Input */}
      <MessageInput onSend={handleSend} placeholder="Reply in thread..." />
    </div>
  );
}
