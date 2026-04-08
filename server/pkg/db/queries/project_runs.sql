-- name: CreateProjectRun :one
INSERT INTO project_run (plan_id, project_id, status)
VALUES (@plan_id, @project_id, @status)
RETURNING *;

-- name: GetProjectRun :one
SELECT * FROM project_run WHERE id = @id;

-- name: ListProjectRuns :many
SELECT * FROM project_run WHERE project_id = @project_id ORDER BY created_at DESC;

-- name: UpdateProjectRunStatus :exec
UPDATE project_run SET status = @status, updated_at = NOW() WHERE id = @id;

-- name: StartProjectRun :exec
UPDATE project_run SET status = 'running', start_at = NOW(), updated_at = NOW() WHERE id = @id;

-- name: CompleteProjectRun :exec
UPDATE project_run SET status = 'completed', end_at = NOW(), output_refs = @output_refs, updated_at = NOW() WHERE id = @id;

-- name: FailProjectRun :exec
UPDATE project_run SET status = 'failed', end_at = NOW(), failure_reason = @failure_reason, retry_count = retry_count + 1, updated_at = NOW() WHERE id = @id;

-- name: GetActiveProjectRun :one
SELECT * FROM project_run WHERE project_id = @project_id AND status IN ('pending', 'running') LIMIT 1;
