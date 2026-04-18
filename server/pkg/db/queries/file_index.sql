-- name: GetFileIndex :one
SELECT * FROM file_index WHERE id = @id;

-- name: CreateFileIndex :one
INSERT INTO file_index (workspace_id, uploader_identity_id, uploader_identity_type, owner_id, source_type, source_id, file_name, file_size, content_type, storage_path, access_scope, channel_id, project_id)
VALUES (@workspace_id, @uploader_identity_id, @uploader_identity_type, @owner_id, @source_type, @source_id, @file_name, @file_size, @content_type, @storage_path, @access_scope, @channel_id, @project_id)
RETURNING *;

-- name: ListFilesByWorkspace :many
SELECT * FROM file_index WHERE workspace_id = @workspace_id ORDER BY created_at DESC LIMIT @limit_val OFFSET @offset_val;

-- name: ListFilesByOwner :many
SELECT * FROM file_index WHERE workspace_id = @workspace_id AND owner_id = @owner_id ORDER BY created_at DESC;

-- name: ListFilesByOwnerAndAgents :many
SELECT * FROM file_index WHERE workspace_id = @workspace_id AND owner_id = ANY(@owner_ids::uuid[]) ORDER BY created_at DESC;

-- name: ListFilesByProject :many
SELECT * FROM file_index WHERE project_id = @project_id ORDER BY created_at DESC;

-- name: ListFilesByChannel :many
SELECT * FROM file_index WHERE channel_id = @channel_id ORDER BY created_at DESC;

-- name: ListFilesBySource :many
SELECT * FROM file_index WHERE source_type = @source_type AND source_id = @source_id ORDER BY created_at DESC;

-- name: DeleteFileIndex :exec
DELETE FROM file_index WHERE id = @id;
