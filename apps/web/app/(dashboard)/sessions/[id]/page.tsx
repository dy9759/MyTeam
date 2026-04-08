"use client";

import { useEffect, useRef, useState } from "react";
import { useParams } from "next/navigation";
import { useSessionStore } from "@/features/sessions/store";
import { MessageList } from "@/features/messaging/components/message-list";
import { MessageInput } from "@/features/messaging/components/message-input";
import { api } from "@/shared/api";
import { toast } from "sonner";

const STATUS_COLORS: Record<string, string> = {
  active: "bg-green-100 text-green-700",
  waiting: "bg-yellow-100 text-yellow-700",
  completed: "bg-blue-100 text-blue-700",
  failed: "bg-red-100 text-red-700",
  archived: "bg-gray-100 text-gray-700",
};

export default function SessionDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { currentSession, sessionMessages, fetchSession, fetchSessionMessages } = useSessionStore();
  const [showContext, setShowContext] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (!id) return;
    fetchSession(id);
    fetchSessionMessages(id);
  }, [id, fetchSession, fetchSessionMessages]);

  useEffect(() => {
    if (pollRef.current) clearInterval(pollRef.current);
    if (!id) return;
    pollRef.current = setInterval(() => {
      fetchSessionMessages(id);
    }, 3000);
    return () => { if (pollRef.current) clearInterval(pollRef.current); };
  }, [id, fetchSessionMessages]);

  async function handleSend(content: string) {
    if (!id) return;
    try {
      await api.sendMessage({ session_id: id, content });
      fetchSessionMessages(id);
    } catch {
      toast.error("发送消息失败");
    }
  }

  const ctx = currentSession?.context;
  const isTerminal = currentSession?.status === "completed" || currentSession?.status === "failed" || currentSession?.status === "archived";

  return (
    <div className="flex h-full">
      <div className="flex-1 flex flex-col">
        {/* Header */}
        <div className="p-4 border-b flex items-center justify-between">
          <div>
            <h2 className="font-semibold text-lg">{currentSession?.title || "..."}</h2>
            <div className="flex items-center gap-2 mt-1">
              {currentSession && (
                <span className={`text-xs px-2 py-0.5 rounded ${STATUS_COLORS[currentSession.status] ?? "bg-gray-100"}`}>
                  {currentSession.status}
                </span>
              )}
              {currentSession && (
                <span className="text-xs text-muted-foreground">
                  轮次 {currentSession.current_turn}/{currentSession.max_turns || "\u221E"}
                </span>
              )}
            </div>
          </div>
          <button
            onClick={() => setShowContext(!showContext)}
            className="px-3 py-1 text-sm border rounded-md hover:bg-muted/50"
          >
            {showContext ? "隐藏上下文" : "上下文"}
          </button>
        </div>

        {/* Messages (turn-by-turn) */}
        <MessageList messages={sessionMessages} />
        <MessageInput
          onSend={handleSend}
          placeholder="向会话发送消息..."
          disabled={isTerminal}
        />
      </div>

      {/* Context sidebar */}
      {showContext && ctx && (
        <div className="w-72 border-l flex flex-col overflow-auto">
          <div className="p-4 border-b">
            <h3 className="font-medium text-sm">会话上下文</h3>
          </div>
          <div className="p-4 space-y-4 text-sm">
            {ctx.topic && (
              <div>
                <div className="font-medium text-muted-foreground mb-1">主题</div>
                <div>{ctx.topic}</div>
              </div>
            )}
            {ctx.summary && (
              <div>
                <div className="font-medium text-muted-foreground mb-1">摘要</div>
                <div>{ctx.summary}</div>
              </div>
            )}
            {ctx.decisions && ctx.decisions.length > 0 && (
              <div>
                <div className="font-medium text-muted-foreground mb-1">决策</div>
                <ul className="list-disc pl-4 space-y-1">
                  {ctx.decisions.map((d, i) => (
                    <li key={i}>
                      {d.decision}
                      <span className="text-xs text-muted-foreground ml-1">- {d.by}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {ctx.files && ctx.files.length > 0 && (
              <div>
                <div className="font-medium text-muted-foreground mb-1">文件</div>
                <ul className="space-y-1">
                  {ctx.files.map((f, i) => (
                    <li key={i} className="text-xs bg-muted px-2 py-1 rounded">{f.name}</li>
                  ))}
                </ul>
              </div>
            )}
            {ctx.code_snippets && ctx.code_snippets.length > 0 && (
              <div>
                <div className="font-medium text-muted-foreground mb-1">代码片段</div>
                {ctx.code_snippets.map((s, i) => (
                  <div key={i} className="mb-2">
                    <div className="text-xs text-muted-foreground">{s.description} ({s.language})</div>
                    <pre className="text-xs bg-muted p-2 rounded mt-1 overflow-auto">{s.code}</pre>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
