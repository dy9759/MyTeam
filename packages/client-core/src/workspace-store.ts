import { create } from "zustand";
import type {
  SessionAgent,
  SessionBrowserContext,
  SessionBrowserTab,
  SessionCollaborator,
  SessionMember,
  SessionRuntime,
  SessionStorageLike,
  SessionWorkspace,
  SessionWorkspaceSnapshot,
  WorkspaceApiClient,
} from "./host";
import {
  clearStoredWorkspaceId,
  readStoredWorkspaceId,
  writeStoredWorkspaceId,
} from "./storage-keys";

export interface WorkspaceStoreState {
  workspace: SessionWorkspace | null;
  workspaces: SessionWorkspace[];
  members: SessionMember[];
  agents: SessionAgent[];
  workspaceSnapshot: SessionWorkspaceSnapshot | null;
  browserTabs: SessionBrowserTab[];
  browserContexts: SessionBrowserContext[];
  collaborators: SessionCollaborator[];
  runtimePresence: SessionRuntime[];
  bootstrap: (preferredWorkspaceId?: string | null) => Promise<SessionWorkspace | null>;
  hydrateWorkspace: (
    workspaces: SessionWorkspace[],
    preferredWorkspaceId?: string | null,
  ) => Promise<SessionWorkspace | null>;
  switchWorkspace: (workspaceId: string) => Promise<void>;
  refreshMembers: () => Promise<void>;
  refreshAgents: () => Promise<void>;
  refreshWorkspaceSnapshot: () => Promise<SessionWorkspaceSnapshot | null>;
  clearWorkspace: () => void;
}

export function createWorkspaceStore({
  api,
  storage,
}: {
  api: WorkspaceApiClient;
  storage: SessionStorageLike;
}) {
  return create<WorkspaceStoreState>((set, get) => ({
    workspace: null,
    workspaces: [],
    members: [],
    agents: [],
    workspaceSnapshot: null,
    browserTabs: [],
    browserContexts: [],
    collaborators: [],
    runtimePresence: [],

    bootstrap: async (preferredWorkspaceId) => {
      const workspaces = await api.listWorkspaces();
      return get().hydrateWorkspace(
        workspaces,
        preferredWorkspaceId ?? readStoredWorkspaceId(storage),
      );
    },

    hydrateWorkspace: async (workspaces, preferredWorkspaceId) => {
      set({ workspaces });

      const nextWorkspace =
        (preferredWorkspaceId
          ? workspaces.find((workspace) => workspace.id === preferredWorkspaceId)
          : null) ??
        workspaces[0] ??
        null;

      if (!nextWorkspace) {
        api.setWorkspaceId(null);
        clearStoredWorkspaceId(storage);
        set({
          workspace: null,
          members: [],
          agents: [],
          workspaceSnapshot: null,
          browserTabs: [],
          browserContexts: [],
          collaborators: [],
          runtimePresence: [],
        });
        return null;
      }

      api.setWorkspaceId(nextWorkspace.id);
      writeStoredWorkspaceId(storage, nextWorkspace.id);
      set({ workspace: nextWorkspace });

      const [members, agents, snapshot] = await Promise.all([
        api.listMembers(nextWorkspace.id).catch(() => []),
        api.listAgents({
          workspace_id: nextWorkspace.id,
          include_archived: true,
        }).catch(() => []),
        api.getWorkspaceSnapshot().catch(() => null),
      ]);

      set({
        members,
        agents: snapshot?.agents ?? agents,
        workspaceSnapshot: snapshot,
        browserTabs: snapshot?.browser_tabs ?? [],
        browserContexts: snapshot?.browser_contexts ?? [],
        collaborators: snapshot?.collaborators ?? [],
        runtimePresence: snapshot?.runtimes ?? [],
      });

      return nextWorkspace;
    },

    switchWorkspace: async (workspaceId) => {
      const workspace = get().workspaces.find((item) => item.id === workspaceId);
      if (!workspace) return;
      api.setWorkspaceId(workspace.id);
      writeStoredWorkspaceId(storage, workspace.id);
      set({
        workspace,
        members: [],
        agents: [],
        workspaceSnapshot: null,
        browserTabs: [],
        browserContexts: [],
        collaborators: [],
        runtimePresence: [],
      });
      await get().hydrateWorkspace(get().workspaces, workspace.id);
    },

    refreshMembers: async () => {
      const workspace = get().workspace;
      if (!workspace) return;
      const members = await api.listMembers(workspace.id);
      set({ members });
    },

    refreshAgents: async () => {
      const workspace = get().workspace;
      if (!workspace) return;
      const agents = await api.listAgents({
        workspace_id: workspace.id,
        include_archived: true,
      });
      set({ agents });
    },

    refreshWorkspaceSnapshot: async () => {
      const workspace = get().workspace;
      if (!workspace) return null;
      const snapshot = await api.getWorkspaceSnapshot().catch(() => null);
      if (!snapshot) return null;

      set((state) => ({
        workspaceSnapshot: snapshot,
        agents: snapshot.agents ?? state.agents,
        browserTabs: snapshot.browser_tabs ?? [],
        browserContexts: snapshot.browser_contexts ?? [],
        collaborators: snapshot.collaborators ?? [],
        runtimePresence: snapshot.runtimes ?? [],
      }));
      return snapshot;
    },

    clearWorkspace: () => {
      api.setWorkspaceId(null);
      clearStoredWorkspaceId(storage);
      set({
        workspace: null,
        workspaces: [],
        members: [],
        agents: [],
        workspaceSnapshot: null,
        browserTabs: [],
        browserContexts: [],
        collaborators: [],
        runtimePresence: [],
      });
    },
  }));
}
