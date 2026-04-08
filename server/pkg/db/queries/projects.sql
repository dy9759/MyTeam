-- name: CreateProject :one
INSERT INTO project (workspace_id, title, description, status, schedule_type, cron_expr, source_conversations, channel_id, creator_owner_id)
VALUES (@workspace_id, @title, @description, @status, @schedule_type, @cron_expr, @source_conversations, @channel_id, @creator_owner_id)
RETURNING *;

-- name: GetProject :one
SELECT * FROM project WHERE id = @id;

-- name: ListProjects :many
SELECT * FROM project WHERE workspace_id = @workspace_id ORDER BY created_at DESC;

-- name: UpdateProject :one
UPDATE project SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status),
    schedule_type = COALESCE(sqlc.narg('schedule_type'), schedule_type),
    cron_expr = sqlc.narg('cron_expr'),
    updated_at = NOW()
WHERE id = @id
RETURNING *;

-- name: UpdateProjectStatus :exec
UPDATE project SET status = @status, updated_at = NOW() WHERE id = @id;

-- name: DeleteProject :exec
DELETE FROM project WHERE id = @id;

-- name: UpdateProjectChannel :exec
UPDATE project SET channel_id = @channel_id, updated_at = NOW() WHERE id = @id;
