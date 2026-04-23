CREATE TABLE project_pr (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  source_branch_id UUID NOT NULL REFERENCES project_branch(id),
  target_branch_id UUID NOT NULL REFERENCES project_branch(id),
  source_version_id UUID NOT NULL REFERENCES project_version(id),
  title TEXT NOT NULL,
  description TEXT,
  status TEXT NOT NULL DEFAULT 'open',
  has_conflicts BOOLEAN NOT NULL DEFAULT FALSE,
  merged_version_id UUID REFERENCES project_version(id),
  created_by UUID NOT NULL,
  merged_by UUID,
  merged_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_project_pr_project ON project_pr(project_id);
CREATE INDEX idx_project_pr_status ON project_pr(project_id, status);
