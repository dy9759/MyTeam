import { describe, expect, it } from "vitest";

import type {
  Artifact,
  Execution,
  ParticipantSlot,
  Review,
  Task,
} from "@/shared/types";

import {
  selectArtifactsForTask,
  selectExecutionsForTask,
  selectReviewsForArtifact,
  selectSlotsForTask,
  selectTasksForPlan,
} from "./task-store";

function createState() {
  return {
    tasksByPlan: {} as Record<string, Task[]>,
    slotsByTask: {} as Record<string, ParticipantSlot[]>,
    executionsByTask: {} as Record<string, Execution[]>,
    artifactsByTask: {} as Record<string, Artifact[]>,
    reviewsByArtifact: {} as Record<string, Review[]>,
    loading: {},
    error: null,
    loadTasks: async () => {},
    loadTaskDetails: async () => {},
    loadArtifactReviews: async () => {},
    createTask: async () => {
      throw new Error("not implemented");
    },
    updateSlot: () => {},
    submitReview: async () => {
      throw new Error("not implemented");
    },
    startRun: async () => {},
  };
}

describe("task store selectors", () => {
  it("return stable empty arrays when task data is missing", () => {
    const state = createState();

    expect(selectTasksForPlan("plan-1")(state)).toBe(selectTasksForPlan("plan-1")(state));
    expect(selectSlotsForTask("task-1")(state)).toBe(selectSlotsForTask("task-1")(state));
    expect(selectExecutionsForTask("task-1")(state)).toBe(selectExecutionsForTask("task-1")(state));
    expect(selectArtifactsForTask("task-1")(state)).toBe(selectArtifactsForTask("task-1")(state));
    expect(selectReviewsForArtifact("artifact-1")(state)).toBe(
      selectReviewsForArtifact("artifact-1")(state),
    );
  });
});
