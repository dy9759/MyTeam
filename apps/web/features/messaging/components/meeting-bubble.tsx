"use client";

import { Mic, Loader2, CheckCircle2, AlertTriangle } from "lucide-react";
import type { ChannelMeeting } from "@/shared/types";
import { useWorkspaceStore } from "@/features/workspace";

interface MeetingBubbleProps {
  meeting: ChannelMeeting;
  onOpen: (id: string) => void;
}

function fmtDuration(sec?: number) {
  if (!sec) return "";
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  if (m === 0) return `${s}s`;
  return `${m}m ${s}s`;
}

function fmtTime(iso?: string) {
  if (!iso) return "";
  return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

export function MeetingBubble({ meeting, onOpen }: MeetingBubbleProps) {
  const { status, topic, started_at, ended_at, audio_duration, failure_reason, started_by } = meeting;
  const clickable = status === "completed";
  const summaryText = extractSummaryText(meeting.summary);

  // Resolve host display from workspace members so the row matches the
  // normal message bubble (avatar + name + time). Falls back to the
  // first two chars of the user id when the member is not yet loaded.
  const members = useWorkspaceStore((s) => s.members);
  const host = members.find((m) => m.user_id === started_by);
  const hostName = host?.name ?? started_by?.slice(0, 12) ?? "";
  const hostInitials = (host?.name ?? started_by ?? "").slice(0, 2).toUpperCase();

  return (
    <div
      role={clickable ? "button" : undefined}
      tabIndex={clickable ? 0 : undefined}
      onClick={clickable ? () => onOpen(meeting.id) : undefined}
      onKeyDown={
        clickable
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onOpen(meeting.id);
              }
            }
          : undefined
      }
      className={`group relative px-3 py-2 rounded-[6px] transition-colors ${
        clickable ? "hover:bg-accent/50 cursor-pointer" : "cursor-default"
      }`}
    >
      <div className="flex items-start gap-3">
        {/* Avatar — same shape as MessageList rows */}
        <div className="w-8 h-8 rounded-full bg-secondary flex items-center justify-center text-[12px] font-medium text-secondary-foreground shrink-0 mt-0.5">
          {hostInitials || "?"}
        </div>
        <div className="flex-1 min-w-0">
          {/* Header: sender name + timestamp */}
          <div className="flex items-center gap-2 mb-0.5">
            <span className="text-[13px] font-medium text-foreground">
              {hostName}
            </span>
            <span className="text-[11px] text-muted-foreground">
              {fmtTime(started_at)}
            </span>
          </div>
          {/* Meeting card body */}
          <div className="mt-0.5 rounded-md border border-border bg-card/60 px-3 py-2">
            <div className="flex items-start gap-2">
              <StatusIcon status={status} />
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 min-w-0">
                  <span className="text-[13px] font-medium text-foreground truncate">
                    {topic || "会议"}
                  </span>
                  <StatusBadge status={status} />
                </div>
                <div className="mt-0.5 text-[11px] text-muted-foreground truncate">
                  {fmtTime(started_at)}
                  {ended_at && ` — ${fmtTime(ended_at)}`}
                  {audio_duration != null && audio_duration > 0 && ` · ${fmtDuration(audio_duration)}`}
                </div>
                {status === "failed" && failure_reason && (
                  <div className="mt-1 text-[12px] text-destructive break-words">
                    {failure_reason}
                  </div>
                )}
                {status === "completed" && summaryText && (
                  <div className="mt-1 text-[12px] text-muted-foreground line-clamp-2 break-words">
                    {summaryText}
                  </div>
                )}
                {clickable && (
                  <div className="mt-1 text-[11px] text-primary opacity-0 group-hover:opacity-100 transition-opacity">
                    点击查看转写 / 总结 / 笔记
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function StatusIcon({ status }: { status: ChannelMeeting["status"] }) {
  const base = "h-6 w-6 rounded-full flex items-center justify-center shrink-0";
  if (status === "recording") {
    return (
      <div className={`${base} bg-primary/15 text-primary`}>
        <Mic className="h-3.5 w-3.5 animate-pulse" />
      </div>
    );
  }
  if (status === "processing") {
    return (
      <div className={`${base} bg-primary/10 text-primary`}>
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
      </div>
    );
  }
  if (status === "failed") {
    return (
      <div className={`${base} bg-destructive/10 text-destructive`}>
        <AlertTriangle className="h-3.5 w-3.5" />
      </div>
    );
  }
  return (
    <div className={`${base} bg-primary/10 text-primary`}>
      <CheckCircle2 className="h-3.5 w-3.5" />
    </div>
  );
}

function StatusBadge({ status }: { status: ChannelMeeting["status"] }) {
  const label: Record<ChannelMeeting["status"], string> = {
    recording: "录制中",
    processing: "转写中",
    completed: "已完成",
    failed: "失败",
  };
  const tone: Record<ChannelMeeting["status"], string> = {
    recording: "bg-primary/15 text-primary",
    processing: "bg-primary/10 text-primary",
    completed: "bg-secondary text-secondary-foreground",
    failed: "bg-destructive/10 text-destructive",
  };
  return (
    <span className={`text-[10px] px-1.5 py-0.5 rounded ${tone[status]} shrink-0`}>
      {label[status]}
    </span>
  );
}

// summary is an opaque JSON blob (Doubao payload). Best-effort pull out
// the most common shapes so the bubble can hint at content without
// needing to open the full panel.
export function extractSummaryText(summary?: Record<string, unknown>): string {
  if (!summary) return "";
  const candidates: Array<unknown> = [
    (summary as { summary?: unknown }).summary,
    (summary as { text?: unknown }).text,
    (summary as { content?: unknown }).content,
    (summary as { tldr?: unknown }).tldr,
    (summary as { abstract?: unknown }).abstract,
  ];
  for (const c of candidates) {
    if (typeof c === "string" && c.trim()) return c.trim();
  }
  return "";
}
