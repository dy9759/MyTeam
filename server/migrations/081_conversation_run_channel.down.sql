DROP INDEX IF EXISTS idx_conversation_agent_run_channel;

ALTER TABLE conversation_agent_run
  DROP CONSTRAINT IF EXISTS conversation_agent_run_target_check;

ALTER TABLE conversation_agent_run
  DROP COLUMN IF EXISTS thread_id,
  DROP COLUMN IF EXISTS channel_id;

-- Rows with NULL peer_user_id must be purged before re-tightening the
-- NOT NULL constraint.
DELETE FROM conversation_agent_run WHERE peer_user_id IS NULL;

ALTER TABLE conversation_agent_run
  ALTER COLUMN peer_user_id SET NOT NULL;
