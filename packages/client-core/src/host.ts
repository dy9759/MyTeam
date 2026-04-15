import type {
  Agent,
  BrowserContext,
  BrowserTab,
  MemberWithUser,
  User,
  Workspace,
  WorkspaceCollaborator,
  WorkspaceSnapshot,
  AgentRuntime,
  Message,
} from "../../../apps/web/shared/types";

export type SessionUser = User;
export type SessionWorkspace = Workspace;
export type SessionWorkspaceSnapshot = WorkspaceSnapshot;
export type SessionAgent = Agent;
export type SessionMember = MemberWithUser;
export type SessionBrowserTab = BrowserTab;
export type SessionBrowserContext = BrowserContext;
export type SessionCollaborator = WorkspaceCollaborator;
export type SessionRuntime = AgentRuntime;
export type { Message };

export interface SessionStorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface NativeSecrets {
  getToken(): Promise<string | null>;
  setToken(token: string): Promise<void>;
  deleteToken(): Promise<void>;
}

export interface DesktopShell {
  openExternal(url: string): Promise<void>;
}

export interface FileSystemBridge {
  openPath(path: string): Promise<void>;
  revealPath(path: string): Promise<void>;
}

export interface RuntimeController {
  startDaemon(): Promise<void>;
  stopDaemon(): Promise<void>;
  listRuntimes(): Promise<SessionRuntime[]>;
  watchWorkspace(workspaceId: string): Promise<void>;
}

export interface AuthApiClient {
  setToken(token: string | null): void;
  setWorkspaceId(workspaceId: string | null): void;
  getMe(): Promise<SessionUser>;
}

export interface WorkspaceApiClient {
  setWorkspaceId(workspaceId: string | null): void;
  listWorkspaces(): Promise<SessionWorkspace[]>;
  listMembers(workspaceId: string): Promise<SessionMember[]>;
  listAgents(params?: {
    workspace_id?: string;
    include_archived?: boolean;
  }): Promise<SessionAgent[]>;
  getWorkspaceSnapshot(): Promise<SessionWorkspaceSnapshot>;
}
