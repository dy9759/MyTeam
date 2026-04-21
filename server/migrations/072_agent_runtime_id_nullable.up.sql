-- Allow agent.runtime_id to be NULL so subagent templates (introduced
-- in migration 069) can be inserted without a runtime binding.
-- Subagents wrap skills and never execute on their own; they pick up
-- a runtime transitively through the agent that invokes them.
--
-- Existing kind='agent' rows already carry a runtime_id; CreateAgent
-- continues to require it at the call site. The NOT NULL constraint
-- was overly strict for the new template use case.

ALTER TABLE agent ALTER COLUMN runtime_id DROP NOT NULL;
