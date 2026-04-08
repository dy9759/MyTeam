-- name: CreateFileSnapshot :one
INSERT INTO file_snapshot (file_id, snapshot_at, storage_path, referenced_by)
VALUES (@file_id, NOW(), @storage_path, @referenced_by)
RETURNING *;

-- name: GetFileSnapshot :one
SELECT * FROM file_snapshot WHERE id = @id;

-- name: ListFileSnapshotsByFile :many
SELECT * FROM file_snapshot WHERE file_id = @file_id ORDER BY snapshot_at DESC;
