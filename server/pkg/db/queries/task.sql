-- name: CreateTask :one
INSERT INTO task (
    plan_id, run_id, workspace_id,
    title, description, step_order, depends_on,
    primary_assignee_id, fallback_agent_ids, required_skills,
    collaboration_mode, acceptance_criteria,
    timeout_rule, retry_rule, escalation_policy,
    input_context_refs
) VALUES (
    @plan_id, sqlc.narg('run_id'), @workspace_id,
    @title, sqlc.narg('description'), COALESCE(sqlc.narg('step_order')::int, 0), COALESCE(sqlc.narg('depends_on')::uuid[], '{}'),
    sqlc.narg('primary_assignee_id'), COALESCE(sqlc.narg('fallback_agent_ids')::uuid[], '{}'), COALESCE(sqlc.narg('required_skills')::text[], '{}'),
    COALESCE(sqlc.narg('collaboration_mode')::text, 'agent_exec_human_review'), sqlc.narg('acceptance_criteria'),
    COALESCE(sqlc.narg('timeout_rule')::jsonb, '{"max_duration_seconds":1800,"action":"retry"}'),
    COALESCE(sqlc.narg('retry_rule')::jsonb, '{"max_retries":2,"retry_delay_seconds":30}'),
    COALESCE(sqlc.narg('escalation_policy')::jsonb, '{"escalate_after_seconds":600}'),
    COALESCE(sqlc.narg('input_context_refs')::jsonb, '[]')
)
RETURNING *;

-- name: GetTask :one
SELECT * FROM task WHERE id = $1;

-- name: ListTasksByPlan :many
SELECT * FROM task WHERE plan_id = @plan_id ORDER BY step_order ASC, created_at ASC;

-- name: ListTasksByRun :many
SELECT * FROM task WHERE run_id = @run_id ORDER BY step_order ASC;

-- name: ListReadyTasks :many
SELECT * FROM task
WHERE run_id = @run_id AND status = 'ready'
ORDER BY step_order ASC;

-- name: UpdateTaskStatus :one
UPDATE task SET
    status = @status,
    started_at = CASE WHEN @status = 'running' AND started_at IS NULL THEN now() ELSE started_at END,
    completed_at = CASE WHEN @status IN ('completed','failed','cancelled') AND completed_at IS NULL THEN now() ELSE completed_at END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: AssignTaskAgent :exec
UPDATE task SET
    actual_agent_id = @actual_agent_id,
    status = 'assigned',
    updated_at = now()
WHERE id = $1;

-- name: AssignTaskFallbackAgent :exec
-- Switches the task to a fallback agent and zeroes current_retry in the same
-- statement so the new agent gets a fresh retry budget. Use this whenever the
-- scheduler hands the task off because the previous agent exhausted its budget.
UPDATE task SET
    actual_agent_id = @actual_agent_id,
    current_retry = 0,
    status = 'assigned',
    updated_at = now()
WHERE id = $1;

-- name: UpdateTaskDependsOn :exec
UPDATE task SET
    depends_on = @depends_on,
    updated_at = now()
WHERE id = $1;

-- name: IncrementTaskRetry :exec
UPDATE task SET
    current_retry = current_retry + 1,
    updated_at = now()
WHERE id = $1;

-- name: ResetTaskForNewRun :exec
UPDATE task SET
    run_id = @run_id,
    status = 'draft',
    actual_agent_id = NULL,
    current_retry = 0,
    started_at = NULL,
    completed_at = NULL,
    result = NULL,
    error = NULL,
    updated_at = now()
WHERE plan_id = @plan_id;

-- name: SetTaskResult :exec
UPDATE task SET
    result = @result,
    status = @status,
    completed_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: SetTaskError :exec
UPDATE task SET
    error = @error,
    status = @status,
    updated_at = now()
WHERE id = $1;

-- name: DeleteTask :exec
DELETE FROM task WHERE id = $1;
