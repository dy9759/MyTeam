-- Reverse Account Phase 1 additive changes.

ALTER TABLE message
    DROP CONSTRAINT IF EXISTS message_effective_actor_type_check,
    DROP CONSTRAINT IF EXISTS message_real_operator_type_check,
    DROP COLUMN IF EXISTS effective_actor_id,
    DROP COLUMN IF EXISTS effective_actor_type,
    DROP COLUMN IF EXISTS real_operator_id,
    DROP COLUMN IF EXISTS real_operator_type;

ALTER TABLE agent
    DROP CONSTRAINT IF EXISTS agent_owner_type_check,
    DROP CONSTRAINT IF EXISTS agent_scope_values_check,
    DROP COLUMN IF EXISTS scope,
    DROP COLUMN IF EXISTS owner_type;

DROP INDEX IF EXISTS idx_agent_runtime_lease;
ALTER TABLE agent_runtime
    DROP CONSTRAINT IF EXISTS agent_runtime_mode_check;
ALTER TABLE agent_runtime DROP CONSTRAINT IF EXISTS agent_runtime_status_check;

-- Normalize any 'degraded' rows back into 'offline' so the narrow CHECK can apply.
UPDATE agent_runtime SET status = 'offline' WHERE status = 'degraded';

ALTER TABLE agent_runtime
    ADD CONSTRAINT agent_runtime_status_check
    CHECK (status IN ('online', 'offline'));
ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS concurrency_limit,
    DROP COLUMN IF EXISTS current_load,
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS last_heartbeat_at,
    DROP COLUMN IF EXISTS mode;
