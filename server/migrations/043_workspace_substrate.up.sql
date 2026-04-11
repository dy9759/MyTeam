ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS server_host TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS working_dir TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS capabilities JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS last_heartbeat TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS readiness TEXT NOT NULL DEFAULT 'unknown';

UPDATE agent_runtime
SET last_heartbeat = COALESCE(last_heartbeat, last_seen_at)
WHERE last_heartbeat IS NULL;

CREATE TABLE IF NOT EXISTS workspace_collaborator (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'editor',
    added_by TEXT,
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, email)
);

CREATE INDEX IF NOT EXISTS idx_workspace_collaborator_workspace
    ON workspace_collaborator(workspace_id);

CREATE TABLE IF NOT EXISTS browser_context (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    domain TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    created_by TEXT NOT NULL,
    shared_with JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, name)
);

CREATE INDEX IF NOT EXISTS idx_browser_context_workspace
    ON browser_context(workspace_id, status);

CREATE TABLE IF NOT EXISTS browser_tab (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    url TEXT NOT NULL DEFAULT 'about:blank',
    title TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    created_by TEXT NOT NULL,
    shared_with JSONB NOT NULL DEFAULT '[]',
    context_id UUID REFERENCES browser_context(id) ON DELETE SET NULL,
    session_id TEXT,
    live_url TEXT,
    screenshot_url TEXT,
    conversation_id UUID REFERENCES channel(id) ON DELETE SET NULL,
    project_id UUID REFERENCES project(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_browser_tab_workspace
    ON browser_tab(workspace_id, status);

CREATE TABLE IF NOT EXISTS browser_usage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    tab_id UUID NOT NULL REFERENCES browser_tab(id) ON DELETE CASCADE,
    session_id TEXT,
    opened_by TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    duration_seconds INTEGER
);

CREATE INDEX IF NOT EXISTS idx_browser_usage_workspace
    ON browser_usage(workspace_id, started_at DESC);

ALTER TABLE project_version
    ADD COLUMN IF NOT EXISTS context_imports JSONB NOT NULL DEFAULT '[]';
