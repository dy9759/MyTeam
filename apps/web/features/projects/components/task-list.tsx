"use client";

import Link from "next/link";
import { useEffect } from "react";
import { selectTasksForPlan, useTaskStore } from "../task-store";
import type { TaskStatus } from "@/shared/types";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";

const STATUS_VARIANT: Record<
  TaskStatus,
  "default" | "secondary" | "outline" | "destructive"
> = {
  draft: "outline",
  ready: "outline",
  queued: "secondary",
  assigned: "secondary",
  running: "default",
  needs_human: "destructive",
  under_review: "default",
  needs_attention: "destructive",
  completed: "default",
  failed: "destructive",
  cancelled: "outline",
  skipped: "outline",
};

export function TaskList({ planID }: { planID: string }) {
  const tasks = useTaskStore(selectTasksForPlan(planID));
  const loading = useTaskStore((s) => s.loading[planID] ?? false);
  const error = useTaskStore((s) => s.error);
  const loadTasks = useTaskStore((s) => s.loadTasks);

  useEffect(() => {
    loadTasks(planID).catch(() => void 0);
  }, [planID, loadTasks]);

  if (loading && tasks.length === 0) {
    return <p className="text-muted-foreground text-sm">Loading…</p>;
  }
  if (error) return <p className="text-destructive text-sm">{error}</p>;
  if (tasks.length === 0) {
    return <p className="text-muted-foreground text-sm">No tasks yet.</p>;
  }

  // Sort by step_order so the visual order reflects DAG layering.
  const sorted = [...tasks].sort((a, b) => a.step_order - b.step_order);

  return (
    <div className="flex flex-col gap-2">
      {sorted.map((t) => (
        <Card key={t.id} className="flex flex-row items-center justify-between gap-3 p-3">
          <div className="min-w-0 flex-1">
            <Link href={`/tasks/${t.id}`} className="font-medium hover:underline">
              {t.title}
            </Link>
            {t.description && (
              <div className="text-muted-foreground mt-1 truncate text-xs">
                {t.description}
              </div>
            )}
            {t.depends_on.length > 0 && (
              <div className="text-muted-foreground mt-1 text-xs">
                Depends on: {t.depends_on.length} task
                {t.depends_on.length > 1 ? "s" : ""}
              </div>
            )}
          </div>
          <Badge variant={STATUS_VARIANT[t.status]}>{t.status}</Badge>
        </Card>
      ))}
    </div>
  );
}
