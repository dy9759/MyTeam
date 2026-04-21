-- Seed a workspace-scoped runnable agent (kind='agent') for each
-- bundle subagent so the plan-generation flow has a non-system,
-- category-tagged assignee to pick from. This is the "结合 system
-- agent 生成几个 agent" request: the new rows sit next to the
-- existing System Agent and carry the subagent's instructions as
-- their system prompt, but they're plain agents the scheduler can
-- dispatch rather than templates.
--
-- agent_type is 'personal_agent' because 'system_agent' has a
-- uq_workspace_global_system_agent partial unique (only one per
-- workspace when scope IS NULL) and a scope CHECK that only allows
-- {account, conversation, project, file} — none of which map to a
-- role-prompt clone. owner_type='organization' + owner_id=NULL keeps
-- the row workspace-owned rather than attaching it to any one user.
--
-- Idempotent via a NOT EXISTS guard on (workspace_id, name). Safe to
-- re-run.

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
  -- Only touch workspaces that already have their System Agent (a
  -- proxy for "real user workspace, not an orphaned test fixture").
  AND EXISTS (
      SELECT 1
      FROM agent sys
      WHERE sys.workspace_id = w.id
        AND sys.agent_type   = 'system_agent'
        AND sys.kind         = 'agent'
        AND sys.archived_at IS NULL
  );
