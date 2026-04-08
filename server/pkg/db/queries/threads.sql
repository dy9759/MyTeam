-- name: UpsertThread :one
INSERT INTO thread (id, channel_id, title, reply_count, last_reply_at, created_at)
VALUES ($1, $2, $3, 1, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
    reply_count = thread.reply_count + 1,
    last_reply_at = NOW()
RETURNING *;

-- name: GetThread :one
SELECT * FROM thread WHERE id = $1;

-- name: ListThreadsByChannel :many
SELECT * FROM thread
WHERE channel_id = $1
ORDER BY last_reply_at DESC
LIMIT $2 OFFSET $3;

-- name: IncrementThreadReplyCount :exec
UPDATE thread
SET reply_count = reply_count + 1, last_reply_at = NOW()
WHERE id = $1;

-- name: DeleteThread :exec
DELETE FROM thread WHERE id = $1;
