-- Multi-local-agent rollout (plan: docs/plans/2026-04-23-multiple-local-agents.md).
--
-- Adds a third agent_type 'local_agent' so a user can bind N daemon
-- runtimes as distinct agent rows alongside their single cloud
-- personal_agent. Cloud personal_agent behavior and uniqueness
-- (migration 062) are unchanged.
--
-- Constraints:
--   * agent_type CHECK extended to include 'local_agent'.
--   * agent_type_owner_match (last edited by migration 079) gains a
--     'local_agent' + user + owner_id NOT NULL branch.
--   * Separate CHECK: agent_type='local_agent' implies runtime_id
--     IS NOT NULL. Kept separate so the two concerns (owner shape,
--     runtime presence) can be diagnosed independently.
--   * Partial unique index on (workspace, owner, runtime) for active
--     local_agents — prevents duplicate binds when the user saves
--     twice for the same runtime.

ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_agent_type_check;
ALTER TABLE agent ADD CONSTRAINT agent_agent_type_check
    CHECK (agent_type IN ('personal_agent', 'system_agent', 'local_agent'));

ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_type_owner_match;
ALTER TABLE agent ADD CONSTRAINT agent_type_owner_match CHECK (
  (agent_type = 'personal_agent' AND owner_type = 'user'         AND owner_id IS NOT NULL)
  OR
  (agent_type = 'personal_agent' AND owner_type = 'organization' AND owner_id IS NULL)
  OR
  (agent_type = 'system_agent'   AND owner_type = 'organization' AND owner_id IS NULL)
  OR
  (agent_type = 'local_agent'    AND owner_type = 'user'         AND owner_id IS NOT NULL)
);

ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_local_requires_runtime;
ALTER TABLE agent ADD CONSTRAINT agent_local_requires_runtime CHECK (
  agent_type <> 'local_agent' OR runtime_id IS NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_workspace_owner_runtime_local_agent
    ON agent (workspace_id, owner_id, runtime_id)
    WHERE agent_type = 'local_agent' AND archived_at IS NULL;
