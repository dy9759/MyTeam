CREATE TABLE remote_session (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL REFERENCES agent(id),
    workspace_id UUID NOT NULL REFERENCES workspace(id),
    owner_id UUID NOT NULL REFERENCES "user"(id),
    status TEXT NOT NULL DEFAULT 'created',
    title TEXT,
    environment JSONB,
    events JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_remote_session_agent ON remote_session(agent_id);
CREATE INDEX idx_remote_session_workspace ON remote_session(workspace_id);
