-- Migration 060: align project.status CHECK with the real state machine and
-- relax project.created_by so the new sqlc-driven path can use creator_owner_id
-- as the authoritative author column.
--
-- Background: migration 039 created `project` with
--   status TEXT CHECK (status IN ('draft','active','completed','archived'))
--   created_by UUID NOT NULL REFERENCES "user"(id)
-- Migration 040 then added the chat-to-project columns (creator_owner_id,
-- schedule_type, ...) and changed the default status to 'not_started', but
-- could not retroactively widen the CHECK or relax the NOT NULL because
-- ALTER COLUMN inside CREATE TABLE IF NOT EXISTS is a no-op.
--
-- Plan 5's HTTP CRUD handlers and the validProjectStatuses map use
-- not_started/running/paused/completed/failed/archived — none of which match
-- the legacy CHECK. INSERT fails today with `null value in column "created_by"`
-- before the CHECK even gets a chance to fire. Both fixes belong in the same
-- migration because relaxing one without the other still breaks INSERT.

-- Backfill legacy status values before tightening the CHECK. The legacy set
-- was ('draft','active','completed','archived'); 'completed' and 'archived'
-- carry over unchanged, the others map to the closest new state.
UPDATE project SET status = 'not_started' WHERE status = 'draft';
UPDATE project SET status = 'running' WHERE status = 'active';

ALTER TABLE project DROP CONSTRAINT IF EXISTS project_status_check;
ALTER TABLE project ADD CONSTRAINT project_status_check
    CHECK (status IN ('not_started', 'running', 'paused', 'completed', 'failed', 'archived'));

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='project' AND column_name='created_by') THEN
        ALTER TABLE project ALTER COLUMN created_by DROP NOT NULL;
    END IF;
END $$;
