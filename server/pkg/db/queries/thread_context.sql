-- name: CreateThreadContextItem :one
INSERT INTO thread_context_item (
    id, workspace_id, thread_id, item_type, title, body,
    metadata, source_message_id, retention_class, expires_at,
    created_by, created_by_type, created_at
) VALUES (
    gen_random_uuid(),
    @workspace_id,
    @thread_id,
    @item_type,
    sqlc.narg('title'),
    sqlc.narg('body'),
    COALESCE(sqlc.narg('metadata')::jsonb, '{}'::jsonb),
    sqlc.narg('source_message_id'),
    COALESCE(sqlc.narg('retention_class')::text, 'ttl'),
    sqlc.narg('expires_at'),
    sqlc.narg('created_by'),
    COALESCE(sqlc.narg('created_by_type')::text, 'system'),
    now()
)
RETURNING *;

-- name: GetThreadContextItem :one
SELECT * FROM thread_context_item WHERE id = $1;

-- name: ListThreadContextItems :many
SELECT * FROM thread_context_item
WHERE thread_id = $1
ORDER BY created_at ASC;

-- name: ListThreadContextItemsByType :many
SELECT * FROM thread_context_item
WHERE thread_id = $1 AND item_type = $2
ORDER BY created_at ASC;

-- name: UpdateThreadContextItemMetadata :exec
UPDATE thread_context_item SET metadata = $2 WHERE id = $1;

-- name: DeleteThreadContextItem :exec
DELETE FROM thread_context_item WHERE id = $1;

-- name: ExpireTTLContextItems :exec
DELETE FROM thread_context_item
WHERE retention_class = 'ttl'
  AND expires_at IS NOT NULL
  AND expires_at < now();
