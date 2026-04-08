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
  content: string;
  content_type: "text" | "json" | "file";
  file_id?: string;
  file_name?: string;
  file_size?: number;
  file_content_type?: string;
  is_impersonated?: boolean;
  metadata?: Record<string, unknown>;
  status: "sent" | "delivered" | "read";
  reply_count?: number;
  created_at: string;
  updated_at: string;
}

export interface Thread {
  id: string;
  channel_id: string;
  title?: string;
  reply_count: number;
  last_reply_at?: string;
  created_at: string;
}

export interface Channel {
  id: string;
  workspace_id: string;
  name: string;
  description?: string;
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
