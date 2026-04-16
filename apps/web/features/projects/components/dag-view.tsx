"use client";

import { useMemo } from "react";
import type { WorkflowStep } from "@/shared/types/workflow";

const STATUS_COLORS: Record<string, string> = {
  pending: "bg-muted text-muted-foreground",
  ready: "bg-blue-500/10 text-blue-400 border-blue-500/30",
  queued: "bg-blue-500/10 text-blue-400",
  assigned: "bg-blue-500/20 text-blue-400",
  running: "bg-blue-500/30 text-blue-300 animate-pulse",
  waiting_input: "bg-yellow-500/20 text-yellow-300",
  blocked: "bg-orange-500/20 text-orange-300",
  retrying: "bg-orange-500/30 text-orange-300 animate-pulse",
  timeout: "bg-red-500/20 text-red-300",
  completed: "bg-green-500/20 text-green-300",
  failed: "bg-red-500/20 text-red-300",
  cancelled: "bg-muted text-muted-foreground line-through",
  skipped: "bg-muted text-muted-foreground italic",
};

interface DagViewProps {
  steps: WorkflowStep[];
  onSelectStep?: (step: WorkflowStep) => void;
  selectedStepId?: string;
}

export function DagView({ steps, onSelectStep, selectedStepId }: DagViewProps) {
  const layers = useMemo(() => {
    const placed = new Set<string>();
    const result: WorkflowStep[][] = [];
    let remaining = [...steps];

    while (remaining.length > 0) {
      const layer: WorkflowStep[] = [];
      const nextRemaining: WorkflowStep[] = [];

      for (const step of remaining) {
        const depsResolved = step.depends_on.every((d) => placed.has(d));
        if (depsResolved) {
          layer.push(step);
        } else {
          nextRemaining.push(step);
        }
      }

      if (layer.length === 0) {
        result.push(nextRemaining);
        break;
      }

      for (const s of layer) placed.add(s.id);
      result.push(layer);
      remaining = nextRemaining;
    }

    return result;
  }, [steps]);

  if (steps.length === 0) {
    return (
      <div className="rounded-xl border border-dashed border-border/70 bg-background/50 p-8 text-center text-sm text-muted-foreground">
        No workflow steps defined.
      </div>
    );
  }

  return (
    <div className="space-y-4 overflow-x-auto">
      {layers.map((layer, layerIdx) => (
        <div key={layerIdx} className="flex flex-wrap gap-3">
          {layerIdx > 0 && (
            <div className="mb-1 w-full border-t border-dashed border-border/40" />
          )}
          <span className="w-full text-[10px] uppercase tracking-wider text-muted-foreground">
            Stage {layerIdx + 1}
          </span>
          {layer.map((step) => (
            <button
              key={step.id}
              type="button"
              onClick={() => onSelectStep?.(step)}
              className={`flex min-w-[200px] flex-col gap-1 rounded-xl border p-3 text-left text-sm transition ${
                selectedStepId === step.id
                  ? "border-primary ring-1 ring-primary"
                  : "border-border/70 hover:border-border"
              } ${STATUS_COLORS[step.status] ?? ""}`}
            >
              <div className="flex items-center justify-between">
                <span className="font-medium">
                  {step.title || step.description.slice(0, 40)}
                </span>
                <span className="rounded-full px-1.5 py-0.5 text-[10px] font-bold">
                  {step.status}
                </span>
              </div>
              {step.agent_id && (
                <span className="text-xs opacity-70">
                  Agent: {step.actual_agent_id || step.agent_id}
                </span>
              )}
              {step.depends_on.length > 0 && (
                <span className="text-[10px] opacity-50">
                  depends on {step.depends_on.length} step(s)
                </span>
              )}
            </button>
          ))}
        </div>
      ))}
    </div>
  );
}
