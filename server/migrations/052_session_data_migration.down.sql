-- Best-effort reverse of 052. The up migration was DESTRUCTIVE in the sense
-- that it created new rows (channel, thread, thread_context_item,
-- channel_member) and rewrote message routing. This down migration:
--
--   * Deletes thread_context_item rows keyed by migrated thread_ids.
--   * Clears channel_id/thread_id on messages that were rerouted, restoring
--     session_id-based routing (session_id was preserved by the up migration
--     for exactly this reason — Phase 6 is what drops it).
--   * Deletes channel_member rows for the migrated channels.
--   * Deletes the migrated threads.
--   * Deletes the migrated channels.
--   * Clears session_migration_map so the up migration can be re-applied.
--
-- NOTE: any data appended to a migrated thread or channel AFTER migration
-- WILL be deleted on down. This is a best-effort rollback, not a snapshot
-- restore. The session / session_participant rows themselves are untouched.

DO $$
DECLARE
  m RECORD;
BEGIN
  FOR m IN SELECT session_id, channel_id, thread_id FROM session_migration_map LOOP
    -- Delete derived context items.
    DELETE FROM thread_context_item WHERE thread_id = m.thread_id;

    -- Revert message routing: keep session_id (never cleared), drop channel/thread,
    -- and strip the source_session_id marker the up migration added.
    UPDATE message
    SET channel_id = NULL,
        thread_id  = NULL,
        metadata   = CASE
                       WHEN metadata IS NULL THEN NULL
                       ELSE metadata - 'source_session_id'
                     END
    WHERE session_id = m.session_id
       OR thread_id  = m.thread_id;

    -- Delete channel membership created by the migration.
    DELETE FROM channel_member WHERE channel_id = m.channel_id;

    -- Drop the migration_map row FIRST so the thread/channel deletes don't
    -- trip the session_migration_map FKs on thread_id / channel_id.
    DELETE FROM session_migration_map WHERE session_id = m.session_id;

    -- Delete the thread.
    DELETE FROM thread WHERE id = m.thread_id;

    -- Delete the channel.
    DELETE FROM channel WHERE id = m.channel_id;
  END LOOP;
END$$;
