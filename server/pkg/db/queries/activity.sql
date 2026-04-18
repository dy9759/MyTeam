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

-- name: ListActivityForMember :many
-- PRD §3.4 row-level isolation for non-admin members.
-- A member sees activity_log rows where ANY of:
--   * actor_id matches the member's own user_id, OR
--   * related_project_id is a project the member created or joined via channel, OR
--   * related_task_id belongs to a task whose actual_agent OR primary_assignee
--     is owned by the member. Both columns matter because primary_assignee_id
--     is set at plan time while actual_agent_id is only populated once the
--     scheduler claims the task — draft/queued tasks would otherwise be
--     invisible to the assignee's owner.
-- Optional sqlc.narg filters narrow the result by project_id / task_id / event_type
-- so the same query backs the existing route variants.
WITH accessible_projects AS (
    SELECT id FROM project
    WHERE workspace_id = @workspace_id AND creator_owner_id = @self_user_id
    UNION
    SELECT c.project_id FROM channel_member cm
    JOIN channel c ON cm.channel_id = c.id
    WHERE cm.member_id = @self_user_id
      AND cm.member_type = 'member'
      AND c.workspace_id = @workspace_id
      AND c.project_id IS NOT NULL
), owned_agents AS (
    SELECT id FROM agent
    WHERE workspace_id = @workspace_id AND owner_id = @self_user_id
)
SELECT al.* FROM activity_log al
WHERE al.workspace_id = @workspace_id
  AND (
      al.actor_id = @self_user_id
      OR al.related_project_id IN (SELECT id FROM accessible_projects)
      OR al.related_task_id IN (
          SELECT t.id FROM task t
          WHERE t.workspace_id = @workspace_id
            AND (
                t.actual_agent_id IN (SELECT id FROM owned_agents)
                OR t.primary_assignee_id IN (SELECT id FROM owned_agents)
            )
      )
  )
  AND (sqlc.narg('project_filter')::uuid IS NULL OR al.related_project_id = sqlc.narg('project_filter')::uuid)
  AND (sqlc.narg('task_filter')::uuid IS NULL OR al.related_task_id = sqlc.narg('task_filter')::uuid)
  AND (sqlc.narg('event_type_filter')::text IS NULL OR al.event_type LIKE sqlc.narg('event_type_filter')::text)
ORDER BY al.created_at DESC
LIMIT @limit_count OFFSET @offset_count;
