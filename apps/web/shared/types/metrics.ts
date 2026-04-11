export interface WorkspaceMetrics {
  task_completion_rate: number;
  average_task_duration_seconds: number;
  timeout_rate: number;
  active_projects: number;
  active_runs: number;
  pending_escalations: number;
}
