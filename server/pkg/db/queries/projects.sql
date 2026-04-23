-- name: CreateProject :one
INSERT INTO project (workspace_id, title, description, status, schedule_type, cron_expr, source_conversations, channel_id, creator_owner_id)
VALUES (@workspace_id, @title, @description, @status, @schedule_type, @cron_expr, @source_conversations, @channel_id, @creator_owner_id)
RETURNING
    id,
    workspace_id,
    title,
    description,
    status,
    created_by,
    plan_id,
    created_at,
    updated_at,
    schedule_type,
    cron_expr,
    source_conversations,
    channel_id,
    creator_owner_id;

-- name: GetProject :one
SELECT
    id,
    workspace_id,
    title,
    description,
    status,
    created_by,
    plan_id,
    created_at,
    updated_at,
    schedule_type,
    cron_expr,
    source_conversations,
    channel_id,
    creator_owner_id
FROM project
WHERE id = @id;

-- name: ListProjects :many
SELECT
    id,
    workspace_id,
    title,
    description,
    status,
    created_by,
    plan_id,
    created_at,
    updated_at,
    schedule_type,
    cron_expr,
    source_conversations,
    channel_id,
    creator_owner_id
FROM project
WHERE workspace_id = @workspace_id
ORDER BY created_at DESC;

-- name: UpdateProject :one
UPDATE project SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status),
    schedule_type = COALESCE(sqlc.narg('schedule_type'), schedule_type),
    cron_expr = sqlc.narg('cron_expr'),
    updated_at = NOW()
WHERE id = @id
RETURNING
    id,
    workspace_id,
    title,
    description,
    status,
    created_by,
    plan_id,
    created_at,
    updated_at,
    schedule_type,
    cron_expr,
    source_conversations,
    channel_id,
    creator_owner_id;

-- name: UpdateProjectStatus :exec
UPDATE project SET status = @status, updated_at = NOW() WHERE id = @id;

-- name: DeleteProject :exec
DELETE FROM project WHERE id = @id;

-- name: UpdateProjectChannel :exec
UPDATE project SET channel_id = @channel_id, updated_at = NOW() WHERE id = @id;

-- name: ListScheduledProjects :many
SELECT * FROM project
WHERE status = 'scheduled'
  AND schedule_type IN ('scheduled_once', 'recurring')
ORDER BY created_at ASC;

-- name: ListRunningRecurringProjects :many
SELECT * FROM project
WHERE status = 'running'
  AND schedule_type = 'recurring'
ORDER BY created_at ASC;

-- name: CountProjectRuns :one
SELECT COUNT(*) FROM project_run WHERE project_id = @project_id;

-- name: CountConsecutiveFailedRuns :one
SELECT COUNT(*) FROM (
  SELECT status FROM project_run
  WHERE project_id = @project_id
  ORDER BY created_at DESC
  LIMIT @limit_count
) sub
WHERE sub.status = 'failed';
