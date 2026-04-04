-- name: CreateRemoteSession :one
INSERT INTO remote_session (agent_id, workspace_id, owner_id, title, environment)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: GetRemoteSession :one
SELECT * FROM remote_session WHERE id = $1;

-- name: ListRemoteSessionsByAgent :many
SELECT * FROM remote_session WHERE agent_id = $1 ORDER BY updated_at DESC LIMIT $2 OFFSET $3;

-- name: ListRemoteSessionsByWorkspace :many
SELECT * FROM remote_session WHERE workspace_id = $1 ORDER BY updated_at DESC LIMIT $2 OFFSET $3;

-- name: UpdateRemoteSessionStatus :exec
UPDATE remote_session SET status = $2, updated_at = NOW() WHERE id = $1;

-- name: AddRemoteSessionEvent :exec
UPDATE remote_session SET events = events || $2::jsonb, updated_at = NOW() WHERE id = $1;

-- name: DeleteRemoteSession :exec
DELETE FROM remote_session WHERE id = $1;
