-- One-shot backfill: tasks created before applyDefaultAssignees shipped
-- may have primary_assignee_id IS NULL, which renders as "未分配" in the
-- UI. Same fallback ladder as the generator applies at runtime:
--
--   1. skill-bearing task → subagent whose skill roster covers a required skill
--   2. skill-bearing task still unresolved → any visible subagent (global or
--      workspace-scoped)
--   3. any remaining task → first active workspace agent
--
-- Steps 1 and 2 are no-ops on databases where the skills bundle loader
-- has not yet seeded global subagents; those rows fall through to step 3
-- and get the default workspace agent.

-- Step 1 — match a subagent to a required skill.
UPDATE task t
SET primary_assignee_id = (
    SELECT a.id
    FROM agent a
    JOIN subagent_skill ss ON ss.subagent_id = a.id
    JOIN skill s          ON s.id          = ss.skill_id
    WHERE a.kind        = 'subagent'
      AND a.archived_at IS NULL
      AND (a.is_global = true OR a.workspace_id = t.workspace_id)
      AND s.name = ANY(t.required_skills)
    ORDER BY a.is_global ASC, a.created_at ASC
    LIMIT 1
)
WHERE t.primary_assignee_id IS NULL
  AND cardinality(t.required_skills) > 0;

-- Step 2 — any visible subagent for a still-unassigned skill-bearing task.
UPDATE task t
SET primary_assignee_id = (
    SELECT a.id
    FROM agent a
    WHERE a.kind        = 'subagent'
      AND a.archived_at IS NULL
      AND (a.is_global = true OR a.workspace_id = t.workspace_id)
    ORDER BY a.is_global ASC, a.created_at ASC
    LIMIT 1
)
WHERE t.primary_assignee_id IS NULL
  AND cardinality(t.required_skills) > 0;

-- Step 3 — first active workspace agent for anything still NULL.
UPDATE task t
SET primary_assignee_id = (
    SELECT a.id
    FROM agent a
    WHERE a.workspace_id = t.workspace_id
      AND a.kind        = 'agent'
      AND a.archived_at IS NULL
    ORDER BY a.created_at ASC
    LIMIT 1
)
WHERE t.primary_assignee_id IS NULL;
