DROP INDEX IF EXISTS uq_workspace_owner_runtime_local_agent;

ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_local_requires_runtime;

ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_type_owner_match;
ALTER TABLE agent ADD CONSTRAINT agent_type_owner_match CHECK (
  (agent_type = 'personal_agent' AND owner_type = 'user'         AND owner_id IS NOT NULL)
  OR
  (agent_type = 'personal_agent' AND owner_type = 'organization' AND owner_id IS NULL)
  OR
  (agent_type = 'system_agent'   AND owner_type = 'organization' AND owner_id IS NULL)
);

ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_agent_type_check;
ALTER TABLE agent ADD CONSTRAINT agent_agent_type_check
    CHECK (agent_type IN ('personal_agent', 'system_agent'));
