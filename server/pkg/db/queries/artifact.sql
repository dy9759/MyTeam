-- name: CreateArtifact :one
INSERT INTO artifact (
    task_id, slot_id, execution_id, run_id,
    artifact_type, version, title, summary, content,
    file_index_id, file_snapshot_id, retention_class,
    created_by_id, created_by_type
) VALUES (
    @task_id, sqlc.narg('slot_id'), sqlc.narg('execution_id'), @run_id,
    @artifact_type, @version, sqlc.narg('title'), sqlc.narg('summary'),
    sqlc.narg('content'),
    sqlc.narg('file_index_id'), sqlc.narg('file_snapshot_id'),
    COALESCE(sqlc.narg('retention_class')::text, 'permanent'),
    sqlc.narg('created_by_id'), sqlc.narg('created_by_type')
)
RETURNING *;

-- name: GetArtifact :one
SELECT * FROM artifact WHERE id = $1;

-- name: ListArtifactsByTask :many
SELECT * FROM artifact WHERE task_id = @task_id ORDER BY version DESC, created_at DESC;

-- name: GetLatestArtifactForTask :one
SELECT * FROM artifact WHERE task_id = @task_id ORDER BY version DESC LIMIT 1;

-- name: NextArtifactVersion :one
SELECT COALESCE(MAX(version), 0) + 1 FROM artifact WHERE task_id = @task_id;
