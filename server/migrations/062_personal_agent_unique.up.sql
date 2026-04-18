-- Enforce one active personal agent per (workspace, owner). The application's
-- EnsurePersonalAgent does a get-then-create, which is racy under concurrent
-- VerifyCode / CreateMember calls; this constraint makes the race safe at the
-- DB layer (the second insert hits a unique violation that the caller already
-- ignores via the existing get-then-return path).
--
-- Scoped to archived_at IS NULL so archiving and re-creating remains possible.

-- Archive any existing duplicates (keep the oldest active row per pair).
-- Hard-delete is unsafe because several tables (execution, daemon_connection,
-- agent_skill, ...) reference agent.id with ON DELETE RESTRICT.
WITH dupes AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY workspace_id, owner_id
            ORDER BY created_at, id
        ) AS rn
    FROM agent
    WHERE agent_type = 'personal_agent' AND archived_at IS NULL
)
UPDATE agent
SET archived_at = now()
WHERE id IN (SELECT id FROM dupes WHERE rn > 1);

CREATE UNIQUE INDEX IF NOT EXISTS uq_workspace_owner_personal_agent
    ON agent (workspace_id, owner_id)
    WHERE agent_type = 'personal_agent' AND archived_at IS NULL;
