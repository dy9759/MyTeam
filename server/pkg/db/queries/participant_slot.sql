-- name: CreateParticipantSlot :one
INSERT INTO participant_slot (
    task_id, slot_type, slot_order,
    participant_id, participant_type,
    responsibility, trigger, blocking, required, expected_output,
    timeout_seconds
) VALUES (
    @task_id, @slot_type, COALESCE(sqlc.narg('slot_order')::int, 0),
    sqlc.narg('participant_id'), sqlc.narg('participant_type'),
    sqlc.narg('responsibility'), COALESCE(sqlc.narg('trigger')::text, 'during_execution'),
    COALESCE(sqlc.narg('blocking')::boolean, TRUE), COALESCE(sqlc.narg('required')::boolean, TRUE),
    sqlc.narg('expected_output'),
    sqlc.narg('timeout_seconds')
)
RETURNING *;

-- name: GetSlot :one
SELECT * FROM participant_slot WHERE id = $1;

-- name: ListSlotsByTask :many
SELECT * FROM participant_slot WHERE task_id = @task_id ORDER BY slot_order ASC, created_at ASC;

-- name: UpdateSlotStatus :one
UPDATE participant_slot SET
    status = @status,
    started_at = CASE WHEN @status = 'in_progress' AND started_at IS NULL THEN now() ELSE started_at END,
    completed_at = CASE WHEN @status IN ('approved','revision_requested','rejected','expired','skipped','submitted') AND completed_at IS NULL THEN now() ELSE completed_at END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ResetSlotsForNewRun :exec
UPDATE participant_slot SET
    status = 'waiting',
    started_at = NULL,
    completed_at = NULL,
    updated_at = now()
WHERE task_id = ANY(@task_ids::uuid[]);
