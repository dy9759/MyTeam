-- Plan 5: artifact (versioned task output) + review (verdict on artifact)
-- + plan.thread_id FK + cleanup of legacy plan/version/run JSONBs

CREATE TABLE IF NOT EXISTS artifact (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id UUID NOT NULL REFERENCES task(id) ON DELETE CASCADE,
  slot_id UUID REFERENCES participant_slot(id) ON DELETE SET NULL,
  execution_id UUID REFERENCES execution(id) ON DELETE SET NULL,
  run_id UUID NOT NULL REFERENCES project_run(id) ON DELETE CASCADE,
  artifact_type TEXT NOT NULL DEFAULT 'document'
    CHECK (artifact_type IN ('document','design','code_patch','report','file','plan_doc')),
  version INTEGER NOT NULL DEFAULT 1,
  title TEXT,
  summary TEXT,
  content JSONB,
  file_index_id UUID REFERENCES file_index(id) ON DELETE SET NULL,
  file_snapshot_id UUID REFERENCES file_snapshot(id) ON DELETE SET NULL,
  retention_class TEXT NOT NULL DEFAULT 'permanent'
    CHECK (retention_class IN ('permanent','ttl','temp')),
  created_by_id UUID,
  created_by_type TEXT
    CHECK (created_by_type IS NULL OR created_by_type IN ('member','agent')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  -- Headless rule: if no FileIndex, content must be present.
  CHECK (file_index_id IS NOT NULL OR content IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_artifact_task_version ON artifact(task_id, version DESC);
CREATE INDEX IF NOT EXISTS idx_artifact_run ON artifact(run_id);

CREATE TABLE IF NOT EXISTS review (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id UUID NOT NULL REFERENCES task(id) ON DELETE CASCADE,
  artifact_id UUID NOT NULL REFERENCES artifact(id) ON DELETE CASCADE,
  slot_id UUID REFERENCES participant_slot(id) ON DELETE SET NULL,
  reviewer_id UUID,
  reviewer_type TEXT
    CHECK (reviewer_type IS NULL OR reviewer_type IN ('member','agent')),
  decision TEXT NOT NULL
    CHECK (decision IN ('approve','request_changes','reject')),
  comment TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_review_artifact ON review(artifact_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_review_task ON review(task_id);

-- plan.thread_id FK — Plan 3 left thread.id independent (gen_random_uuid).
ALTER TABLE plan
  ADD COLUMN IF NOT EXISTS thread_id UUID REFERENCES thread(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_plan_thread ON plan(thread_id) WHERE thread_id IS NOT NULL;
