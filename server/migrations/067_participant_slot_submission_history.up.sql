CREATE TABLE participant_slot_submission (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slot_id UUID NOT NULL REFERENCES participant_slot(id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES task(id) ON DELETE CASCADE,
    run_id UUID REFERENCES project_run(id) ON DELETE SET NULL,
    submitted_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    content JSONB NOT NULL,
    comment TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX participant_slot_submission_slot_created_idx
    ON participant_slot_submission (slot_id, created_at DESC);

CREATE INDEX participant_slot_submission_run_idx
    ON participant_slot_submission (run_id);
