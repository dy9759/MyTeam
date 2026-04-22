import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";

vi.mock("@/shared/api", () => ({
  api: {
    listTasksByPlan: vi.fn(),
    listSlotsByTask: vi.fn(),
    listExecutionsByTask: vi.fn(),
    listArtifactsByTask: vi.fn(),
    listReviewsForArtifact: vi.fn(),
    createTask: vi.fn(),
    createReview: vi.fn(),
    startRun: vi.fn(),
  },
}));

import { api } from "@/shared/api";
import { useTaskStore } from "../task-store";
import { TaskList } from "./task-list";

describe("TaskList", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useTaskStore.setState({
      tasksByPlan: {},
      slotsByTask: {},
      executionsByTask: {},
      artifactsByTask: {},
      reviewsByArtifact: {},
      loading: {},
      error: null,
    });
  });

  it("loads tasks for a plan once on mount", async () => {
    vi.mocked(api.listTasksByPlan).mockResolvedValue([]);

    render(<TaskList planID="plan-1" />);

    await waitFor(() => {
      expect(api.listTasksByPlan).toHaveBeenCalledTimes(1);
    });
    expect(api.listTasksByPlan).toHaveBeenCalledWith("plan-1");
  });
});
