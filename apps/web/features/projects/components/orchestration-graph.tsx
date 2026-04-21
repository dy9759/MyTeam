"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { Agent, Subagent, Task } from "@/shared/types";

// Port of the Hi-Fi orchestration view (hifi.projects.jsx) driven by
// real MyTeam data. Nodes are the human creator + every distinct
// assignee (agent or subagent) referenced across the plan's tasks.
// Edges encode each task as an arrow from its upstream dependency's
// assignee (or from the creator when the task has no deps) to its
// own assignee. The visual grammar — color by task status, dotted
// pending lines, animated flow packets for running tasks — matches
// the reference mock.

type NodeKind = "human" | "agent" | "subagent";
type EdgeKind = "task" | "review" | "signal";

interface GraphNode {
  id: string;
  label: string;
  kind: NodeKind;
  avatar: string;
  role: string;
  state: "idle" | "running" | "done" | "failed";
  // Normalized coordinates in [0, 1], mapped into the container on layout.
  x: number;
  y: number;
}

interface GraphEdge {
  from: string;
  to: string;
  label: string;
  kind: EdgeKind;
  state: "pending" | "running" | "done" | "failed";
  taskId: string;
}

// ProjectSummary feeds the always-on overview block at the bottom of
// the inspector pane. The block replaces the old Slots tab — instead
// of a full sub-page of slot rows, the right pane shows progress /
// status / completion.
//
// Scope is driven by the current graph selection:
//   - No selection (or creator node) → whole-project stats.
//   - Agent / subagent node selected → stats filtered to tasks owned
//     by that node.
//   - Edge selected → stats for the single task on that edge.
// The parent passes base project info (title / runStatus / thread);
// the component derives counts from `tasks` + selection.
export interface ProjectSummary {
  title?: string;
  status?: string;
  runStatus?: string;
  channelId?: string | null;
  threadId?: string | null;
  onOpenThread?: () => void;
}

interface Props {
  tasks: Task[];
  agents: Agent[];
  subagents: Subagent[];
  creatorLabel?: string;
  projectSummary?: ProjectSummary;
}

const CREATOR_ID = "__creator__";

const STATE_RING: Record<GraphNode["state"], string> = {
  idle: "rgba(60,47,32,0.12)",
  running: "rgba(217,119,87,0.35)",
  done: "rgba(39,166,68,0.3)",
  failed: "rgba(239,68,68,0.35)",
};

// Concrete colors — SVG attribute fill/stroke do not evaluate CSS
// custom properties, so referencing shadcn theme tokens here fell
// back to black and painted the label pills as solid bars on the
// canvas. Palette picked to read against the beige app background.
const EDGE_COLOR: Record<EdgeKind, string> = {
  task: "#d9775e",
  review: "#f0b440",
  signal: "#4ade80",
};

export function OrchestrationGraph({
  tasks,
  agents,
  subagents,
  creatorLabel = "你",
  projectSummary,
}: Props) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ w: 960, h: 560 });
  const [selected, setSelected] = useState<
    { type: "node"; id: string } | { type: "edge"; id: string } | null
  >(null);

  useEffect(() => {
    const ro = new ResizeObserver(() => {
      const r = wrapRef.current?.getBoundingClientRect();
      if (r) setSize({ w: r.width, h: r.height });
    });
    if (wrapRef.current) ro.observe(wrapRef.current);
    return () => ro.disconnect();
  }, []);

  const { nodes, edges } = useMemo(
    () => buildGraph(tasks, agents, subagents, creatorLabel),
    [tasks, agents, subagents, creatorLabel],
  );

  const layout = useMemo(() => {
    const pad = { x: 110, y: 80 };
    const out: Record<string, { x: number; y: number }> = {};
    for (const n of nodes) {
      out[n.id] = {
        x: pad.x + n.x * (size.w - pad.x * 2),
        y: pad.y + n.y * (size.h - pad.y * 2),
      };
    }
    return out;
  }, [nodes, size]);

  const selectedNode =
    selected?.type === "node" ? nodes.find((n) => n.id === selected.id) : null;
  const selectedEdge =
    selected?.type === "edge"
      ? edges.find((e) => edgeId(e) === selected.id)
      : null;

  if (nodes.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center min-h-[300px] text-sm text-muted-foreground">
        暂无可编排的任务数据
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-[520px] border border-border rounded-lg overflow-hidden bg-card">
      {/* Canvas */}
      <div
        ref={wrapRef}
        className="flex-1 min-w-0 relative overflow-hidden"
        style={{
          background:
            "radial-gradient(circle at 50% 50%, rgba(94,106,210,0.06), transparent 60%)",
        }}
      >
        {/* Subtle grid */}
        <div
          aria-hidden
          className="absolute inset-0"
          style={{
            backgroundImage:
              "linear-gradient(rgba(0,0,0,0.03) 1px, transparent 1px), linear-gradient(90deg, rgba(0,0,0,0.03) 1px, transparent 1px)",
            backgroundSize: "32px 32px",
            maskImage:
              "radial-gradient(ellipse 80% 80% at 50% 50%, black, transparent)",
          }}
        />
        <svg
          className="absolute inset-0 w-full h-full"
          style={{ pointerEvents: "none" }}
        >
          <defs>
            {(Object.keys(EDGE_COLOR) as EdgeKind[]).map((k) => (
              <marker
                key={k}
                id={`arr-${k}`}
                viewBox="0 0 10 10"
                refX="9"
                refY="5"
                markerWidth="7"
                markerHeight="7"
                orient="auto"
              >
                <path d="M0,0 L10,5 L0,10 z" fill={EDGE_COLOR[k]} />
              </marker>
            ))}
          </defs>
          {edges.map((e) => {
            const a = layout[e.from];
            const b = layout[e.to];
            if (!a || !b) return null;
            const id = edgeId(e);
            const sel = selected?.type === "edge" && selected.id === id;
            const isActive = e.state === "running";
            const isDone = e.state === "done";
            const path = curveBetween(a, b);
            const color = EDGE_COLOR[e.kind];
            return (
              <g key={id}>
                <path
                  d={path}
                  stroke="transparent"
                  strokeWidth={18}
                  fill="none"
                  style={{ cursor: "pointer", pointerEvents: "stroke" }}
                  onClick={() => setSelected({ type: "edge", id })}
                />
                <path
                  d={path}
                  stroke={color}
                  strokeWidth={sel ? 2.5 : 1.4}
                  strokeDasharray={
                    isDone || isActive ? "none" : "4 4"
                  }
                  fill="none"
                  opacity={sel ? 1 : isActive || isDone ? 0.85 : 0.45}
                  markerEnd={`url(#arr-${e.kind})`}
                />
                {isActive && (
                  <>
                    <circle r="3.5" fill={color} opacity="0.95">
                      <animateMotion
                        dur="2.4s"
                        repeatCount="indefinite"
                        path={path}
                      />
                    </circle>
                    <circle r="2" fill={color} opacity="0.5">
                      <animateMotion
                        dur="2.4s"
                        begin="0.6s"
                        repeatCount="indefinite"
                        path={path}
                      />
                    </circle>
                  </>
                )}
              </g>
            );
          })}
          {edges.map((e) => {
            const a = layout[e.from];
            const b = layout[e.to];
            if (!a || !b) return null;
            const mx = (a.x + b.x) / 2;
            const my = (a.y + b.y) / 2;
            const id = edgeId(e);
            const color = EDGE_COLOR[e.kind];
            return (
              <g
                key={`L-${id}`}
                transform={`translate(${mx} ${my})`}
                style={{ pointerEvents: "all", cursor: "pointer" }}
                onClick={() => setSelected({ type: "edge", id })}
              >
                <rect
                  x={-40}
                  y={-9}
                  width={80}
                  height={18}
                  rx={9}
                  fill="#fffdf8"
                  stroke={color}
                  strokeOpacity={0.4}
                />
                <text
                  x={0}
                  y={3}
                  textAnchor="middle"
                  fontFamily="var(--font-mono, monospace)"
                  fontSize={9}
                  fill={color}
                  opacity={0.9}
                >
                  {truncate(e.label, 10)}
                </text>
              </g>
            );
          })}
        </svg>

        {nodes.map((n) => {
          const p = layout[n.id];
          if (!p) return null;
          const sel = selected?.type === "node" && selected.id === n.id;
          return (
            <NodeBubble
              key={n.id}
              node={n}
              x={p.x}
              y={p.y}
              selected={sel}
              onClick={() => setSelected({ type: "node", id: n.id })}
            />
          );
        })}

        {/* Stats overlay */}
        <div className="absolute top-3 left-3 flex flex-col gap-1.5">
          <StatChip
            label="节点"
            value={`${nodes.length}`}
          />
          <StatChip
            label="连线"
            value={`${edges.length}`}
          />
          <StatChip
            label="运行中"
            value={`${edges.filter((e) => e.state === "running").length}`}
          />
        </div>
      </div>

      {/* Inspector — top half drills into the selected node/edge;
          bottom half is the always-on project summary that replaces
          the old Slots tab (progress / status / done count + thread
          jump). */}
      <aside className="w-[320px] shrink-0 border-l border-border flex flex-col bg-background/60">
        <div className="flex-1 min-h-0 overflow-y-auto p-4">
          {selectedNode ? (
            <NodeInspector
              node={selectedNode}
              tasks={tasks}
              allTasks={tasks}
              nodes={nodes}
            />
          ) : selectedEdge ? (
            <EdgeInspector
              edge={selectedEdge}
              task={tasks.find((t) => t.id === selectedEdge.taskId)}
              nodes={nodes}
            />
          ) : (
            <div className="text-sm text-muted-foreground text-center pt-10">
              点击节点或连线查看详情
            </div>
          )}
        </div>
        {projectSummary && (
          <ProjectSummaryBlock
            summary={projectSummary}
            scope={summaryScope({
              selectedNode,
              selectedEdge,
              tasks,
            })}
          />
        )}
      </aside>
    </div>
  );
}

// summaryScope resolves the set of tasks the overview block should
// aggregate given the current selection.
//   - Node (agent / subagent) → tasks where the node is the assignee.
//   - Edge → single task referenced by that edge.
//   - No selection / creator → whole project.
// The label in the block header mirrors the scope so the user always
// knows what the percentage refers to.
function summaryScope({
  selectedNode,
  selectedEdge,
  tasks,
}: {
  selectedNode: GraphNode | null | undefined;
  selectedEdge: GraphEdge | null | undefined;
  tasks: Task[];
}): { label: string; tasks: Task[] } {
  if (selectedEdge) {
    const t = tasks.find((x) => x.id === selectedEdge.taskId);
    return {
      label: `任务执行情况 · ${t?.title ?? "?"}`,
      tasks: t ? [t] : [],
    };
  }
  if (selectedNode && selectedNode.id !== CREATOR_ID) {
    const owned = tasks.filter(
      (t) => (t.actual_agent_id ?? t.primary_assignee_id) === selectedNode.id,
    );
    return {
      label: `${selectedNode.label} · 执行情况`,
      tasks: owned,
    };
  }
  return { label: "整体执行情况", tasks };
}

function ProjectSummaryBlock({
  summary,
  scope,
}: {
  summary: ProjectSummary;
  scope: { label: string; tasks: Task[] };
}) {
  const completed = scope.tasks.filter((t) => t.status === "completed").length;
  const total = scope.tasks.length;
  const pct = total === 0 ? 0 : Math.round((completed / total) * 100);

  // Per-scope state: aggregate task statuses when we're scoped to an
  // agent; otherwise fall back to the caller-provided runStatus so the
  // whole-project view still reflects active_run.status.
  const scopedStatus =
    total === 0
      ? summary.runStatus
      : scope.tasks.some(
            (t) => t.status === "running" || t.status === "assigned",
          )
        ? "running"
        : scope.tasks.every((t) => t.status === "completed")
          ? "completed"
          : scope.tasks.some((t) => t.status === "failed")
            ? "failed"
            : summary.runStatus;

  const stateClass =
    scopedStatus === "running"
      ? "text-[#d9775e]"
      : scopedStatus === "completed"
        ? "text-[#4ade80]"
        : scopedStatus === "failed"
          ? "text-destructive"
          : "text-muted-foreground";
  return (
    <div className="border-t border-border bg-card/60 p-3 space-y-2 shrink-0">
      <div className="flex items-center justify-between">
        <div
          className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground truncate"
          title={scope.label}
        >
          {scope.label}
        </div>
        <span className={"text-[11px] font-medium " + stateClass}>
          {projectStatusLabel(scopedStatus ?? summary.status)}
        </span>
      </div>
      {summary.title && (
        <div className="text-xs font-semibold truncate" title={summary.title}>
          {summary.title}
        </div>
      )}
      <div className="space-y-1">
        <div className="flex items-center justify-between text-[11px] text-muted-foreground">
          <span>进度</span>
          <span className="font-mono">
            {completed}/{total}
          </span>
        </div>
        <div className="h-1.5 rounded-full bg-muted overflow-hidden">
          <div
            className="h-full bg-[#d9775e] transition-all"
            style={{ width: pct + "%" }}
          />
        </div>
        <div className="text-[10px] text-muted-foreground text-right">
          {pct}%
        </div>
      </div>
      {summary.onOpenThread && (
        <button
          type="button"
          onClick={summary.onOpenThread}
          className="w-full text-[11px] px-2 py-1.5 border border-border rounded bg-background hover:bg-accent transition-colors"
        >
          打开任务 Thread
        </button>
      )}
    </div>
  );
}

function projectStatusLabel(s: string | undefined): string {
  if (!s) return "未开始";
  const map: Record<string, string> = {
    not_started: "未开始",
    pending: "未开始",
    in_progress: "进行中",
    running: "进行中",
    completed: "已完成",
    failed: "失败",
    cancelled: "已取消",
    paused: "已暂停",
  };
  return map[s] ?? s;
}

/* ---------- Graph build ---------- */

function buildGraph(
  tasks: Task[],
  agents: Agent[],
  subagents: Subagent[],
  creatorLabel: string,
): { nodes: GraphNode[]; edges: GraphEdge[] } {
  if (tasks.length === 0) return { nodes: [], edges: [] };
  const byId = new Map<string, Task>();
  for (const t of tasks) byId.set(t.id, t);

  const assigneeIds = Array.from(
    new Set(
      tasks
        .map((t) => t.actual_agent_id ?? t.primary_assignee_id)
        .filter((id): id is string => !!id),
    ),
  );

  const humanNode: GraphNode = {
    id: CREATOR_ID,
    label: creatorLabel,
    kind: "human",
    avatar: creatorLabel.slice(0, 2),
    role: "调度者",
    state: "idle",
    x: 0.5,
    y: 0.15,
  };

  const assigneeNodes: GraphNode[] = assigneeIds.map((id, i) => {
    const agent = agents.find((a) => a.id === id);
    const subagent = subagents.find((s) => s.id === id);
    const name = agent?.name ?? subagent?.name ?? id.slice(0, 8);
    const kind: NodeKind = agent ? "agent" : subagent ? "subagent" : "agent";
    // Aggregate state across tasks this assignee owns.
    const ownTasks = tasks.filter(
      (t) => (t.actual_agent_id ?? t.primary_assignee_id) === id,
    );
    const state = aggregateState(ownTasks);
    // Distribute evenly around a lower-half arc so the creator at the
    // top always feels like the source.
    const theta = Math.PI * (0.15 + (0.7 * (i + 1)) / (assigneeIds.length + 1));
    return {
      id,
      label: name,
      kind,
      avatar: name.slice(0, 2).toUpperCase(),
      role: kind === "subagent" ? "subagent" : "agent",
      state,
      x: 0.5 + 0.38 * Math.cos(theta),
      y: 0.62 + 0.26 * Math.sin(theta - Math.PI / 2),
    };
  });

  const nodes = [humanNode, ...assigneeNodes];

  const edges: GraphEdge[] = tasks.map((t) => {
    const toId = t.actual_agent_id ?? t.primary_assignee_id ?? CREATOR_ID;
    // Pick first resolvable dependency — the assignee of the upstream
    // task is the "from" node; when a task has no deps it comes from
    // the creator.
    let fromId = CREATOR_ID;
    for (const depId of t.depends_on ?? []) {
      const up = byId.get(depId);
      const upAssignee = up?.actual_agent_id ?? up?.primary_assignee_id;
      if (upAssignee) {
        fromId = upAssignee;
        break;
      }
    }
    return {
      from: fromId,
      to: toId,
      label: t.title,
      kind: classifyEdgeKind(t),
      state: mapTaskState(t),
      taskId: t.id,
    };
  });

  return { nodes, edges };
}

function aggregateState(tasks: Task[]): GraphNode["state"] {
  if (tasks.some((t) => t.status === "running" || t.status === "assigned")) {
    return "running";
  }
  if (tasks.some((t) => t.status === "failed")) return "failed";
  if (tasks.length > 0 && tasks.every((t) => t.status === "completed")) {
    return "done";
  }
  return "idle";
}

function classifyEdgeKind(t: Task): EdgeKind {
  if (t.collaboration_mode === "agent_exec_human_review") return "review";
  if (t.collaboration_mode === "agent_prepare_human_action") return "signal";
  return "task";
}

function mapTaskState(t: Task): GraphEdge["state"] {
  if (t.status === "completed") return "done";
  if (t.status === "running" || t.status === "assigned") return "running";
  if (t.status === "failed") return "failed";
  return "pending";
}

/* ---------- SVG path helpers ---------- */

function curveBetween(
  a: { x: number; y: number },
  b: { x: number; y: number },
): string {
  const mx = (a.x + b.x) / 2;
  const my = (a.y + b.y) / 2;
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const curve = Math.min(120, Math.hypot(dx, dy) * 0.25);
  return `M ${a.x} ${a.y} Q ${mx} ${my - curve} ${b.x} ${b.y}`;
}

function edgeId(e: GraphEdge): string {
  return `${e.from}->${e.to}:${e.taskId}`;
}

function truncate(s: string, n: number): string {
  return s.length <= n ? s : s.slice(0, n - 1) + "…";
}

/* ---------- UI pieces ---------- */

function NodeBubble({
  node,
  x,
  y,
  selected,
  onClick,
}: {
  node: GraphNode;
  x: number;
  y: number;
  selected: boolean;
  onClick: () => void;
}) {
  const running = node.state === "running";
  const done = node.state === "done";
  const failed = node.state === "failed";
  const sizePx = node.kind === "human" ? 60 : 48;
  const gradient =
    node.kind === "human"
      ? "linear-gradient(135deg, #7c83ff, #5261d8)"
      : node.kind === "subagent"
        ? "linear-gradient(135deg, #f0b440, #d9775e)"
        : "linear-gradient(135deg, #d9775e, #e6a276)";
  return (
    <button
      type="button"
      onClick={onClick}
      className="absolute z-10 text-center focus:outline-none"
      style={{
        left: x,
        top: y,
        transform: "translate(-50%, -50%)",
      }}
    >
      <div
        className="relative grid place-items-center mx-auto font-semibold text-white"
        style={{
          width: sizePx,
          height: sizePx,
          borderRadius: "50%",
          background: gradient,
          fontSize: node.kind === "human" ? 14 : 12,
          boxShadow: `0 0 0 ${selected ? 3 : 2}px ${
            selected ? "hsl(var(--primary))" : STATE_RING[node.state]
          }, 0 8px 24px rgba(0,0,0,0.12)`,
          border: "2px solid rgba(255,255,255,0.15)",
          animation: running ? "nodepulse 2s infinite" : "none",
        }}
      >
        {node.avatar}
        {done && (
          <span
            className="absolute grid place-items-center text-white font-bold"
            style={{
              right: -4,
              bottom: -4,
              width: 18,
              height: 18,
              borderRadius: "50%",
              background: "#4ade80",
              fontSize: 10,
              border: "2px solid hsl(var(--background))",
            }}
          >
            ✓
          </span>
        )}
        {failed && (
          <span
            className="absolute grid place-items-center text-white font-bold"
            style={{
              right: -4,
              bottom: -4,
              width: 18,
              height: 18,
              borderRadius: "50%",
              background: "#ef4444",
              fontSize: 10,
              border: "2px solid hsl(var(--background))",
            }}
          >
            !
          </span>
        )}
      </div>
      <div className="mt-2 whitespace-nowrap">
        <div className="text-xs font-semibold text-foreground">
          {node.label}
        </div>
        <div className="text-[10px] text-muted-foreground font-mono">
          {node.role}
        </div>
      </div>
      <style jsx>{`
        @keyframes nodepulse {
          0%,
          100% {
            box-shadow: 0 0 0 2px rgba(217, 119, 87, 0.3),
              0 8px 24px rgba(0, 0, 0, 0.12);
          }
          50% {
            box-shadow: 0 0 0 2px rgba(255, 140, 107, 0.55),
              0 0 40px rgba(217, 119, 87, 0.3),
              0 8px 24px rgba(0, 0, 0, 0.12);
          }
        }
      `}</style>
    </button>
  );
}

function StatChip({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center gap-2 px-2.5 py-1 bg-background/85 border border-border rounded-md backdrop-blur-sm">
      <span className="text-[9px] text-muted-foreground font-mono uppercase tracking-wider">
        {label}
      </span>
      <span className="text-[11px] text-foreground">{value}</span>
    </div>
  );
}

function NodeInspector({
  node,
  tasks,
  allTasks,
  nodes,
}: {
  node: GraphNode;
  tasks: Task[];
  allTasks: Task[];
  nodes: GraphNode[];
}) {
  // Creator node has no workload — skip the IO sections.
  const ownTasks =
    node.id === CREATOR_ID
      ? []
      : tasks.filter(
          (t) => (t.actual_agent_id ?? t.primary_assignee_id) === node.id,
        );
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3 pb-3 border-b border-border">
        <div
          className="w-9 h-9 rounded-full grid place-items-center text-white font-semibold text-sm"
          style={{
            background:
              node.kind === "human"
                ? "linear-gradient(135deg, #7c83ff, #5261d8)"
                : node.kind === "subagent"
                  ? "linear-gradient(135deg, #f0b440, #d9775e)"
                  : "linear-gradient(135deg, #d9775e, #e6a276)",
          }}
        >
          {node.avatar}
        </div>
        <div className="flex-1 min-w-0">
          <div className="text-sm font-semibold truncate">{node.label}</div>
          <div className="text-[10px] text-muted-foreground font-mono">
            {nodeKindLabel(node.kind)} · {node.role}
          </div>
        </div>
        <span
          className="text-[10px] px-2 py-0.5 rounded-full font-mono"
          style={{
            background: STATE_RING[node.state],
            color: "hsl(var(--foreground))",
          }}
        >
          {stateLabel(node.state)}
        </span>
      </div>
      {node.id !== CREATOR_ID && (
        <div className="text-xs text-muted-foreground">
          ID: <code className="font-mono">{node.id.slice(0, 12)}</code>
        </div>
      )}

      {ownTasks.length === 0 ? (
        node.id === CREATOR_ID ? (
          <div className="text-xs text-muted-foreground">
            调度者节点，负责发起任务与最终审批。
          </div>
        ) : (
          <div className="text-xs text-muted-foreground">
            暂无分配给该节点的任务。
          </div>
        )
      ) : (
        <div className="space-y-4">
          {ownTasks.map((t) => (
            <TaskIOBlock
              key={t.id}
              task={t}
              allTasks={allTasks}
              nodes={nodes}
            />
          ))}
        </div>
      )}
    </div>
  );
}

/* Inspector block rendering the 输入 / 输出 / 执行过程 tri-panel per
   task. Shown inside NodeInspector when a node owns tasks, and meant
   to answer "what does this subagent actually consume, produce, and
   how is it progressing?" at a glance. */
function TaskIOBlock({
  task,
  allTasks,
  nodes,
}: {
  task: Task;
  allTasks: Task[];
  nodes: GraphNode[];
}) {
  const upstream = (task.depends_on ?? [])
    .map((id) => allTasks.find((x) => x.id === id))
    .filter((x): x is Task => !!x);
  const upstreamLines = upstream.map((u) => {
    const assigneeId = u.actual_agent_id ?? u.primary_assignee_id ?? null;
    const assignee = nodes.find((n) => n.id === assigneeId);
    return {
      id: u.id,
      title: u.title,
      by: assignee?.label ?? "未分配",
    };
  });

  const outputSummary = summarizeResult(task.result);
  const outputCount = Array.isArray(task.output_refs)
    ? task.output_refs.length
    : 0;

  const steps = buildExecutionSteps(task);

  return (
    <div className="rounded-md border border-border/70 bg-card/60 p-2.5 space-y-2">
      <div className="flex items-start justify-between gap-2">
        <div className="text-xs font-semibold leading-snug break-words">
          {task.title}
        </div>
        <span className="shrink-0 text-[10px] px-1.5 py-0.5 rounded font-mono bg-muted text-muted-foreground">
          {taskStatusLabel(task.status)}
        </span>
      </div>

      <IOSection title="输入">
        {task.description && (
          <p className="text-[11px] text-muted-foreground whitespace-pre-wrap break-words">
            {task.description}
          </p>
        )}
        {upstreamLines.length > 0 && (
          <div className="space-y-0.5">
            <div className="text-[10px] text-muted-foreground/70 font-mono uppercase tracking-wider">
              上游产物
            </div>
            {upstreamLines.map((u) => (
              <div
                key={u.id}
                className="text-[11px] text-foreground/80 truncate"
                title={`${u.by} · ${u.title}`}
              >
                · {u.title}
                <span className="text-muted-foreground"> — {u.by}</span>
              </div>
            ))}
          </div>
        )}
        {(task.required_skills?.length ?? 0) > 0 && (
          <div className="flex flex-wrap gap-1 pt-0.5">
            {task.required_skills.map((s) => (
              <span
                key={s}
                className="text-[10px] px-1.5 py-0.5 rounded bg-muted/60 font-mono text-muted-foreground"
              >
                {s}
              </span>
            ))}
          </div>
        )}
        {!task.description &&
          upstreamLines.length === 0 &&
          (task.required_skills?.length ?? 0) === 0 && (
            <EmptyHint>未声明输入</EmptyHint>
          )}
      </IOSection>

      <IOSection title="输出">
        {task.acceptance_criteria && (
          <div>
            <div className="text-[10px] text-muted-foreground/70 font-mono uppercase tracking-wider">
              验收标准
            </div>
            <p className="text-[11px] text-muted-foreground whitespace-pre-wrap break-words">
              {task.acceptance_criteria}
            </p>
          </div>
        )}
        {outputSummary && (
          <div>
            <div className="text-[10px] text-muted-foreground/70 font-mono uppercase tracking-wider">
              结果
            </div>
            <p className="text-[11px] text-foreground/80 whitespace-pre-wrap break-words">
              {outputSummary}
            </p>
          </div>
        )}
        {outputCount > 0 && (
          <div className="text-[11px] text-muted-foreground">
            关联产物: {outputCount} 项
          </div>
        )}
        {task.error && (
          <div className="text-[11px] text-destructive whitespace-pre-wrap break-words">
            错误: {task.error}
          </div>
        )}
        {!task.acceptance_criteria &&
          !outputSummary &&
          outputCount === 0 &&
          !task.error && <EmptyHint>尚无输出</EmptyHint>}
      </IOSection>

      <IOSection title="执行过程">
        <ol className="space-y-1.5">
          {steps.map((s, i) => (
            <li key={i} className="flex items-start gap-2">
              <span
                className="mt-1 shrink-0 inline-block w-1.5 h-1.5 rounded-full"
                style={{ background: s.color }}
              />
              <div className="min-w-0">
                <div className="text-[11px] text-foreground/90 leading-tight">
                  {s.label}
                </div>
                {s.detail && (
                  <div className="text-[10px] text-muted-foreground font-mono leading-tight">
                    {s.detail}
                  </div>
                )}
              </div>
            </li>
          ))}
        </ol>
      </IOSection>
    </div>
  );
}

function IOSection({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1">
      <div className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
        {title}
      </div>
      <div className="space-y-1 pl-0.5">{children}</div>
    </div>
  );
}

function EmptyHint({ children }: { children: React.ReactNode }) {
  return <div className="text-[11px] text-muted-foreground/60">{children}</div>;
}

function summarizeResult(r: unknown): string | null {
  if (!r) return null;
  if (typeof r === "string") return r.trim() || null;
  if (typeof r === "object") {
    const obj = r as Record<string, unknown>;
    const pick = obj.summary ?? obj.output ?? obj.message ?? obj.text;
    if (typeof pick === "string") return pick.trim() || null;
    try {
      const s = JSON.stringify(obj);
      return s.length > 200 ? s.slice(0, 200) + "…" : s;
    } catch {
      return null;
    }
  }
  return String(r);
}

function buildExecutionSteps(task: Task): {
  label: string;
  detail?: string;
  color: string;
}[] {
  const steps: { label: string; detail?: string; color: string }[] = [];
  const hasAssignee = !!(task.actual_agent_id ?? task.primary_assignee_id);
  steps.push({
    label: hasAssignee ? "已分配" : "待分配",
    detail: task.primary_assignee_id?.slice(0, 8),
    color: hasAssignee ? "#d9775e" : "rgba(60,47,32,0.2)",
  });
  if (task.started_at) {
    steps.push({
      label: "开始执行",
      detail: new Date(task.started_at).toLocaleString(),
      color: "#f0b440",
    });
  }
  if (task.current_retry > 0) {
    steps.push({
      label: `重试 ${task.current_retry} 次`,
      color: "#f0b440",
    });
  }
  if (task.status === "under_review") {
    steps.push({ label: "等待审核", color: "#7c83ff" });
  }
  if (task.status === "needs_human" || task.status === "needs_attention") {
    steps.push({ label: "需要人工介入", color: "#ef4444" });
  }
  if (task.completed_at) {
    steps.push({
      label: task.status === "failed" ? "失败" : "已完成",
      detail: new Date(task.completed_at).toLocaleString(),
      color: task.status === "failed" ? "#ef4444" : "#4ade80",
    });
  } else if (!task.started_at) {
    steps.push({ label: "尚未开始", color: "rgba(60,47,32,0.2)" });
  } else {
    steps.push({ label: "进行中…", color: "#d9775e" });
  }
  return steps;
}

function EdgeInspector({
  edge,
  task,
  nodes,
}: {
  edge: GraphEdge;
  task?: Task;
  nodes: GraphNode[];
}) {
  const from = nodes.find((n) => n.id === edge.from);
  const to = nodes.find((n) => n.id === edge.to);
  return (
    <div className="space-y-3 text-sm">
      <div className="pb-3 border-b border-border">
        <div className="text-[10px] text-muted-foreground font-mono uppercase tracking-wider">
          连线 · {edgeKindLabel(edge.kind)}
        </div>
        <div className="font-semibold mt-1">
          {from?.label ?? "?"} → {to?.label ?? "?"}
        </div>
        <div className="text-xs text-muted-foreground mt-0.5">
          {edge.label}
        </div>
      </div>
      {task && (
        <div className="space-y-1.5 text-xs font-mono text-muted-foreground">
          <Row k="状态" v={taskStatusLabel(task.status)} />
          <Row k="模式" v={task.collaboration_mode} />
          <Row k="依赖" v={String(task.depends_on?.length ?? 0)} />
          {task.required_skills && task.required_skills.length > 0 && (
            <Row k="技能" v={task.required_skills.join(", ")} />
          )}
          {task.started_at && (
            <Row
              k="开始"
              v={new Date(task.started_at).toLocaleTimeString()}
            />
          )}
        </div>
      )}
    </div>
  );
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex justify-between gap-2">
      <span className="text-muted-foreground/70">{k}</span>
      <span className="text-foreground truncate text-right">{v}</span>
    </div>
  );
}

function nodeKindLabel(k: NodeKind): string {
  return k === "human" ? "人类" : k === "subagent" ? "Subagent" : "Agent";
}

function stateLabel(s: GraphNode["state"]): string {
  return {
    idle: "空闲",
    running: "运行中",
    done: "已完成",
    failed: "失败",
  }[s];
}

function edgeKindLabel(k: EdgeKind): string {
  return { task: "任务", review: "评审", signal: "信号" }[k];
}

function taskStatusLabel(s: Task["status"]): string {
  const map: Partial<Record<Task["status"], string>> = {
    draft: "草稿",
    ready: "就绪",
    queued: "排队中",
    assigned: "已分配",
    running: "执行中",
    needs_human: "等待输入",
    under_review: "审核中",
    needs_attention: "需关注",
    completed: "已完成",
    failed: "失败",
    cancelled: "已取消",
  };
  return map[s] ?? s;
}
