-- server/migrations/045_project_branch_result.up.sql

-- 1. project_branch: first-class branch entity
CREATE TABLE project_branch (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  parent_branch_id UUID REFERENCES project_branch(id),
  is_default BOOLEAN NOT NULL DEFAULT FALSE,
  status TEXT NOT NULL DEFAULT 'active',
  created_by UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(project_id, name)
);

CREATE INDEX idx_project_branch_project ON project_branch(project_id);

-- 2. project_result: run output/artifacts
CREATE TABLE project_result (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES project_run(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  version_id UUID REFERENCES project_version(id),
  summary TEXT,
  artifacts JSONB DEFAULT '[]',
  deliverables JSONB DEFAULT '[]',
  acceptance_status TEXT NOT NULL DEFAULT 'pending',
  accepted_by UUID,
  accepted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_project_result_run ON project_result(run_id);
CREATE INDEX idx_project_result_project ON project_result(project_id);

-- 3. Extend project table
ALTER TABLE project ADD COLUMN IF NOT EXISTS default_branch_id UUID REFERENCES project_branch(id);
ALTER TABLE project ADD COLUMN IF NOT EXISTS max_runs INTEGER;
ALTER TABLE project ADD COLUMN IF NOT EXISTS end_time TIMESTAMPTZ;
ALTER TABLE project ADD COLUMN IF NOT EXISTS consecutive_failure_threshold INTEGER DEFAULT 3;
ALTER TABLE project ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ;
ALTER TABLE project ADD COLUMN IF NOT EXISTS plan_visibility TEXT NOT NULL DEFAULT 'owner_only';

-- 4. Extend project_version with branch_id
ALTER TABLE project_version ADD COLUMN IF NOT EXISTS branch_id UUID REFERENCES project_branch(id);

-- 5. Extend workflow_step with sub-task fields
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS title TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS goal TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS priority TEXT DEFAULT 'medium';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS candidate_agent_ids UUID[] DEFAULT '{}';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS owner_reviewer_id UUID;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS context_md_path TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS instruction_md_path TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS worktree_path TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS expected_outputs JSONB DEFAULT '[]';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS actual_outputs JSONB DEFAULT '[]';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS skippable BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS acceptance_checks JSONB DEFAULT '[]';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS done_definition TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS error_code TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS error_summary TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS on_failure TEXT DEFAULT 'block';
