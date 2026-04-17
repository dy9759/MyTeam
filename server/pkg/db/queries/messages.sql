-- name: CreateMessage :one
INSERT INTO message (workspace_id, sender_id, sender_type, channel_id, recipient_id, recipient_type, session_id, content, content_type, file_id, file_name, file_size, file_content_type, metadata, parent_id, type)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
RETURNING *;

-- name: GetMessage :one
SELECT * FROM message WHERE id = $1;

-- name: ListChannelMessages :many
SELECT * FROM message WHERE channel_id = $1 ORDER BY created_at ASC LIMIT $2 OFFSET $3;

-- name: ListDMMessages :many
SELECT * FROM message
WHERE workspace_id = $1
  AND ((sender_id = $2 AND sender_type = $3 AND recipient_id = $4 AND recipient_type = $5)
    OR (sender_id = $4 AND sender_type = $5 AND recipient_id = $2 AND recipient_type = $3))
ORDER BY created_at ASC LIMIT $6 OFFSET $7;

-- name: ListSessionMessages :many
SELECT * FROM message WHERE session_id = $1 ORDER BY created_at ASC LIMIT $2 OFFSET $3;

-- name: UpdateMessageStatus :exec
UPDATE message SET status = $2, updated_at = NOW() WHERE id = $1;

-- name: ListThreadMessages :many
SELECT * FROM message WHERE parent_id = $1 ORDER BY created_at ASC LIMIT $2 OFFSET $3;

-- name: CountUnreadMessages :one
SELECT COUNT(*) FROM message
WHERE recipient_id = $1 AND recipient_type = $2 AND status = 'sent';

-- name: ListMessagesByType :many
SELECT * FROM message WHERE channel_id = $1 AND type = $2
ORDER BY created_at ASC LIMIT $3 OFFSET $4;

-- name: ListMessagesByThread :many
SELECT * FROM message
WHERE thread_id = $1
ORDER BY created_at ASC
LIMIT $2 OFFSET $3;

-- name: ListMessagesForOwnerAgents :many
SELECT * FROM message
WHERE workspace_id = $1 AND sender_id = ANY(@owner_agent_ids::uuid[])
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: InsertMessageWithAudit :one
-- Inserts a message that records both the effective actor (whose voice the
-- message speaks in) and the real operator (who actually performed the
-- action). Adapted from spec: real `message` table uses `content` (not `body`)
-- and requires `content_type`/`type` (defaults apply but exposed for clarity).
INSERT INTO message (
    id, workspace_id, channel_id, thread_id, session_id,
    sender_id, sender_type, content, content_type, type, metadata,
    is_impersonated,
    effective_actor_id, effective_actor_type,
    real_operator_id, real_operator_type,
    created_at
) VALUES (
    gen_random_uuid(), $1, $2, $3, $4,
    $5, $6, $7, $8, $9, $10,
    $11,
    $12, $13,
    $14, $15,
    now()
)
RETURNING *;
