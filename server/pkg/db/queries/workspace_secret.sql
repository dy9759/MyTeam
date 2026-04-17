-- name: CreateWorkspaceSecret :one
INSERT INTO workspace_secret (workspace_id, key, value_encrypted, created_by)
VALUES (@workspace_id, @key, @value_encrypted, @created_by)
ON CONFLICT (workspace_id, key) DO UPDATE
SET value_encrypted = EXCLUDED.value_encrypted,
    rotated_at = now()
RETURNING *;

-- name: GetWorkspaceSecret :one
SELECT * FROM workspace_secret
WHERE workspace_id = @workspace_id AND key = @key;

-- name: ListWorkspaceSecretKeys :many
SELECT id, workspace_id, key, created_by, created_at, rotated_at
FROM workspace_secret
WHERE workspace_id = @workspace_id
ORDER BY key;

-- name: DeleteWorkspaceSecret :exec
DELETE FROM workspace_secret
WHERE workspace_id = @workspace_id AND key = @key;
