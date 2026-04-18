-- 063_meeting_in_session.up.sql
-- Treat a meeting as a specialization of `thread`. The session table was
-- retired in migration 053; threads under channels are the current
-- "conversation" primitive. No new columns — thread.metadata is already
-- JSONB NOT NULL DEFAULT '{}', so meeting state goes in there.
--
-- Recommended thread.metadata shape when this is a meeting:
--   {
--     "kind": "meeting",
--     "meeting_status": "planned" | "recording" | "transcribing"
--                       | "summarized" | "completed" | "cancelled",
--     "audio_file_id":      <uuid|null>,   -- pointer into file_index
--     "transcript_file_id": <uuid|null>,   -- pointer into file_index
--     "agenda":   ["..."],
--     "briefing": { "summary": "...", "highlights": [...] },
--     "summary":  { "sections": [...], "decisions": [...] },
--     "asr_provider": "doubao_streaming" | "doubao_miaoji",
--     "started_at": <ts>,
--     "ended_at":   <ts>
--   }
-- Action items live in thread_context_item rows with item_type='action_item'
-- and a JSON body of { task, owner?, due_date?, confidence, task_id? }.
-- Audio + transcript live in file_index with source_type='thread',
-- source_id=thread.id (no new FKs needed).

-- Partial index for "list every meeting thread in a workspace" — the most
-- common new query. WHERE-filtered so it doesn't bloat for the common
-- discussion-thread case.
CREATE INDEX IF NOT EXISTS idx_thread_meeting_workspace
    ON thread (workspace_id, last_activity_at DESC NULLS LAST)
    WHERE metadata @> '{"kind":"meeting"}'::jsonb;

-- And a generic idx on metadata->>'kind' for cheap filter on any thread kind.
CREATE INDEX IF NOT EXISTS idx_thread_metadata_kind
    ON thread ((metadata->>'kind'))
    WHERE metadata ? 'kind';
