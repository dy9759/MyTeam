-- name: CreateMemoryRecord :one
INSERT INTO memory_record (
    workspace_id, type, scope, source,
    raw_kind, raw_id,
    summary, body, tags, entities,
    confidence, status, version, created_by
) VALUES (
    @workspace_id, @type, @scope, @source,
    @raw_kind, @raw_id,
    sqlc.narg('summary'), sqlc.narg('body'),
    @tags, @entities,
    @confidence, @status, @version,
    sqlc.narg('created_by')
)
RETURNING *;

-- name: GetMemoryRecord :one
SELECT * FROM memory_record WHERE id = $1;

-- name: ListMemoryRecordsByWorkspace :many
SELECT * FROM memory_record
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.narg('type_filter')::text IS NULL OR type = sqlc.narg('type_filter')::text)
  AND (sqlc.narg('scope_filter')::text IS NULL OR scope = sqlc.narg('scope_filter')::text)
  AND (sqlc.narg('status_filter')::text IS NULL OR status = sqlc.narg('status_filter')::text)
ORDER BY updated_at DESC
LIMIT sqlc.arg('limit_count')::int
OFFSET sqlc.arg('offset_count')::int;

-- name: ListMemoryRecordsByRaw :many
SELECT * FROM memory_record
WHERE raw_kind = @raw_kind AND raw_id = @raw_id
ORDER BY updated_at DESC;

-- name: PromoteMemoryRecord :one
UPDATE memory_record
SET status = 'confirmed',
    version = version + 1,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveMemoryRecord :one
UPDATE memory_record
SET status = 'archived',
    version = version + 1,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateMemoryRecordSummary :exec
UPDATE memory_record
SET summary = sqlc.narg('summary'),
    body = sqlc.narg('body'),
    tags = @tags,
    entities = @entities,
    confidence = @confidence,
    version = version + 1,
    updated_at = now()
WHERE id = $1;

-- name: DeleteMemoryRecord :exec
DELETE FROM memory_record WHERE id = $1;
