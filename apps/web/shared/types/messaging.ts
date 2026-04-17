export interface Message {
  id: string;
  workspace_id: string;
  sender_id: string;
  sender_type: "member" | "agent";
  channel_id?: string;
  recipient_id?: string;
  recipient_type?: "member" | "agent";
  session_id?: string;
  thread_id?: string;
  parent_id?: string;
  content: string;
  content_type: "text" | "json" | "file";
  file_id?: string;
  file_name?: string;
  file_size?: number;
  file_content_type?: string;
  is_impersonated?: boolean;
  effective_actor_id?: string | null;
  effective_actor_type?: "member" | "agent" | "system" | null;
  real_operator_id?: string | null;
  real_operator_type?: "member" | "agent" | "system" | null;
  metadata?: Record<string, unknown>;
  status: "sent" | "delivered" | "read";
  reply_count?: number;
  created_at: string;
  updated_at: string;
}

export type ThreadStatus = "active" | "archived";
export type ThreadCreatorType = "member" | "agent" | "system";

export interface Thread {
  id: string;
  channel_id: string;
  workspace_id?: string;
  root_message_id?: string | null;
  issue_id?: string | null;
  title?: string | null;
  status?: ThreadStatus;
  created_by?: string | null;
  created_by_type?: ThreadCreatorType | null;
  metadata?: Record<string, unknown>;
  reply_count: number;
  last_reply_at?: string | null;
  last_activity_at?: string | null;
  created_at: string;
}

export type ThreadContextItemType =
  | "decision"
  | "file"
  | "code_snippet"
  | "summary"
  | "reference";

export type RetentionClass = "permanent" | "ttl" | "temp";

export interface ThreadContextItem {
  id: string;
  workspace_id: string;
  thread_id: string;
  item_type: ThreadContextItemType;
  title: string | null;
  body: string | null;
  metadata: Record<string, unknown>;
  source_message_id: string | null;
  retention_class: RetentionClass;
  expires_at: string | null;
  created_by: string | null;
  created_by_type: ThreadCreatorType | null;
  created_at: string;
}

export interface CreateThreadRequest {
  root_message_id?: string;
  issue_id?: string;
  title?: string;
  status?: ThreadStatus;
  metadata?: Record<string, unknown>;
}

export interface CreateThreadContextItemRequest {
  item_type: ThreadContextItemType;
  title?: string;
  body?: string;
  metadata?: Record<string, unknown>;
  source_message_id?: string;
  retention_class?: RetentionClass;
  expires_at?: string;
}

export interface Channel {
  id: string;
  workspace_id: string;
  name: string;
  description?: string;
  visibility?: "public" | "private" | "invite_code";
  created_by: string;
  created_by_type: "member" | "agent";
  created_at: string;
}

export interface ChannelMember {
  channel_id: string;
  member_id: string;
  member_type: "member" | "agent";
  joined_at: string;
}

export interface Session {
  id: string;
  workspace_id: string;
  title: string;
  creator_id: string;
  creator_type: "member" | "agent";
  status: "active" | "waiting" | "completed" | "failed" | "archived";
  max_turns: number;
  current_turn: number;
  context?: {
    topic?: string;
    files?: Array<{ name: string; content?: string }>;
    code_snippets?: Array<{ language: string; code: string; description: string }>;
    decisions?: Array<{ decision: string; by: string; at: string }>;
    summary?: string;
  };
  issue_id?: string;
  created_at: string;
  updated_at: string;
}

export interface SessionParticipant {
  session_id: string;
  participant_id: string;
  participant_type: "member" | "agent";
  role: "creator" | "participant";
  joined_at: string;
}

export interface Conversation {
  peer_id: string;
  peer_type: "member" | "agent";
  peer_name?: string;
  last_message?: Message;
  unread_count?: number;
}

export interface RemoteSession {
  id: string;
  agent_id: string;
  title?: string;
  status: string;
  events?: RemoteSessionEvent[];
  created_at: string;
  updated_at: string;
}

export interface RemoteSessionEvent {
  id: string;
  type: string;
  data: Record<string, unknown>;
  created_at: string;
}
