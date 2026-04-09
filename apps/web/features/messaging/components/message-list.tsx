"use client";
import { MessageSquare } from "lucide-react";

interface MessageListProps {
  messages: Array<{
    id: string;
    sender_id: string;
    sender_type: string;
    content: string;
    created_at: string;
    file_name?: string;
    file_id?: string;
    reply_count?: number;
    is_impersonated?: boolean;
  }>;
  currentUserId?: string;
  onOpenThread?: (messageId: string) => void;
}

export function MessageList({ messages, currentUserId, onOpenThread }: MessageListProps) {
  return (
    <div className="flex-1 overflow-auto p-4 space-y-1">
      {messages.map((msg) => (
        <div
          key={msg.id}
          className="group relative px-3 py-2 rounded-[6px] hover:bg-accent/50 transition-colors"
        >
          <div className="flex items-start gap-3">
            {/* Avatar */}
            <div className="w-8 h-8 rounded-full bg-secondary flex items-center justify-center text-[12px] font-medium text-secondary-foreground shrink-0 mt-0.5">
              {msg.sender_id.slice(0, 2).toUpperCase()}
            </div>

            <div className="flex-1 min-w-0">
              {/* Header: sender + time */}
              <div className="flex items-center gap-2 mb-0.5">
                <span className="text-[13px] font-medium text-foreground">
                  {msg.sender_id.slice(0, 12)}
                </span>
                {msg.is_impersonated && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-primary/10 text-primary">附身</span>
                )}
                <span className="text-[11px] text-muted-foreground">
                  {new Date(msg.created_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                </span>
              </div>

              {/* Content */}
              <div className="text-[14px] text-foreground leading-relaxed">{msg.content}</div>

              {/* File attachment */}
              {msg.file_name && (
                <div className="mt-1 flex items-center gap-1.5 text-[12px] text-muted-foreground bg-secondary rounded-[4px] px-2 py-1 w-fit">
                  📎 {msg.file_name}
                </div>
              )}

              {/* Thread indicator */}
              {(msg.reply_count ?? 0) > 0 && onOpenThread && (
                <button
                  onClick={() => onOpenThread(msg.id)}
                  className="mt-1.5 flex items-center gap-1 text-[12px] text-primary hover:underline"
                >
                  <MessageSquare className="h-3 w-3" />
                  {msg.reply_count} 条回复
                </button>
              )}
            </div>

            {/* Hover action: reply in thread */}
            {onOpenThread && (
              <div className="opacity-0 group-hover:opacity-100 transition-opacity shrink-0 flex items-center gap-0.5">
                <button
                  onClick={() => onOpenThread(msg.id)}
                  className="p-1 rounded-[4px] hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors"
                  title="回复讨论串"
                >
                  <MessageSquare className="h-3.5 w-3.5" />
                </button>
              </div>
            )}
          </div>
        </div>
      ))}
      {messages.length === 0 && (
        <div className="text-center text-muted-foreground mt-8 text-[13px]">
          暂无消息
        </div>
      )}
    </div>
  );
}
