"use client";

import { useEffect, useRef, useState, useCallback, useMemo } from "react";
import { useParams, useRouter } from "next/navigation";
import { toast } from "sonner";
import {
  ArrowLeft,
  Loader2,
  Play,
  GitFork,
  Check,
  X as XIcon,
  StopCircle,
  ListTodo,
} from "lucide-react";
import { useProjectStore } from "@/features/projects";
import { useWorkspaceStore } from "@/features/workspace";
import { useIssueStore } from "@/features/issues/store";
import { useIssueViewStore, initFilterWorkspaceSync } from "@/features/issues/stores/view-store";
import { useIssuesScopeStore } from "@/features/issues/stores/issues-scope-store";
import { ViewStoreProvider } from "@/features/issues/stores/view-store-context";
import { filterIssues } from "@/features/issues/utils/filter";
import { BOARD_STATUSES } from "@/features/issues/config";
import { useIssueSelectionStore } from "@/features/issues/stores/selection-store";
import { ListView } from "@/features/issues/components/list-view";
import { BoardView } from "@/features/issues/components/board-view";
import { IssuesHeader } from "@/features/issues/components/issues-header";
import { BatchActionToolbar } from "@/features/issues/components/batch-action-toolbar";
import { PlanEditor } from "@/features/projects/components/plan-editor";
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
import type {
  ProjectStatus,
  ProjectRun,
  PlanStep,
  Agent,
  IssueStatus,
  Task,
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

interface PlanEditorStep extends PlanStep {
  agent_id?: string;
}

/* ---------- Props for inline usage from projects list ---------- */

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
  const [planSteps, setPlanSteps] = useState<PlanEditorStep[]>([]);

  // Channel messages for channel tab
  const [channelMessages, setChannelMessages] = useState<Message[]>([]);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Execution polling
  const execPollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const [executionTasks, setExecutionTasks] = useState<Task[]>([]);
  const [startingExecution, setStartingExecution] = useState(false);

  // Agents for execution task display
  const agents = useWorkspaceStore((s) => s.agents) as Agent[];

  // Issues state for tasks/board tabs
  const allIssues = useIssueStore((s) => s.issues);
  const scope = useIssuesScopeStore((s) => s.scope);
  const viewMode = useIssueViewStore((s) => s.viewMode);
  const statusFilters = useIssueViewStore((s) => s.statusFilters);
  const priorityFilters = useIssueViewStore((s) => s.priorityFilters);
  const assigneeFilters = useIssueViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useIssueViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useIssueViewStore((s) => s.creatorFilters);

  useEffect(() => {
    initFilterWorkspaceSync();
  }, []);

  const scopedIssues = useMemo(() => {
    if (scope === "members")
      return allIssues.filter((i) => i.assignee_type === "member");
    if (scope === "agents")
      return allIssues.filter((i) => i.assignee_type === "agent");
    return allIssues;
  }, [allIssues, scope]);

  const filteredIssues = useMemo(
    () =>
      filterIssues(scopedIssues, {
        statusFilters,
        priorityFilters,
        assigneeFilters,
        includeNoAssignee,
        creatorFilters,
      }),
    [scopedIssues, statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters]
  );

  const visibleStatuses = useMemo(() => {
    if (statusFilters.length > 0)
      return BOARD_STATUSES.filter((s) => statusFilters.includes(s));
    return BOARD_STATUSES;
  }, [statusFilters]);

  const hiddenStatuses = useMemo(
    () => BOARD_STATUSES.filter((s) => !visibleStatuses.includes(s)),
    [visibleStatuses]
  );

  const handleMoveIssue = useCallback(
    (issueId: string, newStatus: IssueStatus, newPosition?: number) => {
      const viewState = useIssueViewStore.getState();
      if (viewState.sortBy !== "position") {
        viewState.setSortBy("position");
        viewState.setSortDirection("asc");
      }

      const updates: Partial<{ status: IssueStatus; position: number }> = {
        status: newStatus,
      };
      if (newPosition !== undefined) updates.position = newPosition;

      useIssueStore.getState().updateIssue(issueId, updates);

      api.updateIssue(issueId, updates).catch(() => {
        toast.error("移动任务失败");
        api
          .listIssues({ limit: 200 })
          .then((res) => {
            useIssueStore.getState().setIssues(res.issues);
          })
          .catch(console.error);
      });
    },
    []
  );

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

    // Bypass the store's built-in try/catch so we can classify each fetch
    // independently and expose per-call error state to the UI. On success
    // we still push the results into the store so the rest of the page
    // reacts as it did before.
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

  // Sync title and plan steps when project loads
  useEffect(() => {
    if (currentProject) {
      setTitleValue(currentProject.title);
      if (currentProject.plan?.steps) {
        setPlanSteps(currentProject.plan.steps as PlanEditorStep[]);
      }
    }
  }, [currentProject]);

  // Poll channel messages
  useEffect(() => {
    if (pollRef.current) clearInterval(pollRef.current);
    const channelId = currentProject?.channel_id;
    if (!channelId) return;

    async function loadMessages() {
      try {
        const res = await api.getChannelMessages(channelId!);
        setChannelMessages(res.messages);
      } catch {
        // silently fail
      }
    }

    loadMessages();
    pollRef.current = setInterval(loadMessages, 3000);
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [currentProject?.channel_id]);

  // Fetch execution tasks when project has a plan / active run
  const fetchExecutionTasks = useCallback(async () => {
    if (!currentProject?.plan?.id) {
      setExecutionTasks([]);
      return;
    }
    try {
      const tasks = await api.listTasksByPlan(currentProject.plan.id);
      setExecutionTasks(tasks);
    } catch {
      // silently fail
    }
  }, [currentProject?.plan?.id]);

  // Poll execution status
  useEffect(() => {
    if (execPollRef.current) clearInterval(execPollRef.current);
    const activeRun = currentProject?.active_run;
    const isActive =
      activeRun &&
      (activeRun.status === "running" || activeRun.status === "paused");
    if (!isActive) return;

    async function pollExecution() {
      try {
        await Promise.all([
          fetchRuns(id),
          fetchProject(id),
          fetchExecutionTasks(),
        ]);
      } catch {
        // silently fail
      }
    }

    fetchExecutionTasks();

    execPollRef.current = setInterval(pollExecution, 3000);
    return () => {
      if (execPollRef.current) clearInterval(execPollRef.current);
    };
  }, [
    currentProject?.active_run?.status,
    id,
    fetchRuns,
    fetchProject,
    fetchExecutionTasks,
  ]);

  useEffect(() => {
    if (currentProject?.active_run) {
      fetchExecutionTasks();
    }
  }, [currentProject?.active_run, fetchExecutionTasks]);

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
    await approvePlan(id);
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
  const activeRun = currentProject.active_run;

  const nonFatalLoadErrors = [
    loadErrors.versions ? `版本：${loadErrors.versions}` : null,
    loadErrors.runs ? `运行记录：${loadErrors.runs}` : null,
  ].filter(Boolean) as string[];

  return (
    <div className="flex flex-1 min-h-0 flex-col overflow-auto p-6">
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
        </div>
      </div>

      {/* Version selector */}
      {versions.length > 1 && (
        <div className="mb-4 border border-border rounded-lg p-3 bg-card">
          <h3 className="text-sm font-medium mb-2">版本</h3>
          <VersionTree versions={versions} onSelect={() => {}} />
        </div>
      )}

      {/* Tabs - 5 tabs */}
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
          {/* Task brief */}
          {(plan as any)?.task_brief && (
            <div className="border border-border rounded-lg p-4 bg-card">
              <h3 className="text-sm font-medium text-foreground mb-2">任务简报</h3>
              <div className="text-sm text-secondary-foreground whitespace-pre-wrap">
                {(plan as any).task_brief}
              </div>
            </div>
          )}

          {/* Plan steps */}
          <div>
            <h3 className="text-sm font-medium mb-3">计划步骤</h3>
            <PlanEditor
              steps={planSteps}
              onUpdate={setPlanSteps}
              readOnly={approvalStatus === "approved"}
            />
          </div>

          {/* Approval section */}
          <div className="border border-border rounded-lg p-4 bg-card">
            <h3 className="text-sm font-medium text-foreground mb-3">审批</h3>
            {approvalStatus === "draft" ||
            approvalStatus === "pending_approval" ||
            !approvalStatus ? (
              <div className="flex items-center gap-3">
                <Button
                  onClick={handleApprove}
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
                  {approvalStatus === "pending_approval" ? "待审批" : "草稿"}
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

            {/* Start execution button */}
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

        {/* Tab 2: Tasks (List View) */}
        <TabsContent value="tasks" className="flex flex-col flex-1 min-h-0">
          <ViewStoreProvider store={useIssueViewStore}>
            <IssuesHeader scopedIssues={scopedIssues} />
            {scopedIssues.length === 0 ? (
              <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
                <ListTodo className="h-10 w-10 text-muted-foreground/40" />
                <p className="text-sm">暂无任务</p>
                <p className="text-xs">创建一个任务以开始。</p>
              </div>
            ) : (
              <ListView
                issues={filteredIssues}
                visibleStatuses={visibleStatuses}
              />
            )}
            <BatchActionToolbar />
          </ViewStoreProvider>
        </TabsContent>

        {/* Tab 3: Board (Kanban View) */}
        <TabsContent value="board" className="flex flex-col flex-1 min-h-0">
          <ViewStoreProvider store={useIssueViewStore}>
            <IssuesHeader scopedIssues={scopedIssues} />
            {scopedIssues.length === 0 ? (
              <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
                <ListTodo className="h-10 w-10 text-muted-foreground/40" />
                <p className="text-sm">暂无任务</p>
                <p className="text-xs">创建一个任务以开始。</p>
              </div>
            ) : (
              <BoardView
                issues={filteredIssues}
                allIssues={scopedIssues}
                visibleStatuses={visibleStatuses}
                hiddenStatuses={hiddenStatuses}
                onMoveIssue={handleMoveIssue}
              />
            )}
          </ViewStoreProvider>
        </TabsContent>

        {/* Tab 4: Execution */}
        <TabsContent value="execution" className="space-y-4">
          {activeRun ? (
            <div className="space-y-4">
              <RunSummaryBar run={activeRun} tasks={executionTasks} />
              <div className="space-y-3">
                    <h3 className="text-sm font-medium">步骤</h3>
                {executionTasks.length > 0 ? (
                  [...executionTasks]
                    .sort((a, b) => a.step_order - b.step_order)
                    .map((step) => (
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

          {/* Run history */}
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
