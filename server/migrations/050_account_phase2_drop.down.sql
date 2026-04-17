-- Best-effort reverse of 050. Dropped data CANNOT be recovered.

ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS is_system BOOLEAN DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS page_scope TEXT,
    ADD COLUMN IF NOT EXISTS runtime_mode TEXT,
    ADD COLUMN IF NOT EXISTS runtime_config JSONB,
    ADD COLUMN IF NOT EXISTS cloud_llm_config JSONB,
    ADD COLUMN IF NOT EXISTS capabilities TEXT[],
    ADD COLUMN IF NOT EXISTS tools JSONB,
    ADD COLUMN IF NOT EXISTS triggers JSONB,
    ADD COLUMN IF NOT EXISTS system_config JSONB,
    ADD COLUMN IF NOT EXISTS agent_metadata JSONB,
    ADD COLUMN IF NOT EXISTS accessible_files_scope JSONB,
    ADD COLUMN IF NOT EXISTS allowed_channels_scope JSONB,
    ADD COLUMN IF NOT EXISTS online_status TEXT,
    ADD COLUMN IF NOT EXISTS workload_status TEXT;

ALTER TABLE agent ALTER COLUMN status DROP NOT NULL;
ALTER TABLE agent ALTER COLUMN owner_type DROP NOT NULL;
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_status_check;
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_type_owner_match;

ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_agent_type_check;
ALTER TABLE agent ADD CONSTRAINT agent_agent_type_check
    CHECK (agent_type IN ('personal_agent', 'system_agent', 'page_system_agent'));

ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS runtime_mode TEXT,
    ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;

DROP INDEX IF EXISTS uq_workspace_global_system_agent;
DROP INDEX IF EXISTS uq_workspace_scoped_system_agent;

-- Recreate the legacy system_agent uniqueness invariant (from migration 037).
-- Depends on is_system being re-added above, so it sits at the end of the file.
CREATE UNIQUE INDEX IF NOT EXISTS idx_system_agent_workspace
    ON agent(workspace_id) WHERE is_system = TRUE;
