-- name: CreateProjectResult :one
INSERT INTO project_result (run_id, project_id, version_id, summary, artifacts, deliverables)
VALUES (@run_id, @project_id, @version_id, @summary, @artifacts, @deliverables)
RETURNING *;

-- name: GetProjectResult :one
SELECT * FROM project_result WHERE id = @id;

-- name: GetProjectResultByRun :one
SELECT * FROM project_result WHERE run_id = @run_id LIMIT 1;

-- name: ListProjectResults :many
SELECT * FROM project_result WHERE project_id = @project_id ORDER BY created_at DESC;

-- name: UpdateProjectResultAcceptance :exec
UPDATE project_result
SET acceptance_status = @acceptance_status, accepted_by = @accepted_by, accepted_at = NOW()
WHERE id = @id;

-- name: UpdateProjectResultSummary :exec
UPDATE project_result
SET summary = @summary, artifacts = @artifacts, deliverables = @deliverables
WHERE id = @id;
