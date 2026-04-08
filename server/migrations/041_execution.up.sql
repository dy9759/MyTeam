-- Extend agent_task_queue with workflow step and run references
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS workflow_step_id UUID REFERENCES workflow_step(id);
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS run_id UUID REFERENCES project_run(id);

CREATE INDEX IF NOT EXISTS idx_task_queue_workflow_step ON agent_task_queue(workflow_step_id);
CREATE INDEX IF NOT EXISTS idx_task_queue_run ON agent_task_queue(run_id);

-- Extend workflow_step with execution engine fields
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS run_id UUID REFERENCES project_run(id);
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS owner_escalation_policy JSONB DEFAULT '{}';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS timeout_rule JSONB DEFAULT '{}';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS retry_rule JSONB DEFAULT '{}';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS human_approval_required BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS input_context_refs JSONB DEFAULT '[]';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS output_refs JSONB DEFAULT '[]';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS actual_agent_id UUID REFERENCES agent(id);
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS current_retry INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_workflow_step_run ON workflow_step(run_id);

-- Extend inbox_item with escalation fields
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS action_required BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS action_type TEXT;
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS deadline TIMESTAMPTZ;
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS resolution_status TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS related_project_id UUID REFERENCES project(id);
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS related_run_id UUID REFERENCES project_run(id);
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS related_conversation_id UUID;
