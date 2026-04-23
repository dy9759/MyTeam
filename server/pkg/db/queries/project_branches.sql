-- name: CreateProjectBranch :one
INSERT INTO project_branch (project_id, name, parent_branch_id, is_default, created_by)
VALUES (@project_id, @name, @parent_branch_id, @is_default, @created_by)
RETURNING *;

-- name: ListProjectBranches :many
SELECT * FROM project_branch WHERE project_id = @project_id ORDER BY created_at ASC;

-- name: GetProjectBranch :one
SELECT * FROM project_branch WHERE id = @id;

-- name: GetDefaultBranch :one
SELECT * FROM project_branch WHERE project_id = @project_id AND is_default = TRUE LIMIT 1;

-- name: UpdateProjectBranchStatus :exec
UPDATE project_branch SET status = @status WHERE id = @id;

-- name: DeleteProjectBranch :exec
DELETE FROM project_branch WHERE id = @id;
