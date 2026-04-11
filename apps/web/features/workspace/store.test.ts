import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
  },
}));

vi.mock("@/shared/api", () => ({
  api: {
    getWorkspaceSnapshot: vi.fn(),
  },
}));

import type { WorkspaceSnapshot } from "@/shared/types";
import { api } from "@/shared/api";
import { useRuntimeStore } from "@/features/runtimes";
import { useWorkspaceStore } from "./store";

const snapshotFixture: WorkspaceSnapshot = {
  workspace: {
    id: "ws-1",
    name: "MyTeam",
    slug: "myteam",
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: "MY",
    created_at: "2026-04-10T00:00:00Z",
    updated_at: "2026-04-10T00:00:00Z",
  },
  agents: [],
  conversations: [
    {
      peer_id: "agent-1",
      peer_type: "agent",
      peer_name: "Builder",
      unread_count: 2,
    },
  ],
  channels: [],
  files: [],
  browser_tabs: [
    {
      id: "tab-1",
      workspace_id: "ws-1",
      url: "https://example.com",
      title: "Example",
      status: "active",
      created_by: "member:user-1",
      shared_with: ["agent:agent-1"],
      context_id: null,
      session_id: "session-1",
      live_url: null,
      screenshot_url: null,
      conversation_id: null,
      project_id: null,
      created_at: "2026-04-10T00:00:00Z",
      last_active_at: "2026-04-10T00:00:00Z",
    },
  ],
  browser_contexts: [],
  collaborators: [
    {
      id: "collab-1",
      workspace_id: "ws-1",
      email: "reviewer@example.com",
      role: "editor",
      added_by: "member:user-1",
      added_at: "2026-04-10T00:00:00Z",
    },
  ],
  inbox: {
    unread_count: 4,
  },
  runtimes: [
    {
      id: "runtime-1",
      workspace_id: "ws-1",
      daemon_id: "daemon-1",
      name: "Codex Runtime",
      runtime_mode: "local",
      provider: "codex",
      status: "online",
      device_info: "macbook",
      server_host: "macbook.local",
      working_dir: "/tmp/myteam",
      capabilities: ["code", "review"],
      readiness: "ready",
      metadata: {},
      last_seen_at: "2026-04-10T00:00:00Z",
      last_heartbeat: "2026-04-10T00:00:00Z",
      created_at: "2026-04-10T00:00:00Z",
      updated_at: "2026-04-10T00:00:00Z",
    },
  ],
};

describe("workspace store", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    useWorkspaceStore.setState({
      workspace: snapshotFixture.workspace,
      workspaces: [],
      members: [],
      agents: [],
      skills: [],
      workspaceSnapshot: null,
      browserTabs: [],
      browserContexts: [],
      collaborators: [],
      runtimePresence: [],
    });
    useRuntimeStore.setState({
      runtimes: [],
      selectedId: "",
      fetching: false,
    });
  });

  it("refreshWorkspaceSnapshot hydrates substrate resources and runtime presence", async () => {
    vi.mocked(api.getWorkspaceSnapshot).mockResolvedValue(snapshotFixture);

    const snapshot = await useWorkspaceStore.getState().refreshWorkspaceSnapshot();

    expect(snapshot?.browser_tabs).toHaveLength(1);
    const workspaceState = useWorkspaceStore.getState();
    expect(workspaceState.workspaceSnapshot?.inbox.unread_count).toBe(4);
    expect(workspaceState.browserTabs[0]?.url).toBe("https://example.com");
    expect(workspaceState.collaborators[0]?.email).toBe("reviewer@example.com");
    expect(workspaceState.runtimePresence[0]?.server_host).toBe("macbook.local");
    expect(useRuntimeStore.getState().runtimes[0]?.provider).toBe("codex");
  });
});
