-- name: CreateMessage :one
INSERT INTO message (workspace_id, sender_id, sender_type, channel_id, recipient_id, recipient_type, content, content_type, file_id, file_name, file_size, file_content_type, metadata, parent_id, type, thread_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, sqlc.narg('thread_id'))
RETURNING *;

-- name: GetMessage :one
SELECT * FROM message WHERE id = $1;

-- name: ListChannelMessages :many
SELECT * FROM message WHERE channel_id = $1 ORDER BY created_at ASC LIMIT $2 OFFSET $3;

-- name: ListDMMessages :many
-- Returns messages exchanged between a self actor (user/member) and a peer
-- actor that may be either a member or an agent. Pass `peer_type` so the
-- recipient_type filter matches the actual rows.
SELECT * FROM message
WHERE workspace_id = @workspace_id
  AND ((sender_id = @self_id AND sender_type = @self_type
        AND recipient_id = @peer_id AND recipient_type = @peer_type)
    OR (sender_id = @peer_id AND sender_type = @peer_type
        AND recipient_id = @self_id AND recipient_type = @self_type))
ORDER BY created_at ASC
LIMIT @limit_count OFFSET @offset_count;

-- name: UpdateMessageStatus :exec
UPDATE message SET status = $2, updated_at = NOW() WHERE id = $1;

-- name: MarkMessagesRead :many
-- Marks every message in the id list as read, but only when the caller
-- is the legitimate recipient. Returns the rows that were actually
-- updated so the handler can broadcast message:read events for them
-- (rows where status was already 'read' still come back — de-dup is
-- left to the client).
UPDATE message
SET status = 'read', updated_at = NOW()
WHERE id = ANY(@ids::uuid[])
  AND status <> 'read'
  AND (
    -- DM to this user
    (recipient_id = @actor_id AND recipient_type = @actor_type)
    -- OR channel message in a channel this user is a member of
    OR (channel_id IS NOT NULL AND channel_id IN (
      SELECT channel_id FROM channel_member WHERE member_id = @actor_id AND member_type = @actor_type
    ))
  )
RETURNING id, channel_id, sender_id, sender_type;

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

-- name: CountAgentRepliesInThread :one
-- Counts auto-replies issued by a specific agent inside a thread since a cutoff.
-- Used by MediationService anti-loop checks (Plan 3 Task 6).
SELECT COUNT(*) FROM message
WHERE thread_id = $1
  AND sender_id = $2
  AND sender_type = 'agent'
  AND created_at >= $3;

-- name: ListRecentThreadMessages :many
-- Returns the most recent N messages of a thread in chronological order.
-- Used by MediationService anti-loop tail-scan (Plan 3 Task 6).
SELECT * FROM (
    SELECT * FROM message
    WHERE thread_id = $1
    ORDER BY created_at DESC
    LIMIT $2
) latest
ORDER BY created_at ASC;

-- name: InsertMessageWithAudit :one
-- Inserts a message that records both the effective actor (whose voice the
-- message speaks in) and the real operator (who actually performed the
-- action). Adapted from spec: real `message` table uses `content` (not `body`)
-- and requires `content_type`/`type` (defaults apply but exposed for clarity).
INSERT INTO message (
    id, workspace_id, channel_id, thread_id,
    sender_id, sender_type, content, content_type, type, metadata,
    is_impersonated,
    effective_actor_id, effective_actor_type,
    real_operator_id, real_operator_type,
    created_at
) VALUES (
    gen_random_uuid(), $1, $2, $3,
    $4, $5, $6, $7, $8, $9,
    $10,
    $11, $12,
    $13, $14,
    now()
)
RETURNING *;
