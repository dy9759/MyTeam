CREATE TABLE project_context (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  version_id UUID REFERENCES project_version(id),
  source_type TEXT NOT NULL,
  source_id UUID NOT NULL,
  source_name TEXT,
  message_range_start TIMESTAMPTZ,
  message_range_end TIMESTAMPTZ,
  snapshot_md TEXT NOT NULL,
  message_count INTEGER NOT NULL DEFAULT 0,
  imported_by UUID NOT NULL,
  imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_project_context_project ON project_context(project_id);

ALTER TABLE plan ALTER COLUMN task_brief TYPE JSONB USING
  CASE
    WHEN task_brief IS NULL THEN NULL
    WHEN task_brief::text = '' THEN NULL
    ELSE jsonb_build_object('goal', task_brief::text)
  END;

ALTER TABLE plan ALTER COLUMN task_brief SET DEFAULT '{}';
