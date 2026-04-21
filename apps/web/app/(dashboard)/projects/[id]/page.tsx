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
import { OrchestrationGraph } from "@/features/projects/components/orchestration-graph";
import { OrchestrationDAG } from "@/features/projects/components/orchestration-dag";
import { PlanStepper } from "@/features/projects/components/plan-stepper";
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
  Subagent,
  Task,
  TaskStatus,
  ParticipantSlot,
  Artifact,
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
  const [subagents, setSubagents] = useState<Subagent[]>([]);
  const [startingExecution, setStartingExecution] = useState(false);
  // 任务 tab sub-view. "list" is the existing ExecutionStepCard stack;
  // "graph" is the force-directed bubble view from the Hi-Fi reference;
  // "dag" is a rectilinear column-per-rank view for reading depends_on
  // chains without bezier noise.
  const [taskView, setTaskView] = useState<"list" | "graph" | "dag" | "board">("list");
  // Slots tab state — selected task id drives the right-pane slot list.
  const [slotSelectedTaskId, setSlotSelectedTaskId] = useState<string | null>(null);
  const [slotsByTask, setSlotsByTask] = useState<Record<string, ParticipantSlot[]>>({});
  // Results tab — artifacts loaded lazily per task on expand.
  const [artifactsByTask, setArtifactsByTask] = useState<Record<string, Artifact[]>>({});

  const agents = useWorkspaceStore((s) => s.agents) as Agent[];

  // Subagents are fetched once and reused across all ExecutionStepCard
  // renders so the assignee name resolves when PlanGenerator's default-
  // assign picks a subagent template. Kept separate from the workspace
  // store because the subagent pool includes globals.
  useEffect(() => {
    let cancelled = false;
    void api
      .listSubagents()
      .then((list) => {
        if (!cancelled) setSubagents(list);
      })
      .catch(() => {
        // Non-fatal: cards fall back to truncated UUID.
      });
    return () => {
      cancelled = true;
    };
  }, []);

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
    setDeleting(true);
    try {
      // deleteProject now re-throws after surfacing its own error toast,
      // so the success branch below runs only when the delete actually
      // succeeded. The earlier version toasted "项目已删除" even when the
      // API call failed, which is how the dialog kept dismissing without
      // the row actually going away.
      await deleteProject(id);
      toast.success("项目已删除");
      setDeleteOpen(false);
      if (isInline) {
        await useProjectStore.getState().fetch();
      } else {
        router.push("/projects");
      }
    } catch {
      // Store already showed a toast; keep the dialog open so the user
      // can retry after the error.
    } finally {
      setDeleting(false);
    }
  }

  // Slots tab — fetch slots on demand per selected task and cache so
  // switching back doesn't re-query. participant_slot rows carry the
  // "who + when + blocking" semantics the product UI needs.
  async function ensureSlotsLoaded(taskId: string) {
    if (slotsByTask[taskId]) return;
    try {
      const slots = await api.listSlotsByTask(taskId);
      setSlotsByTask((prev) => ({ ...prev, [taskId]: slots }));
    } catch {
      toast.error("加载 slot 失败");
    }
  }

  // Results tab — artifacts load lazily per task the first time a task
  // card is expanded. Cached so toggling doesn't spam the server.
  async function ensureArtifactsLoaded(taskId: string) {
    if (artifactsByTask[taskId]) return;
    try {
      const artifacts = await api.listArtifactsByTask(taskId);
      setArtifactsByTask((prev) => ({ ...prev, [taskId]: artifacts }));
    } catch {
      // Non-fatal — tab just shows "(暂无)" when fetch fails.
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

  // Edit gate for the plan stepper. Inline field edits (A) are cheap
  // and safe, so we allow them any time a run isn't actively touching
  // the tasks; lifecycle-changing regeneration (B) has its own guard
  // wherever it's wired.
  const runStatus = activeRun?.status;
  const planEditable =
    currentProject.status !== "archived" &&
    runStatus !== "running" &&
    runStatus !== "paused";
  const planReadOnlyReason = !planEditable
    ? runStatus === "running" || runStatus === "paused"
      ? "执行中,暂不可编辑"
      : currentProject.status === "archived"
        ? "项目已归档"
        : undefined
    : undefined;

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

      {/* Tabs — 版本/计划/任务/Slots/结果/频道. 看板 folded into 任务
          view toggle; 执行 folded into 结果. */}
      <Tabs defaultValue="plan" className="flex flex-col flex-1 min-h-0">
        <TabsList>
          <TabsTrigger value="versions">版本</TabsTrigger>
          <TabsTrigger value="plan">计划</TabsTrigger>
          <TabsTrigger value="tasks">任务</TabsTrigger>
          <TabsTrigger value="slots">Slots</TabsTrigger>
          <TabsTrigger value="results">结果</TabsTrigger>
          <TabsTrigger value="channel">频道</TabsTrigger>
        </TabsList>

        {/* Tab: 版本 */}
        <TabsContent value="versions" className="space-y-3">
          {versions.length > 0 ? (
            <div className="border border-border rounded-lg p-4 bg-card">
              <h3 className="text-sm font-medium mb-3">版本树</h3>
              <VersionTree versions={versions} onSelect={() => {}} />
            </div>
          ) : (
            <div className="text-sm text-muted-foreground border border-dashed rounded-lg p-6 text-center">
              暂无版本记录，使用右上角“分叉”创建第一个版本。
            </div>
          )}
        </TabsContent>

        {/* Tab: 计划 */}
        <TabsContent value="plan" className="space-y-4">
          {/* 上下文 — chat refs + placeholders for files / user inputs */}
          <PlanContextSection project={currentProject} plan={plan} />

          {(plan as any)?.task_brief && (
            <div className="border border-border rounded-lg p-4 bg-card">
              <h3 className="text-sm font-medium text-foreground mb-2">任务简报</h3>
              <div className="text-sm text-secondary-foreground whitespace-pre-wrap">
                {(plan as any).task_brief}
              </div>
            </div>
          )}

          <div>
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-medium">计划步骤</h3>
              {hasTasks && (
                <span className="text-[11px] text-muted-foreground">
                  {sortedTasks.length} 步
                </span>
              )}
            </div>
            {!hasPlan ? (
              <div className="text-center py-8 border-2 border-dashed rounded-lg text-muted-foreground text-sm">
                暂无计划。将聊天消息选中并通过“Generate Project”生成计划。
              </div>
            ) : (
              <PlanStepper
                tasks={sortedTasks}
                agents={agents}
                subagents={subagents}
                editable={planEditable}
                readOnlyReason={planReadOnlyReason}
                onTaskUpdated={(updated) => {
                  setTasks((prev) =>
                    prev.map((t) => (t.id === updated.id ? updated : t)),
                  );
                }}
              />
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

        {/* Tab: 任务 — 列表/气泡图/DAG/看板 four-way toggle */}
        <TabsContent value="tasks" className="flex flex-col flex-1 min-h-0 space-y-3">
          {!hasTasks ? (
            <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
              <ListTodo className="h-10 w-10 text-muted-foreground/40" />
              <p className="text-sm">暂无任务</p>
              <p className="text-xs">在计划中生成任务以开始。</p>
            </div>
          ) : (
            <>
              <TaskProgressBar tasks={sortedTasks} />
              <div className="flex items-center gap-2">
                {(
                  [
                    { key: "list", label: "列表" },
                    { key: "graph", label: "编排图" },
                    { key: "dag", label: "DAG" },
                    { key: "board", label: "看板" },
                  ] as const
                ).map(({ key, label }) => (
                  <Button
                    key={key}
                    variant={taskView === key ? "default" : "outline"}
                    size="sm"
                    onClick={() => setTaskView(key)}
                  >
                    {label}
                  </Button>
                ))}
              </div>

              {taskView === "list" && (
                <div className="space-y-3">
                  {sortedTasks.map((t) => (
                    <ExecutionStepCard
                      key={t.id}
                      step={t}
                      agents={agents}
                      subagents={subagents}
                    />
                  ))}
                </div>
              )}

              {taskView === "graph" && (
                <OrchestrationGraph
                  tasks={sortedTasks}
                  agents={agents}
                  subagents={subagents}
                />
              )}

              {taskView === "dag" && (
                <OrchestrationDAG
                  tasks={sortedTasks}
                  agents={agents}
                  subagents={subagents}
                />
              )}

              {taskView === "board" && (
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
                                subagents={subagents}
                              />
                            ))
                          )}
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </>
          )}
        </TabsContent>

        {/* Tab: Slots — drills into each task's participant_slot rows
            so the reviewer can see who does what, when, and whether
            it blocks downstream work. */}
        <TabsContent value="slots" className="flex flex-col flex-1 min-h-0">
          <SlotsPanel
            tasks={sortedTasks}
            selectedTaskId={slotSelectedTaskId}
            onSelect={(id) => {
              setSlotSelectedTaskId(id);
              void ensureSlotsLoaded(id);
            }}
            slotsByTask={slotsByTask}
            agents={agents}
            subagents={subagents}
          />
        </TabsContent>

        {/* Tab: 结果 — aggregates artifacts + per-task output_refs +
            active run's output_refs + run history. Former 执行 tab
            data is folded in here so the user has one place for
            "what did we produce". */}
        <TabsContent value="results" className="space-y-4">
          <ResultsPanel
            tasks={sortedTasks}
            activeRun={activeRun}
            runs={runs}
            artifactsByTask={artifactsByTask}
            ensureArtifactsLoaded={ensureArtifactsLoaded}
          />
          {activeRun?.failure_reason && (
            <div className="border border-[rgba(239,68,68,0.3)] rounded-lg p-3 bg-[rgba(239,68,68,0.05)]">
              <div className="text-sm font-medium text-destructive">
                失败原因
              </div>
              <div className="text-sm text-muted-foreground mt-1">
                {activeRun.failure_reason}
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

/* ---------- Plan context section ---------- */

// Renders the plan's input surface — chat refs today, plus slotted
// placeholders for input_files and user_inputs so the UI is ready to
// pick up those fields once Phase 3 lands them in the plan row.
function PlanContextSection({
  project,
  plan,
}: {
  project: import("@/shared/types").Project;
  plan: import("@/shared/types").Plan | undefined;
}) {
  const convs = project.source_conversations ?? [];
  const files = ((plan as any)?.input_files ?? []) as Array<{
    id?: string;
    name?: string;
  }>;
  const userInputs = ((plan as any)?.user_inputs ?? {}) as Record<string, unknown>;

  const emptyEverywhere =
    convs.length === 0 &&
    files.length === 0 &&
    Object.keys(userInputs).length === 0;

  return (
    <div className="border border-border rounded-lg bg-card">
      <div className="px-4 py-3 border-b border-border flex items-center justify-between">
        <h3 className="text-sm font-medium">上下文</h3>
        <span className="text-[11px] text-muted-foreground font-mono">
          会话 {convs.length} · 文件 {files.length} · 字段 {Object.keys(userInputs).length}
        </span>
      </div>
      {emptyEverywhere ? (
        <div className="p-4 text-xs text-muted-foreground">
          尚无上下文。生成计划时选中的消息、附加文件、填写字段都会在此汇总。
        </div>
      ) : (
        <div className="divide-y divide-border">
          {convs.length > 0 && (
            <div className="p-4">
              <div className="text-[10px] text-muted-foreground/80 font-mono uppercase tracking-wider mb-1.5">
                会话引用
              </div>
              <ul className="space-y-1">
                {convs.map((c, i) => {
                  const subset = (c as any).type === "message_subset";
                  const msgIds =
                    subset && Array.isArray((c as any).message_ids)
                      ? (c as any).message_ids
                      : [];
                  return (
                    <li
                      key={`${c.conversation_id}-${i}`}
                      className="flex items-center gap-2 text-xs font-mono text-muted-foreground"
                    >
                      <Badge variant="outline" className="text-[10px]">
                        {c.type}
                      </Badge>
                      <span className="truncate">
                        {c.conversation_id.slice(0, 12)}…
                      </span>
                      {subset && (
                        <span className="text-[10px] text-muted-foreground/70">
                          {msgIds.length} 条消息
                        </span>
                      )}
                    </li>
                  );
                })}
              </ul>
            </div>
          )}
          {files.length > 0 && (
            <div className="p-4">
              <div className="text-[10px] text-muted-foreground/80 font-mono uppercase tracking-wider mb-1.5">
                附加文件
              </div>
              <ul className="space-y-1">
                {files.map((f, i) => (
                  <li
                    key={f.id ?? i}
                    className="text-xs font-mono text-muted-foreground truncate"
                  >
                    {f.name ?? f.id ?? "file"}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {Object.keys(userInputs).length > 0 && (
            <div className="p-4">
              <div className="text-[10px] text-muted-foreground/80 font-mono uppercase tracking-wider mb-1.5">
                用户填写
              </div>
              <dl className="space-y-1 text-xs">
                {Object.entries(userInputs).map(([k, v]) => (
                  <div key={k} className="flex gap-2">
                    <dt className="text-muted-foreground shrink-0 font-mono">
                      {k}
                    </dt>
                    <dd className="truncate">
                      {typeof v === "string" ? v : JSON.stringify(v)}
                    </dd>
                  </div>
                ))}
              </dl>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/* ---------- 任务 tab — progress bar ---------- */

function TaskProgressBar({ tasks }: { tasks: Task[] }) {
  const completed = tasks.filter((t) => t.status === "completed").length;
  const total = tasks.length;
  const pct = total > 0 ? Math.round((completed / total) * 100) : 0;
  return (
    <div className="flex items-center gap-3 border border-border rounded-lg p-3 bg-card">
      <span className="text-xs text-muted-foreground shrink-0">
        开发进度 {completed}/{total}
      </span>
      <Progress value={pct} className="h-2 flex-1" />
      <span className="text-xs text-muted-foreground font-mono shrink-0">
        {pct}%
      </span>
    </div>
  );
}

/* ---------- Slots panel ---------- */

function SlotsPanel({
  tasks,
  selectedTaskId,
  onSelect,
  slotsByTask,
  agents,
  subagents,
}: {
  tasks: Task[];
  selectedTaskId: string | null;
  onSelect: (id: string) => void;
  slotsByTask: Record<string, ParticipantSlot[]>;
  agents: Agent[];
  subagents: Subagent[];
}) {
  if (tasks.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
        暂无任务
      </div>
    );
  }
  const selected = tasks.find((t) => t.id === selectedTaskId) ?? null;
  const slots = selected ? slotsByTask[selected.id] ?? null : null;
  return (
    <div className="flex flex-1 min-h-0 border border-border rounded-lg overflow-hidden bg-card">
      <div className="w-[260px] shrink-0 border-r border-border overflow-y-auto">
        {tasks.map((t) => {
          const active = t.id === selectedTaskId;
          return (
            <button
              key={t.id}
              type="button"
              onClick={() => onSelect(t.id)}
              className={`w-full text-left px-3 py-2.5 border-b border-border hover:bg-accent text-sm transition-colors ${
                active ? "bg-muted" : ""
              }`}
            >
              <div className="flex items-center gap-2 text-[10px] text-muted-foreground font-mono">
                <span>#{t.step_order}</span>
                <span>{t.status}</span>
              </div>
              <div className="text-xs font-medium mt-0.5 line-clamp-2">
                {t.title}
              </div>
            </button>
          );
        })}
      </div>
      <div className="flex-1 min-w-0 overflow-y-auto p-4">
        {!selected && (
          <div className="text-sm text-muted-foreground text-center mt-10">
            选择左侧任务查看 slot 时间线
          </div>
        )}
        {selected && slots === null && (
          <div className="text-sm text-muted-foreground">加载中…</div>
        )}
        {selected && slots && slots.length === 0 && (
          <div className="text-sm text-muted-foreground">
            该任务未定义 slot
          </div>
        )}
        {selected && slots && slots.length > 0 && (
          <div className="space-y-2">
            {[...slots]
              .sort((a, b) => a.slot_order - b.slot_order)
              .map((s) => (
                <SlotRow
                  key={s.id}
                  slot={s}
                  agents={agents}
                  subagents={subagents}
                />
              ))}
          </div>
        )}
      </div>
    </div>
  );
}

function SlotRow({
  slot,
  agents,
  subagents,
}: {
  slot: ParticipantSlot;
  agents: Agent[];
  subagents: Subagent[];
}) {
  const participantName = slot.participant_id
    ? agents.find((a) => a.id === slot.participant_id)?.name ??
      subagents.find((s) => s.id === slot.participant_id)?.name ??
      slot.participant_id.slice(0, 8)
    : slot.participant_type === "agent"
      ? "待分配 agent"
      : slot.participant_type === "member"
        ? "待分配成员"
        : "—";
  return (
    <div className="border border-border rounded-md p-3 bg-background/50">
      <div className="flex items-center gap-2 text-[11px] font-mono text-muted-foreground">
        <span>#{slot.slot_order}</span>
        <span>{SLOT_TYPE_LABEL[slot.slot_type] ?? slot.slot_type}</span>
        <span>·</span>
        <span>{SLOT_TRIGGER_LABEL[slot.trigger] ?? slot.trigger}</span>
        {slot.blocking && (
          <Badge variant="outline" className="text-[9px] h-4 px-1.5">
            阻塞
          </Badge>
        )}
        {!slot.required && (
          <Badge variant="outline" className="text-[9px] h-4 px-1.5">
            可选
          </Badge>
        )}
        <span className="ml-auto">{slot.status}</span>
      </div>
      <div className="mt-2 text-sm font-medium">{participantName}</div>
      {slot.responsibility && (
        <div className="text-xs text-muted-foreground mt-1 whitespace-pre-wrap">
          {slot.responsibility}
        </div>
      )}
      {slot.expected_output && (
        <div className="text-[11px] text-muted-foreground mt-1 font-mono">
          期望产出:{slot.expected_output}
        </div>
      )}
    </div>
  );
}

const SLOT_TYPE_LABEL: Record<string, string> = {
  human_input: "人工输入",
  agent_execution: "Agent 执行",
  human_review: "人工评审",
};

const SLOT_TRIGGER_LABEL: Record<string, string> = {
  before_execution: "执行前",
  during_execution: "执行中",
  before_done: "完成前",
};

/* ---------- 结果 panel ---------- */

function ResultsPanel({
  tasks,
  activeRun,
  runs,
  artifactsByTask,
  ensureArtifactsLoaded,
}: {
  tasks: Task[];
  activeRun: ProjectRun | undefined;
  runs: ProjectRun[];
  artifactsByTask: Record<string, Artifact[]>;
  ensureArtifactsLoaded: (taskId: string) => Promise<void>;
}) {
  useEffect(() => {
    for (const t of tasks) {
      void ensureArtifactsLoaded(t.id);
    }
  }, [tasks, ensureArtifactsLoaded]);

  const totalArtifacts = Object.values(artifactsByTask).reduce(
    (sum, arr) => sum + arr.length,
    0,
  );

  return (
    <div className="space-y-4">
      {activeRun && <RunSummaryBar run={activeRun} tasks={tasks} />}

      {/* Task outputs section */}
      <div className="border border-border rounded-lg bg-card">
        <div className="px-4 py-3 border-b border-border flex items-center justify-between">
          <h3 className="text-sm font-medium">任务产出</h3>
          <span className="text-xs text-muted-foreground">
            {totalArtifacts} artifact{totalArtifacts !== 1 ? "s" : ""}
          </span>
        </div>
        <div className="divide-y divide-border">
          {tasks.map((t) => {
            const artifacts = artifactsByTask[t.id] ?? [];
            const outRefs = (t.output_refs ?? []) as unknown[];
            const empty = artifacts.length === 0 && outRefs.length === 0;
            return (
              <div key={t.id} className="p-3">
                <div className="flex items-center gap-2 text-xs">
                  <span className="text-muted-foreground font-mono">
                    #{t.step_order}
                  </span>
                  <span className="font-medium truncate">{t.title}</span>
                  <span className="ml-auto text-muted-foreground">
                    {t.status}
                  </span>
                </div>
                {empty ? (
                  <div className="text-[11px] text-muted-foreground/70 mt-1">
                    暂无产出
                  </div>
                ) : (
                  <div className="mt-2 space-y-1">
                    {artifacts.map((a) => (
                      <div
                        key={a.id}
                        className="text-xs text-muted-foreground font-mono flex gap-2"
                      >
                        <span className="shrink-0">{a.artifact_type}</span>
                        <span className="truncate">
                          {a.title ?? a.summary ?? a.id.slice(0, 8)}
                        </span>
                        <span className="text-muted-foreground/60 shrink-0">
                          v{a.version}
                        </span>
                      </div>
                    ))}
                    {outRefs.length > 0 && (
                      <div className="text-[11px] text-muted-foreground">
                        output_refs: {outRefs.length}
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {/* Run history */}
      {runs.length > 0 && (
        <div className="border border-border rounded-lg bg-card">
          <div className="px-4 py-3 border-b border-border">
            <h3 className="text-sm font-medium">运行历史</h3>
          </div>
          <div className="divide-y divide-border">
            {runs.map((run) => (
              <div
                key={run.id}
                className="flex items-center gap-3 p-3 text-sm"
              >
                <Badge
                  className={RUN_STATUS_BADGE[run.status] ?? ""}
                  variant="outline"
                >
                  {run.status}
                </Badge>
                <div className="flex-1 min-w-0 text-xs text-muted-foreground">
                  {run.start_at &&
                    new Date(run.start_at).toLocaleString()}
                  {run.end_at && run.start_at && (
                    <>
                      {" → "}
                      {new Date(run.end_at).toLocaleString()}
                    </>
                  )}
                </div>
                {run.retry_count > 0 && (
                  <span className="text-[10px] text-muted-foreground">
                    重试 {run.retry_count}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
