-- name: CreatePlan :one
INSERT INTO plan (workspace_id, title, description, source_type, source_ref_id, constraints, expected_output, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetPlan :one
SELECT * FROM plan WHERE id = $1;

-- name: ListPlans :many
SELECT * FROM plan WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;

-- name: ApprovePlan :one
UPDATE plan SET approval_status = 'approved', approved_by = $2, approved_at = NOW(), updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: GetPlanBySourceRef :one
SELECT * FROM plan WHERE source_type = $1 AND source_ref_id = $2 ORDER BY created_at DESC LIMIT 1;

-- name: GetPlanByThread :one
SELECT * FROM plan WHERE thread_id = $1 LIMIT 1;

-- name: DeletePlan :exec
DELETE FROM plan WHERE id = $1;

-- name: GetPlanByProject :one
SELECT * FROM plan WHERE project_id = @project_id ORDER BY created_at DESC LIMIT 1;

-- name: UpdatePlanApproval :exec
UPDATE plan SET approval_status = @approval_status, approved_by = @approved_by, approved_at = NOW(), updated_at = NOW() WHERE id = @id;

-- name: UpdatePlanProject :exec
UPDATE plan SET project_id = @project_id, version_id = @version_id, updated_at = NOW() WHERE id = @id;
