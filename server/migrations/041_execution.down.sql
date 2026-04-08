-- Revert inbox_item escalation fields
ALTER TABLE inbox_item DROP COLUMN IF EXISTS related_conversation_id;
ALTER TABLE inbox_item DROP COLUMN IF EXISTS related_run_id;
ALTER TABLE inbox_item DROP COLUMN IF EXISTS related_project_id;
ALTER TABLE inbox_item DROP COLUMN IF EXISTS resolution_status;
ALTER TABLE inbox_item DROP COLUMN IF EXISTS deadline;
ALTER TABLE inbox_item DROP COLUMN IF EXISTS action_type;
ALTER TABLE inbox_item DROP COLUMN IF EXISTS action_required;

-- Revert workflow_step execution engine fields
DROP INDEX IF EXISTS idx_workflow_step_run;

ALTER TABLE workflow_step DROP COLUMN IF EXISTS current_retry;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS actual_agent_id;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS output_refs;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS input_context_refs;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS human_approval_required;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS retry_rule;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS timeout_rule;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS owner_escalation_policy;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS run_id;

-- Revert agent_task_queue extensions
DROP INDEX IF EXISTS idx_task_queue_run;
DROP INDEX IF EXISTS idx_task_queue_workflow_step;

ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS run_id;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS workflow_step_id;
