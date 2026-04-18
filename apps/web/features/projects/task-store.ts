"use client";

import { create } from "zustand";
import { api } from "@/shared/api";
import type {
  Task,
  ParticipantSlot,
  Execution,
  Artifact,
  Review,
  CreateTaskRequest,
  CreateReviewRequest,
} from "@/shared/types";

// State for Plan 5 task / slot / execution / artifact / review entities.
// Kept separate from the existing project store to avoid clobbering its API.
interface TaskState {
  tasksByPlan: Record<string, Task[]>;
  slotsByTask: Record<string, ParticipantSlot[]>;
  executionsByTask: Record<string, Execution[]>;
  artifactsByTask: Record<string, Artifact[]>;
  reviewsByArtifact: Record<string, Review[]>;
  loading: Record<string, boolean>;
  error: string | null;
}

interface TaskActions {
  loadTasks: (planID: string) => Promise<void>;
  loadTaskDetails: (taskID: string) => Promise<void>;
  loadArtifactReviews: (artifactID: string) => Promise<void>;
  createTask: (req: CreateTaskRequest) => Promise<Task>;
  updateSlot: (slot: ParticipantSlot) => void;
  submitReview: (req: CreateReviewRequest) => Promise<Review>;
  startRun: (runID: string) => Promise<void>;
}

export const useTaskStore = create<TaskState & TaskActions>((set, get) => ({
  tasksByPlan: {},
  slotsByTask: {},
  executionsByTask: {},
  artifactsByTask: {},
  reviewsByArtifact: {},
  loading: {},
  error: null,

  async loadTasks(planID) {
    set({ error: null, loading: { ...get().loading, [planID]: true } });
    try {
      const tasks = await api.listTasksByPlan(planID);
      set((s) => ({
        tasksByPlan: { ...s.tasksByPlan, [planID]: tasks },
        loading: { ...s.loading, [planID]: false },
      }));
    } catch (e) {
      set((s) => ({
        error: e instanceof Error ? e.message : "Failed to load tasks",
        loading: { ...s.loading, [planID]: false },
      }));
    }
  },

  async loadTaskDetails(taskID) {
    set({ error: null });
    try {
      const [slots, executions, artifacts] = await Promise.all([
        api.listSlotsByTask(taskID),
        api.listExecutionsByTask(taskID),
        api.listArtifactsByTask(taskID),
      ]);
      set((s) => ({
        slotsByTask: { ...s.slotsByTask, [taskID]: slots },
        executionsByTask: { ...s.executionsByTask, [taskID]: executions },
        artifactsByTask: { ...s.artifactsByTask, [taskID]: artifacts },
      }));
    } catch (e) {
      set({ error: e instanceof Error ? e.message : "Failed to load task" });
    }
  },

  async loadArtifactReviews(artifactID) {
    try {
      const reviews = await api.listReviewsForArtifact(artifactID);
      set((s) => ({
        reviewsByArtifact: { ...s.reviewsByArtifact, [artifactID]: reviews },
      }));
    } catch {
      // swallow — reviews are non-critical
    }
  },

  async createTask(req) {
    const task = await api.createTask(req);
    set((s) => {
      const list = s.tasksByPlan[req.plan_id] ?? [];
      return {
        tasksByPlan: { ...s.tasksByPlan, [req.plan_id]: [...list, task] },
      };
    });
    return task;
  },

  updateSlot(slot) {
    set((s) => {
      const list = s.slotsByTask[slot.task_id] ?? [];
      return {
        slotsByTask: {
          ...s.slotsByTask,
          [slot.task_id]: list.map((item) => item.id === slot.id ? slot : item),
        },
      };
    });
  },

  async submitReview(req) {
    const review = await api.createReview(req);
    set((s) => {
      const list = s.reviewsByArtifact[req.artifact_id] ?? [];
      return {
        reviewsByArtifact: {
          ...s.reviewsByArtifact,
          [req.artifact_id]: [...list, review],
        },
      };
    });
    return review;
  },

  async startRun(runID) {
    // Fire-and-forget: SchedulerService.ScheduleRun runs asynchronously and
    // emits run:started over WS, which the realtime sync hook picks up to
    // refresh derived state. No local cache to mutate here.
    await api.startRun(runID);
  },
}));
