-- Remove FK constraint on channel.project_id (column stays, was added in 039)
ALTER TABLE channel DROP CONSTRAINT IF EXISTS fk_channel_project;

-- Remove plan extensions
ALTER TABLE plan DROP COLUMN IF EXISTS approved_at;
ALTER TABLE plan DROP COLUMN IF EXISTS approved_by;
ALTER TABLE plan DROP COLUMN IF EXISTS approval_status;
ALTER TABLE plan DROP COLUMN IF EXISTS risk_points;
ALTER TABLE plan DROP COLUMN IF EXISTS assigned_agents;
ALTER TABLE plan DROP COLUMN IF EXISTS task_brief;
ALTER TABLE plan DROP COLUMN IF EXISTS version_id;
-- Note: plan.project_id is also added by 039; leave it in place for 039's down to remove.
ALTER TABLE plan DROP COLUMN IF EXISTS project_id;

-- Drop run table outright (introduced in 040 only).
DROP TABLE IF EXISTS project_run;

-- For project_version: drop the columns this migration added; leave the table itself for 039's down.
ALTER TABLE project_version DROP COLUMN IF EXISTS version_status;
ALTER TABLE project_version DROP COLUMN IF EXISTS workflow_snapshot;
ALTER TABLE project_version DROP COLUMN IF EXISTS fork_reason;
ALTER TABLE project_version DROP COLUMN IF EXISTS branch_name;
ALTER TABLE project_version DROP COLUMN IF EXISTS version_number;
ALTER TABLE project_version DROP COLUMN IF EXISTS parent_version_id;

-- For project: drop the columns this migration added; leave the table itself for 039's down.
ALTER TABLE project DROP COLUMN IF EXISTS creator_owner_id;
ALTER TABLE project DROP COLUMN IF EXISTS channel_id;
ALTER TABLE project DROP COLUMN IF EXISTS source_conversations;
ALTER TABLE project DROP COLUMN IF EXISTS cron_expr;
ALTER TABLE project DROP COLUMN IF EXISTS schedule_type;
