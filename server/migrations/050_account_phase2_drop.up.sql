-- Account Phase 2 - drop deprecated columns and tighten constraints.
-- DESTRUCTIVE: only apply after Tasks 3-7 of Plan 2 have landed.

-- ===== Agent table cleanup =====
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_owner_type_check;
ALTER TABLE agent ADD CONSTRAINT agent_owner_type_check
    CHECK (owner_type IN ('user', 'organization'));
ALTER TABLE agent ALTER COLUMN owner_type SET NOT NULL;

ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_type_owner_match;
ALTER TABLE agent ADD CONSTRAINT agent_type_owner_match CHECK (
  (agent_type = 'personal_agent' AND owner_type = 'user' AND owner_id IS NOT NULL)
  OR
  (agent_type = 'system_agent' AND owner_type = 'organization' AND owner_id IS NULL)
);

-- Status NOT NULL after data migration filled it.
ALTER TABLE agent ALTER COLUMN status SET NOT NULL;
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_status_check;
ALTER TABLE agent ADD CONSTRAINT agent_status_check
    CHECK (status IN ('offline','online','idle','busy','blocked','degraded','suspended'));

-- Drop legacy columns.
ALTER TABLE agent
    DROP COLUMN IF EXISTS is_system,
    DROP COLUMN IF EXISTS page_scope,
    DROP COLUMN IF EXISTS runtime_mode,
    DROP COLUMN IF EXISTS runtime_config,
    DROP COLUMN IF EXISTS cloud_llm_config,
    DROP COLUMN IF EXISTS capabilities,
    DROP COLUMN IF EXISTS tools,
    DROP COLUMN IF EXISTS triggers,
    DROP COLUMN IF EXISTS system_config,
    DROP COLUMN IF EXISTS agent_metadata,
    DROP COLUMN IF EXISTS accessible_files_scope,
    DROP COLUMN IF EXISTS allowed_channels_scope,
    DROP COLUMN IF EXISTS online_status,
    DROP COLUMN IF EXISTS workload_status;

-- agent_type CHECK enum tightened to two values.
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_agent_type_check;
ALTER TABLE agent ADD CONSTRAINT agent_agent_type_check
    CHECK (agent_type IN ('personal_agent', 'system_agent'));

-- New uniqueness constraints for system_agent + scope (replaces old is_system unique).
-- The pre-existing index was created in migration 037 as idx_system_agent_workspace;
-- older tooling may also have named it uq_workspace_system_agent, so drop both defensively.
DROP INDEX IF EXISTS uq_workspace_system_agent;
DROP INDEX IF EXISTS idx_system_agent_workspace;
CREATE UNIQUE INDEX IF NOT EXISTS uq_workspace_global_system_agent
    ON agent(workspace_id)
    WHERE agent_type = 'system_agent' AND scope IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_workspace_scoped_system_agent
    ON agent(workspace_id, scope)
    WHERE agent_type = 'system_agent' AND scope IS NOT NULL;

-- ===== Runtime table cleanup =====
ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS runtime_mode,
    DROP COLUMN IF EXISTS last_seen_at;
