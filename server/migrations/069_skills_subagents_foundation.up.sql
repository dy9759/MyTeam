-- Foundation for skills + subagents, tied to the new rule that skills are
-- only reachable through a subagent (not directly from an agent):
--
-- 1. `skill` gains category/source/source_ref/is_global so we can ship a
--    curated bundle (addyosmani, superpowers) as global, read-only entries
--    alongside per-workspace user uploads.
-- 2. `agent` gains a `kind` column so the same table can hold both
--    workspace agents (kind='agent') and global-or-workspace subagent
--    templates (kind='subagent') without a parallel table.
-- 3. New `subagent_skill` join table replaces the direct agent↔skill
--    path at the schema level — PlanGenerator must now resolve skills
--    via a subagent.

-- ---------- Skill ----------

ALTER TABLE skill
    ADD COLUMN category TEXT NOT NULL DEFAULT 'general',
    ADD COLUMN source TEXT NOT NULL DEFAULT 'manual'
        CHECK (source IN ('manual', 'bundle', 'upload')),
    ADD COLUMN source_ref TEXT,
    ADD COLUMN is_global BOOLEAN NOT NULL DEFAULT false;

-- Relax workspace_id for global bundle/upload entries. The existing
-- UNIQUE(workspace_id, name) constraint becomes invalid because
-- Postgres treats NULL as distinct in UNIQUE, so split it into two
-- partial indexes that carry the same intent.
ALTER TABLE skill DROP CONSTRAINT IF EXISTS skill_workspace_id_name_key;
ALTER TABLE skill ALTER COLUMN workspace_id DROP NOT NULL;

CREATE UNIQUE INDEX uq_skill_workspace_name
    ON skill(workspace_id, name)
    WHERE workspace_id IS NOT NULL;

CREATE UNIQUE INDEX uq_skill_global_name
    ON skill(name)
    WHERE is_global = true;

-- source_ref is the bundle path (e.g. "addyosmani/skills/debugging/SKILL.md")
-- or the upload identifier. Bundle rows must be unique per ref so the
-- startup loader can upsert idempotently.
CREATE UNIQUE INDEX uq_skill_bundle_ref
    ON skill(source_ref)
    WHERE source = 'bundle' AND source_ref IS NOT NULL;

CREATE INDEX idx_skill_category ON skill(category);
CREATE INDEX idx_skill_source ON skill(source);
CREATE INDEX idx_skill_global ON skill(is_global);

-- ---------- Agent ----------

ALTER TABLE agent
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'agent'
        CHECK (kind IN ('agent', 'subagent')),
    ADD COLUMN is_global BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN source TEXT NOT NULL DEFAULT 'manual'
        CHECK (source IN ('manual', 'bundle', 'upload')),
    ADD COLUMN source_ref TEXT,
    ADD COLUMN category TEXT NOT NULL DEFAULT 'general';

ALTER TABLE agent ALTER COLUMN workspace_id DROP NOT NULL;

CREATE UNIQUE INDEX uq_agent_bundle_ref
    ON agent(source_ref)
    WHERE source = 'bundle' AND source_ref IS NOT NULL;

CREATE INDEX idx_agent_kind ON agent(kind);
CREATE INDEX idx_agent_global ON agent(is_global);

-- ---------- Subagent ↔ Skill ----------

CREATE TABLE subagent_skill (
    subagent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    skill_id    UUID NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (subagent_id, skill_id)
);

CREATE INDEX idx_subagent_skill_subagent ON subagent_skill(subagent_id);
CREATE INDEX idx_subagent_skill_skill    ON subagent_skill(skill_id);
