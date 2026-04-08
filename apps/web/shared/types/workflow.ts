export interface PlanStep {
  order: number;
  description: string;
  required_skills: string[];
  estimated_minutes: number;
  depends_on: number[];
  parallelizable: boolean;
}

export interface Plan {
  id: string;
  workspace_id: string;
  title: string;
  description?: string;
  source_type?: string;
  steps: PlanStep[];
  constraints?: string;
  expected_output?: string;
  created_by: string;
  created_at: string;
}

export type WorkflowStepStatus =
  | 'pending'
  | 'queued'
  | 'assigned'
  | 'running'
  | 'waiting_input'
  | 'blocked'
  | 'retrying'
  | 'timeout'
  | 'completed'
  | 'failed'
  | 'cancelled';

export interface WorkflowStep {
  id: string;
  workflow_id: string;
  step_order: number;
  description: string;
  agent_id?: string;
  fallback_agent_ids: string[];
  required_skills: string[];
  timeout_ms: number;
  retry_count: number;
  depends_on: string[];
  status: WorkflowStepStatus;
  started_at?: string;
  completed_at?: string;
  result?: any;
  error?: string;

  // Execution fields
  run_id?: string;
  owner_escalation_policy?: { escalate_after_seconds: number; escalate_to: string };
  timeout_rule?: { max_duration_seconds: number; action: 'retry' | 'fail' | 'escalate' };
  retry_rule?: { max_retries: number; retry_delay_seconds: number };
  human_approval_required?: boolean;
  input_context_refs?: any[];
  output_refs?: any[];
  actual_agent_id?: string;
  current_retry?: number;
}

export interface Workflow {
  id: string;
  plan_id?: string;
  workspace_id: string;
  title: string;
  status: "draft" | "pending" | "running" | "completed" | "failed" | "cancelled";
  type: "once" | "scheduled" | "recurring";
  cron_expr?: string;
  version: number;
  dag?: any;
  steps?: WorkflowStep[];
  created_by: string;
  created_at: string;
  updated_at: string;
}
