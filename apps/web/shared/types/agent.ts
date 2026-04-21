// AgentType — collapsed to two values per Account PRD §6.2
export type AgentType = "personal_agent" | "system_agent";

// AgentStatus — single 7-value enum (PRD §3.4)
export type AgentStatus =
  | "offline"
  | "online"
  | "idle"
  | "busy"
  | "blocked"
  | "degraded"
  | "suspended";

// AgentScope — System Agent's functional scope. `null` means the global System Agent.
export type AgentScope = "account" | "conversation" | "project" | "file" | null;

export interface IdentityCard {
  title?: string;
  capabilities: string[];
  tools: string[];
  skills: string[];
  subagents: string[];
  completed_projects: { project_id: string; title: string; completed_at: string }[];
  description_auto: string;
  description_manual: string;
  needs_attention?: boolean;
  pinned_fields?: string[];
  visibility?: string;
}

export type AgentRuntimeMode = "local" | "cloud";

export type AgentVisibility = "workspace" | "private";

export interface RuntimeDevice {
  id: string;
  workspace_id: string;
  daemon_id: string | null;
  name: string;
  mode: AgentRuntimeMode;
  provider: string;
  status: "online" | "offline" | "degraded";
  device_info: string;
  metadata: Record<string, unknown>;
  last_heartbeat_at: string | null;
  concurrency_limit: number;
  current_load: number;
  lease_expires_at: string | null;
  created_at: string;
  updated_at: string;
  server_host?: string;
  working_dir?: string;
  capabilities?: string[];
  readiness?: string;
}

export type AgentRuntime = RuntimeDevice;

export interface AgentTask {
  id: string;
  agent_id: string;
  runtime_id: string;
  issue_id: string;
  status: "queued" | "dispatched" | "running" | "completed" | "failed" | "cancelled";
  priority: number;
  dispatched_at: string | null;
  started_at: string | null;
  completed_at: string | null;
  result: unknown;
  error: string | null;
  created_at: string;
}

export interface Agent {
  id: string;
  workspace_id: string;
  runtime_id: string;
  owner_id: string | null;
  owner_type: "user" | "organization";
  agent_type: AgentType;
  scope: AgentScope;
  name: string;
  display_name?: string;
  description: string;
  instructions: string;
  avatar_url: string | null;
  visibility: AgentVisibility;
  status: AgentStatus;
  identity_card?: IdentityCard;
  last_active_at?: string;
  max_concurrent_tasks: number;
  skills: Skill[];
  auto_reply_enabled?: boolean;
  auto_reply_config?: AgentAutoReplyConfig;
  needs_attention?: boolean;
  needs_attention_reason?: string | null;
  created_at: string;
  updated_at: string;
  archived_at: string | null;
  archived_by: string | null;
}

export interface CreateAgentRequest {
  name: string;
  description?: string;
  instructions?: string;
  avatar_url?: string;
  runtime_id: string;
  visibility?: AgentVisibility;
  max_concurrent_tasks?: number;
}

export interface UpdateAgentRequest {
  name?: string;
  description?: string;
  instructions?: string;
  avatar_url?: string;
  runtime_id?: string;
  visibility?: AgentVisibility;
  status?: AgentStatus;
  max_concurrent_tasks?: number;
}

// Skills

export interface Skill {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  content: string;
  config: Record<string, unknown>;
  category: string;
  source: "manual" | "bundle" | "upload";
  source_ref?: string | null;
  is_global: boolean;
  files?: SkillFile[];
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface Subagent {
  id: string;
  workspace_id: string | null;
  name: string;
  description: string;
  category: string;
  is_global: boolean;
  source: "manual" | "bundle" | "upload";
  source_ref?: string | null;
  instructions: string;
  created_at: string;
  updated_at: string;
  skills?: Skill[];
}

export interface CreateSubagentRequest {
  name: string;
  description?: string;
  instructions?: string;
  category?: string;
}

export interface UpdateSubagentRequest {
  name?: string;
  description?: string;
  instructions?: string;
  category?: string;
}

// Agent-to-agent interaction protocol (mirrors server migration 075 /
// AgentMesh's unified schema). One row per sent message; push via WS,
// pull via `/api/agents/:id/inbox`.
export interface AgentInteraction {
  id: string;
  workspace_id?: string;
  from_id: string;
  from_type: "agent" | "user";
  target: {
    agent_id?: string;
    channel?: string;
    capability?: string;
    session_id?: string;
  };
  type: "message" | "task" | "query" | "event" | "broadcast";
  content_type: "text" | "json" | "file";
  schema?: string;
  payload: unknown;
  metadata?: Record<string, unknown>;
  status: "pending" | "delivered" | "read" | "failed";
  created_at: string;
}

export interface SkillFile {
  id: string;
  skill_id: string;
  path: string;
  content: string;
  created_at: string;
  updated_at: string;
}

export interface CreateSkillRequest {
  name: string;
  description?: string;
  content?: string;
  config?: Record<string, unknown>;
  files?: { path: string; content: string }[];
}

export interface UpdateSkillRequest {
  name?: string;
  description?: string;
  content?: string;
  config?: Record<string, unknown>;
  files?: { path: string; content: string }[];
}

export interface SetAgentSkillsRequest {
  skill_ids: string[];
}

export type RuntimePingStatus = "pending" | "running" | "completed" | "failed" | "timeout";

export interface RuntimePing {
  id: string;
  runtime_id: string;
  status: RuntimePingStatus;
  output?: string;
  error?: string;
  duration_ms?: number;
  created_at: string;
  updated_at: string;
}

export interface RuntimeUsage {
  runtime_id: string;
  date: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
}

export interface RuntimeHourlyActivity {
  hour: number;
  count: number;
}

export type RuntimeUpdateStatus =
  | "pending"
  | "running"
  | "completed"
  | "failed"
  | "timeout";

export interface RuntimeUpdate {
  id: string;
  runtime_id: string;
  status: RuntimeUpdateStatus;
  target_version: string;
  output?: string;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface AgentAutoReplyConfig {
  enabled: boolean;
  model?: string;
  system_prompt?: string;
}

export interface AgentProfile {
  display_name: string;
  avatar?: string;
  bio?: string;
  tags?: string[];
}
