CREATE TABLE project_share (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  owner_id UUID NOT NULL,
  role TEXT NOT NULL DEFAULT 'viewer',
  can_merge_pr BOOLEAN NOT NULL DEFAULT FALSE,
  granted_by UUID NOT NULL,
  granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(project_id, owner_id)
);

CREATE INDEX idx_project_share_project ON project_share(project_id);
CREATE INDEX idx_project_share_owner ON project_share(owner_id);
