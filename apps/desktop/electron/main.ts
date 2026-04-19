import { app, BrowserWindow, ipcMain, shell } from "electron";
import path from "node:path";
import { fileURLToPath } from "node:url";
import Store from "electron-store";
import { isAllowedExternalURL, safeIPC, validateOpenablePath } from "./security";
import type { SessionUser } from "@myteam/client-core";
import {
  IPC_CHANNELS,
  type DesktopAuthSession,
  type DesktopShellConfig,
} from "./preload-api";
import { resolveDesktopConfig } from "../src/lib/default-config";
import { NativeBridge } from "./native-bridge";
import { DesktopRuntimeController } from "./runtime-controller";

const moduleDir = path.dirname(fileURLToPath(import.meta.url));
const projectRoot = path.resolve(moduleDir, "../../..");
const VITE_DEV_SERVER_URL = process.env.VITE_DEV_SERVER_URL;
const desktopConfig = resolveDesktopConfig(
  VITE_DEV_SERVER_URL ? "development" : "production",
  process.env,
);
const { appUrl, apiBaseUrl, wsUrl } = desktopConfig;

const store = new Store<Record<string, string>>({
  name: "desktop-preferences",
});

const nativeBridge = new NativeBridge(projectRoot);
const runtimeController = new DesktopRuntimeController(projectRoot, apiBaseUrl);

let mainWindow: BrowserWindow | null = null;

function createMainWindow() {
  mainWindow = new BrowserWindow({
    width: 1440,
    height: 920,
    minWidth: 1180,
    minHeight: 760,
    backgroundColor: "#08090a",
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 16, y: 18 },
    webPreferences: {
      preload: path.join(moduleDir, "preload.mjs"),
      // Hardening per Electron security guide. Defaults are already
      // secure in Electron 20+, but explicit pins protect against
      // future regressions if a contributor copy-pastes from old docs.
      // Issue #43.
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      // webSecurity off only in dev so the renderer can hit the API on
      // a different origin without a CORS proxy. Production keeps it
      // on (CORS is configured server-side via FRONTEND_ORIGIN).
      webSecurity: !VITE_DEV_SERVER_URL,
    },
  });

  if (VITE_DEV_SERVER_URL) {
    void mainWindow.loadURL(VITE_DEV_SERVER_URL);
  } else {
    void mainWindow.loadFile(path.join(moduleDir, "../dist/index.html"));
  }
}

async function fetchJSON<T>(
  url: string,
  init?: RequestInit,
): Promise<T> {
  const response = await fetch(url, init);
  if (!response.ok) {
    const body = await response.text();
    throw new Error(body || `Request failed: ${response.status}`);
  }
  return (await response.json()) as T;
}

async function resolveSessionFromToken(token: string): Promise<DesktopAuthSession> {
  const user = await fetchJSON<SessionUser>(`${apiBaseUrl}/api/me`, {
    headers: {
      Authorization: `Bearer ${token}`,
    },
  });

  return {
    token,
    user,
    serverUrl: apiBaseUrl,
    appUrl,
  };
}

async function createPersonalAccessToken(jwtToken: string): Promise<string> {
  const hostname = "MyTeam Desktop";
  const response = await fetchJSON<{ token: string }>(`${apiBaseUrl}/api/tokens`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${jwtToken}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      name: hostname,
      expires_in_days: 90,
    }),
  });
  return response.token;
}

async function sendVerificationCode(email: string): Promise<void> {
  const normalizedEmail = email.trim().toLowerCase();
  if (!normalizedEmail) {
    throw new Error("Email is required");
  }

  await fetchJSON<{ message: string }>(`${apiBaseUrl}/auth/send-code`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      email: normalizedEmail,
    }),
  });
}

async function verifyCodeAndCreateSession(
  email: string,
  code: string,
): Promise<DesktopAuthSession> {
  const normalizedEmail = email.trim().toLowerCase();
  const normalizedCode = code.trim();
  if (!normalizedEmail || !normalizedCode) {
    throw new Error("Email and code are required");
  }

  const response = await fetchJSON<{ token: string }>(`${apiBaseUrl}/auth/verify-code`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      email: normalizedEmail,
      code: normalizedCode,
    }),
  });

  const pat = await createPersonalAccessToken(response.token);
  await nativeBridge.setToken(pat);
  return resolveSessionFromToken(pat);
}

function registerIpc() {
  ipcMain.handle(IPC_CHANNELS.authSendCode, async (_event, email: string) => {
    await sendVerificationCode(email);
  });
  ipcMain.handle(
    IPC_CHANNELS.authVerifyCode,
    async (_event, email: string, code: string) =>
      verifyCodeAndCreateSession(email, code),
  );
  ipcMain.handle(IPC_CHANNELS.authGetStoredSession, async () => {
    const token = await nativeBridge.getToken();
    if (!token) return null;
    try {
      return await resolveSessionFromToken(token);
    } catch {
      await nativeBridge.deleteToken();
      return null;
    }
  });
  ipcMain.handle(IPC_CHANNELS.authGetStoredToken, async () => nativeBridge.getToken());
  ipcMain.handle(IPC_CHANNELS.authSetStoredToken, async (_event, token: string) => {
    await nativeBridge.setToken(token);
  });
  ipcMain.handle(IPC_CHANNELS.authClearSession, async () => {
    await nativeBridge.deleteToken();
    store.delete("workspaceId");
  });

  ipcMain.handle(
    IPC_CHANNELS.shellOpenExternal,
    safeIPC(IPC_CHANNELS.shellOpenExternal, async (_event, url: string) => {
      if (!isAllowedExternalURL(url)) {
        throw new Error(`shell.openExternal: blocked scheme in ${url}`);
      }
      await shell.openExternal(url);
    }),
  );
  ipcMain.handle(IPC_CHANNELS.shellGetConfig, async (): Promise<DesktopShellConfig> => ({
    apiBaseUrl,
    appUrl,
    wsUrl,
    cliPath: runtimeController.getCliPath(),
    platform: process.platform,
  }));
  ipcMain.handle(IPC_CHANNELS.shellGetPreference, async (_event, key: string) => {
    const value = store.get(key);
    return typeof value === "string" ? value : null;
  });
  ipcMain.handle(
    IPC_CHANNELS.shellSetPreference,
    async (_event, key: string, value: string) => {
      store.set(key, value);
    },
  );
  ipcMain.handle(IPC_CHANNELS.shellRemovePreference, async (_event, key: string) => {
    store.delete(key);
  });
  ipcMain.handle(IPC_CHANNELS.windowMinimize, async () => mainWindow?.minimize());
  ipcMain.handle(IPC_CHANNELS.windowMaximize, async () => {
    if (!mainWindow) return;
    if (mainWindow.isMaximized()) {
      mainWindow.unmaximize();
      return;
    }
    mainWindow.maximize();
  });
  ipcMain.handle(IPC_CHANNELS.windowClose, async () => mainWindow?.close());

  ipcMain.handle(
    IPC_CHANNELS.runtimeStartDaemon,
    safeIPC(IPC_CHANNELS.runtimeStartDaemon, async () => {
      await runtimeController.startDaemon();
    }),
  );
  ipcMain.handle(
    IPC_CHANNELS.runtimeStopDaemon,
    safeIPC(IPC_CHANNELS.runtimeStopDaemon, async () => {
      await runtimeController.stopDaemon();
    }),
  );
  ipcMain.handle(
    IPC_CHANNELS.runtimeList,
    safeIPC(IPC_CHANNELS.runtimeList, async () => runtimeController.listRuntimes()),
  );
  ipcMain.handle(
    IPC_CHANNELS.runtimeWatchWorkspace,
    safeIPC(IPC_CHANNELS.runtimeWatchWorkspace, async (_event, workspaceId: string) => {
      store.set("workspaceId", workspaceId);
      await runtimeController.watchWorkspace(workspaceId);
    }),
  );

  ipcMain.handle(
    IPC_CHANNELS.fileOpenPath,
    safeIPC(IPC_CHANNELS.fileOpenPath, async (_event, targetPath: string) => {
      await nativeBridge.openPath(validateOpenablePath(targetPath));
    }),
  );
  ipcMain.handle(
    IPC_CHANNELS.fileRevealPath,
    safeIPC(IPC_CHANNELS.fileRevealPath, async (_event, targetPath: string) => {
      await nativeBridge.revealPath(validateOpenablePath(targetPath));
    }),
  );
  ipcMain.handle(IPC_CHANNELS.fileOpenPanel, async () => nativeBridge.openPanel());
  ipcMain.on(IPC_CHANNELS.notificationShow, (_event, payload: { title: string; body: string }) => {
    void nativeBridge.showNotification(payload.title, payload.body);
  });
}

app.whenReady().then(() => {
  registerIpc();
  createMainWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createMainWindow();
    }
  });
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.on("web-contents-created", (_event, contents) => {
  contents.setWindowOpenHandler(({ url }) => {
    // Same scheme whitelist as the IPC handler — protects against
    // window.open('javascript:...') bypassing the explicit IPC path.
    if (isAllowedExternalURL(url)) {
      void shell.openExternal(url);
    } else {
      console.warn(`[window-open] blocked: ${url}`);
    }
    return { action: "deny" };
  });
});
