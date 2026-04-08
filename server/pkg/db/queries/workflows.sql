-- name: CreateWorkflow :one
INSERT INTO workflow (plan_id, workspace_id, title, status, type, cron_expr, dag, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetWorkflow :one
SELECT * FROM workflow WHERE id = $1;

-- name: ListWorkflows :many
SELECT * FROM workflow WHERE workspace_id = $1 ORDER BY updated_at DESC LIMIT $2 OFFSET $3;

-- name: UpdateWorkflowStatus :exec
UPDATE workflow SET status = $2, updated_at = NOW() WHERE id = $1;

-- name: UpdateWorkflowDAG :exec
UPDATE workflow SET dag = $2, version = version + 1, updated_at = NOW() WHERE id = $1;

-- name: DeleteWorkflow :exec
DELETE FROM workflow WHERE id = $1;

-- name: CreateWorkflowStep :one
INSERT INTO workflow_step (workflow_id, step_order, description, agent_id, fallback_agent_ids, required_skills, timeout_ms, retry_count, depends_on)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListWorkflowSteps :many
SELECT * FROM workflow_step WHERE workflow_id = $1 ORDER BY step_order ASC;

-- name: UpdateWorkflowStepStatus :exec
UPDATE workflow_step SET status = $2, started_at = CASE WHEN $2 = 'running' THEN NOW() ELSE started_at END, completed_at = CASE WHEN $2 IN ('completed','failed') THEN NOW() ELSE completed_at END, result = $3, error = $4 WHERE id = $1;

-- name: GetWorkflowStep :one
SELECT * FROM workflow_step WHERE id = $1;

-- name: UpdateWorkflowStep :one
UPDATE workflow_step
SET description = COALESCE(sqlc.narg('description'), description),
    timeout_ms = COALESCE(sqlc.narg('timeout_ms'), timeout_ms),
    retry_count = COALESCE(sqlc.narg('retry_count'), retry_count)
WHERE id = @id
RETURNING *;

-- name: DeleteWorkflowStep :exec
DELETE FROM workflow_step WHERE id = $1;

-- name: CreateWorkflowStepTask :one
-- Creates a task queue entry linked to a workflow step
INSERT INTO agent_task_queue (agent_id, issue_id, status, priority, workflow_step_id, run_id)
VALUES (@agent_id, NULL, 'pending', @priority, @workflow_step_id, @run_id)
RETURNING *;

-- name: GetTaskByWorkflowStep :one
SELECT * FROM agent_task_queue WHERE workflow_step_id = @workflow_step_id AND status NOT IN ('completed', 'failed', 'cancelled') LIMIT 1;

-- name: ListTasksByRun :many
SELECT * FROM agent_task_queue WHERE run_id = @run_id ORDER BY created_at;

-- name: UpdateWorkflowStepActualAgent :exec
UPDATE workflow_step SET actual_agent_id = @actual_agent_id WHERE id = @id;

-- name: IncrementWorkflowStepRetry :exec
UPDATE workflow_step SET current_retry = current_retry + 1 WHERE id = @id;

-- name: ListWorkflowStepsByRun :many
SELECT * FROM workflow_step WHERE run_id = @run_id ORDER BY step_order;

-- name: UpdateWorkflowStepOutputRefs :exec
UPDATE workflow_step SET output_refs = @output_refs WHERE id = @id;
