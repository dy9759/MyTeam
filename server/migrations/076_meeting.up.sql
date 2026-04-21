-- Channel-scoped meeting records. A meeting pins a recording session
-- (audio + transcript + notes + highlights + AI summary) to one
-- channel so teams can kick off from any room. Inspired by
-- MyIsland's MeetingRecord model but reduced to the fields the Web
-- UI actually consumes — pure audit data is delegated to
-- message/activity_log via metadata references.
--
-- Lifecycle: `recording` → `processing` → (`completed` | `failed`).
--   - recording : UI is actively capturing audio; audio_url empty
--   - processing: audio uploaded, Doubao transcription in flight
--   - completed : transcript + summary persisted
--   - failed    : Doubao/transcribe error; surface `failure_reason`

CREATE TABLE meeting (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id      UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    started_by      UUID NOT NULL REFERENCES "user"(id),

    topic           TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'recording'
        CHECK (status IN ('recording', 'processing', 'completed', 'failed')),

    -- Audio bookkeeping. audio_url points at the public storage URL
    -- (Volcengine TOS in production) so Doubao can fetch it. task_id
    -- is the job handle returned by Doubao's auc/lark/submit.
    audio_url       TEXT,
    audio_duration  INTEGER,        -- seconds
    task_id         TEXT,

    -- Doubao memo result payloads — persisted as JSONB so the app
    -- can render/extract fields lazily without another migration
    -- when upstream shapes evolve. transcript carries the speaker-
    -- segmented utterances; summary carries chapters + summary +
    -- extracted todos/qa.
    transcript      JSONB,
    summary         JSONB,

    -- In-app annotation — user takes notes live during the meeting
    -- and marks highlights on top of the transcript. Both are used
    -- by the summary renderer to bold the user-curated bits.
    notes           TEXT NOT NULL DEFAULT '',
    highlights      JSONB NOT NULL DEFAULT '[]'::jsonb,

    failure_reason  TEXT,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at        TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_meeting_channel_started
    ON meeting (channel_id, started_at DESC);
CREATE INDEX idx_meeting_workspace
    ON meeting (workspace_id, started_at DESC);
CREATE INDEX idx_meeting_status
    ON meeting (status)
    WHERE status IN ('recording', 'processing');
