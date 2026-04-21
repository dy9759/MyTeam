"use client";

import { Badge } from "@/components/ui/badge";
import type { Agent, Subagent, Task, TaskStatus } from "@/shared/types";
import { Bot, Clock } from "lucide-react";

interface ExecutionStepCardProps {
  step: Task;
  agents: Agent[];
  // Optional subagent roster — required when PlanGenerator's default-
  // assign picks a subagent (post migration 069). Without it the card
  // falls back to the truncated UUID.
  subagents?: Subagent[];
}

const STATUS_CONFIG: Record<
  TaskStatus,
  { label: string; className: string }
> = {
  draft: { label: "草稿", className: "bg-muted text-muted-foreground" },
  ready: { label: "就绪", className: "bg-muted text-muted-foreground" },
  queued: { label: "排队中", className: "bg-muted text-muted-foreground" },
  assigned: {
    label: "已分配",
    className:
      "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200 animate-pulse",
  },
  running: {
    label: "执行中",
    className:
      "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200 animate-pulse",
  },
  needs_human: {
    label: "等待输入",
    className:
      "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200",
  },
  under_review: {
    label: "审核中",
    className:
      "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200",
  },
  needs_attention: {
    label: "需关注",
    className:
      "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200",
  },
  completed: {
    label: "已完成",
    className:
      "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
  },
  failed: {
    label: "已失败",
    className: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
  },
  cancelled: {
    label: "已取消",
    className: "bg-muted text-muted-foreground",
  },
};

function formatDuration(startedAt?: string | null, completedAt?: string | null): string | null {
  if (!startedAt) return null;
  const start = new Date(startedAt).getTime();
  const end = completedAt ? new Date(completedAt).getTime() : Date.now();
  const diffSec = Math.floor((end - start) / 1000);
  if (diffSec < 60) return `${diffSec}秒`;
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)}分${diffSec % 60}秒`;
  const hours = Math.floor(diffSec / 3600);
  const mins = Math.floor((diffSec % 3600) / 60);
  return `${hours}时${mins}分`;
}

function getAssigneeDisplayName(
  assigneeId: string | undefined,
  agents: Agent[],
  subagents: Subagent[],
): string {
  if (!assigneeId) return "未分配";
  const agent = agents.find((a) => a.id === assigneeId);
  if (agent) return agent.name;
  const subagent = subagents.find((s) => s.id === assigneeId);
  if (subagent) return subagent.name;
  return assigneeId.slice(0, 8);
}

export function ExecutionStepCard({
  step,
  agents,
  subagents = [],
}: ExecutionStepCardProps) {
  const statusConfig = STATUS_CONFIG[step.status] ?? STATUS_CONFIG.draft;
  const displayAgentId = step.actual_agent_id ?? step.primary_assignee_id ?? undefined;
  const agentName = getAssigneeDisplayName(displayAgentId, agents, subagents);
  const duration = formatDuration(step.started_at, step.completed_at);
  const maxRetries = step.retry_rule?.max_retries ?? 2;
  const currentRetry = step.current_retry ?? 0;

  // Step number badge color
  const numberClass =
    step.status === "completed"
      ? "bg-green-500 text-white"
      : step.status === "running" || step.status === "assigned"
        ? "bg-blue-500 text-white"
        : step.status === "failed"
          ? "bg-destructive text-destructive-foreground"
          : step.status === "needs_attention"
            ? "bg-orange-500 text-white"
            : "bg-muted text-muted-foreground";

  return (
    <div className="border rounded-lg p-4 space-y-3">
      <div className="flex items-start justify-between gap-3">
        {/* Left: step number + description */}
        <div className="flex items-start gap-3 min-w-0 flex-1">
          <div
            className={`w-8 h-8 rounded-full flex items-center justify-center text-sm font-bold shrink-0 ${numberClass}`}
          >
            {step.status === "completed"
              ? "\u2713"
              : step.status === "failed"
                ? "\u2715"
                : step.step_order}
          </div>
          <div className="min-w-0 flex-1">
            <div className="font-medium text-sm break-words line-clamp-3">
              {step.description ?? step.title}
            </div>
            <div className="flex flex-wrap items-center gap-2 mt-1.5 text-xs text-muted-foreground">
              {/* Agent */}
              <span className="inline-flex items-center gap-1">
                <Bot className="size-3" />
                {agentName}
              </span>
              {/* Duration */}
              {duration && (
                <span className="inline-flex items-center gap-1">
                  <Clock className="size-3" />
                  {duration}
                </span>
              )}
              {/* Retry count */}
              {currentRetry > 0 && (
                <span className="text-orange-600 dark:text-orange-400">
                  重试 {currentRetry}/{maxRetries}
                </span>
              )}
              {/* Dependencies */}
              {step.depends_on.length > 0 && (
                <span>依赖: {step.depends_on.join(", ")}</span>
              )}
            </div>
          </div>
        </div>

        {/* Right: status badge */}
        <div className="flex items-center gap-2 shrink-0">
          <Badge className={statusConfig.className} variant="outline">
            {statusConfig.label}
          </Badge>
        </div>
      </div>

      {/* Skills */}
      {step.required_skills?.length > 0 && (
        <div className="flex gap-1 flex-wrap">
          {step.required_skills.map((s) => (
            <span
              key={s}
              className="text-xs bg-primary/10 text-primary px-1.5 py-0.5 rounded"
            >
              {s}
            </span>
          ))}
        </div>
      )}

      {/* Error display */}
      {step.error && (
        <div className="text-xs bg-destructive/10 text-destructive p-2 rounded">
          {step.error}
        </div>
      )}

      {/* Result display */}
      {step.result != null && step.status === "completed" && (
        <div className="text-xs bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300 p-2 rounded">
          {typeof step.result === "string"
            ? step.result.slice(0, 200)
            : JSON.stringify(step.result).slice(0, 200)}
        </div>
      )}
    </div>
  );
}
