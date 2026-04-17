-- name: UpsertThread :one
INSERT INTO thread (id, channel_id, title, reply_count, last_reply_at, last_activity_at, created_at)
VALUES ($1, $2, $3, 1, NOW(), NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
    reply_count       = thread.reply_count + 1,
    last_reply_at     = NOW(),
    last_activity_at  = NOW()
RETURNING *;

-- name: IncrementThreadReplyCount :exec
-- Legacy increment kept for the current message handler. New callers should
-- use IncrementThreadReply (member/agent only) and TouchThreadActivity
-- (system messages). Migration to the new contract lands in Task 4.
UPDATE thread
SET reply_count = reply_count + 1, last_reply_at = NOW()
WHERE id = $1;

-- name: DeleteThread :exec
DELETE FROM thread WHERE id = $1;

-- ===== Plan 3 / Phase 1: Thread extension =====

-- name: CreateThread :one
INSERT INTO thread (
    id, channel_id, workspace_id, root_message_id, issue_id,
    title, status, created_by, created_by_type, metadata,
    reply_count, last_reply_at, last_activity_at, created_at
) VALUES (
    COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()),
    @channel_id,
    @workspace_id,
    sqlc.narg('root_message_id'),
    sqlc.narg('issue_id'),
    sqlc.narg('title'),
    COALESCE(sqlc.narg('status')::text, 'active'),
    sqlc.narg('created_by'),
    sqlc.narg('created_by_type'),
    COALESCE(sqlc.narg('metadata')::jsonb, '{}'::jsonb),
    0, NULL, now(), now()
)
RETURNING *;

-- name: GetThread :one
SELECT * FROM thread WHERE id = $1;

-- name: GetThreadByRootMessage :one
SELECT * FROM thread WHERE root_message_id = $1 LIMIT 1;

-- name: ListThreadsByChannel :many
SELECT * FROM thread
WHERE channel_id = @channel_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY last_activity_at DESC NULLS LAST, created_at DESC;

-- name: ListThreadsByWorkspace :many
SELECT * FROM thread
WHERE workspace_id = @workspace_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY last_activity_at DESC NULLS LAST, created_at DESC
LIMIT @limit_count OFFSET @offset_count;

-- name: ListThreadsByIssue :many
SELECT * FROM thread
WHERE issue_id = @issue_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY created_at DESC;

-- name: UpdateThreadStatus :exec
UPDATE thread
SET status = $2
WHERE id = $1;

-- name: UpdateThreadTitle :exec
UPDATE thread
SET title = $2
WHERE id = $1;

-- name: UpdateThreadMetadata :exec
UPDATE thread
SET metadata = $2
WHERE id = $1;

-- name: TouchThreadActivity :exec
UPDATE thread
SET last_activity_at = now()
WHERE id = $1;

-- name: IncrementThreadReply :exec
-- New-semantics: callers invoke this only for human/agent messages,
-- NOT system messages. System-message handlers call TouchThreadActivity
-- instead. Handler migration lands in Task 4.
UPDATE thread
SET reply_count      = reply_count + 1,
    last_reply_at    = now(),
    last_activity_at = now()
WHERE id = $1;
