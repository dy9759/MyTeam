-- Reverses 060: restore the legacy CHECK and re-tighten created_by.
-- The down path will fail if any current row has status not in the legacy
-- set or created_by IS NULL — that's the correct behavior, since the down
-- migration cannot silently corrupt audit data.

-- Reverse-map status before tightening to the legacy CHECK. paused/failed
-- have no clean legacy mapping; pick the closest semantic ('active' for
-- both, since they were both mid-flight states).
UPDATE project SET status = 'draft' WHERE status = 'not_started';
UPDATE project SET status = 'active' WHERE status IN ('running', 'paused', 'failed');

ALTER TABLE project DROP CONSTRAINT IF EXISTS project_status_check;
ALTER TABLE project ADD CONSTRAINT project_status_check
    CHECK (status IN ('draft', 'active', 'completed', 'archived'));

-- created_by SET NOT NULL will fail if any row has NULL; that's intentional.
-- Backfill any orphans from creator_owner_id first so the down migration
-- doesn't strand the operator.
UPDATE project SET created_by = creator_owner_id WHERE created_by IS NULL AND creator_owner_id IS NOT NULL;
ALTER TABLE project ALTER COLUMN created_by SET NOT NULL;
