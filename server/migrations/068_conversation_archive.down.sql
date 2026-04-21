DROP TABLE IF EXISTS dm_conversation_state;
DROP INDEX IF EXISTS idx_channel_active;
ALTER TABLE channel DROP COLUMN IF EXISTS archived_at;
