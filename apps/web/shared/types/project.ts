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
  plan?: PlanSummary;
  active_run?: RunSummary;
}

export type ProjectStatus =
  | 'draft'
  | 'scheduled'
  | 'running'
  | 'paused'
  | 'completed'
  | 'failed'
  | 'stopped'
  | 'archived';

export type ProjectScheduleType = 'one_time' | 'scheduled_once' | 'recurring';

export interface PlanSummary {
  id: string;
  title: string;
  approval_status: string;
}

export interface RunSummary {
  id: string;
  status: string;
  start_at?: string;
}

export interface SourceConversation {
  conversation_id: string;
  type: 'channel' | 'dm' | 'thread';
  snapshot_at?: string;
}

export interface ProjectBranch {
  id: string;
  project_id: string;
  name: string;
  parent_branch_id?: string;
  is_default: boolean;
  status: 'active' | 'merged' | 'archived';
  created_by: string;
  created_at: string;
}

export interface ProjectVersion {
  id: string;
  project_id: string;
  parent_version_id?: string;
  version_number: number;
  branch_name?: string;
  branch_id?: string;
  fork_reason?: string;
  plan_snapshot?: unknown;
  workflow_snapshot?: unknown;
  version_status: 'active' | 'ready' | 'running' | 'completed' | 'failed' | 'cancelled' | 'archived';
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
  step_logs: unknown[];
  output_refs: unknown[];
  failure_reason?: string;
  retry_count: number;
  created_at: string;
}

export type RunStatus =
  | 'pending'
  | 'queued'
  | 'running'
  | 'blocked'
  | 'paused'
  | 'success'
  | 'partial_success'
  | 'completed'
  | 'failed'
  | 'cancelled';

export interface ProjectResult {
  id: string;
  run_id: string;
  project_id: string;
  version_id?: string;
  summary?: string;
  artifacts: unknown[];
  deliverables: unknown[];
  acceptance_status: 'pending' | 'accepted' | 'rejected';
  accepted_by?: string;
  created_at: string;
}

export interface CreateProjectFromChatRequest {
  title: string;
  source_refs: { type: 'channel' | 'dm' | 'thread'; id: string }[];
  agent_ids: string[];
  schedule_type: ProjectScheduleType;
  cron_expr?: string;
}

export interface ProjectContext {
  id: string;
  project_id: string;
  source_type: 'channel' | 'dm' | 'thread';
  source_name?: string;
  message_count: number;
  imported_at: string;
}

export interface TaskBrief {
  goal?: string;
  background?: string;
  referenced_files?: { file_id: string; file_name: string; description?: string }[];
  constraints?: string[];
  participant_scope?: string;
  deliverables?: { name: string; description: string; type: string }[];
  acceptance_criteria?: string[];
  timeline?: string;
}

import type { Plan } from './workflow';
export type { Plan };
