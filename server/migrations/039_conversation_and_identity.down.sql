-- Drop indexes
DROP INDEX IF EXISTS idx_channel_invite_code;
DROP INDEX IF EXISTS idx_message_thread;
DROP INDEX IF EXISTS idx_channel_project;
DROP INDEX IF EXISTS idx_channel_conversation_type;
DROP INDEX IF EXISTS idx_agent_online;
DROP INDEX IF EXISTS idx_agent_type;
DROP INDEX IF EXISTS idx_thread_channel;

-- Remove message extension
ALTER TABLE message DROP COLUMN IF EXISTS thread_id;

-- Remove channel extensions
ALTER TABLE channel DROP COLUMN IF EXISTS linked_project_ids;
ALTER TABLE channel DROP COLUMN IF EXISTS project_id;
ALTER TABLE channel DROP COLUMN IF EXISTS auto_assignment_policy;
ALTER TABLE channel DROP COLUMN IF EXISTS reply_policy;
ALTER TABLE channel DROP COLUMN IF EXISTS invite_code;
ALTER TABLE channel DROP COLUMN IF EXISTS parent_conversation_id;
ALTER TABLE channel DROP COLUMN IF EXISTS conversation_type;

-- Drop thread table
DROP TABLE IF EXISTS thread;

-- Remove agent extensions
ALTER TABLE agent DROP COLUMN IF EXISTS last_active_at;
ALTER TABLE agent DROP COLUMN IF EXISTS allowed_channels_scope;
ALTER TABLE agent DROP COLUMN IF EXISTS accessible_files_scope;
ALTER TABLE agent DROP COLUMN IF EXISTS identity_card;
ALTER TABLE agent DROP COLUMN IF EXISTS workload_status;
ALTER TABLE agent DROP COLUMN IF EXISTS online_status;
ALTER TABLE agent DROP COLUMN IF EXISTS agent_type;
