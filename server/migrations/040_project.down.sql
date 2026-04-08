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
ALTER TABLE plan DROP COLUMN IF EXISTS project_id;

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS project_run;
DROP TABLE IF EXISTS project_version;
DROP TABLE IF EXISTS project;
