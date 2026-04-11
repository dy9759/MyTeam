import {
  DesktopApiClient,
  createAuthStore,
  WORKSPACE_STORAGE_KEY,
  createWorkspaceStore,
  type NativeSecrets,
  type SessionStorageLike,
} from "@myteam/client-core";
import { resolveDesktopConfig } from "./default-config";

const apiBaseUrl = resolveDesktopConfig(
  import.meta.env.DEV ? "development" : "production",
  import.meta.env,
).apiBaseUrl;

const preferenceCache = new Map<string, string>();

const preferenceStorage: SessionStorageLike = {
  getItem(key) {
    return preferenceCache.get(key) ?? null;
  },
  setItem(key, value) {
    preferenceCache.set(key, value);
    void window.myteam.shell.setPreference(key, value);
  },
  removeItem(key) {
    preferenceCache.delete(key);
    void window.myteam.shell.removePreference(key);
  },
};

const secrets: NativeSecrets = {
  getToken: () => window.myteam.auth.getStoredToken(),
  setToken: (token: string) => window.myteam.auth.setStoredToken(token),
  deleteToken: () => window.myteam.auth.clearSession(),
};

export const desktopApi = new DesktopApiClient(apiBaseUrl, {
  async onUnauthorized() {
    await window.myteam.auth.clearSession();
    useDesktopWorkspaceStore.getState().clearWorkspace();
    useDesktopAuthStore.setState({
      user: null,
      isLoading: false,
    });
  },
});

export const useDesktopAuthStore = createAuthStore({
  api: desktopApi,
  secrets,
});

export const useDesktopWorkspaceStore = createWorkspaceStore({
  api: desktopApi,
  storage: preferenceStorage,
});

export async function bootstrapDesktopApp() {
  const storedWorkspaceId = await window.myteam.shell.getPreference(WORKSPACE_STORAGE_KEY);
  if (storedWorkspaceId) {
    preferenceCache.set(WORKSPACE_STORAGE_KEY, storedWorkspaceId);
  }

  const session = await window.myteam.auth.getStoredSession();
  if (session) {
    await useDesktopAuthStore.getState().setSession(session.token, session.user);
  } else {
    await useDesktopAuthStore.getState().initialize();
  }

  if (useDesktopAuthStore.getState().user) {
    await useDesktopWorkspaceStore.getState().bootstrap(storedWorkspaceId);
  }
}
