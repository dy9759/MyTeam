ALTER TABLE agent ADD COLUMN IF NOT EXISTS needs_attention BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE agent ADD COLUMN IF NOT EXISTS needs_attention_reason TEXT;

-- Plan approval fields
ALTER TABLE plan ADD COLUMN IF NOT EXISTS approval_status TEXT NOT NULL DEFAULT 'pending' CHECK (approval_status IN ('pending', 'approved', 'rejected'));
ALTER TABLE plan ADD COLUMN IF NOT EXISTS approved_by UUID REFERENCES "user"(id);
ALTER TABLE plan ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ;
ALTER TABLE plan ADD COLUMN IF NOT EXISTS project_id UUID;

-- Project table for chat-to-project workflow
CREATE TABLE IF NOT EXISTS project (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'completed', 'archived')),
    created_by UUID NOT NULL REFERENCES "user"(id),
    plan_id UUID REFERENCES plan(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS project_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    version INT NOT NULL DEFAULT 1,
    title TEXT NOT NULL,
    description TEXT,
    plan_snapshot JSONB,
    created_by UUID NOT NULL REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE plan ADD CONSTRAINT fk_plan_project FOREIGN KEY (project_id) REFERENCES project(id) ON DELETE SET NULL;
