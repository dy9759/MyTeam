-- name: CreateProjectShare :one
INSERT INTO project_share (project_id, owner_id, role, can_merge_pr, granted_by)
VALUES (@project_id, @owner_id, @role, @can_merge_pr, @granted_by)
RETURNING *;

-- name: GetProjectShare :one
SELECT * FROM project_share WHERE project_id = @project_id AND owner_id = @owner_id;

-- name: ListProjectShares :many
SELECT * FROM project_share WHERE project_id = @project_id ORDER BY granted_at ASC;

-- name: ListSharedProjects :many
SELECT p.* FROM project p
JOIN project_share ps ON ps.project_id = p.id
WHERE ps.owner_id = @owner_id
ORDER BY p.updated_at DESC;

-- name: UpdateProjectShare :exec
UPDATE project_share SET role = @role, can_merge_pr = @can_merge_pr WHERE id = @id;

-- name: DeleteProjectShare :exec
DELETE FROM project_share WHERE project_id = @project_id AND owner_id = @owner_id;
