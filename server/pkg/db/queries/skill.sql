-- Skill CRUD

-- name: ListSkillsByWorkspace :many
SELECT * FROM skill
WHERE workspace_id = $1
ORDER BY name ASC;

-- Returns workspace-scoped skills plus all globals, optionally filtered
-- by category. Migration 069 made workspace_id nullable to hold globals
-- (is_global = true), so both sides union here.
-- name: ListSkillsCombined :many
SELECT * FROM skill
WHERE (workspace_id = sqlc.narg('workspace_id') OR is_global = true)
  AND (sqlc.narg('category')::text IS NULL OR category = sqlc.narg('category'))
  AND (sqlc.narg('source')::text   IS NULL OR source   = sqlc.narg('source'))
ORDER BY is_global DESC, name ASC;

-- name: GetSkill :one
SELECT * FROM skill
WHERE id = $1;

-- name: GetSkillInWorkspace :one
SELECT * FROM skill
WHERE id = $1 AND (workspace_id = $2 OR is_global = true);

-- name: CreateSkill :one
INSERT INTO skill (workspace_id, name, description, content, config, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- Used by the /api/skills/upload endpoint. Scope may be null (global
-- upload by an admin) or a workspace UUID.
-- name: CreateUploadSkill :one
INSERT INTO skill (
    workspace_id, name, description, content, config, created_by,
    category, source, source_ref, is_global
)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'upload', $8, $9)
RETURNING *;

-- Idempotent upsert used by the startup bundle loader. Keyed by
-- source_ref so the same bundle file always maps to the same row.
-- name: UpsertBundleSkill :one
INSERT INTO skill (
    workspace_id, name, description, content, config,
    category, source, source_ref, is_global
)
VALUES (NULL, $1, $2, $3, '{}'::jsonb, $4, 'bundle', $5, true)
ON CONFLICT (source_ref) WHERE source = 'bundle' AND source_ref IS NOT NULL
DO UPDATE SET
    name        = EXCLUDED.name,
    description = EXCLUDED.description,
    content     = EXCLUDED.content,
    category    = EXCLUDED.category,
    updated_at  = now()
RETURNING *;

-- name: ListBundleSkillRefs :many
SELECT id, source_ref FROM skill
WHERE source = 'bundle' AND source_ref IS NOT NULL;

-- name: ListBundleSkillsForLinking :many
-- Minimal fields the bundle loader needs to wire subagent_skill rows:
-- id to insert, name to substring-match, source_ref to group by source
-- so we never link a subagent to a skill from a different upstream.
SELECT id, name, source_ref FROM skill
WHERE source = 'bundle' AND source_ref IS NOT NULL
ORDER BY name ASC;

-- Removes bundle rows whose source_ref is no longer on disk. The join
-- table cascades, so linked subagents drop the skill automatically.
-- name: DeleteBundleSkillsNotInRefs :exec
DELETE FROM skill
WHERE source = 'bundle'
  AND source_ref IS NOT NULL
  AND source_ref <> ALL(@refs::text[]);

-- name: UpdateSkill :one
UPDATE skill SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    content = COALESCE(sqlc.narg('content'), content),
    config = COALESCE(sqlc.narg('config'), config),
    category = COALESCE(sqlc.narg('category'), category),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteSkill :exec
DELETE FROM skill WHERE id = $1;

-- Skill File CRUD

-- name: ListSkillFiles :many
SELECT * FROM skill_file
WHERE skill_id = $1
ORDER BY path ASC;

-- name: GetSkillFile :one
SELECT * FROM skill_file
WHERE id = $1;

-- name: UpsertSkillFile :one
INSERT INTO skill_file (skill_id, path, content)
VALUES ($1, $2, $3)
ON CONFLICT (skill_id, path) DO UPDATE SET
    content = EXCLUDED.content,
    updated_at = now()
RETURNING *;

-- name: DeleteSkillFile :exec
DELETE FROM skill_file WHERE id = $1;

-- name: DeleteSkillFilesBySkill :exec
DELETE FROM skill_file WHERE skill_id = $1;

-- Agent-Skill junction — kept for legacy lookups, but migration 069
-- moved the authoritative link to subagent_skill. New code should call
-- the subagent_skill queries instead of agent_skill directly.

-- name: ListAgentSkills :many
SELECT s.* FROM skill s
JOIN agent_skill ask ON ask.skill_id = s.id
WHERE ask.agent_id = $1
ORDER BY s.name ASC;

-- name: AddAgentSkill :exec
INSERT INTO agent_skill (agent_id, skill_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveAgentSkill :exec
DELETE FROM agent_skill
WHERE agent_id = $1 AND skill_id = $2;

-- name: RemoveAllAgentSkills :exec
DELETE FROM agent_skill WHERE agent_id = $1;

-- name: ListAgentSkillsByWorkspace :many
SELECT ask.agent_id, s.id, s.name, s.description
FROM agent_skill ask
JOIN skill s ON s.id = ask.skill_id
WHERE s.workspace_id = $1
ORDER BY s.name ASC;
