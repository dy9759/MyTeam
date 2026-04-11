import type { Agent, AgentRuntime } from "./agent";
import type { FileIndex } from "./file";
import type { Channel, Conversation } from "./messaging";
import type { Workspace } from "./workspace";

export interface WorkspaceCollaborator {
  id: string;
  workspace_id: string;
  email: string;
  role: "viewer" | "editor";
  added_by: string | null;
  added_at: string;
}

export interface BrowserContext {
  id: string;
  workspace_id: string;
  name: string;
  domain: string | null;
  status: string;
  created_by: string;
  shared_with: string[];
  created_at: string;
  last_used_at: string;
}

export interface BrowserTab {
  id: string;
  workspace_id: string;
  url: string;
  title: string | null;
  status: string;
  created_by: string;
  shared_with: string[];
  context_id: string | null;
  session_id: string | null;
  live_url: string | null;
  screenshot_url: string | null;
  conversation_id: string | null;
  project_id: string | null;
  created_at: string;
  last_active_at: string;
}

export interface WorkspaceSnapshot {
  workspace: Workspace;
  agents: Agent[];
  conversations: Conversation[];
  channels: Channel[];
  files: FileIndex[];
  browser_tabs: BrowserTab[];
  browser_contexts: BrowserContext[];
  collaborators: WorkspaceCollaborator[];
  inbox: {
    unread_count: number;
  };
  runtimes: AgentRuntime[];
}
