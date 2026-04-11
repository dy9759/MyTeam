-- name: ListWorkspaceCollaborators :many
SELECT * FROM workspace_collaborator
WHERE workspace_id = @workspace_id
ORDER BY added_at ASC;

-- name: CreateWorkspaceCollaborator :one
INSERT INTO workspace_collaborator (workspace_id, email, role, added_by)
VALUES (@workspace_id, lower(@email), @role, @added_by)
RETURNING *;

-- name: DeleteWorkspaceCollaborator :exec
DELETE FROM workspace_collaborator
WHERE workspace_id = @workspace_id AND email = lower(@email);
