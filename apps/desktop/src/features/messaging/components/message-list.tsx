import { useEffect, useRef } from "react";
import type { Message } from "@myteam/client-core";

interface Props {
  messages: Message[];
  resolveName: (senderId: string, senderType: "member" | "agent") => string;
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

export function MessageList({ messages, resolveName }: Props) {
  const bottomRef = useRef<HTMLDivElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    if (nearBottom && bottomRef.current?.scrollIntoView) {
      bottomRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [messages.length]);

  if (messages.length === 0) {
    return (
      <div className="flex h-full items-center justify-center rounded-3xl border border-dashed border-border/70 bg-background/50 px-4 py-10 text-center text-sm text-muted-foreground">
        No messages yet. Say something to get started.
      </div>
    );
  }

  return (
    <div ref={scrollRef} className="flex max-h-full flex-col gap-3 overflow-y-auto">
      {messages.map((msg) => (
        <article
          key={msg.id}
          className="rounded-3xl border border-border/70 bg-background/70 px-4 py-3"
        >
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span>{msg.sender_type === "agent" ? "🤖" : "👤"}</span>
            <span className="font-medium text-foreground">
              {resolveName(msg.sender_id, msg.sender_type)}
            </span>
            <span>{formatTime(msg.created_at)}</span>
          </div>
          <p className="mt-2 whitespace-pre-wrap text-sm leading-6 text-foreground">
            {msg.content}
          </p>
        </article>
      ))}
      <div ref={bottomRef} />
    </div>
  );
}
