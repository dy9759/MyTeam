import type { SessionRuntime, SessionUser } from "@myteam/client-core";

export interface DesktopAuthSession {
  token: string;
  user: SessionUser;
  serverUrl: string;
  appUrl: string;
}

export interface DesktopNotificationPayload {
  title: string;
  body: string;
}

export interface DesktopShellConfig {
  apiBaseUrl: string;
  appUrl: string;
  wsUrl: string;
  cliPath: string;
  platform: string;
}

export const IPC_CHANNELS = {
  authSendCode: "auth:send-code",
  authVerifyCode: "auth:verify-code",
  authGetStoredSession: "auth:get-stored-session",
  authGetStoredToken: "auth:get-stored-token",
  authSetStoredToken: "auth:set-stored-token",
  authClearSession: "auth:clear-session",
  shellOpenExternal: "shell:open-external",
  shellGetConfig: "shell:get-config",
  shellGetPreference: "shell:get-preference",
  shellSetPreference: "shell:set-preference",
  shellRemovePreference: "shell:remove-preference",
  windowMinimize: "window:minimize",
  windowMaximize: "window:maximize",
  windowClose: "window:close",
  runtimeStartDaemon: "runtime:start-daemon",
  runtimeStopDaemon: "runtime:stop-daemon",
  runtimeList: "runtime:list",
  runtimeWatchWorkspace: "runtime:watch-workspace",
  fileOpenPath: "file:open-path",
  fileRevealPath: "file:reveal-path",
  fileOpenPanel: "file:open-panel",
  notificationShow: "notification:show",
} as const;

type Invoke = <T = unknown>(channel: string, ...args: unknown[]) => Promise<T>;
type Send = (channel: string, ...args: unknown[]) => void;

export interface DesktopApi {
  auth: {
    sendCode(email: string): Promise<void>;
    verifyCode(email: string, code: string): Promise<DesktopAuthSession>;
    getStoredSession(): Promise<DesktopAuthSession | null>;
    getStoredToken(): Promise<string | null>;
    setStoredToken(token: string): Promise<void>;
    clearSession(): Promise<void>;
  };
  runtime: {
    startDaemon(): Promise<void>;
    stopDaemon(): Promise<void>;
    listRuntimes(): Promise<SessionRuntime[]>;
    watchWorkspace(workspaceId: string): Promise<void>;
  };
  files: {
    openPath(path: string): Promise<void>;
    revealPath(path: string): Promise<void>;
    openPanel(): Promise<string[]>;
  };
  notifications: {
    show(payload: DesktopNotificationPayload): void;
  };
  shell: {
    openExternal(url: string): Promise<void>;
    getConfig(): Promise<DesktopShellConfig>;
    getPreference(key: string): Promise<string | null>;
    setPreference(key: string, value: string): Promise<void>;
    removePreference(key: string): Promise<void>;
    minimizeWindow(): Promise<void>;
    maximizeWindow(): Promise<void>;
    closeWindow(): Promise<void>;
  };
}

export function buildDesktopApi({
  invoke,
  send,
}: {
  invoke: Invoke;
  send: Send;
}): DesktopApi {
  return {
    auth: {
      sendCode: (email: string) =>
        invoke<void>(IPC_CHANNELS.authSendCode, email),
      verifyCode: (email: string, code: string) =>
        invoke<DesktopAuthSession>(IPC_CHANNELS.authVerifyCode, email, code),
      getStoredSession: () =>
        invoke<DesktopAuthSession | null>(IPC_CHANNELS.authGetStoredSession),
      getStoredToken: () =>
        invoke<string | null>(IPC_CHANNELS.authGetStoredToken),
      setStoredToken: (token: string) =>
        invoke<void>(IPC_CHANNELS.authSetStoredToken, token),
      clearSession: () => invoke<void>(IPC_CHANNELS.authClearSession),
    },
    runtime: {
      startDaemon: () => invoke<void>(IPC_CHANNELS.runtimeStartDaemon),
      stopDaemon: () => invoke<void>(IPC_CHANNELS.runtimeStopDaemon),
      listRuntimes: () => invoke<SessionRuntime[]>(IPC_CHANNELS.runtimeList),
      watchWorkspace: (workspaceId: string) =>
        invoke<void>(IPC_CHANNELS.runtimeWatchWorkspace, workspaceId),
    },
    files: {
      openPath: (path: string) => invoke<void>(IPC_CHANNELS.fileOpenPath, path),
      revealPath: (path: string) =>
        invoke<void>(IPC_CHANNELS.fileRevealPath, path),
      openPanel: () => invoke<string[]>(IPC_CHANNELS.fileOpenPanel),
    },
    notifications: {
      show: (payload: DesktopNotificationPayload) => {
        send(IPC_CHANNELS.notificationShow, payload);
      },
    },
    shell: {
      openExternal: (url: string) =>
        invoke<void>(IPC_CHANNELS.shellOpenExternal, url),
      getConfig: () => invoke<DesktopShellConfig>(IPC_CHANNELS.shellGetConfig),
      getPreference: (key: string) =>
        invoke<string | null>(IPC_CHANNELS.shellGetPreference, key),
      setPreference: (key: string, value: string) =>
        invoke<void>(IPC_CHANNELS.shellSetPreference, key, value),
      removePreference: (key: string) =>
        invoke<void>(IPC_CHANNELS.shellRemovePreference, key),
      minimizeWindow: () => invoke<void>(IPC_CHANNELS.windowMinimize),
      maximizeWindow: () => invoke<void>(IPC_CHANNELS.windowMaximize),
      closeWindow: () => invoke<void>(IPC_CHANNELS.windowClose),
    },
  };
}
