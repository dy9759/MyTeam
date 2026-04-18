-- Plan 5: participant_slot — human/agent collaboration slots within a Task.

CREATE TABLE IF NOT EXISTS participant_slot (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id UUID NOT NULL REFERENCES task(id) ON DELETE CASCADE,
  slot_type TEXT NOT NULL
    CHECK (slot_type IN ('human_input','agent_execution','human_review')),
  slot_order INTEGER NOT NULL DEFAULT 0,
  participant_id UUID,
  participant_type TEXT
    CHECK (participant_type IS NULL OR participant_type IN ('member','agent')),
  responsibility TEXT,
  trigger TEXT NOT NULL DEFAULT 'during_execution'
    CHECK (trigger IN ('before_execution','during_execution','before_done')),
  blocking BOOLEAN NOT NULL DEFAULT TRUE,
  required BOOLEAN NOT NULL DEFAULT TRUE,
  expected_output TEXT,
  status TEXT NOT NULL DEFAULT 'waiting'
    CHECK (status IN ('waiting','ready','in_progress','submitted','approved','revision_requested','rejected','expired','skipped')),
  timeout_seconds INTEGER,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_slot_task ON participant_slot(task_id, slot_order);
CREATE INDEX IF NOT EXISTS idx_slot_status ON participant_slot(status, task_id);
