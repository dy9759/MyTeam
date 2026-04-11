import { describe, expect, it } from "vitest";
import { createWorkspaceStore, type SessionStorageLike } from "./index";

function createStorage(): SessionStorageLike {
  const data = new Map<string, string>();
  return {
    getItem(key: string) {
      return data.get(key) ?? null;
    },
    setItem(key: string, value: string) {
      data.set(key, value);
    },
    removeItem(key: string) {
      data.delete(key);
    },
  };
}

function createWorkspaceApi() {
  return {
    workspaceId: null as string | null,
    setWorkspaceId(nextId: string | null) {
      this.workspaceId = nextId;
    },
    async listWorkspaces() {
      return [
        { id: "ws-1", name: "Workspace One", slug: "workspace-one", description: "", context: "", settings: {}, repos: [], issue_prefix: "W1", created_at: "", updated_at: "" },
        { id: "ws-2", name: "Workspace Two", slug: "workspace-two", description: "", context: "", settings: {}, repos: [], issue_prefix: "W2", created_at: "", updated_at: "" },
      ];
    },
    async listMembers() {
      return [];
    },
    async listAgents() {
      return [];
    },
    async getWorkspaceSnapshot() {
      return {
        workspace: {
          id: "ws-1",
          name: "Workspace One",
          slug: "workspace-one",
          description: "",
          context: "",
          settings: {},
          repos: [],
          issue_prefix: "W1",
          created_at: "",
          updated_at: "",
        },
        agents: [],
        conversations: [],
        channels: [],
        files: [],
        browser_tabs: [],
        browser_contexts: [],
        collaborators: [],
        inbox: {
          unread_count: 0,
        },
        runtimes: [],
      };
    },
  };
}

describe("createWorkspaceStore", () => {
  it("hydrates the preferred workspace and persists it", async () => {
    const api = createWorkspaceApi();
    const storage = createStorage();
    const store = createWorkspaceStore({ api, storage });

    await store.getState().bootstrap("ws-2");

    expect(store.getState().workspace?.id).toBe("ws-2");
    expect(api.workspaceId).toBe("ws-2");
    expect(storage.getItem("myteam_workspace_id")).toBe("ws-2");
  });

  it("refreshes the snapshot without dropping existing workspace identity", async () => {
    const api = createWorkspaceApi();
    const storage = createStorage();
    const store = createWorkspaceStore({ api, storage });

    await store.getState().bootstrap("ws-1");
    await store.getState().refreshWorkspaceSnapshot();

    expect(store.getState().workspace?.id).toBe("ws-1");
    expect(store.getState().workspaceSnapshot?.workspace.id).toBe("ws-1");
  });
});
