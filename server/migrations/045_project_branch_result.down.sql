-- server/migrations/045_project_branch_result.down.sql

ALTER TABLE workflow_step DROP COLUMN IF EXISTS on_failure;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS error_summary;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS error_code;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS done_definition;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS acceptance_checks;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS skippable;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS actual_outputs;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS expected_outputs;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS worktree_path;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS instruction_md_path;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS context_md_path;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS owner_reviewer_id;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS candidate_agent_ids;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS priority;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS goal;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS title;

ALTER TABLE project_version DROP COLUMN IF EXISTS branch_id;

ALTER TABLE project DROP COLUMN IF EXISTS plan_visibility;
ALTER TABLE project DROP COLUMN IF EXISTS scheduled_at;
ALTER TABLE project DROP COLUMN IF EXISTS consecutive_failure_threshold;
ALTER TABLE project DROP COLUMN IF EXISTS end_time;
ALTER TABLE project DROP COLUMN IF EXISTS max_runs;
ALTER TABLE project DROP COLUMN IF EXISTS default_branch_id;

DROP TABLE IF EXISTS project_result;
DROP TABLE IF EXISTS project_branch;
