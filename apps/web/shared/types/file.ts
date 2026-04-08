export interface FileIndex {
  id: string;
  workspace_id: string;
  uploader_identity_id: string;
  uploader_identity_type: "member" | "agent";
  owner_id: string;
  source_type: "conversation" | "project" | "external";
  source_id: string;
  file_name: string;
  file_size?: number;
  content_type?: string;
  storage_path?: string;
  access_scope?: { type: "private" | "conversation" | "project" | "organization" };
  channel_id?: string;
  project_id?: string;
  created_at: string;
}

export interface FileSnapshot {
  id: string;
  file_id: string;
  snapshot_at: string;
  storage_path: string;
  referenced_by: { type: string; id: string }[];
  created_at: string;
}

export interface WorkspaceMetrics {
  task_completion_rate: number;
  average_task_duration_seconds: number;
  timeout_rate: number;
  active_projects: number;
  active_runs: number;
  pending_escalations: number;
}
