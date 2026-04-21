"use client";

import { useEffect, useState, useCallback, useMemo } from "react";
import { useParams, useRouter } from "next/navigation";
import { toast } from "sonner";
import {
  ArrowLeft,
  Loader2,
  Play,
  GitFork,
  Check,
  X as XIcon,
  ListTodo,
  Trash2,
} from "lucide-react";
import { useProjectStore } from "@/features/projects";
import { useWorkspaceStore } from "@/features/workspace";
import { VersionTree } from "@/features/projects/components/version-tree";
import { ExecutionStepCard } from "@/features/projects/components/execution-step-card";
import { MessageInput } from "@/features/messaging/components/message-input";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { api } from "@/shared/api";
import { useRetryingPoll } from "./polling";
import type {
  ProjectStatus,
  ProjectRun,
  Agent,
  Task,
  TaskStatus,
} from "@/shared/types";
import type { Message } from "@/shared/types/messaging";

const STATUS_BADGE: Record<ProjectStatus, string> = {
  not_started: "bg-accent text-muted-foreground border-border",
  running: "bg-[rgba(94,106,210,0.15)] text-[#8b9cf7] border-[rgba(94,106,210,0.25)]",
  paused: "bg-[rgba(255,180,50,0.15)] text-[#f0b440] border-[rgba(255,180,50,0.25)]",
  completed: "bg-[rgba(39,166,68,0.15)] text-[#4ade80] border-[rgba(39,166,68,0.25)]",
  failed: "bg-[rgba(239,68,68,0.15)] text-[#f87171] border-[rgba(239,68,68,0.25)]",
  archived: "bg-accent text-muted-foreground/60 border-border",
};

const STATUS_LABEL: Record<ProjectStatus, string> = {
  not_started: "未开始",
  running: "运行中",
  paused: "已暂停",
  completed: "已完成",
  failed: "失败",
  archived: "已归档",
};

const RUN_STATUS_BADGE: Record<string, string> = {
  pending: "bg-accent text-muted-foreground border-border",
  running: "bg-[rgba(94,106,210,0.15)] text-[#8b9cf7] border-[rgba(94,106,210,0.25)]",
  paused: "bg-[rgba(255,180,50,0.15)] text-[#f0b440] border-[rgba(255,180,50,0.25)]",
  completed: "bg-[rgba(39,166,68,0.15)] text-[#4ade80] border-[rgba(39,166,68,0.25)]",
  failed: "bg-[rgba(239,68,68,0.15)] text-[#f87171] border-[rgba(239,68,68,0.25)]",
  cancelled: "bg-accent text-muted-foreground/60 border-border",
};

// Buckets for the board view. Kept intentionally small so a project with a
// handful of tasks doesn't scatter them across too many columns.
const BOARD_COLUMNS: Array<{
  key: "todo" | "in_progress" | "waiting" | "done" | "failed";
  label: string;
  statuses: TaskStatus[];
}> = [
  {
    key: "todo",
    label: "待办",
    statuses: ["draft", "ready", "queued"],
  },
  {
    key: "in_progress",
    label: "进行中",
    statuses: ["assigned", "running"],
  },
  {
    key: "waiting",
    label: "等待/审核",
    statuses: ["needs_human", "under_review", "needs_attention"],
  },
  {
    key: "done",
    label: "已完成",
    statuses: ["completed"],
  },
  {
    key: "failed",
    label: "失败/取消",
    statuses: ["failed", "cancelled"],
  },
];

interface ProjectDetailProps {
  projectId?: string;
}

export default function ProjectDetailPage({ projectId }: ProjectDetailProps) {
  const params = useParams();
  const router = useRouter();
  const id = projectId ?? (params?.id as string);
  const isInline = !!projectId;

  const {
    currentProject,
    versions,
    runs,
    fetchProject,
    fetchVersions,
    fetchRuns,
    updateProject,
    deleteProject,
    approvePlan,
    rejectPlan,
  } = useProjectStore();

  const [loading, setLoading] = useState(true);
  const [loadErrors, setLoadErrors] = useState<{
    project?: string;
    versions?: string;
    runs?: string;
  }>({});
  const [editingTitle, setEditingTitle] = useState(false);
  const [titleValue, setTitleValue] = useState("");
  const [forkOpen, setForkOpen] = useState(false);
  const [forkBranch, setForkBranch] = useState("");
  const [forkReason, setForkReason] = useState("");
  const [rejectOpen, setRejectOpen] = useState(false);
  const [rejectReason, setRejectReason] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  // Channel messages for channel tab
  const [channelMessages, setChannelMessages] = useState<Message[]>([]);

  // Project tasks (from /api/plans/{id}/tasks). Drives Plan / Tasks /
  // Board / Execution tabs — migration 059 removed the legacy Plan.steps
  // column so everything flows from Task rows now.
  const [tasks, setTasks] = useState<Task[]>([]);
  const [startingExecution, setStartingExecution] = useState(false);

  const agents = useWorkspaceStore((s) => s.agents) as Agent[];

  useEffect(() => {
    if (!id) return;
    setLoading(true);
    setLoadErrors({});
    const controller = new AbortController();
    const signal = controller.signal;

    const describe = (err: unknown, fallback: string) => {
      if (err instanceof Error && err.message) return err.message;
      return fallback;
    };

    const isAbort = (err: unknown) =>
      signal.aborted || (err as Error)?.name === "AbortError";

    const loadProject = async () => {
      try {
        const project = await api.getProject(id, { signal });
        if (signal.aborted) return;
        useProjectStore.setState({ currentProject: project });
      } catch (err) {
        if (isAbort(err)) return;
        setLoadErrors((prev) => ({
          ...prev,
          project: describe(err, "加载项目详情失败"),
        }));
      }
    };
    const loadVersions = async () => {
      try {
        const versions = await api.listProjectVersions(id, { signal });
        if (signal.aborted) return;
        useProjectStore.setState({ versions });
      } catch (err) {
        if (isAbort(err)) return;
        setLoadErrors((prev) => ({
          ...prev,
          versions: describe(err, "加载版本失败"),
        }));
      }
    };
    const loadRuns = async () => {
      try {
        const runs = await api.listProjectRuns(id, { signal });
        if (signal.aborted) return;
        useProjectStore.setState({ runs });
      } catch (err) {
        if (isAbort(err)) return;
        setLoadErrors((prev) => ({
          ...prev,
          runs: describe(err, "加载运行记录失败"),
        }));
      }
    };

    const load = async () => {
      await Promise.allSettled([loadProject(), loadVersions(), loadRuns()]);
      if (signal.aborted) return;
      setLoading(false);
    };
    load();

    return () => controller.abort();
  }, [id]);

  useEffect(() => {
    if (currentProject) {
      setTitleValue(currentProject.title);
    }
  }, [currentProject]);

  const channelId = currentProject?.channel_id ?? null;
  useEffect(() => {
    setChannelMessages([]);
  }, [channelId]);

  const channelPollError = useRetryingPoll({
    enabled: Boolean(channelId),
    fallbackError: "加载项目频道消息失败",
    resetKey: channelId ?? "no-channel",
    poll: useCallback(async () => {
      if (!channelId) return;
      const res = await api.getChannelMessages(channelId);
      setChannelMessages(res.messages);
    }, [channelId]),
  });

  const activeRun = currentProject?.active_run;
  const isExecutionPollingActive =
    Boolean(activeRun) &&
    (activeRun?.status === "running" || activeRun?.status === "paused");

  useEffect(() => {
    if (!currentProject?.plan?.id) {
      setTasks([]);
    }
  }, [currentProject?.plan?.id]);

  useEffect(() => {
    const planID = currentProject?.plan?.id;
    if (!planID) return;

    let cancelled = false;
    void api
      .listTasksByPlan(planID)
      .then((loaded) => {
        if (!cancelled) setTasks(loaded);
      })
      .catch(() => {
        // Keep the last snapshot on refresh failure.
      });

    return () => {
      cancelled = true;
    };
  }, [
    currentProject?.plan?.id,
    currentProject?.active_run?.id,
    currentProject?.active_run?.status,
  ]);

  const executionPollError = useRetryingPoll({
    enabled: isExecutionPollingActive,
    fallbackError: "同步执行状态失败",
    resetKey: id ?? "no-project",
    poll: useCallback(async () => {
      if (!id) return;
      const [project, runs] = await Promise.all([
        api.getProject(id),
        api.listProjectRuns(id),
      ]);
      useProjectStore.setState({ currentProject: project, runs });

      if (!project.plan?.id) {
        setTasks([]);
        return;
      }

      const loaded = await api.listTasksByPlan(project.plan.id);
      setTasks(loaded);
    }, [id]),
  });

  const sortedTasks = useMemo(
    () => [...tasks].sort((a, b) => a.step_order - b.step_order),
    [tasks],
  );

  const tasksByColumn = useMemo(() => {
    const buckets: Record<string, Task[]> = {};
    for (const col of BOARD_COLUMNS) buckets[col.key] = [];
    for (const t of sortedTasks) {
      const col = BOARD_COLUMNS.find((c) => c.statuses.includes(t.status));
      if (col) buckets[col.key]!.push(t);
    }
    return buckets;
  }, [sortedTasks]);

  async function handleTitleSave() {
    if (!titleValue.trim() || !id) return;
    await updateProject(id, { title: titleValue.trim() });
    setEditingTitle(false);
  }

  async function handleFork() {
    if (!forkBranch.trim()) return;
    await useProjectStore
      .getState()
      .forkProject(id, forkBranch.trim(), forkReason.trim() || undefined);
    setForkOpen(false);
    setForkBranch("");
    setForkReason("");
    fetchVersions(id);
  }

  async function handleApprove() {
    const planId = currentProject?.plan?.id;
    if (!planId) {
      toast.error("当前项目没有可审批的计划");
      return;
    }
    await approvePlan(id, planId);
  }

  async function handleReject() {
    if (!rejectReason.trim()) return;
    await rejectPlan(id, rejectReason.trim());
    setRejectOpen(false);
    setRejectReason("");
  }

  async function handleStartExecution() {
    try {
      setStartingExecution(true);
      await api.startProjectExecution(id);
      toast.success("执行已开始");
      await Promise.all([fetchProject(id), fetchRuns(id)]);
    } catch {
      toast.error("启动执行失败");
    } finally {
      setStartingExecution(false);
    }
  }

  async function handleDelete() {
    if (!id) return;
    try {
      setDeleting(true);
      await deleteProject(id);
      toast.success("项目已删除");
      setDeleteOpen(false);
      if (isInline) {
        // Inline usage (projects list) has its own selection state — just
        // let the parent re-render by refreshing the project list.
        await useProjectStore.getState().fetch();
      } else {
        router.push("/projects");
      }
    } catch {
      toast.error("删除项目失败");
    } finally {
      setDeleting(false);
    }
  }

  async function handleSendChannelMessage(content: string) {
    if (!currentProject?.channel_id) return;
    try {
      const msg = await api.sendMessage({
        channel_id: currentProject.channel_id,
        content,
      });
      setChannelMessages((prev) => [...prev, msg]);
    } catch {
      toast.error("发送消息失败");
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center flex-1 py-20">
        <Loader2 className="size-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!currentProject) {
    return (
      <div className="flex-1 p-6 space-y-3">
        {loadErrors.project ? (
          <p className="text-sm text-destructive">
            加载项目详情失败：{loadErrors.project}
          </p>
        ) : (
          <p className="text-muted-foreground">未找到项目。</p>
        )}
      </div>
    );
  }

  const plan = currentProject.plan;
  const approvalStatus = (plan as any)?.approval_status as string | undefined;
  const isAwaitingApproval =
    !approvalStatus ||
    approvalStatus === "draft" ||
    approvalStatus === "pending" ||
    approvalStatus === "pending_approval";
  const approvalLabel =
    approvalStatus === "draft" ? "草稿" : "待审批";
  const pollingErrors = [
    channelPollError.error ? `频道：${channelPollError.error}` : null,
    executionPollError.error ? `执行：${executionPollError.error}` : null,
  ].filter(Boolean) as string[];

  const nonFatalLoadErrors = [
    loadErrors.versions ? `版本：${loadErrors.versions}` : null,
    loadErrors.runs ? `运行记录：${loadErrors.runs}` : null,
  ].filter(Boolean) as string[];

  const hasPlan = Boolean(plan);
  const hasTasks = sortedTasks.length > 0;

  return (
    <div className="flex flex-1 min-h-0 flex-col overflow-auto p-6">
      {pollingErrors.length > 0 && (
        <div
          className="mb-4 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-warning"
          role="status"
          aria-live="polite"
        >
          实时同步暂时受阻，正在自动重试：{pollingErrors.join("；")}
        </div>
      )}
      {nonFatalLoadErrors.length > 0 && (
        <div className="mb-4 border border-destructive/40 bg-destructive/10 text-destructive text-sm rounded-md px-3 py-2">
          部分数据加载失败：{nonFatalLoadErrors.join("；")}
        </div>
      )}
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        {!isInline && (
          <Button
            variant="ghost"
            size="icon"
            onClick={() => router.push("/projects")}
          >
            <ArrowLeft className="size-4" />
          </Button>
        )}
        <div className="flex-1 min-w-0">
          {editingTitle ? (
            <div className="flex items-center gap-2">
              <Input
                value={titleValue}
                onChange={(e) => setTitleValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleTitleSave();
                  if (e.key === "Escape") setEditingTitle(false);
                }}
                className="text-2xl font-semibold h-auto py-0"
                autoFocus
              />
              <Button size="sm" onClick={handleTitleSave}>
                保存
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => setEditingTitle(false)}
              >
                取消
              </Button>
            </div>
          ) : (
            <h1
              className="text-2xl font-semibold truncate cursor-pointer hover:text-primary/80"
              onClick={() => setEditingTitle(true)}
            >
              {currentProject.title}
            </h1>
          )}
          <div className="flex items-center gap-2 mt-1">
            <Badge
              className={STATUS_BADGE[currentProject.status]}
              variant="outline"
            >
              {STATUS_LABEL[currentProject.status]}
            </Badge>
            <span className="text-sm text-muted-foreground">
              {currentProject.schedule_type === "one_time"
                ? "一次性"
                : currentProject.schedule_type === "scheduled"
                  ? "定时"
                  : "周期性"}
            </span>
            {currentProject.cron_expr && (
              <span className="text-sm text-muted-foreground font-mono">
                {currentProject.cron_expr}
              </span>
            )}
          </div>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => setForkOpen(true)}>
            <GitFork className="size-4 mr-1" />
            分叉
          </Button>
          <Button
            variant="outline"
            className="text-destructive border-destructive/40 hover:bg-destructive/10"
            onClick={() => setDeleteOpen(true)}
          >
            <Trash2 className="size-4 mr-1" />
            删除
          </Button>
        </div>
      </div>

      {/* Version selector */}
      {versions.length > 1 && (
        <div className="mb-4 border border-border rounded-lg p-3 bg-card">
          <h3 className="text-sm font-medium mb-2">版本</h3>
          <VersionTree versions={versions} onSelect={() => {}} />
        </div>
      )}

      {/* Tabs */}
      <Tabs defaultValue="plan" className="flex flex-col flex-1 min-h-0">
        <TabsList>
          <TabsTrigger value="plan">计划</TabsTrigger>
          <TabsTrigger value="tasks">任务</TabsTrigger>
          <TabsTrigger value="board">看板</TabsTrigger>
          <TabsTrigger value="execution">执行</TabsTrigger>
          <TabsTrigger value="channel">频道</TabsTrigger>
        </TabsList>

        {/* Tab 1: Plan */}
        <TabsContent value="plan" className="space-y-4">
          {(plan as any)?.task_brief && (
            <div className="border border-border rounded-lg p-4 bg-card">
              <h3 className="text-sm font-medium text-foreground mb-2">任务简报</h3>
              <div className="text-sm text-secondary-foreground whitespace-pre-wrap">
                {(plan as any).task_brief}
              </div>
            </div>
          )}

          <div>
            <h3 className="text-sm font-medium mb-3">计划步骤</h3>
            {!hasPlan ? (
              <div className="text-center py-8 border-2 border-dashed rounded-lg text-muted-foreground text-sm">
                暂无计划。将聊天消息选中并通过“Generate Project”生成计划。
              </div>
            ) : !hasTasks ? (
              <div className="text-center py-8 border-2 border-dashed rounded-lg text-muted-foreground text-sm">
                计划已创建，但尚未生成任务。稍后刷新或检查 Plan Generator 日志。
              </div>
            ) : (
              <div className="space-y-3">
                {sortedTasks.map((t) => (
                  <ExecutionStepCard key={t.id} step={t} agents={agents} />
                ))}
              </div>
            )}
          </div>

          {/* Approval section */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-foreground mb-3">审批</h3>
            {isAwaitingApproval ? (
              <div className="flex items-center gap-3">
                <Button
                  onClick={handleApprove}
                  disabled={!currentProject?.plan?.id}
                  className="bg-[#27a644] hover:bg-[#1f8a38] text-white"
                >
                  <Check className="size-4 mr-1" />
                  批准计划
                </Button>
                <Button
                  variant="outline"
                  onClick={() => setRejectOpen(true)}
                  className="text-destructive border-destructive hover:bg-destructive/10"
                >
                  <XIcon className="size-4 mr-1" />
                  拒绝计划
                </Button>
                <span className="text-sm text-muted-foreground">
                  {approvalLabel}
                </span>
              </div>
            ) : approvalStatus === "approved" ? (
              <div className="flex items-center gap-2">
                <Badge className="bg-[rgba(39,166,68,0.15)] text-[#4ade80] border-[rgba(39,166,68,0.25)]">
                  已批准
                </Badge>
                {(plan as any)?.approved_by && (
                  <span className="text-sm text-muted-foreground">
                    由 {(plan as any).approved_by.slice(0, 8)} 批准
                  </span>
                )}
                {(plan as any)?.approved_at && (
                  <span className="text-sm text-muted-foreground">
                    · {new Date((plan as any).approved_at).toLocaleString()}
                  </span>
                )}
              </div>
            ) : approvalStatus === "rejected" ? (
              <div>
                <Badge className="bg-[rgba(239,68,68,0.15)] text-[#f87171] border-[rgba(239,68,68,0.25)]">
                  已拒绝
                </Badge>
              </div>
            ) : null}

            {approvalStatus === "approved" &&
              currentProject.status === "not_started" && (
                <div className="mt-4">
                  <Button
                    onClick={handleStartExecution}
                    disabled={startingExecution}
                  >
                    {startingExecution ? (
                      <Loader2 className="size-4 mr-1 animate-spin" />
                    ) : (
                      <Play className="size-4 mr-1" />
                    )}
                    开始执行
                  </Button>
                </div>
              )}
          </div>
        </TabsContent>

        {/* Tab 2: Tasks */}
        <TabsContent value="tasks" className="flex flex-col flex-1 min-h-0">
          {!hasTasks ? (
            <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
              <ListTodo className="h-10 w-10 text-muted-foreground/40" />
              <p className="text-sm">暂无任务</p>
              <p className="text-xs">在计划中生成任务以开始。</p>
            </div>
          ) : (
            <div className="space-y-3">
              {sortedTasks.map((t) => (
                <ExecutionStepCard key={t.id} step={t} agents={agents} />
              ))}
            </div>
          )}
        </TabsContent>

        {/* Tab 3: Board */}
        <TabsContent value="board" className="flex flex-col flex-1 min-h-0">
          {!hasTasks ? (
            <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
              <ListTodo className="h-10 w-10 text-muted-foreground/40" />
              <p className="text-sm">暂无任务</p>
            </div>
          ) : (
            <div className="grid grid-cols-5 gap-3 min-h-0">
              {BOARD_COLUMNS.map((col) => {
                const items = tasksByColumn[col.key] ?? [];
                return (
                  <div
                    key={col.key}
                    className="flex flex-col border border-border rounded-lg bg-card overflow-hidden"
                  >
                    <div className="px-3 py-2 border-b border-border flex items-center justify-between">
                      <span className="text-sm font-medium">{col.label}</span>
                      <span className="text-xs text-muted-foreground">
                        {items.length}
                      </span>
                    </div>
                    <div className="flex-1 p-2 space-y-2 overflow-y-auto">
                      {items.length === 0 ? (
                        <div className="text-xs text-muted-foreground/60 text-center py-3">
                          空
                        </div>
                      ) : (
                        items.map((t) => (
                          <ExecutionStepCard
                            key={t.id}
                            step={t}
                            agents={agents}
                          />
                        ))
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </TabsContent>

        {/* Tab 4: Execution */}
        <TabsContent value="execution" className="space-y-4">
          {activeRun ? (
            <div className="space-y-4">
              <RunSummaryBar run={activeRun} tasks={sortedTasks} />
              <div className="space-y-3">
                <h3 className="text-sm font-medium">步骤</h3>
                {sortedTasks.length > 0 ? (
                  sortedTasks.map((step) => (
                    <ExecutionStepCard
                      key={step.id}
                      step={step}
                      agents={agents}
                    />
                  ))
                ) : (
                  <div className="text-sm text-muted-foreground border border-border rounded-lg p-4 text-center bg-card">
                    暂无步骤数据
                  </div>
                )}
              </div>
              {activeRun.failure_reason && (
                <div className="border border-[rgba(239,68,68,0.3)] rounded-lg p-3 bg-[rgba(239,68,68,0.05)]">
                  <div className="text-sm font-medium text-destructive">
                    失败原因
                  </div>
                  <div className="text-sm text-muted-foreground mt-1">
                    {activeRun.failure_reason}
                  </div>
                </div>
              )}
            </div>
          ) : (
            <div className="text-center py-12 text-muted-foreground">
              <p className="mb-2">暂无运行中的执行</p>
              {approvalStatus === "approved" && (
                <Button
                  variant="outline"
                  onClick={handleStartExecution}
                  disabled={startingExecution}
                >
                  {startingExecution ? (
                    <Loader2 className="size-4 mr-1 animate-spin" />
                  ) : (
                    <Play className="size-4 mr-1" />
                  )}
                  开始执行
                </Button>
              )}
            </div>
          )}

          {runs.length > 0 && (
            <div>
              <h3 className="text-sm font-medium mb-3">运行历史</h3>
              <div className="space-y-2">
                {runs.map((run: ProjectRun) => (
                  <div
                    key={run.id}
                    className="flex items-center gap-3 p-3 border border-border rounded-lg bg-card"
                  >
                    <Badge
                      className={RUN_STATUS_BADGE[run.status] ?? ""}
                      variant="outline"
                    >
                      {run.status}
                    </Badge>
                    <div className="flex-1 min-w-0 text-sm">
                      {run.start_at && (
                        <span className="text-muted-foreground">
                          {new Date(run.start_at).toLocaleString()}
                        </span>
                      )}
                      {run.end_at && run.start_at && (
                        <span className="text-muted-foreground">
                          {" \u2192 "}
                          {new Date(run.end_at).toLocaleString()}
                        </span>
                      )}
                    </div>
                    {run.retry_count > 0 && (
                      <span className="text-xs text-muted-foreground">
                        重试 {run.retry_count} 次
                      </span>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}
        </TabsContent>

        {/* Tab 5: Channel */}
        <TabsContent value="channel">
          {currentProject.channel_id ? (
            <div
              className="flex flex-col border border-border rounded-lg"
              style={{ height: "500px" }}
            >
              <div className="p-3 border-b border-border">
                <h3 className="font-medium text-sm text-foreground">项目频道</h3>
              </div>
              <div className="flex-1 overflow-auto p-4 space-y-3">
                {channelMessages.map((msg) => (
                  <div key={msg.id} className="flex justify-start">
                    <div className="max-w-[70%] px-4 py-2 rounded-lg text-sm bg-accent">
                      <div className="text-xs opacity-70 mb-1">
                        {msg.sender_id?.slice(0, 12)} ·{" "}
                        {new Date(msg.created_at).toLocaleTimeString()}
                      </div>
                      <div>{msg.content}</div>
                    </div>
                  </div>
                ))}
                {channelMessages.length === 0 && (
                  <div className="text-center text-muted-foreground mt-8">
                    暂无消息
                  </div>
                )}
              </div>
              <MessageInput
                onSend={handleSendChannelMessage}
                placeholder="发送消息到项目频道..."
              />
            </div>
          ) : (
            <div className="text-center py-12 text-muted-foreground">
              <p>该项目暂无关联频道</p>
            </div>
          )}
        </TabsContent>
      </Tabs>

      {/* Fork Dialog */}
      <Dialog open={forkOpen} onOpenChange={setForkOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>分叉项目</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div>
              <label className="text-sm text-muted-foreground">
                分支名称 *
              </label>
              <Input
                value={forkBranch}
                onChange={(e) => setForkBranch(e.target.value)}
                placeholder="例如：experiment-v2"
                className="mt-1"
              />
            </div>
            <div>
              <label className="text-sm text-muted-foreground">分叉原因</label>
              <Input
                value={forkReason}
                onChange={(e) => setForkReason(e.target.value)}
                placeholder="可选"
                className="mt-1"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setForkOpen(false)}>
              取消
            </Button>
            <Button onClick={handleFork} disabled={!forkBranch.trim()}>
              <GitFork className="size-4 mr-1" />
              分叉
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Reject Dialog */}
      <Dialog open={rejectOpen} onOpenChange={setRejectOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>拒绝计划</DialogTitle>
          </DialogHeader>
          <div>
            <label className="text-sm text-muted-foreground">
              拒绝原因 *
            </label>
            <Input
              value={rejectReason}
              onChange={(e) => setRejectReason(e.target.value)}
              placeholder="请输入拒绝原因"
              className="mt-1"
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRejectOpen(false)}>
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={handleReject}
              disabled={!rejectReason.trim()}
            >
              拒绝
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Dialog */}
      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>删除项目</DialogTitle>
          </DialogHeader>
          <div className="text-sm text-muted-foreground">
            确认删除项目「{currentProject.title}」？此操作不可恢复，项目、计划、任务及相关消息/频道关联将一并清理。
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteOpen(false)}>
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={handleDelete}
              disabled={deleting}
            >
              {deleting ? (
                <Loader2 className="size-4 mr-1 animate-spin" />
              ) : (
                <Trash2 className="size-4 mr-1" />
              )}
              删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/* ---------- Run Summary Bar ---------- */

const RUN_STATUS_LABEL: Record<string, string> = {
  pending: "待运行",
  running: "运行中",
  paused: "已暂停",
  completed: "已完成",
  failed: "已失败",
  cancelled: "已取消",
};

function RunSummaryBar({
  run,
  tasks,
}: {
  run: ProjectRun;
  tasks: Task[];
}) {
  const completedCount = tasks.filter((s) => s.status === "completed").length;
  const totalCount = tasks.length;
  const progressPct =
    totalCount > 0 ? Math.round((completedCount / totalCount) * 100) : 0;

  const startTime = run.start_at ? new Date(run.start_at).getTime() : null;
  const endTime = run.end_at ? new Date(run.end_at).getTime() : Date.now();
  let durationStr: string | null = null;
  if (startTime) {
    const diffSec = Math.floor((endTime - startTime) / 1000);
    if (diffSec < 60) durationStr = `${diffSec}秒`;
    else if (diffSec < 3600)
      durationStr = `${Math.floor(diffSec / 60)}分${diffSec % 60}秒`;
    else {
      const hours = Math.floor(diffSec / 3600);
      const mins = Math.floor((diffSec % 3600) / 60);
      durationStr = `${hours}时${mins}分`;
    }
  }

  return (
    <div className="border border-border rounded-lg p-4 space-y-3 bg-card">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h3 className="text-sm font-medium text-foreground">当前运行</h3>
          <Badge
            className={RUN_STATUS_BADGE[run.status] ?? ""}
            variant="outline"
          >
            {RUN_STATUS_LABEL[run.status] ?? run.status}
          </Badge>
          {durationStr && (
            <span className="text-xs text-muted-foreground">
              耗时 {durationStr}
            </span>
          )}
        </div>
        <span className="text-sm text-muted-foreground">
          进度: {completedCount}/{totalCount} 步骤已完成
        </span>
      </div>
      <Progress value={progressPct} className="h-2" />
    </div>
  );
}
