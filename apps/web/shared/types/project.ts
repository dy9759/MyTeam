export interface Project {
  id: string;
  workspace_id: string;
  title: string;
  description?: string;
  status: ProjectStatus;
  schedule_type: ProjectScheduleType;
  cron_expr?: string;
  source_conversations: SourceConversation[];
  channel_id?: string;
  creator_owner_id: string;
  created_at: string;
  updated_at: string;
  // Joined fields (from API)
  plan?: Plan;
  active_run?: ProjectRun;
}

export type ProjectStatus = 'not_started' | 'running' | 'paused' | 'completed' | 'failed' | 'archived';
export type ProjectScheduleType = 'one_time' | 'scheduled' | 'recurring';

export interface SourceConversation {
  conversation_id: string;
  type: 'channel' | 'dm' | 'thread';
  snapshot_at?: string;
}

export interface ProjectVersion {
  id: string;
  project_id: string;
  parent_version_id?: string;
  version_number: number;
  branch_name?: string;
  fork_reason?: string;
  plan_snapshot?: any;
  workflow_snapshot?: any;
  version_status: 'active' | 'archived';
  created_by?: string;
  created_at: string;
}

export interface ProjectRun {
  id: string;
  plan_id?: string;
  project_id: string;
  status: RunStatus;
  start_at?: string;
  end_at?: string;
  step_logs: any[];
  output_refs: any[];
  failure_reason?: string;
  retry_count: number;
  created_at: string;
}

export type RunStatus = 'pending' | 'running' | 'paused' | 'completed' | 'failed' | 'cancelled';

export interface CreateProjectFromChatRequest {
  title: string;
  source_refs: { type: 'channel' | 'dm' | 'thread'; id: string }[];
  agent_ids: string[];
  schedule_type: ProjectScheduleType;
  cron_expr?: string;
}

// Re-export Plan from workflow since project references it
import type { Plan } from './workflow';
export type { Plan };
