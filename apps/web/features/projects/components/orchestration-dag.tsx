"use client";

import { useMemo } from "react";
import type { Agent, Subagent, Task } from "@/shared/types";

// DAG view — complementary to the force-directed OrchestrationGraph.
// Nodes are tasks arranged into columns by topological rank (the
// longest dependency chain to the task), so the left-to-right reading
// order reflects execution order. Edges are the task-level depends_on
// relation, same source of truth as the graph view but drawn
// rectilinearly so a reviewer can trace I/O without the noise of
// bezier curves.

interface Props {
  tasks: Task[];
  agents: Agent[];
  subagents: Subagent[];
  onSelectTask?: (taskId: string) => void;
  selectedTaskId?: string | null;
}

const COLUMN_WIDTH = 240;
const COLUMN_GAP = 48;
const ROW_HEIGHT = 100;
const ROW_GAP = 16;
const NODE_WIDTH = 208;
const NODE_HEIGHT = 84;

export function OrchestrationDAG({
  tasks,
  agents,
  subagents,
  onSelectTask,
  selectedTaskId,
}: Props) {
  const layout = useMemo(() => buildLayout(tasks), [tasks]);

  if (tasks.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center min-h-[300px] text-sm text-muted-foreground">
        暂无任务
      </div>
    );
  }

  const svgWidth =
    layout.columns.length * COLUMN_WIDTH +
    Math.max(0, layout.columns.length - 1) * COLUMN_GAP +
    32;
  const svgHeight =
    Math.max(...layout.columns.map((c) => c.length)) * (ROW_HEIGHT + ROW_GAP) +
    32;

  return (
    <div className="flex-1 border border-border rounded-lg bg-card overflow-auto">
      <svg width={svgWidth} height={svgHeight} className="block">
        {/* Concrete edge/arrow color — SVG attribute fill/stroke
            don't resolve CSS custom properties, so referencing the
            theme token directly was painting everything black. */}
        {layout.edges.map((e, i) => (
          <path
            key={i}
            d={orthogonalPath(e.from, e.to)}
            stroke="#d9775e"
            strokeWidth={e.done ? 1.6 : 1.2}
            strokeDasharray={e.done || e.active ? "none" : "4 4"}
            opacity={e.active ? 0.9 : e.done ? 0.6 : 0.4}
            fill="none"
            markerEnd="url(#dag-arrow)"
          />
        ))}
        <defs>
          <marker
            id="dag-arrow"
            viewBox="0 0 10 10"
            refX="9"
            refY="5"
            markerWidth="6"
            markerHeight="6"
            orient="auto"
          >
            <path d="M0,0 L10,5 L0,10 z" fill="#d9775e" />
          </marker>
        </defs>
        {layout.nodes.map((n) => (
          <DAGNode
            key={n.task.id}
            task={n.task}
            agents={agents}
            subagents={subagents}
            x={n.x}
            y={n.y}
            selected={selectedTaskId === n.task.id}
            onClick={() => onSelectTask?.(n.task.id)}
          />
        ))}
      </svg>
    </div>
  );
}

interface NodePos {
  task: Task;
  x: number;
  y: number;
}

interface EdgePath {
  from: { x: number; y: number };
  to: { x: number; y: number };
  done: boolean;
  active: boolean;
}

function buildLayout(tasks: Task[]): {
  nodes: NodePos[];
  edges: EdgePath[];
  columns: Task[][];
} {
  if (tasks.length === 0) {
    return { nodes: [], edges: [], columns: [] };
  }
  const byId = new Map<string, Task>();
  for (const t of tasks) byId.set(t.id, t);

  // Rank each task = max chain length through depends_on.
  const rank = new Map<string, number>();
  const visit = (id: string, stack: Set<string>): number => {
    if (rank.has(id)) return rank.get(id)!;
    if (stack.has(id)) return 0; // cycle guard
    stack.add(id);
    const t = byId.get(id);
    if (!t) {
      rank.set(id, 0);
      return 0;
    }
    let best = 0;
    for (const dep of t.depends_on ?? []) {
      if (byId.has(dep)) best = Math.max(best, visit(dep, stack) + 1);
    }
    stack.delete(id);
    rank.set(id, best);
    return best;
  };
  for (const t of tasks) visit(t.id, new Set());

  const maxRank = Math.max(...Array.from(rank.values()), 0);
  const columns: Task[][] = Array.from({ length: maxRank + 1 }, () => []);
  // Stable intra-column order by step_order then id.
  const sorted = [...tasks].sort(
    (a, b) => a.step_order - b.step_order || a.id.localeCompare(b.id),
  );
  for (const t of sorted) columns[rank.get(t.id) ?? 0]!.push(t);

  const positions = new Map<string, { x: number; y: number }>();
  const nodes: NodePos[] = [];
  for (let col = 0; col < columns.length; col++) {
    const xLeft = 16 + col * (COLUMN_WIDTH + COLUMN_GAP);
    for (let row = 0; row < columns[col]!.length; row++) {
      const task = columns[col]![row]!;
      const yTop = 16 + row * (ROW_HEIGHT + ROW_GAP);
      positions.set(task.id, {
        x: xLeft + NODE_WIDTH / 2,
        y: yTop + NODE_HEIGHT / 2,
      });
      nodes.push({ task, x: xLeft, y: yTop });
    }
  }

  const edges: EdgePath[] = [];
  for (const t of tasks) {
    for (const dep of t.depends_on ?? []) {
      const fromCenter = positions.get(dep);
      const toCenter = positions.get(t.id);
      if (!fromCenter || !toCenter) continue;
      // Emit from the right edge of the upstream node into the left
      // edge of the downstream node.
      const from = {
        x: fromCenter.x + NODE_WIDTH / 2,
        y: fromCenter.y,
      };
      const to = {
        x: toCenter.x - NODE_WIDTH / 2,
        y: toCenter.y,
      };
      const upstream = byId.get(dep);
      const done = upstream?.status === "completed" && t.status !== "draft";
      const active =
        (upstream?.status === "completed" &&
          (t.status === "running" || t.status === "assigned")) ||
        false;
      edges.push({ from, to, done, active });
    }
  }

  return { nodes, edges, columns };
}

function orthogonalPath(
  a: { x: number; y: number },
  b: { x: number; y: number },
): string {
  const midX = (a.x + b.x) / 2;
  return `M ${a.x} ${a.y} L ${midX} ${a.y} L ${midX} ${b.y} L ${b.x} ${b.y}`;
}

function DAGNode({
  task,
  agents,
  subagents,
  x,
  y,
  selected,
  onClick,
}: {
  task: Task;
  agents: Agent[];
  subagents: Subagent[];
  x: number;
  y: number;
  selected: boolean;
  onClick: () => void;
}) {
  const assigneeId = task.actual_agent_id ?? task.primary_assignee_id ?? undefined;
  const agent = agents.find((a) => a.id === assigneeId);
  const subagent = subagents.find((s) => s.id === assigneeId);
  const assigneeName =
    agent?.name ?? subagent?.name ?? (assigneeId ? assigneeId.slice(0, 8) : "未分配");
  // Use concrete colors because SVG attribute fill/stroke don't
  // evaluate CSS variables — the theme tokens would fall back to
  // black and paint every node as a solid block. Keep the palette
  // aligned with the shadcn neutral theme we use elsewhere.
  const fill = task.status === "failed"
    ? "rgba(239,68,68,0.10)"
    : task.status === "completed"
      ? "rgba(74,222,128,0.12)"
      : task.status === "running" || task.status === "assigned"
        ? "rgba(94,106,210,0.12)"
        : "#fffdf8";
  const stroke = selected ? "#d9775e" : "#e5d9c4";

  return (
    <g
      transform={`translate(${x} ${y})`}
      style={{ cursor: "pointer" }}
      onClick={onClick}
    >
      <rect
        width={NODE_WIDTH}
        height={NODE_HEIGHT}
        rx={8}
        fill={fill}
        stroke={stroke}
        strokeWidth={selected ? 2 : 1}
      />
      <foreignObject x={10} y={8} width={NODE_WIDTH - 20} height={NODE_HEIGHT - 16}>
        <div
          className="text-[11px] leading-tight text-foreground"
          style={{
            height: "100%",
            display: "flex",
            flexDirection: "column",
            justifyContent: "space-between",
            overflow: "hidden",
          }}
        >
          <div>
            <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground font-mono">
              <span>#{task.step_order}</span>
              <StatusDot status={task.status} />
              <span>{statusLabel(task.status)}</span>
            </div>
            <div className="text-[12px] font-medium mt-1 line-clamp-2">
              {task.title}
            </div>
          </div>
          <div className="text-[10px] text-muted-foreground truncate">
            {assigneeName}
          </div>
        </div>
      </foreignObject>
    </g>
  );
}

function StatusDot({ status }: { status: Task["status"] }) {
  const color =
    status === "completed"
      ? "#4ade80"
      : status === "failed"
        ? "#ef4444"
        : status === "running" || status === "assigned"
          ? "#5e6ad2"
          : "#9ca3af";
  return (
    <span
      aria-hidden
      className="inline-block rounded-full"
      style={{ width: 6, height: 6, background: color }}
    />
  );
}

function statusLabel(s: Task["status"]): string {
  const m: Partial<Record<Task["status"], string>> = {
    draft: "草稿",
    ready: "就绪",
    queued: "排队",
    assigned: "已分配",
    running: "执行中",
    needs_human: "等待",
    under_review: "审核",
    needs_attention: "需关注",
    completed: "已完成",
    failed: "失败",
    cancelled: "已取消",
  };
  return m[s] ?? s;
}
