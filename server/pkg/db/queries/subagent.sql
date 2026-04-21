-- Subagents — rows in `agent` where kind = 'subagent'. Treated as
-- templates rather than runnable agents: they wrap one or more skills
-- and are the only legitimate bridge between a workspace agent and a
-- skill after migration 069.

-- name: ListSubagents :many
-- Returns global + workspace-scoped subagents. Pass NULL workspace_id
-- to get only globals. Pass a UUID to get globals + that workspace.
SELECT
    id, workspace_id, name, description, avatar_url, category,
    kind, is_global, source, source_ref,
    created_at, updated_at, archived_at, owner_id, instructions
FROM agent
WHERE kind = 'subagent'
  AND archived_at IS NULL
  AND (is_global = true OR workspace_id = sqlc.narg('workspace_id'))
  AND (sqlc.narg('category')::text IS NULL OR category = sqlc.narg('category'))
ORDER BY is_global DESC, name ASC;

-- name: GetSubagent :one
SELECT
    id, workspace_id, name, description, avatar_url, category,
    kind, is_global, source, source_ref,
    created_at, updated_at, archived_at, owner_id, instructions
FROM agent
WHERE id = $1 AND kind = 'subagent';

-- name: CreateWorkspaceSubagent :one
-- runtime_mode was dropped in migration 050 (Account Phase 2). Subagents
-- are templates that wrap skills; they don't run themselves, so they
-- don't need a runtime assignment at create time.
INSERT INTO agent (
    workspace_id, name, description, instructions,
    category, kind, is_global, source,
    owner_id, owner_type, agent_type
)
VALUES (
    $1, $2, $3, $4,
    $5, 'subagent', false, 'manual',
    $6, 'user', 'personal_agent'
)
RETURNING
    id, workspace_id, name, description, avatar_url, category,
    kind, is_global, source, source_ref,
    created_at, updated_at, archived_at, owner_id, instructions;

-- Idempotent upsert for the bundle loader. Keyed by source_ref so a
-- file at the same path always maps to the same global subagent.
-- name: UpsertBundleSubagent :one
-- Bundle subagents are organization-owned globals — no individual user
-- owner and no runtime binding, matching migration 069's is_global
-- semantics. owner_type='organization' satisfies the agent_type_owner_
-- match constraint from migration 001.
INSERT INTO agent (
    workspace_id, name, description, instructions,
    category, kind, is_global, source, source_ref,
    agent_type, owner_type
)
VALUES (
    NULL, $1, $2, $3,
    $4, 'subagent', true, 'bundle', $5,
    'system_agent', 'organization'
)
ON CONFLICT (source_ref) WHERE source = 'bundle' AND source_ref IS NOT NULL
DO UPDATE SET
    name         = EXCLUDED.name,
    description  = EXCLUDED.description,
    instructions = EXCLUDED.instructions,
    category     = EXCLUDED.category,
    updated_at   = now()
RETURNING
    id, workspace_id, name, description, avatar_url, category,
    kind, is_global, source, source_ref,
    created_at, updated_at, archived_at, owner_id, instructions;

-- name: ListBundleSubagentRefs :many
SELECT id, source_ref FROM agent
WHERE kind = 'subagent' AND source = 'bundle' AND source_ref IS NOT NULL;

-- name: ListBundleSubagentsForLinking :many
-- Full text surfaces the loader scans while inferring subagent_skill
-- links — description + instructions carry the skill mentions.
SELECT id, name, source_ref, description, instructions FROM agent
WHERE kind = 'subagent' AND source = 'bundle' AND source_ref IS NOT NULL;

-- name: DeleteBundleSubagentsNotInRefs :exec
DELETE FROM agent
WHERE kind = 'subagent'
  AND source = 'bundle'
  AND source_ref IS NOT NULL
  AND source_ref <> ALL(@refs::text[]);

-- name: UpdateSubagent :one
UPDATE agent SET
    name         = COALESCE(sqlc.narg('name'),         name),
    description  = COALESCE(sqlc.narg('description'),  description),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    category     = COALESCE(sqlc.narg('category'),     category),
    updated_at   = now()
WHERE id = $1 AND kind = 'subagent'
RETURNING
    id, workspace_id, name, description, avatar_url, category,
    kind, is_global, source, source_ref,
    created_at, updated_at, archived_at, owner_id, instructions;

-- name: DeleteSubagent :exec
DELETE FROM agent WHERE id = $1 AND kind = 'subagent' AND source <> 'bundle';

-- Idempotent INSERT…SELECT mirroring migration 074. Called by the
-- skills bundle loader after each sync so newly-added bundle
-- subagents automatically materialize into workspace-level role
-- agents the plan generator can pick from. NOT EXISTS on
-- (workspace_id, name) keeps it a no-op for subagents that were
-- already seeded on a prior boot.
-- name: SeedRoleAgentsFromBundleSubagents :exec
INSERT INTO agent (
    workspace_id, name, description, instructions,
    status, visibility, agent_type, kind,
    owner_type, owner_id, category, source,
    identity_card, runtime_id
)
SELECT
    w.id AS workspace_id,
    sa.name,
    sa.description,
    sa.instructions,
    'idle',
    'workspace',
    'personal_agent',
    'agent',
    'organization',
    NULL::uuid,
    sa.category,
    'manual',
    jsonb_build_object(
        'seeded_from_subagent_id', sa.id,
        'source_ref',              sa.source_ref,
        'category',                sa.category,
        'capabilities',            jsonb_build_array(sa.category)
    ) AS identity_card,
    (
        SELECT r.id
        FROM agent_runtime r
        WHERE r.workspace_id = w.id
          AND r.mode         = 'cloud'
        ORDER BY r.created_at ASC
        LIMIT 1
    ) AS runtime_id
FROM workspace w
CROSS JOIN agent sa
WHERE sa.kind      = 'subagent'
  AND sa.source    = 'bundle'
  AND sa.is_global = TRUE
  AND NOT EXISTS (
      SELECT 1
      FROM agent existing
      WHERE existing.workspace_id = w.id
        AND existing.kind         = 'agent'
        AND existing.name         = sa.name
  )
  AND EXISTS (
      SELECT 1
      FROM agent sys
      WHERE sys.workspace_id = w.id
        AND sys.agent_type   = 'system_agent'
        AND sys.kind         = 'agent'
        AND sys.archived_at IS NULL
  );

-- ---------- subagent_skill ----------

-- name: LinkSubagentSkill :exec
INSERT INTO subagent_skill (subagent_id, skill_id, position)
VALUES ($1, $2, $3)
ON CONFLICT (subagent_id, skill_id) DO UPDATE SET position = EXCLUDED.position;

-- name: UnlinkSubagentSkill :exec
DELETE FROM subagent_skill WHERE subagent_id = $1 AND skill_id = $2;

-- name: UnlinkAllSubagentSkills :exec
DELETE FROM subagent_skill WHERE subagent_id = $1;

-- name: ListSubagentSkills :many
SELECT s.* FROM skill s
JOIN subagent_skill ss ON ss.skill_id = s.id
WHERE ss.subagent_id = $1
ORDER BY ss.position ASC, s.name ASC;

-- name: ListSkillSubagents :many
SELECT a.id, a.name, a.category, a.is_global, a.workspace_id
FROM agent a
JOIN subagent_skill ss ON ss.subagent_id = a.id
WHERE ss.skill_id = $1 AND a.kind = 'subagent' AND a.archived_at IS NULL
ORDER BY a.name ASC;
