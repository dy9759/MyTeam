-- name: CreateParticipantSlotSubmission :one
INSERT INTO participant_slot_submission (
    slot_id,
    task_id,
    run_id,
    submitted_by,
    content,
    comment
)
VALUES (
    @slot_id,
    @task_id,
    sqlc.narg('run_id'),
    sqlc.narg('submitted_by'),
    @content,
    sqlc.narg('comment')
)
RETURNING *;

-- name: ListParticipantSlotSubmissions :many
SELECT * FROM participant_slot_submission
WHERE slot_id = @slot_id
ORDER BY created_at DESC, id DESC;
