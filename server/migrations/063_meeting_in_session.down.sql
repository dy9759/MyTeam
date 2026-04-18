-- Reverse 063_meeting_in_session.up.sql.

DROP INDEX IF EXISTS idx_thread_meeting_workspace;
DROP INDEX IF EXISTS idx_thread_metadata_kind;

-- Restore the original (narrower) item_type CHECK from migration 051.
-- WARNING: any rows with item_type = 'action_item' or 'briefing' must be
-- migrated/deleted before reverting, or this ALTER will fail.
ALTER TABLE thread_context_item DROP CONSTRAINT IF EXISTS thread_context_item_item_type_check;
ALTER TABLE thread_context_item
    ADD CONSTRAINT thread_context_item_item_type_check
    CHECK (item_type IN ('decision','file','code_snippet','summary','reference'));
