-- Agent interaction queries — the messaging layer between agents.
-- Task engine rows stay in `task`; this table only handles the
-- message / broadcast / schema-typed-event flow introduced by
-- migration 075.

-- name: CreateAgentInteraction :one
INSERT INTO agent_interaction (
    workspace_id, from_id, from_type,
    to_agent_id, channel, capability, session_id,
    type, content_type, schema, payload, metadata
)
VALUES (
    $1, $2, $3,
    $4, $5, $6, $7,
    $8, $9, $10, $11, $12
)
RETURNING *;

-- Inbox read — default feed for a given agent. `after` is a cursor on
-- created_at so a polling client can ask for "anything newer than what
-- I've already processed" without caring about IDs.
-- name: ListAgentInbox :many
SELECT *
FROM agent_interaction
WHERE to_agent_id = $1
  AND (sqlc.narg('after')::timestamptz IS NULL
       OR created_at > sqlc.narg('after'))
ORDER BY created_at ASC
LIMIT sqlc.arg('limit');

-- name: ListAgentInteractionsByCapability :many
SELECT *
FROM agent_interaction
WHERE capability = $1
  AND (sqlc.narg('after')::timestamptz IS NULL
       OR created_at > sqlc.narg('after'))
ORDER BY created_at ASC
LIMIT sqlc.arg('limit');

-- name: MarkAgentInteractionDelivered :exec
UPDATE agent_interaction
SET status       = 'delivered',
    delivered_at = now()
WHERE id = $1 AND status = 'pending';

-- Bulk variant used by the inbox GET so we don't fire N UPDATEs per
-- page read. Ignores rows already past 'pending' (idempotent).
-- name: MarkAgentInteractionsDelivered :exec
UPDATE agent_interaction
SET status       = 'delivered',
    delivered_at = now()
WHERE id = ANY(@ids::uuid[]) AND status = 'pending';

-- name: MarkAgentInteractionRead :exec
UPDATE agent_interaction
SET status  = 'read',
    read_at = now()
WHERE id = $1 AND status <> 'read';

-- name: GetAgentInteraction :one
SELECT * FROM agent_interaction WHERE id = $1;
