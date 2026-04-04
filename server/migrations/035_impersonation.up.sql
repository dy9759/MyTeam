CREATE TABLE impersonation_session (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL REFERENCES "user"(id),
    agent_id UUID NOT NULL REFERENCES agent(id),
    workspace_id UUID NOT NULL REFERENCES workspace(id),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '30 minutes',
    ended_at TIMESTAMPTZ
);

CREATE INDEX idx_impersonation_active ON impersonation_session(agent_id) WHERE ended_at IS NULL;
