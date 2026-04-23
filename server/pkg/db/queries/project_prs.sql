-- name: CreateProjectPR :one
INSERT INTO project_pr (project_id, source_branch_id, target_branch_id, source_version_id, title, description, created_by)
VALUES (@project_id, @source_branch_id, @target_branch_id, @source_version_id, @title, @description, @created_by)
RETURNING *;

-- name: GetProjectPR :one
SELECT * FROM project_pr WHERE id = @id;

-- name: ListProjectPRs :many
SELECT * FROM project_pr WHERE project_id = @project_id ORDER BY created_at DESC;

-- name: ListProjectPRsByStatus :many
SELECT * FROM project_pr WHERE project_id = @project_id AND status = @status ORDER BY created_at DESC;

-- name: MergeProjectPR :exec
UPDATE project_pr
SET status = 'merged', merged_version_id = @merged_version_id, merged_by = @merged_by, merged_at = NOW(), updated_at = NOW()
WHERE id = @id;

-- name: CloseProjectPR :exec
UPDATE project_pr SET status = 'closed', updated_at = NOW() WHERE id = @id;

-- name: UpdateProjectPRConflicts :exec
UPDATE project_pr SET has_conflicts = @has_conflicts, updated_at = NOW() WHERE id = @id;
