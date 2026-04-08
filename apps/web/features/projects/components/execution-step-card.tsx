"use client";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { WorkflowStep, WorkflowStepStatus } from "@/shared/types/workflow";
import type { Agent } from "@/shared/types";
import {
  RefreshCw,
  UserRoundCog,
  Bot,
  Clock,
  ChevronDown,
} from "lucide-react";

interface ExecutionStepCardProps {
  step: WorkflowStep;
  agents: Agent[];
  workflowId: string;
  onRetry: (stepId: string) => void;
  onReplaceAgent: (stepId: string, agentId: string) => void;
}

const STATUS_CONFIG: Record<
  WorkflowStepStatus,
  { label: string; className: string }
> = {
  pending: { label: "待处理", className: "bg-muted text-muted-foreground" },
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
  waiting_input: {
    label: "等待输入",
    className:
      "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200",
  },
  blocked: {
    label: "已阻塞",
    className:
      "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200",
  },
  retrying: {
    label: "重试中",
    className:
      "bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200",
  },
  timeout: {
    label: "已超时",
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

function formatDuration(startedAt?: string, completedAt?: string): string | null {
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

function getAgentDisplayName(
  agentId: string | undefined,
  agents: Agent[],
): string {
  if (!agentId) return "未分配";
  const agent = agents.find((a) => a.id === agentId);
  return agent?.name ?? agentId.slice(0, 8);
}

export function ExecutionStepCard({
  step,
  agents,
  workflowId,
  onRetry,
  onReplaceAgent,
}: ExecutionStepCardProps) {
  const statusConfig = STATUS_CONFIG[step.status] ?? STATUS_CONFIG.pending;
  const displayAgentId = step.actual_agent_id ?? step.agent_id;
  const agentName = getAgentDisplayName(displayAgentId, agents);
  const duration = formatDuration(step.started_at, step.completed_at);
  const maxRetries = step.retry_rule?.max_retries ?? 2;
  const currentRetry = step.current_retry ?? 0;
  const showRetry =
    step.status === "failed" ||
    step.status === "timeout" ||
    step.status === "blocked";
  const showReplace =
    step.status === "failed" ||
    step.status === "blocked" ||
    step.status === "timeout";

  // Step number badge color
  const numberClass =
    step.status === "completed"
      ? "bg-green-500 text-white"
      : step.status === "running" || step.status === "assigned"
        ? "bg-blue-500 text-white"
        : step.status === "failed"
          ? "bg-destructive text-destructive-foreground"
          : step.status === "retrying"
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
            <div className="font-medium text-sm">{step.description}</div>
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

        {/* Right: status badge + actions */}
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
      {step.result && step.status === "completed" && (
        <div className="text-xs bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300 p-2 rounded">
          {typeof step.result === "string"
            ? step.result.slice(0, 200)
            : JSON.stringify(step.result).slice(0, 200)}
        </div>
      )}

      {/* Action buttons */}
      {(showRetry || showReplace) && (
        <div className="flex items-center gap-2 pt-1">
          {showRetry && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => onRetry(step.id)}
              className="h-7 text-xs"
            >
              <RefreshCw className="size-3 mr-1" />
              重试
            </Button>
          )}
          {showReplace && (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<Button size="sm" variant="outline" className="h-7 text-xs" />}
              >
                <UserRoundCog className="size-3 mr-1" />
                替换Agent
                <ChevronDown className="size-3 ml-1" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                {agents.length === 0 && (
                  <DropdownMenuItem disabled>暂无可用Agent</DropdownMenuItem>
                )}
                {agents
                  .filter((a) => a.id !== displayAgentId && !a.archived_at)
                  .map((agent) => (
                    <DropdownMenuItem
                      key={agent.id}
                      onClick={() => onReplaceAgent(step.id, agent.id)}
                    >
                      <Bot className="size-3 mr-2 shrink-0" />
                      <div className="min-w-0">
                        <div className="text-sm truncate">{agent.name}</div>
                        {agent.identity_card?.capabilities?.length ? (
                          <div className="text-xs text-muted-foreground truncate">
                            {agent.identity_card.capabilities.slice(0, 3).join(", ")}
                          </div>
                        ) : null}
                      </div>
                    </DropdownMenuItem>
                  ))}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
      )}
    </div>
  );
}
