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
  // message_ids narrows the source to a specific message subset inside the
  // channel/dm/thread. Omit to use the most recent 100 messages.
  source_refs: {
    type: 'channel' | 'dm' | 'thread';
    id: string;
    message_ids?: string[];
    // Required when type === 'dm' so the backend knows the peer's actor type.
    peer_type?: 'member' | 'agent';
  }[];
  agent_ids: string[];
  schedule_type: ProjectScheduleType;
  cron_expr?: string;
}

export interface CreateProjectFromChatTaskRef {
  id: string;
  local_id: string;
  title: string;
  step_order: number;
  collaboration_mode: string;
  slot_count: number;
}

export interface CreateProjectFromChatResponse {
  project: Project;
  plan?: unknown;
  channel?: Record<string, unknown>;
  tasks: CreateProjectFromChatTaskRef[];
  warnings?: string[];
}

// Re-export Plan from workflow since project references it
import type { Plan } from './workflow';
export type { Plan };

// ===== Plan 5 — Project five-layer model =====

// Task
export type TaskStatus =
  | "draft" | "ready" | "queued" | "assigned" | "running"
  | "needs_human" | "under_review" | "needs_attention"
  | "completed" | "failed" | "cancelled";

export type CollaborationMode =
  | "agent_exec_human_review"
  | "human_input_agent_exec"
  | "agent_prepare_human_action"
  | "mixed";

export interface TaskTimeoutRule {
  max_duration_seconds: number;
  action: "retry" | "escalate" | "fail";
}

export interface TaskRetryRule {
  max_retries: number;
  retry_delay_seconds: number;
}

export interface TaskEscalationPolicy {
  escalate_after_seconds: number;
}

export interface Task {
  id: string;
  plan_id: string;
  run_id: string | null;
  workspace_id: string;
  title: string;
  description: string | null;
  step_order: number;
  depends_on: string[];
  primary_assignee_id: string | null;
  fallback_agent_ids: string[];
  required_skills: string[];
  collaboration_mode: CollaborationMode;
  acceptance_criteria: string | null;
  status: TaskStatus;
  actual_agent_id: string | null;
  current_retry: number;
  started_at: string | null;
  completed_at: string | null;
  result: unknown;
  error: string | null;
  timeout_rule: TaskTimeoutRule;
  retry_rule: TaskRetryRule;
  escalation_policy: TaskEscalationPolicy;
  input_context_refs: unknown[];
  output_refs: unknown[];
  created_at: string;
  updated_at: string;
}

// ParticipantSlot
export type SlotType = "human_input" | "agent_execution" | "human_review";
export type SlotTrigger = "before_execution" | "during_execution" | "before_done";
export type SlotStatus =
  | "waiting" | "ready" | "in_progress" | "submitted"
  | "approved" | "revision_requested" | "rejected"
  | "expired" | "skipped";

export interface ParticipantSlot {
  id: string;
  task_id: string;
  slot_type: SlotType;
  slot_order: number;
  participant_id: string | null;
  participant_type: "member" | "agent" | null;
  responsibility: string | null;
  trigger: SlotTrigger;
  blocking: boolean;
  required: boolean;
  expected_output: string | null;
  content?: unknown;
  status: SlotStatus;
  timeout_seconds: number | null;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface SubmitSlotInputResponse {
  slot: ParticipantSlot;
  task_new_status: TaskStatus;
}

export interface SlotSubmission {
  id: string;
  slot_id: string;
  task_id: string;
  run_id: string | null;
  submitted_by: string | null;
  content: unknown;
  comment: string | null;
  created_at: string;
}

// Execution
export type ExecutionStatus =
  | "queued" | "claimed" | "running"
  | "completed" | "failed" | "cancelled" | "timed_out";

export interface ExecutionContextRef {
  mode: "local" | "cloud";
  // local
  working_dir?: string;
  daemon_id?: string;
  // cloud
  sdk_session_id?: string;
  sandbox_id?: string;
  virtual_project_path?: string;
}

export interface Execution {
  id: string;
  task_id: string;
  run_id: string;
  slot_id: string | null;
  agent_id: string;
  runtime_id: string;
  attempt: number;
  status: ExecutionStatus;
  priority: number;
  payload: unknown;
  result: unknown;
  error: string | null;
  context_ref: ExecutionContextRef;
  log_retention_policy: "7d" | "30d" | "90d" | "permanent";
  logs_expires_at: string | null;
  cost_input_tokens: number;
  cost_output_tokens: number;
  cost_usd: number;
  cost_provider: string | null;
  claimed_at: string | null;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
  updated_at: string;
}

// Artifact
export type ArtifactType = "document" | "design" | "code_patch" | "report" | "file" | "plan_doc";

export interface Artifact {
  id: string;
  task_id: string;
  slot_id: string | null;
  execution_id: string | null;
  run_id: string;
  artifact_type: ArtifactType;
  version: number;
  title: string | null;
  summary: string | null;
  content: unknown;
  file_index_id: string | null;
  file_snapshot_id: string | null;
  retention_class: "permanent" | "ttl" | "temp";
  created_by_id: string | null;
  created_by_type: "member" | "agent" | null;
  created_at: string;
}

// Review
export type ReviewDecision = "approve" | "request_changes" | "reject";

export interface Review {
  id: string;
  task_id: string;
  artifact_id: string;
  slot_id: string | null;
  reviewer_id: string | null;
  reviewer_type: "member" | "agent" | null;
  decision: ReviewDecision;
  comment: string | null;
  created_at: string;
}

// Request shapes (subset — most CRUD only needs these; add more as APIs land)

export interface CreateTaskRequest {
  plan_id: string;
  title: string;
  description?: string;
  step_order?: number;
  depends_on?: string[];
  primary_assignee_id?: string;
  fallback_agent_ids?: string[];
  required_skills?: string[];
  collaboration_mode?: CollaborationMode;
  acceptance_criteria?: string;
  timeout_rule?: TaskTimeoutRule;
  retry_rule?: TaskRetryRule;
}

export interface CreateParticipantSlotRequest {
  task_id: string;
  slot_type: SlotType;
  slot_order?: number;
  participant_id?: string;
  participant_type?: "member" | "agent";
  responsibility?: string;
  trigger?: SlotTrigger;
  blocking?: boolean;
  required?: boolean;
  expected_output?: string;
  timeout_seconds?: number;
}

export interface CreateReviewRequest {
  task_id: string;
  artifact_id: string;
  slot_id?: string;
  decision: ReviewDecision;
  comment?: string;
}
