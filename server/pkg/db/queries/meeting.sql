-- Meeting queries — channel-scoped recording/transcription lifecycle
-- added by migration 076.

-- name: CreateMeeting :one
INSERT INTO meeting (channel_id, workspace_id, started_by, topic)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetMeeting :one
SELECT * FROM meeting WHERE id = $1;

-- name: ListMeetingsByChannel :many
SELECT * FROM meeting
WHERE channel_id = $1
ORDER BY started_at DESC
LIMIT $2;

-- name: UpdateMeetingRecording :one
-- Called once the browser finishes recording + uploads the audio
-- file. Flips status from `recording` → `processing` so the
-- background transcriber picks it up.
UPDATE meeting SET
    audio_url       = $2,
    audio_duration  = $3,
    status          = 'processing',
    updated_at      = now(),
    ended_at        = now()
WHERE id = $1
RETURNING *;

-- name: UpdateMeetingTaskID :exec
UPDATE meeting SET task_id = $2, updated_at = now() WHERE id = $1;

-- name: UpdateMeetingResult :one
-- Terminal write from the transcription poller. Dumps whatever the
-- Doubao memo API returned into transcript + summary JSONB and
-- flips status to completed. Called with status='failed' +
-- failure_reason when the job errors out.
UPDATE meeting SET
    status         = $2,
    transcript     = sqlc.narg('transcript')::jsonb,
    summary        = sqlc.narg('summary')::jsonb,
    failure_reason = sqlc.narg('failure_reason'),
    updated_at     = now()
WHERE id = $1
RETURNING *;

-- name: UpdateMeetingNotes :one
UPDATE meeting SET
    notes      = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateMeetingHighlights :one
UPDATE meeting SET
    highlights = @highlights::jsonb,
    updated_at = now()
WHERE id = @id
RETURNING *;
