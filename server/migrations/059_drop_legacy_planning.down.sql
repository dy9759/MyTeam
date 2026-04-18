-- Best-effort restore (data permanently lost).
ALTER TABLE project_run ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE project_version
  ADD COLUMN IF NOT EXISTS plan_snapshot JSONB,
  ADD COLUMN IF NOT EXISTS workflow_snapshot JSONB;
ALTER TABLE plan ADD COLUMN IF NOT EXISTS steps JSONB;

-- Recreate workflow shells (empty)
CREATE TABLE IF NOT EXISTS workflow (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL,
  plan_id UUID,
  status TEXT,
  dag JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS workflow_step (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workflow_id UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
  step_order INTEGER NOT NULL,
  title TEXT,
  status TEXT,
  result JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
