import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

vi.mock("@/shared/logger", () => ({
  createLogger: () => ({
    debug: vi.fn(),
    info: vi.fn(),
    error: vi.fn(),
  }),
}));

vi.mock("@/shared/api", () => ({
  api: {
    listProjects: vi.fn(),
    getProject: vi.fn(),
    createProject: vi.fn(),
    createProjectFromChat: vi.fn(),
    updateProject: vi.fn(),
    deleteProject: vi.fn(),
    forkProject: vi.fn(),
    listProjectVersions: vi.fn(),
    listProjectRuns: vi.fn(),
    approvePlan: vi.fn(),
    rejectPlan: vi.fn(),
  },
}));

import { api } from "@/shared/api";
import type { Project } from "@/shared/types";
import { useProjectStore } from "./store";

function buildProject(overrides: Partial<Project> = {}): Project {
  return {
    id: "project-1",
    workspace_id: "workspace-1",
    title: "Project from chat",
    description: "Generated from chat",
    status: "not_started",
    schedule_type: "one_time",
    source_conversations: [],
    creator_owner_id: "owner-1",
    created_at: "2026-04-20T00:00:00Z",
    updated_at: "2026-04-20T00:00:00Z",
    ...overrides,
  };
}

describe("project store", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    useProjectStore.setState({
      projects: [],
      currentProject: null,
      versions: [],
      runs: [],
      loading: true,
    });
  });

  it("createFromChat appends the created project from the wrapped API response", async () => {
    const project = buildProject();
    vi.mocked(api.createProjectFromChat).mockResolvedValue({
      project,
      warnings: [],
    } as any);

    const created = await useProjectStore.getState().createFromChat({
      title: "Project from chat",
      source_refs: [{ type: "channel", id: "channel-1" }],
      agent_ids: [],
      schedule_type: "one_time",
    });

    expect(created).toEqual(project);
    expect(useProjectStore.getState().projects).toEqual([project]);
  });

  it("approves a project plan by plan id and then refreshes the project", async () => {
    const project = buildProject({
      plan: {
        id: "plan-1",
      } as any,
    });

    vi.mocked(api.approvePlan).mockResolvedValue(undefined);
    vi.mocked(api.getProject).mockResolvedValue(project);

    await (useProjectStore.getState() as any).approvePlan("project-1", "plan-1");

    expect(api.approvePlan).toHaveBeenCalledWith("plan-1");
    expect(api.getProject).toHaveBeenCalledWith("project-1");
    expect(useProjectStore.getState().currentProject).toEqual(project);
  });
});
