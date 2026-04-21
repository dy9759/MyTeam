"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Bot, Layers, Check, X as XIcon, Pencil, Loader2, Lock } from "lucide-react";

import { api } from "@/shared/api";
import type { Agent, Subagent, Task } from "@/shared/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

// Inline plan stepper — each task renders as an editable card. Two
// edit surfaces:
//   1. Metadata (title / description / required_skills /
//      acceptance_criteria) — expanded with the pencil button.
//   2. Assignee swap (agent or subagent) — a popover on the chip.
//
// The "editable" flag is derived once from project/run state by the
// caller so this component doesn't need to know about approval
// semantics — it just enables or disables edit affordances.

interface Props {
  tasks: Task[];
  agents: Agent[];
  subagents: Subagent[];
  editable: boolean;
  readOnlyReason?: string;
  onTaskUpdated: (task: Task) => void;
}

export function PlanStepper({
  tasks,
  agents,
  subagents,
  editable,
  readOnlyReason,
  onTaskUpdated,
}: Props) {
  if (tasks.length === 0) {
    return (
      <div className="text-center py-8 border-2 border-dashed rounded-lg text-muted-foreground text-sm">
        计划已创建，但尚未生成任务。
      </div>
    );
  }
  return (
    <div className="space-y-3">
      {!editable && readOnlyReason && (
        <div className="flex items-center gap-2 text-xs text-muted-foreground border border-border rounded-md px-3 py-2 bg-muted/40">
          <Lock className="size-3.5" />
          {readOnlyReason}
        </div>
      )}
      {tasks.map((t) => (
        <StepCard
          key={t.id}
          task={t}
          agents={agents}
          subagents={subagents}
          editable={editable}
          onUpdated={onTaskUpdated}
        />
      ))}
    </div>
  );
}

function StepCard({
  task,
  agents,
  subagents,
  editable,
  onUpdated,
}: {
  task: Task;
  agents: Agent[];
  subagents: Subagent[];
  editable: boolean;
  onUpdated: (task: Task) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [title, setTitle] = useState(task.title);
  const [description, setDescription] = useState(task.description ?? "");
  const [acceptance, setAcceptance] = useState(task.acceptance_criteria ?? "");
  const [assigneePickerOpen, setAssigneePickerOpen] = useState(false);

  const assigneeId = task.primary_assignee_id ?? null;
  const assigneeAgent = assigneeId
    ? agents.find((a) => a.id === assigneeId) ?? null
    : null;
  const assigneeSubagent =
    !assigneeAgent && assigneeId
      ? subagents.find((s) => s.id === assigneeId) ?? null
      : null;

  const resetForm = () => {
    setTitle(task.title);
    setDescription(task.description ?? "");
    setAcceptance(task.acceptance_criteria ?? "");
  };

  async function save() {
    setSaving(true);
    try {
      const updated = await api.updateTask(task.id, {
        title: title.trim() || task.title,
        description,
        acceptance_criteria: acceptance,
      });
      onUpdated(updated);
      toast.success("任务已更新");
      setEditing(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  async function changeAssignee(newId: string | null) {
    setAssigneePickerOpen(false);
    if (newId === assigneeId) return;
    try {
      const updated = await api.updateTask(task.id, {
        primary_assignee_id: newId ?? "",
      });
      onUpdated(updated);
      toast.success("分配已更新");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "分配失败");
    }
  }

  return (
    <div className="border border-border rounded-lg bg-card overflow-hidden">
      <div className="px-4 py-3 flex items-start gap-3">
        <div className="w-7 h-7 rounded-full bg-muted text-sm font-semibold grid place-items-center shrink-0">
          {task.step_order}
        </div>
        <div className="flex-1 min-w-0">
          {editing ? (
            <div className="space-y-2">
              <Input
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                className="text-sm font-medium h-auto py-1.5"
                placeholder="任务标题"
              />
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={2}
                placeholder="描述"
                className="text-xs"
              />
              <Textarea
                value={acceptance}
                onChange={(e) => setAcceptance(e.target.value)}
                rows={2}
                placeholder="验收标准"
                className="text-xs"
              />
            </div>
          ) : (
            <>
              <div className="text-sm font-medium">{task.title}</div>
              {task.description && (
                <div className="text-xs text-muted-foreground whitespace-pre-wrap mt-1">
                  {task.description}
                </div>
              )}
              {task.acceptance_criteria && (
                <div className="text-[11px] text-muted-foreground/80 mt-1 font-mono">
                  验收:{task.acceptance_criteria}
                </div>
              )}
            </>
          )}

          <div className="flex flex-wrap items-center gap-2 mt-2">
            <AssigneeChip
              agent={assigneeAgent}
              subagent={assigneeSubagent}
              taskId={task.id}
              disabled={!editable || saving}
              open={assigneePickerOpen}
              onOpenChange={setAssigneePickerOpen}
              agents={agents}
              subagents={subagents}
              onPick={changeAssignee}
            />
            {task.required_skills?.map((s) => (
              <Badge key={s} variant="secondary" className="text-[10px]">
                {s}
              </Badge>
            ))}
            <Badge variant="outline" className="text-[10px]">
              {task.status}
            </Badge>
            {task.depends_on?.length > 0 && (
              <span className="text-[10px] text-muted-foreground font-mono">
                依赖 {task.depends_on.length}
              </span>
            )}
          </div>
        </div>

        {editable && (
          <div className="flex flex-col gap-1 shrink-0">
            {editing ? (
              <>
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-7 w-7 p-0"
                  onClick={save}
                  disabled={saving}
                >
                  {saving ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : (
                    <Check className="size-3.5" />
                  )}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-7 w-7 p-0"
                  onClick={() => {
                    resetForm();
                    setEditing(false);
                  }}
                  disabled={saving}
                >
                  <XIcon className="size-3.5" />
                </Button>
              </>
            ) : (
              <Button
                size="sm"
                variant="ghost"
                className="h-7 w-7 p-0"
                onClick={() => setEditing(true)}
              >
                <Pencil className="size-3.5" />
              </Button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function AssigneeChip({
  agent,
  subagent,
  disabled,
  open,
  onOpenChange,
  agents,
  subagents,
  onPick,
}: {
  agent: Agent | null;
  subagent: Subagent | null;
  taskId: string;
  disabled: boolean;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  agents: Agent[];
  subagents: Subagent[];
  onPick: (id: string | null) => void;
}) {
  const label = agent
    ? agent.name
    : subagent
      ? subagent.name
      : "未分配";
  const Icon = subagent ? Layers : Bot;
  return (
    <div className="relative">
      <button
        type="button"
        disabled={disabled}
        onClick={() => onOpenChange(!open)}
        className={`inline-flex items-center gap-1 text-[11px] border border-border rounded-md px-2 py-0.5 bg-background ${
          disabled ? "opacity-60" : "hover:bg-accent"
        }`}
      >
        <Icon className="size-3" />
        <span className="max-w-[160px] truncate">{label}</span>
      </button>
      {open && !disabled && (
        <AssigneePickerMenu
          agents={agents}
          subagents={subagents}
          onPick={onPick}
          onClose={() => onOpenChange(false)}
        />
      )}
    </div>
  );
}

function AssigneePickerMenu({
  agents,
  subagents,
  onPick,
  onClose,
}: {
  agents: Agent[];
  subagents: Subagent[];
  onPick: (id: string | null) => void;
  onClose: () => void;
}) {
  return (
    <>
      <div
        className="fixed inset-0 z-40"
        onClick={onClose}
      />
      <div className="absolute z-50 mt-1 w-56 border border-border rounded-md bg-popover shadow-md p-1 text-sm">
        <button
          type="button"
          className="w-full text-left px-2 py-1.5 rounded hover:bg-accent text-xs text-muted-foreground"
          onClick={() => onPick(null)}
        >
          清空分配
        </button>
        {subagents.length > 0 && (
          <>
            <div className="px-2 pt-2 text-[10px] text-muted-foreground/70 font-mono uppercase tracking-wider">
              Subagents
            </div>
            {subagents.map((s) => (
              <button
                key={s.id}
                type="button"
                className="w-full text-left px-2 py-1.5 rounded hover:bg-accent flex items-center gap-2"
                onClick={() => onPick(s.id)}
              >
                <Layers className="size-3 text-muted-foreground" />
                <span className="truncate">{s.name}</span>
                {s.is_global && (
                  <span className="ml-auto text-[9px] text-muted-foreground font-mono">
                    global
                  </span>
                )}
              </button>
            ))}
          </>
        )}
        {agents.length > 0 && (
          <>
            <div className="px-2 pt-2 text-[10px] text-muted-foreground/70 font-mono uppercase tracking-wider">
              Agents
            </div>
            {agents.map((a) => (
              <button
                key={a.id}
                type="button"
                className="w-full text-left px-2 py-1.5 rounded hover:bg-accent flex items-center gap-2"
                onClick={() => onPick(a.id)}
              >
                <Bot className="size-3 text-muted-foreground" />
                <span className="truncate">{a.name}</span>
              </button>
            ))}
          </>
        )}
      </div>
    </>
  );
}
