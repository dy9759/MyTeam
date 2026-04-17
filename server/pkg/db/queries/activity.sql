-- name: ListActivities :many
SELECT * FROM activity_log
WHERE issue_id = $1
ORDER BY created_at ASC
LIMIT $2 OFFSET $3;

-- name: CreateActivity :one
INSERT INTO activity_log (
    workspace_id, issue_id, actor_type, actor_id, action, event_type, details
) VALUES ($1, $2, $3, $4, $5, $5, $6)
RETURNING *;

-- name: WriteActivityLog :one
INSERT INTO activity_log (
    workspace_id, event_type, action,
    actor_id, actor_type,
    effective_actor_id, effective_actor_type,
    real_operator_id, real_operator_type,
    related_project_id, related_plan_id, related_task_id, related_slot_id,
    related_execution_id, related_channel_id, related_thread_id,
    related_agent_id, related_runtime_id,
    payload, retention_class, details, created_at
) VALUES (
    @workspace_id, @event_type, @event_type,
    sqlc.narg('actor_id'), sqlc.narg('actor_type'),
    sqlc.narg('effective_actor_id'), sqlc.narg('effective_actor_type'),
    sqlc.narg('real_operator_id'), sqlc.narg('real_operator_type'),
    sqlc.narg('related_project_id'), sqlc.narg('related_plan_id'), sqlc.narg('related_task_id'), sqlc.narg('related_slot_id'),
    sqlc.narg('related_execution_id'), sqlc.narg('related_channel_id'), sqlc.narg('related_thread_id'),
    sqlc.narg('related_agent_id'), sqlc.narg('related_runtime_id'),
    COALESCE(sqlc.narg('payload')::jsonb, '{}'::jsonb),
    COALESCE(sqlc.narg('retention_class')::text, 'permanent'),
    COALESCE(sqlc.narg('details')::jsonb, '{}'::jsonb),
    now()
)
RETURNING *;

-- name: ListActivityLogByProject :many
SELECT * FROM activity_log
WHERE workspace_id = @workspace_id AND related_project_id = @project_id
ORDER BY created_at DESC
LIMIT @limit_count OFFSET @offset_count;

-- name: ListActivityLogByTask :many
SELECT * FROM activity_log
WHERE workspace_id = @workspace_id AND related_task_id = @task_id
ORDER BY created_at DESC
LIMIT @limit_count OFFSET @offset_count;

-- name: ListActivityLogByEventType :many
SELECT * FROM activity_log
WHERE workspace_id = @workspace_id AND event_type LIKE @event_type_pattern
ORDER BY created_at DESC
LIMIT @limit_count OFFSET @offset_count;
