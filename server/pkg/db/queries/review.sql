-- name: CreateReview :one
INSERT INTO review (
    task_id, artifact_id, slot_id,
    reviewer_id, reviewer_type, decision, comment
) VALUES (
    @task_id, @artifact_id, sqlc.narg('slot_id'),
    @reviewer_id, @reviewer_type, @decision, sqlc.narg('comment')
)
RETURNING *;

-- name: GetReview :one
SELECT * FROM review WHERE id = $1;

-- name: ListReviewsForArtifact :many
SELECT * FROM review WHERE artifact_id = @artifact_id ORDER BY created_at DESC;

-- name: GetLatestReviewForArtifact :one
SELECT * FROM review WHERE artifact_id = @artifact_id ORDER BY created_at DESC LIMIT 1;
