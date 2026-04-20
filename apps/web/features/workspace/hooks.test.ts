import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";

vi.mock("@/shared/api", () => ({
  api: {
    updateWorkspace: vi.fn(),
    createMember: vi.fn(),
    listMembers: vi.fn(),
  },
}));

import { api } from "@/shared/api";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "./store";
import { useWorkspaceManagement } from "./hooks";

const baseWorkspace = {
  id: "ws-1",
  name: "Alpha",
  slug: "alpha",
  description: "Workspace",
  context: "Context",
  settings: { existing: true },
  repos: [],
  issue_prefix: "ALP",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

describe("useWorkspaceManagement", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    useAuthStore.setState({
      user: {
        id: "user-1",
        name: "Owner",
        email: "owner@example.com",
        avatar_url: null,
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      },
      isLoading: false,
    });
    useWorkspaceStore.setState({
      workspace: baseWorkspace as any,
      workspaces: [baseWorkspace as any],
      members: [
        {
          id: "member-1",
          workspace_id: "ws-1",
          user_id: "user-1",
          role: "owner",
          created_at: "2026-01-01T00:00:00Z",
          name: "Owner",
          email: "owner@example.com",
          avatar_url: null,
        },
      ] as any,
      agents: [],
      skills: [],
    });
  });

  it("exposes workspace permissions for the signed-in member", () => {
    const { result } = renderHook(() => useWorkspaceManagement());

    expect(result.current.workspace?.id).toBe("ws-1");
    expect(result.current.currentMember?.role).toBe("owner");
    expect(result.current.canManageWorkspace).toBe(true);
    expect(result.current.isOwner).toBe(true);
  });

  it("saves merged workspace settings and updates the workspace store", async () => {
    vi.mocked(api.updateWorkspace).mockResolvedValue({
      ...baseWorkspace,
      settings: {
        existing: true,
        runtime_integrations: {
          default_provider: "codex",
        },
      },
    } as any);

    const { result } = renderHook(() => useWorkspaceManagement());

    await act(async () => {
      await result.current.saveWorkspaceSettings((current) => ({
        ...current,
        runtime_integrations: {
          default_provider: "codex",
        },
      }));
    });

    expect(api.updateWorkspace).toHaveBeenCalledWith("ws-1", {
      settings: {
        existing: true,
        runtime_integrations: {
          default_provider: "codex",
        },
      },
    });
    expect(useWorkspaceStore.getState().workspace?.settings).toEqual({
      existing: true,
      runtime_integrations: {
        default_provider: "codex",
      },
    });
  });

  it("invites a member through the current workspace and refreshes store members", async () => {
    vi.mocked(api.createMember).mockResolvedValue({
      id: "member-2",
      workspace_id: "ws-1",
      user_id: "user-2",
      role: "member",
      created_at: "2026-01-01T00:00:00Z",
      name: "Teammate",
      email: "teammate@example.com",
      avatar_url: null,
    } as any);
    vi.mocked(api.listMembers).mockResolvedValue([
      {
        id: "member-1",
        workspace_id: "ws-1",
        user_id: "user-1",
        role: "owner",
        created_at: "2026-01-01T00:00:00Z",
        name: "Owner",
        email: "owner@example.com",
        avatar_url: null,
      },
      {
        id: "member-2",
        workspace_id: "ws-1",
        user_id: "user-2",
        role: "member",
        created_at: "2026-01-01T00:00:00Z",
        name: "Teammate",
        email: "teammate@example.com",
        avatar_url: null,
      },
    ] as any);

    const { result } = renderHook(() => useWorkspaceManagement());

    await act(async () => {
      await result.current.inviteMember({
        email: "teammate@example.com",
        role: "member",
      });
    });

    expect(api.createMember).toHaveBeenCalledWith("ws-1", {
      email: "teammate@example.com",
      role: "member",
    });
    expect(api.listMembers).toHaveBeenCalledWith("ws-1");
    expect(useWorkspaceStore.getState().members).toHaveLength(2);
  });
});
