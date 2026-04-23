-- name: CreateProjectContext :one
INSERT INTO project_context (project_id, version_id, source_type, source_id, source_name, message_range_start, message_range_end, snapshot_md, message_count, imported_by)
VALUES (@project_id, @version_id, @source_type, @source_id, @source_name, @message_range_start, @message_range_end, @snapshot_md, @message_count, @imported_by)
RETURNING *;

-- name: ListProjectContexts :many
SELECT * FROM project_context WHERE project_id = @project_id ORDER BY imported_at DESC;

-- name: GetProjectContext :one
SELECT * FROM project_context WHERE id = @id;

-- name: DeleteProjectContext :exec
DELETE FROM project_context WHERE id = @id;
