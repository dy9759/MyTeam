-- name: CreateAttachment :one
INSERT INTO attachment (workspace_id, issue_id, comment_id, uploader_type, uploader_id, filename, url, content_type, size_bytes, object_key)
VALUES ($1, sqlc.narg(issue_id), sqlc.narg(comment_id), $2, $3, $4, $5, $6, $7, sqlc.narg(object_key))
RETURNING *;

-- name: ListAttachmentsByIssue :many
SELECT * FROM attachment
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListAttachmentsByComment :many
SELECT * FROM attachment
WHERE comment_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: GetAttachment :one
SELECT * FROM attachment
WHERE id = $1 AND workspace_id = $2;

-- name: ListAttachmentsByCommentIDs :many
SELECT * FROM attachment
WHERE comment_id = ANY($1::uuid[])
ORDER BY created_at ASC;

-- name: ListAttachmentURLsByIssueOrComments :many
SELECT a.url FROM attachment a
WHERE a.issue_id = $1
   OR a.comment_id IN (SELECT c.id FROM comment c WHERE c.issue_id = $1);

-- name: ListAttachmentURLsByCommentID :many
SELECT url FROM attachment
WHERE comment_id = $1;

-- name: LinkAttachmentsToComment :exec
UPDATE attachment
SET comment_id = $1
WHERE issue_id = $2
  AND comment_id IS NULL
  AND id = ANY($3::uuid[]);

-- name: DeleteAttachment :exec
DELETE FROM attachment WHERE id = $1 AND workspace_id = $2;

-- name: GetFileVersions :many
SELECT * FROM attachment WHERE parent_file_id = $1 OR id = $1 ORDER BY version ASC;

-- name: CreateFileVersion :one
INSERT INTO attachment (workspace_id, issue_id, comment_id, filename, content_type, size_bytes, url, uploader_type, uploader_id, version, parent_file_id, object_key)
VALUES ($1, sqlc.narg(issue_id), sqlc.narg(comment_id), $2, $3, $4, $5, $6, $7, $8, $9, sqlc.narg(object_key))
RETURNING *;

-- name: GetLatestFileVersion :one
SELECT * FROM attachment WHERE (id = $1 OR parent_file_id = $1) ORDER BY version DESC LIMIT 1;
