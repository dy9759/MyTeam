"use client";

import { useMemo, useState } from "react";
import { Search, X } from "lucide-react";

import type { Message } from "@/shared/types/messaging";
import { useWorkspaceStore } from "@/features/workspace";

interface ChannelSearchPanelProps {
  messages: Message[];
  onClose: () => void;
  onJumpToMessage?: (messageId: string) => void;
}

export function ChannelSearchPanel({ messages, onClose, onJumpToMessage }: ChannelSearchPanelProps) {
  const [query, setQuery] = useState("");
  const members = useWorkspaceStore((s) => s.members);
  const resolveName = (senderId: string) =>
    members.find((m) => m.user_id === senderId)?.name ?? senderId.slice(0, 8);

  const results = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return [] as Message[];
    return messages
      .filter((m) => (m.content ?? "").toLowerCase().includes(q))
      .slice(0, 200);
  }, [messages, query]);

  return (
    <div className="w-[360px] border-l border-border flex flex-col h-full bg-card">
      <div className="px-4 py-3 border-b border-border flex items-center justify-between shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <Search className="h-4 w-4 text-primary shrink-0" />
          <h3 className="font-medium text-[14px] text-foreground">搜索聊天记录</h3>
        </div>
        <button
          type="button"
          onClick={onClose}
          className="p-1 rounded-[4px] hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors"
          title="关闭"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
      <div className="px-4 py-3 border-b border-border shrink-0">
        <div className="relative">
          <Search className="h-3.5 w-3.5 absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground" />
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="输入关键字..."
            className="w-full pl-7 pr-2 h-8 rounded-md text-[13px] bg-secondary/60 border border-transparent focus:border-primary/60 focus:bg-background outline-none"
          />
        </div>
        <div className="mt-1 text-[11px] text-muted-foreground">
          {query.trim() ? `${results.length} 条匹配` : `当前频道共 ${messages.length} 条消息`}
        </div>
      </div>
      <div className="flex-1 overflow-auto">
        {query.trim() === "" ? (
          <div className="p-6 text-[13px] text-muted-foreground text-center">
            输入关键字搜索当前频道的消息。
          </div>
        ) : results.length === 0 ? (
          <div className="p-6 text-[13px] text-muted-foreground text-center">
            没有匹配的消息。
          </div>
        ) : (
          <ul className="p-2 space-y-1">
            {results.map((msg) => (
              <li key={msg.id}>
                <button
                  type="button"
                  onClick={() => onJumpToMessage?.(msg.id)}
                  className="w-full text-left rounded-md px-2 py-1.5 hover:bg-accent/50 transition-colors"
                >
                  <div className="flex items-center gap-2 text-[11px] text-muted-foreground mb-0.5">
                    <span className="font-medium text-foreground">{resolveName(msg.sender_id)}</span>
                    <span>{new Date(msg.created_at).toLocaleString()}</span>
                  </div>
                  <HighlightedSnippet text={msg.content} query={query} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function HighlightedSnippet({ text, query }: { text: string; query: string }) {
  const q = query.trim();
  if (!q) return <span className="text-[13px] text-foreground">{text}</span>;
  const lower = text.toLowerCase();
  const idx = lower.indexOf(q.toLowerCase());
  if (idx < 0) return <span className="text-[13px] text-foreground">{text}</span>;
  const radius = 40;
  const start = Math.max(0, idx - radius);
  const end = Math.min(text.length, idx + q.length + radius);
  const pre = (start > 0 ? "…" : "") + text.slice(start, idx);
  const hit = text.slice(idx, idx + q.length);
  const post = text.slice(idx + q.length, end) + (end < text.length ? "…" : "");
  return (
    <div className="text-[13px] text-foreground break-words leading-relaxed">
      {pre}
      <mark className="bg-primary/20 text-foreground rounded px-0.5">{hit}</mark>
      {post}
    </div>
  );
}
