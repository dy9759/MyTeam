-- Extend conversation_agent_run so it can also represent a channel
-- @mention reply (local_agent fired from MediationService), not only a
-- direct message. DM runs keep peer_user_id set; channel runs set
-- channel_id (+ optional thread_id) and leave peer_user_id NULL.
--
-- Plan: docs/plans/2026-04-23-multiple-local-agents.md (Phase 3).

ALTER TABLE conversation_agent_run
  ALTER COLUMN peer_user_id DROP NOT NULL;

ALTER TABLE conversation_agent_run
  ADD COLUMN IF NOT EXISTS channel_id UUID REFERENCES channel(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS thread_id UUID;

-- Exactly one target: DM (peer_user_id) or channel (channel_id).
ALTER TABLE conversation_agent_run
  DROP CONSTRAINT IF EXISTS conversation_agent_run_target_check;
ALTER TABLE conversation_agent_run
  ADD CONSTRAINT conversation_agent_run_target_check CHECK (
    (peer_user_id IS NOT NULL AND channel_id IS NULL)
    OR
    (peer_user_id IS NULL AND channel_id IS NOT NULL)
  );

CREATE INDEX IF NOT EXISTS idx_conversation_agent_run_channel
  ON conversation_agent_run(workspace_id, channel_id, created_at DESC)
  WHERE channel_id IS NOT NULL;
