"use client";

import { use, useEffect, useState } from "react";
import { api } from "@/shared/api";
import { TaskDetail } from "@/features/projects/components/task-detail";
import { useAuthStore } from "@/features/auth";
import type { Task, TaskStatus } from "@/shared/types";

export default function TaskPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);
  const [task, setTask] = useState<Task | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .getTask(id)
      .then(setTask)
      .catch((e) => setError(e instanceof Error ? e.message : "Failed"));
  }, [id]);

  if (error) return <p className="text-destructive p-4">{error}</p>;
  if (!task) return <p className="text-muted-foreground p-4">Loading…</p>;
  return (
    <div className="p-4">
      <TaskDetail
        task={task}
        currentUserId={currentUserId}
        onTaskStatusChange={(status: TaskStatus) => {
          setTask((current) => current ? { ...current, status } : current);
        }}
      />
    </div>
  );
}
