import { beforeEach, describe, expect, it, vi } from "vitest";
import { createAuthStore, type NativeSecrets, type SessionUser } from "./index";

function createApiStub() {
  return {
    token: null as string | null,
    workspaceId: null as string | null,
    setToken(token: string | null) {
      this.token = token;
    },
    setWorkspaceId(workspaceId: string | null) {
      this.workspaceId = workspaceId;
    },
    async getMe() {
      if (!this.token) {
        throw new Error("unauthorized");
      }

      return {
        id: "user-1",
        name: "Desk User",
        email: "desk@example.com",
        created_at: "",
        updated_at: "",
        avatar_url: null,
      } satisfies SessionUser;
    },
  };
}

function createSecrets(initialToken: string | null = null): NativeSecrets {
  let token = initialToken;
  return {
    async getToken() {
      return token;
    },
    async setToken(nextToken: string) {
      token = nextToken;
    },
    async deleteToken() {
      token = null;
    },
  };
}

describe("createAuthStore", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("hydrates from native secrets and resolves the current user", async () => {
    const api = createApiStub();
    const store = createAuthStore({
      api,
      secrets: createSecrets("myt_test_token"),
    });

    await store.getState().initialize();

    expect(api.token).toBe("myt_test_token");
    expect(store.getState().user?.email).toBe("desk@example.com");
    expect(store.getState().token).toBe("myt_test_token");
    expect(store.getState().isLoading).toBe(false);
  });

  it("persists a session token and clears it on logout", async () => {
    const api = createApiStub();
    const secrets = createSecrets();
    const store = createAuthStore({ api, secrets });

    await store.getState().setSession("myt_new_token", {
      id: "user-2",
      name: "New User",
      email: "new@example.com",
      avatar_url: null,
      created_at: "",
      updated_at: "",
    });

    expect(api.token).toBe("myt_new_token");
    expect(store.getState().user?.email).toBe("new@example.com");
    expect(store.getState().token).toBe("myt_new_token");
    expect(await secrets.getToken()).toBe("myt_new_token");

    await store.getState().logout();

    expect(api.token).toBeNull();
    expect(api.workspaceId).toBeNull();
    expect(store.getState().user).toBeNull();
    expect(store.getState().token).toBeNull();
    expect(await secrets.getToken()).toBeNull();
  });
});
