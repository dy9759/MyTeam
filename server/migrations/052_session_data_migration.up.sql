-- Phase 3 (DESTRUCTIVE): per-session decomposition of legacy `session` rows
-- into `channel` + `thread` + `thread_context_item` + `channel_member`, plus
-- rerouting `message` rows via `channel_id` + `thread_id`.
--
-- Idempotent: sessions that already appear in `session_migration_map` are
-- skipped. The migration is effectively a no-op when the `session` table is
-- empty (the FOR loop iterates zero times), but the SQL must still parse.
--
-- Schema notes (matches actual schema in this worktree — may differ from the
-- plan document in some spots):
--   * `session` has: id, workspace_id, title, creator_id, creator_type,
--     status, max_turns, current_turn, context, issue_id, created_at,
--     updated_at.
--   * `session_participant` has: session_id, participant_id, participant_type,
--     role, joined_at. The `role` column is NOT copied because
--     `channel_member` has no `role` column (PK is
--     (channel_id, member_id, member_type)).
--   * `channel` requires `created_by` (NOT NULL); we use `session.creator_id`.
--   * `channel.name` has a UNIQUE (workspace_id, name) constraint. We
--     generate `session-<8hex>-<slug>` which is unique per session.
--   * `thread_context_item.item_type` CHECK: one of
--     (decision, file, code_snippet, summary, reference).
--   * `thread.status` CHECK: one of (active, archived).

DO $$
DECLARE
  sess           RECORD;
  new_channel_id UUID;
  new_thread_id  UUID;
  ctx            JSONB;
  decision_item  JSONB;
  file_item      JSONB;
  snippet_item   JSONB;
  short_id       TEXT;
  title_slug     TEXT;
BEGIN
  FOR sess IN
    SELECT s.*
    FROM session s
    LEFT JOIN session_migration_map m ON m.session_id = s.id
    WHERE m.session_id IS NULL
    ORDER BY s.created_at
  LOOP
    short_id   := LEFT(REPLACE(sess.id::TEXT, '-', ''), 8);
    title_slug := LOWER(REGEXP_REPLACE(COALESCE(sess.title, 'untitled'), '[^a-zA-Z0-9]+', '-', 'g'));
    title_slug := LEFT(TRIM(BOTH '-' FROM title_slug), 40);
    IF title_slug = '' THEN
      title_slug := 'untitled';
    END IF;

    -- 1. New private channel
    INSERT INTO channel (
      id, workspace_id, name,
      visibility, conversation_type,
      created_by, created_by_type,
      created_at
    ) VALUES (
      gen_random_uuid(),
      sess.workspace_id,
      'session-' || short_id || '-' || title_slug,
      'private',
      'channel',
      sess.creator_id,
      COALESCE(sess.creator_type, 'member'),
      sess.created_at
    )
    RETURNING id INTO new_channel_id;

    -- 2. Copy session_participant → channel_member.
    -- Note: channel_member has no `role` column (PK: channel_id, member_id, member_type).
    INSERT INTO channel_member (channel_id, member_id, member_type, joined_at)
    SELECT new_channel_id, sp.participant_id, sp.participant_type, sp.joined_at
    FROM session_participant sp
    WHERE sp.session_id = sess.id
    ON CONFLICT (channel_id, member_id, member_type) DO NOTHING;

    -- Also ensure the creator is a channel_member (they might not be in session_participant).
    INSERT INTO channel_member (channel_id, member_id, member_type, joined_at)
    VALUES (new_channel_id, sess.creator_id, COALESCE(sess.creator_type, 'member'), sess.created_at)
    ON CONFLICT (channel_id, member_id, member_type) DO NOTHING;

    -- 3. New thread for this session.
    INSERT INTO thread (
      id, channel_id, workspace_id,
      title, issue_id, status,
      created_by, created_by_type,
      metadata,
      reply_count, last_reply_at, last_activity_at,
      created_at
    ) VALUES (
      gen_random_uuid(),
      new_channel_id,
      sess.workspace_id,
      sess.title,
      sess.issue_id,
      CASE WHEN sess.status = 'active' THEN 'active' ELSE 'archived' END,
      sess.creator_id,
      COALESCE(sess.creator_type, 'member'),
      jsonb_build_object(
        'source_session_id', sess.id,
        'max_turns',         sess.max_turns,
        'current_turn',      sess.current_turn
      ),
      0,
      NULL,
      NULL,
      sess.created_at
    )
    RETURNING id INTO new_thread_id;

    -- 4. Reroute messages for this session: fill in channel_id + thread_id.
    -- session_id is intentionally preserved — Phase 6 drops that column.
    UPDATE message
    SET channel_id = new_channel_id,
        thread_id  = new_thread_id,
        metadata   = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('source_session_id', sess.id::TEXT)
    WHERE session_id = sess.id;

    -- 5. Decompose session.context JSONB → thread_context_item rows.
    ctx := COALESCE(sess.context, '{}'::jsonb);

    -- 5a. summary (single string). retention='ttl' is the safe default since
    -- we can't distinguish user-curated vs auto-generated summaries.
    IF (ctx ? 'summary')
       AND jsonb_typeof(ctx->'summary') = 'string'
       AND length(trim(ctx->>'summary')) > 0
    THEN
      INSERT INTO thread_context_item (
        workspace_id, thread_id,
        item_type, body,
        retention_class, created_by_type, created_at
      ) VALUES (
        sess.workspace_id, new_thread_id,
        'summary', ctx->>'summary',
        'ttl', 'system', sess.created_at
      );
    END IF;

    -- 5b. decisions[] — structured decision records, retention='permanent'.
    IF jsonb_typeof(ctx->'decisions') = 'array' THEN
      FOR decision_item IN SELECT value FROM jsonb_array_elements(ctx->'decisions') AS arr(value) LOOP
        INSERT INTO thread_context_item (
          workspace_id, thread_id,
          item_type, title, body, metadata,
          retention_class, created_by_type, created_at
        ) VALUES (
          sess.workspace_id, new_thread_id,
          'decision',
          COALESCE(decision_item->>'title', decision_item->>'decision'),
          COALESCE(decision_item->>'body', decision_item->>'rationale'),
          decision_item - 'title' - 'body' - 'decision' - 'rationale',
          'permanent', 'system', sess.created_at
        );
      END LOOP;
    END IF;

    -- 5c. files[] — file references, retention='ttl'.
    IF jsonb_typeof(ctx->'files') = 'array' THEN
      FOR file_item IN SELECT value FROM jsonb_array_elements(ctx->'files') AS arr(value) LOOP
        INSERT INTO thread_context_item (
          workspace_id, thread_id,
          item_type, title, metadata,
          retention_class, created_by_type, created_at
        ) VALUES (
          sess.workspace_id, new_thread_id,
          'file',
          file_item->>'name',
          file_item,
          'ttl', 'system', sess.created_at
        );
      END LOOP;
    END IF;

    -- 5d. code_snippets[] — language-tagged code, retention='ttl'.
    IF jsonb_typeof(ctx->'code_snippets') = 'array' THEN
      FOR snippet_item IN SELECT value FROM jsonb_array_elements(ctx->'code_snippets') AS arr(value) LOOP
        INSERT INTO thread_context_item (
          workspace_id, thread_id,
          item_type, body, metadata,
          retention_class, created_by_type, created_at
        ) VALUES (
          sess.workspace_id, new_thread_id,
          'code_snippet',
          snippet_item->>'code',
          jsonb_build_object('language', snippet_item->>'language'),
          'ttl', 'system', sess.created_at
        );
      END LOOP;
    END IF;

    -- 5e. topic → thread.title fallback (treat as title only; no separate item).
    IF (sess.title IS NULL OR sess.title = '')
       AND (ctx ? 'topic')
       AND jsonb_typeof(ctx->'topic') = 'string'
    THEN
      UPDATE thread
      SET title = ctx->>'topic'
      WHERE id = new_thread_id;
    END IF;

    -- 6. Migration map.
    INSERT INTO session_migration_map (session_id, channel_id, thread_id, migrated_at)
    VALUES (sess.id, new_channel_id, new_thread_id, now())
    ON CONFLICT (session_id) DO NOTHING;

    -- 7. Recompute thread counters.
    -- reply_count excludes system messages (PRD §4).
    -- last_reply_at is the most recent member/agent message.
    -- last_activity_at is the most recent message regardless of sender_type.
    UPDATE thread
    SET reply_count = COALESCE((
          SELECT COUNT(*)::integer
          FROM message
          WHERE thread_id = new_thread_id
            AND sender_type IN ('member', 'agent')
        ), 0),
        last_reply_at = (
          SELECT MAX(created_at)
          FROM message
          WHERE thread_id = new_thread_id
            AND sender_type IN ('member', 'agent')
        ),
        last_activity_at = COALESCE(
          (
            SELECT MAX(created_at)
            FROM message
            WHERE thread_id = new_thread_id
          ),
          sess.created_at
        )
    WHERE id = new_thread_id;
  END LOOP;
END$$;
