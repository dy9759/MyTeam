-- name: ListInboxItems :many
SELECT i.*,
       iss.status as issue_status
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
WHERE i.workspace_id = $1 AND i.recipient_type = $2 AND i.recipient_id = $3 AND i.archived = false
ORDER BY i.created_at DESC;

-- name: GetInboxItem :one
SELECT * FROM inbox_item
WHERE id = $1;

-- name: GetInboxItemInWorkspace :one
SELECT * FROM inbox_item
WHERE id = $1 AND workspace_id = $2;

-- name: CreateInboxItem :one
INSERT INTO inbox_item (
    workspace_id, recipient_type, recipient_id,
    type, severity, issue_id, title, body,
    actor_type, actor_id, details
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: MarkInboxRead :one
UPDATE inbox_item SET read = true
WHERE id = $1
RETURNING *;

-- name: ArchiveInboxItem :one
UPDATE inbox_item SET archived = true
WHERE id = $1
RETURNING *;

-- name: CountUnreadInbox :one
SELECT count(*) FROM inbox_item
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND read = false AND archived = false;

-- name: MarkAllInboxRead :execrows
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND archived = false AND read = false;

-- name: ArchiveAllInbox :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND archived = false;

-- name: ArchiveAllReadInbox :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND read = true AND archived = false;

-- name: ArchiveCompletedInbox :execrows
UPDATE inbox_item i SET archived = true
WHERE i.workspace_id = $1 AND i.recipient_type = 'member' AND i.recipient_id = $2 AND i.archived = false
  AND i.issue_id IN (SELECT id FROM issue WHERE status IN ('done', 'cancelled'));

-- name: CreateEscalationInboxItem :one
INSERT INTO inbox_item (workspace_id, recipient_id, recipient_type, type, severity, title, body, action_required, action_type, deadline, related_project_id, related_run_id, actor_type, actor_id)
VALUES (@workspace_id, @recipient_id, @recipient_type, @type, @severity, @title, @body, @action_required, @action_type, @deadline, @related_project_id, @related_run_id, @actor_type, @actor_id)
RETURNING *;

-- name: ListInboxUnresolved :many
-- Plan 4 §8: paginated list of unresolved inbox items for a recipient.
-- Uses idx_inbox_item_recipient_active partial index.
SELECT * FROM inbox_item
WHERE recipient_id = @recipient_id
  AND resolved_at IS NULL
ORDER BY created_at DESC
LIMIT @limit_count OFFSET @offset_count;

-- name: ResolveInboxItem :exec
-- Plan 4 §8: mark an inbox item as resolved with a resolution and operator.
-- Recipient ownership check enforced in WHERE clause.
UPDATE inbox_item
SET resolved_at   = now(),
    resolution    = @resolution,
    resolution_by = sqlc.narg('resolution_by')
WHERE id = @id
  AND recipient_id = @recipient_id;

-- name: MarkInboxItemRead :exec
-- Plan 4 §8: mark a single inbox item as read for a specific recipient.
-- Idempotent: no-op when the row is already read.
UPDATE inbox_item
SET read = true
WHERE id = @id
  AND recipient_id = @recipient_id
  AND read = false;

-- name: MarkAllInboxItemsRead :execrows
-- Plan 4 §8: mark all unread items as read for a recipient (workspace-agnostic).
UPDATE inbox_item
SET read = true
WHERE recipient_id = @recipient_id
  AND read = false;
