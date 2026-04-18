"use client";

import { useEffect } from "react";
import { useTaskStore } from "../task-store";
import type { ParticipantSlot, Task, TaskStatus } from "@/shared/types";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ArtifactReviewCard } from "./artifact-review-card";
import { SlotInputForm } from "./slot-input-form";

interface TaskDetailProps {
  task: Task;
  currentUserId: string | null;
  onTaskStatusChange?: (status: TaskStatus) => void;
}

export function TaskDetail({ task, currentUserId, onTaskStatusChange }: TaskDetailProps) {
  const slots = useTaskStore((s) => s.slotsByTask[task.id] ?? []);
  const executions = useTaskStore((s) => s.executionsByTask[task.id] ?? []);
  const artifacts = useTaskStore((s) => s.artifactsByTask[task.id] ?? []);
  const loadTaskDetails = useTaskStore((s) => s.loadTaskDetails);
  const updateSlot = useTaskStore((s) => s.updateSlot);

  useEffect(() => {
    loadTaskDetails(task.id);
  }, [task.id, loadTaskDetails]);

  const handleSlotSubmit = (slot: ParticipantSlot, taskStatus?: TaskStatus) => {
    updateSlot(slot);
    if (taskStatus) onTaskStatusChange?.(taskStatus);
  };

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h2 className="text-xl font-semibold">{task.title}</h2>
        {task.description && (
          <p className="text-muted-foreground mt-1">{task.description}</p>
        )}
        <div className="mt-2 flex gap-2">
          <Badge>{task.status}</Badge>
          <Badge variant="outline">step {task.step_order}</Badge>
          <Badge variant="outline">{task.collaboration_mode}</Badge>
        </div>
      </div>

      <section>
        <h3 className="mb-2 font-medium">Slots ({slots.length})</h3>
        {slots.length === 0 ? (
          <p className="text-muted-foreground text-sm">None.</p>
        ) : (
          <div className="flex flex-col gap-2">
            {[...slots]
              .sort((a, b) => a.slot_order - b.slot_order)
              .map((slot) => {
                const canSubmitInput = Boolean(
                  currentUserId &&
                  slot.status === "ready" &&
                  slot.slot_type === "human_input" &&
                  slot.participant_type === "member" &&
                  slot.participant_id === currentUserId,
                );

                return (
                  <Card key={slot.id} className="p-2">
                    <div className="flex flex-row items-center justify-between gap-3">
                      <div>
                        <div className="text-sm font-medium">{slot.slot_type}</div>
                        <div className="text-muted-foreground text-xs">
                          trigger: {slot.trigger}
                        </div>
                      </div>
                      <Badge variant="outline">{slot.status}</Badge>
                    </div>
                    {canSubmitInput && (
                      <SlotInputForm slot={slot} onSubmit={handleSlotSubmit} />
                    )}
                  </Card>
                );
              })}
          </div>
        )}
      </section>

      <section>
        <h3 className="mb-2 font-medium">Executions ({executions.length})</h3>
        {executions.length === 0 ? (
          <p className="text-muted-foreground text-sm">None.</p>
        ) : (
          <div className="flex flex-col gap-2">
            {executions.map((e) => (
              <Card key={e.id} className="p-2 text-sm">
                <div className="flex items-center justify-between">
                  <span>
                    attempt {e.attempt} ·{" "}
                    {new Date(e.created_at).toLocaleString()}
                  </span>
                  <Badge
                    variant={e.status === "failed" ? "destructive" : "outline"}
                  >
                    {e.status}
                  </Badge>
                </div>
                {e.error && (
                  <div className="text-destructive mt-1 text-xs">{e.error}</div>
                )}
                {e.cost_usd > 0 && (
                  <div className="text-muted-foreground mt-1 text-xs">
                    cost: ${e.cost_usd.toFixed(4)}
                  </div>
                )}
              </Card>
            ))}
          </div>
        )}
      </section>

      <section>
        <h3 className="mb-2 font-medium">Artifacts ({artifacts.length})</h3>
        {artifacts.length === 0 ? (
          <p className="text-muted-foreground text-sm">None.</p>
        ) : (
          <div className="flex flex-col gap-2">
            {artifacts.map((a) => (
              <ArtifactReviewCard key={a.id} taskID={task.id} artifact={a} />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
