import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

const mockGetWorkspaceMetrics = vi.fn();

vi.mock("@/shared/api", () => ({
  api: {
    getWorkspaceMetrics: (...args: any[]) => mockGetWorkspaceMetrics(...args),
  },
}));

import { MetricsOverview } from "./metrics-overview";

describe("MetricsOverview", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows loading skeletons initially", () => {
    mockGetWorkspaceMetrics.mockReturnValue(new Promise(() => {})); // never resolves
    render(<MetricsOverview />);
    const skeletons = document.querySelectorAll("[data-slot='skeleton']");
    expect(skeletons.length).toBeGreaterThan(0);
  });

  it("renders metric cards after loading", async () => {
    mockGetWorkspaceMetrics.mockResolvedValue({
      task_completion_rate: 0.85,
      average_task_duration_seconds: 3600,
      timeout_rate: 0.05,
      active_projects: 3,
      active_runs: 7,
      pending_escalations: 2,
    });

    render(<MetricsOverview />);

    await waitFor(() => {
      expect(screen.getByText("85%")).toBeInTheDocument();
    });

    expect(screen.getByText("Completion Rate")).toBeInTheDocument();
    expect(screen.getByText("1h")).toBeInTheDocument();
    expect(screen.getByText("Avg Duration")).toBeInTheDocument();
    expect(screen.getByText("5%")).toBeInTheDocument();
    expect(screen.getByText("Timeout Rate")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
    expect(screen.getByText("Active Projects")).toBeInTheDocument();
    expect(screen.getByText("7")).toBeInTheDocument();
    expect(screen.getByText("Active Runs")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText("Pending Escalations")).toBeInTheDocument();
  });

  it("renders nothing on error", async () => {
    mockGetWorkspaceMetrics.mockRejectedValue(new Error("API error"));

    const { container } = render(<MetricsOverview />);

    await waitFor(() => {
      // After error, component should render nothing
      expect(container.children.length).toBe(0);
    });
  });

  it("formats duration correctly for minutes", async () => {
    mockGetWorkspaceMetrics.mockResolvedValue({
      task_completion_rate: 0.5,
      average_task_duration_seconds: 300,
      timeout_rate: 0,
      active_projects: 0,
      active_runs: 0,
      pending_escalations: 0,
    });

    render(<MetricsOverview />);

    await waitFor(() => {
      expect(screen.getByText("5m")).toBeInTheDocument();
    });
  });

  it("formats duration correctly for seconds", async () => {
    mockGetWorkspaceMetrics.mockResolvedValue({
      task_completion_rate: 0.5,
      average_task_duration_seconds: 45,
      timeout_rate: 0,
      active_projects: 0,
      active_runs: 0,
      pending_escalations: 0,
    });

    render(<MetricsOverview />);

    await waitFor(() => {
      expect(screen.getByText("45s")).toBeInTheDocument();
    });
  });
});
