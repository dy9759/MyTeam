-- Reverse 063_meeting_in_session.up.sql.

DROP INDEX IF EXISTS idx_thread_meeting_workspace;
DROP INDEX IF EXISTS idx_thread_metadata_kind;

-- Restore the original (narrower) item_type CHECK from migration 051.
-- Guard: refuse rollback if rows exist with the widened item_type values.
-- The operator must migrate or delete those rows explicitly before retrying.
DO $$
DECLARE
    offending_count bigint;
BEGIN
    SELECT count(*) INTO offending_count
    FROM thread_context_item
    WHERE item_type IN ('action_item', 'briefing');

    IF offending_count > 0 THEN
        RAISE EXCEPTION
            'refusing to narrow thread_context_item.item_type CHECK: % row(s) still use item_type IN (''action_item'',''briefing''); migrate or delete them before retrying rollback',
            offending_count;
    END IF;
END $$;

ALTER TABLE thread_context_item DROP CONSTRAINT IF EXISTS thread_context_item_item_type_check;
ALTER TABLE thread_context_item
    ADD CONSTRAINT thread_context_item_item_type_check
    CHECK (item_type IN ('decision','file','code_snippet','summary','reference'));
