-- Restore NOT NULL on agent.runtime_id. Must clear subagent rows
-- first, since those legitimately have runtime_id IS NULL.
DELETE FROM agent WHERE kind = 'subagent';
ALTER TABLE agent ALTER COLUMN runtime_id SET NOT NULL;
