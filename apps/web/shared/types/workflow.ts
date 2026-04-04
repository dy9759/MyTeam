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
  status: "pending" | "running" | "completed" | "failed";
  started_at?: string;
  completed_at?: string;
  result?: any;
  error?: string;
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
