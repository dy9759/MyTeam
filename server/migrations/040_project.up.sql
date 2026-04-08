-- Project table (four-layer: Project → Version → Plan → Run)
CREATE TABLE IF NOT EXISTS project (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'not_started',
    schedule_type TEXT NOT NULL DEFAULT 'one_time',
    cron_expr TEXT,
    source_conversations JSONB DEFAULT '[]',
    channel_id UUID REFERENCES channel(id),
    creator_owner_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_project_workspace ON project(workspace_id);
CREATE INDEX idx_project_status ON project(workspace_id, status);
CREATE INDEX idx_project_creator ON project(creator_owner_id);

-- Project version table (branching/forking support)
CREATE TABLE IF NOT EXISTS project_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    parent_version_id UUID REFERENCES project_version(id),
    version_number INTEGER NOT NULL,
    branch_name TEXT,
    fork_reason TEXT,
    plan_snapshot JSONB,
    workflow_snapshot JSONB,
    version_status TEXT NOT NULL DEFAULT 'active',
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_project_version_project ON project_version(project_id);

-- Project run table (execution instances)
CREATE TABLE IF NOT EXISTS project_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID REFERENCES plan(id),
    project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending',
    start_at TIMESTAMPTZ,
    end_at TIMESTAMPTZ,
    step_logs JSONB DEFAULT '[]',
    output_refs JSONB DEFAULT '[]',
    failure_reason TEXT,
    retry_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_project_run_project ON project_run(project_id);
CREATE INDEX idx_project_run_status ON project_run(status);

-- Extend plan table with project relationship and approval fields
ALTER TABLE plan ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES project(id);
ALTER TABLE plan ADD COLUMN IF NOT EXISTS version_id UUID REFERENCES project_version(id);
ALTER TABLE plan ADD COLUMN IF NOT EXISTS task_brief TEXT;
ALTER TABLE plan ADD COLUMN IF NOT EXISTS assigned_agents JSONB DEFAULT '[]';
ALTER TABLE plan ADD COLUMN IF NOT EXISTS risk_points TEXT;
ALTER TABLE plan ADD COLUMN IF NOT EXISTS approval_status TEXT NOT NULL DEFAULT 'draft';
ALTER TABLE plan ADD COLUMN IF NOT EXISTS approved_by UUID;
ALTER TABLE plan ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ;

-- Add FK constraint for channel.project_id (column added in migration 039, FK deferred to here)
-- Note: channel.project_id column already exists from migration 039, but without FK constraint
-- We add the FK now that the project table exists
ALTER TABLE channel ADD CONSTRAINT fk_channel_project FOREIGN KEY (project_id) REFERENCES project(id);
