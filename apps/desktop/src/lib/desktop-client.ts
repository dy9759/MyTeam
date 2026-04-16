import {
  DesktopApiClient,
  createAuthStore,
  createMessagingStore,
  WORKSPACE_STORAGE_KEY,
  createWorkspaceStore,
  WSClient,
  type MessagingApiClient,
  type NativeSecrets,
  type SessionStorageLike,
  type WSStatus,
} from "@myteam/client-core";
import { create } from "zustand";
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

// WS client singleton — declared here so disconnectWS is hoisted before desktopApi
let wsClient: WSClient | null = null;

export function disconnectWS(): void {
  wsClient?.disconnect();
  wsClient = null;
}

function ensureWSClient(): WSClient {
  if (wsClient) return wsClient;
  const wsUrl =
    (import.meta.env.VITE_WS_URL as string | undefined) ??
    apiBaseUrl.replace(/^http/, "ws") + "/ws";
  wsClient = new WSClient(wsUrl, {
    getToken: () => useDesktopAuthStore.getState().token,
    getWorkspaceId: () => useDesktopWorkspaceStore.getState().workspace?.id ?? null,
    onEvent: (msg) => {
      if (msg.type === "message:created") {
        useDesktopMessagingStore.getState().handleEvent(msg);
      }
    },
  });
  wsClient.subscribeStatus((status) => useWSStatusStore.setState({ status }));
  return wsClient;
}

export const desktopApi = new DesktopApiClient(apiBaseUrl, {
  async onUnauthorized() {
    disconnectWS();
    await window.myteam.auth.clearSession();
    useDesktopWorkspaceStore.getState().clearWorkspace();
    useDesktopAuthStore.setState({
      user: null,
      token: null,
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

export const useWSStatusStore = create<{ status: WSStatus }>(() => ({
  status: "disconnected",
}));

// Messaging store — lives here (not in features/messaging) to break a circular
// dependency: features/messaging would need desktopApi, desktop-client needs
// the store for WS onEvent dispatch. Keeping it here resolves both directions.
const messagingApiAdapter: MessagingApiClient = {
  listConversations: () => desktopApi.listConversations(),
  listChannels: () => desktopApi.listChannels(),
  listMessages: (params) => desktopApi.listMessages(params),
  sendMessage: (params) => desktopApi.sendMessage(params),
  createChannel: (params) => desktopApi.createChannel(params),
};

export const useDesktopMessagingStore = createMessagingStore({
  apiClient: messagingApiAdapter,
  onError: (msg) => {
    // eslint-disable-next-line no-console
    console.error("[messaging]", msg);
  },
});

// Auto-disconnect WS on logout.
useDesktopAuthStore.subscribe((state, prevState) => {
  if (!state.user && prevState.user) {
    disconnectWS();
  }
});

// When workspace loads (first login OR resume), connect WS and provision the personal agent.
// This is the single trigger for both — it fires AFTER workspace.bootstrap() sets workspace,
// which means getWorkspaceId() returns a real value for the WS URL query string.
useDesktopWorkspaceStore.subscribe((state, prevState) => {
  if (state.workspace && !prevState.workspace) {
    // WS connect (or reconnect with correct workspace_id).
    ensureWSClient().connect();
    // Ensure personal agent exists on the server (fire-and-forget).
    void desktopApi.getOrCreateSystemAgent().catch((err) => {
      // eslint-disable-next-line no-console
      console.warn("[bootstrap] ensure system agent failed:", err);
    });
  }
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
    // Fire-and-forget: ensure the personal agent is provisioned on the server.
    // Failure is non-fatal; Session UI will still load without an Assistant DM.
    void desktopApi.getOrCreateSystemAgent().catch((err) => {
      // eslint-disable-next-line no-console
      console.warn("[bootstrap] ensure system agent failed:", err);
    });
    // WS connects via the auth-store subscription above when user populates.
    // Trigger here too in case this app starts with a valid session.
    ensureWSClient().connect();
  }
}
