"use client";
import { useState } from "react";
import { Check, CheckCheck, ChevronDown, ChevronRight, MessageSquare, Send } from "lucide-react";

import { useMessageSelectionStore } from "@/features/messaging/stores/selection-store";
import { useFileViewerStore } from "@/features/messaging/stores/file-viewer-store";
import { useWorkspaceStore } from "@/features/workspace";
import { MemoizedMarkdown } from "@/components/markdown";
import { MeetingBubble } from "./meeting-bubble";
import type { ChannelMeeting } from "@/shared/types";

type MessageStatus = "sent" | "delivered" | "read";

type MessageItem = {
  id: string;
  sender_id: string;
  sender_type: string;
  content: string;
  created_at: string;
  file_name?: string;
  file_id?: string;
  file_size?: number;
  file_content_type?: string;
  reply_count?: number;
  is_impersonated?: boolean;
  status?: MessageStatus;
  // Thread bookkeeping — server stamps thread_id = parent_message_id
  // on every row that belongs to a thread (root + replies both). A
  // row is a reply iff thread_id is set AND not equal to the row's
  // own id. The main channel view collapses replies under their root
  // via the inline expand chip; ThreadPanel drills in for deep focus.
  thread_id?: string;
};

interface MessageListProps {
  messages: MessageItem[];
  currentUserId?: string;
  onOpenThread?: (messageId: string) => void;
  typingUsers?: string[];
  selectionEnabled?: boolean;
  meetings?: ChannelMeeting[];
  onOpenMeeting?: (meetingId: string) => void;
  collapseThreads?: boolean;
  // Extra highlight overlay for rows whose panel is open (thread panel
  // and/or file viewer). Works alongside the checkbox-driven selection
  // set so multiple rows can stay highlighted at once.
  highlightedIds?: Set<string>;
  // Posts a reply into the thread rooted at the given message. Kept as
  // a callback so the session page owns the createThread + refresh
  // choreography.
  onReplyInThread?: (rootMessageId: string, content: string) => Promise<void>;
  // Thread rows for the active channel. Needed because reply messages
  // store thread_id = thread.id (a separate UUID), not the root
  // message id, so grouping replies under their parent requires this
  // mapping to translate thread_id back to root_message_id.
  threads?: Array<{ id: string; root_message_id?: string | null }>;
}

function MessageStatusIndicator({ status }: { status?: MessageStatus }) {
  if (!status) return null;
  if (status === "read") {
    return (
      <span title="已读" className="inline-flex items-center text-primary">
        <CheckCheck className="h-3 w-3" />
      </span>
    );
  }
  if (status === "delivered") {
    return (
      <span title="已送达" className="inline-flex items-center text-muted-foreground">
        <CheckCheck className="h-3 w-3" />
      </span>
    );
  }
  return (
    <span title="已发送" className="inline-flex items-center text-muted-foreground">
      <Check className="h-3 w-3" />
    </span>
  );
}

export function MessageList({
  messages,
  currentUserId,
  onOpenThread,
  typingUsers = [],
  selectionEnabled = false,
  meetings = [],
  onOpenMeeting,
  collapseThreads = true,
  highlightedIds,
  onReplyInThread,
  threads,
}: MessageListProps) {
  const members = useWorkspaceStore((s) => s.members);
  const selectedIds = useMessageSelectionStore((s) => s.selectedIds);
  const toggleSelection = useMessageSelectionStore((s) => s.toggle);
  const openFile = useFileViewerStore((s) => s.open);
  const [expandedThreadIds, setExpandedThreadIds] = useState<Set<string>>(new Set());

  const resolveDisplayName = (senderId: string): string => {
    const member = members.find((m) => m.user_id === senderId);
    return member?.name ?? senderId.slice(0, 12);
  };

  // Resolve reply.thread_id → root_message_id. Thread rows use their
  // own UUID for thread.id (distinct from root_message_id), and replies
  // store message.thread_id = thread.id. Without this map, grouping
  // keyed by thread_id would never hit the root's msg.id.
  const threadIdToRoot = new Map<string, string>();
  for (const t of threads ?? []) {
    if (t.id && t.root_message_id) {
      threadIdToRoot.set(t.id, t.root_message_id);
    }
  }
  const resolveRootId = (rawThreadId: string): string =>
    threadIdToRoot.get(rawThreadId) ?? rawThreadId;

  // Group replies by root message id so each root can render its full
  // thread inline on expand. Replies come in on the same channel list
  // response (ListChannelMessages returns everything), so we can fan
  // them out here without a separate fetch.
  const repliesByRoot = new Map<string, MessageItem[]>();
  for (const m of messages) {
    if (m.thread_id && m.thread_id !== m.id) {
      const rootId = resolveRootId(m.thread_id);
      const list = repliesByRoot.get(rootId) ?? [];
      list.push(m);
      repliesByRoot.set(rootId, list);
    }
  }
  // Ensure replies inside each thread are ordered chronologically —
  // the channel timeline ordering is good, but guarantee it regardless.
  for (const [, arr] of repliesByRoot) {
    arr.sort((a, b) => {
      const ta = Date.parse(a.created_at);
      const tb = Date.parse(b.created_at);
      return (Number.isNaN(ta) ? 0 : ta) - (Number.isNaN(tb) ? 0 : tb);
    });
  }
  const rootMessages = collapseThreads
    ? messages.filter((m) => !m.thread_id || m.thread_id === m.id)
    : messages;

  type TimelineItem =
    | { kind: "msg"; ts: string; data: MessageItem }
    | { kind: "meeting"; ts: string; data: ChannelMeeting };
  const timeline: TimelineItem[] = [
    ...rootMessages.map<TimelineItem>((m) => ({ kind: "msg", ts: m.created_at, data: m })),
    ...meetings.map<TimelineItem>((m) => ({ kind: "meeting", ts: m.started_at, data: m })),
  ].sort((a, b) => {
    const ta = Date.parse(a.ts);
    const tb = Date.parse(b.ts);
    const aNaN = Number.isNaN(ta);
    const bNaN = Number.isNaN(tb);
    if (aNaN && bNaN) return a.data.id < b.data.id ? -1 : a.data.id > b.data.id ? 1 : 0;
    if (aNaN) return 1;
    if (bNaN) return -1;
    if (ta !== tb) return ta - tb;
    return a.data.id < b.data.id ? -1 : a.data.id > b.data.id ? 1 : 0;
  });

  const toggleThread = (rootId: string) => {
    setExpandedThreadIds((prev) => {
      const next = new Set(prev);
      if (next.has(rootId)) next.delete(rootId);
      else next.add(rootId);
      return next;
    });
  };

  const formatTime = (iso: string) =>
    new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });

  const renderRow = (msg: MessageItem, options?: { compact?: boolean }) => {
    const compact = options?.compact ?? false;
    const isSelected = selectedIds.has(msg.id);
    const highlighted = highlightedIds?.has(msg.id) ?? false;
    const bg = isSelected
      ? "bg-primary/10"
      : highlighted
        ? "bg-accent/60"
        : "hover:bg-accent/50";
    return (
      <div className={`group relative px-3 py-2 rounded-[6px] transition-colors ${bg}`}>
        <div className="flex items-start gap-3">
          {!compact && (selectionEnabled || isSelected) && (
            <button
              type="button"
              onClick={() => toggleSelection(msg.id)}
              aria-label={isSelected ? "Deselect message" : "Select message"}
              aria-pressed={isSelected}
              className={`mt-1 w-4 h-4 rounded border flex items-center justify-center shrink-0 transition-colors ${
                isSelected
                  ? "bg-primary border-primary text-primary-foreground"
                  : "border-muted-foreground/40 hover:border-primary"
              }`}
            >
              {isSelected && <Check className="h-3 w-3" />}
            </button>
          )}

          <div
            className={`${compact ? "w-6 h-6 text-[10px]" : "w-8 h-8 text-[12px]"} rounded-full bg-secondary flex items-center justify-center font-medium text-secondary-foreground shrink-0 mt-0.5`}
          >
            {msg.sender_id.slice(0, 2).toUpperCase()}
          </div>

          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-0.5">
              <span className={`${compact ? "text-[12px]" : "text-[13px]"} font-medium text-foreground`}>
                {resolveDisplayName(msg.sender_id)}
              </span>
              {msg.is_impersonated && (
                <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-primary/10 text-primary">附身</span>
              )}
              <span className="text-[11px] text-muted-foreground">
                {formatTime(msg.created_at)}
              </span>
            </div>

            {msg.content && msg.content.trim() !== (msg.file_name ?? "").trim() && (
              <div className={`${compact ? "text-[13px]" : "text-[14px]"} text-foreground leading-relaxed prose prose-sm max-w-none dark:prose-invert`}>
                <MemoizedMarkdown mode="minimal">{msg.content}</MemoizedMarkdown>
              </div>
            )}

            {currentUserId === msg.sender_id && (
              <div className="mt-0.5 flex justify-end">
                <MessageStatusIndicator status={msg.status} />
              </div>
            )}

            {msg.file_name && (
              msg.file_id ? (
                <button
                  type="button"
                  onClick={() =>
                    openFile({
                      file_id: msg.file_id!,
                      file_name: msg.file_name!,
                      file_size: msg.file_size,
                      file_content_type: msg.file_content_type,
                    })
                  }
                  className="mt-1 flex items-center gap-1.5 text-[12px] text-muted-foreground bg-secondary hover:bg-secondary/80 hover:text-foreground rounded-[4px] px-2 py-1 w-fit transition-colors"
                  title="预览文件"
                >
                  <span>📎</span>
                  <span className="underline-offset-2 hover:underline">{msg.file_name}</span>
                </button>
              ) : (
                <div className="mt-1 flex items-center gap-1.5 text-[12px] text-muted-foreground bg-secondary rounded-[4px] px-2 py-1 w-fit">
                  📎 {msg.file_name}
                </div>
              )
            )}

            {!compact && (() => {
              const replies = repliesByRoot.get(msg.id) ?? [];
              const serverCount = msg.reply_count ?? 0;
              const count = Math.max(serverCount, replies.length);
              if (count === 0) return null;
              const isExpanded = expandedThreadIds.has(msg.id);
              const latest = replies[replies.length - 1];
              const snippet = latest?.content
                ? latest.content.replace(/\s+/g, " ").slice(0, 48)
                : "";
              return (
                <button
                  type="button"
                  onClick={() => toggleThread(msg.id)}
                  className={`mt-1.5 flex items-center gap-1.5 text-[12px] px-2 py-1 rounded-[4px] transition-colors w-fit ${
                    isExpanded
                      ? "bg-primary/10 text-primary"
                      : "text-primary hover:bg-primary/5"
                  }`}
                >
                  {isExpanded ? (
                    <ChevronDown className="h-3 w-3" />
                  ) : (
                    <ChevronRight className="h-3 w-3" />
                  )}
                  <MessageSquare className="h-3 w-3" />
                  <span>{count} 条回复</span>
                  {!isExpanded && latest && (
                    <span className="text-muted-foreground font-normal">
                      · 最新 {formatTime(latest.created_at)}
                      {snippet && ` · ${snippet}${latest.content.length > 48 ? "…" : ""}`}
                    </span>
                  )}
                </button>
              );
            })()}
          </div>

          {!compact && onOpenThread && (
            <div className="opacity-0 group-hover:opacity-100 transition-opacity shrink-0 flex items-center gap-0.5">
              <button
                onClick={() => onOpenThread(msg.id)}
                className="p-1 rounded-[4px] hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors"
                title="在侧栏打开讨论串"
              >
                <MessageSquare className="h-3.5 w-3.5" />
              </button>
            </div>
          )}
        </div>
      </div>
    );
  };

  return (
    <div className="flex-1 overflow-auto p-4 space-y-1">
      {timeline.map((item) => {
        if (item.kind === "meeting") {
          return (
            <MeetingBubble
              key={`meeting-${item.data.id}`}
              meeting={item.data}
              onOpen={(id) => onOpenMeeting?.(id)}
            />
          );
        }
        const msg = item.data;
        const isExpanded = expandedThreadIds.has(msg.id);
        const replies = repliesByRoot.get(msg.id) ?? [];
        return (
          <div key={msg.id}>
            {renderRow(msg)}
            {isExpanded && (
              <div className="mt-1 ml-12 border-l-2 border-primary/30 pl-3 space-y-1">
                {replies.map((r) => (
                  <div key={r.id}>{renderRow(r, { compact: true })}</div>
                ))}
                {onReplyInThread && (
                  <ThreadReplyInput
                    rootId={msg.id}
                    onSend={onReplyInThread}
                  />
                )}
              </div>
            )}
          </div>
        );
      })}
      {messages.length === 0 && (
        <div className="text-center text-muted-foreground mt-8 text-[13px]">
          暂无消息
        </div>
      )}
      {typingUsers.length > 0 && (
        <TypingIndicator typingUsers={typingUsers} resolveDisplayName={resolveDisplayName} />
      )}
    </div>
  );
}

function ThreadReplyInput({
  rootId,
  onSend,
}: {
  rootId: string;
  onSend: (rootId: string, content: string) => Promise<void>;
}) {
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);

  const submit = async () => {
    const content = draft.trim();
    if (!content || sending) return;
    setSending(true);
    try {
      await onSend(rootId, content);
      setDraft("");
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="mt-2 flex items-center gap-1.5">
      <input
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            void submit();
          }
        }}
        placeholder="回复讨论串…"
        disabled={sending}
        className="flex-1 rounded-[6px] border border-border bg-background px-2.5 py-1.5 text-[12px] focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-60"
      />
      <button
        type="button"
        onClick={() => void submit()}
        disabled={!draft.trim() || sending}
        className="flex items-center gap-1 rounded-[6px] bg-primary text-primary-foreground px-2 py-1.5 text-[12px] disabled:opacity-50"
      >
        <Send className="h-3 w-3" />
        发送
      </button>
    </div>
  );
}

function TypingIndicator({
  typingUsers,
  resolveDisplayName,
}: {
  typingUsers: string[];
  resolveDisplayName: (id: string) => string;
}) {
  const names = typingUsers.map(resolveDisplayName);
  let label: string;
  if (names.length === 1) {
    label = `${names[0]} is typing`;
  } else if (names.length === 2) {
    label = `${names[0]} and ${names[1]} are typing`;
  } else {
    label = `${names[0]} and ${names.length - 1} others are typing`;
  }

  return (
    <div className="flex items-center gap-1.5 px-4 py-1 text-xs text-muted-foreground">
      <span>{label}</span>
      <span className="inline-flex gap-0.5">
        <span className="animate-bounce [animation-delay:0ms]">&middot;</span>
        <span className="animate-bounce [animation-delay:150ms]">&middot;</span>
        <span className="animate-bounce [animation-delay:300ms]">&middot;</span>
      </span>
    </div>
  );
}
