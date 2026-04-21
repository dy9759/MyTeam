-- Reverse 069_skills_subagents_foundation.
--
-- The rollback drops the new join table + added columns and restores the
-- original UNIQUE(workspace_id, name) constraint. Any rows with
-- workspace_id IS NULL (i.e. globals created while the forward migration
-- was live) must be removed first or the SET NOT NULL will fail — that's
-- intentional: rolling back globals is a data decision, not something
-- the migration should silently mask.

DROP TABLE IF EXISTS subagent_skill;

DROP INDEX IF EXISTS idx_agent_global;
DROP INDEX IF EXISTS idx_agent_kind;
DROP INDEX IF EXISTS uq_agent_bundle_ref;

ALTER TABLE agent DROP COLUMN IF EXISTS category;
ALTER TABLE agent DROP COLUMN IF EXISTS source_ref;
ALTER TABLE agent DROP COLUMN IF EXISTS source;
ALTER TABLE agent DROP COLUMN IF EXISTS is_global;
ALTER TABLE agent DROP COLUMN IF EXISTS kind;
ALTER TABLE agent ALTER COLUMN workspace_id SET NOT NULL;

DROP INDEX IF EXISTS idx_skill_global;
DROP INDEX IF EXISTS idx_skill_source;
DROP INDEX IF EXISTS idx_skill_category;
DROP INDEX IF EXISTS uq_skill_bundle_ref;
DROP INDEX IF EXISTS uq_skill_global_name;
DROP INDEX IF EXISTS uq_skill_workspace_name;

ALTER TABLE skill ALTER COLUMN workspace_id SET NOT NULL;
ALTER TABLE skill ADD CONSTRAINT skill_workspace_id_name_key UNIQUE (workspace_id, name);

ALTER TABLE skill DROP COLUMN IF EXISTS is_global;
ALTER TABLE skill DROP COLUMN IF EXISTS source_ref;
ALTER TABLE skill DROP COLUMN IF EXISTS source;
ALTER TABLE skill DROP COLUMN IF EXISTS category;
