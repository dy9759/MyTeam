-- Agent ↔ agent messaging layer, additive to the existing task engine.
-- Design ported from AgentmeshHub's unified `interaction` protocol:
-- one table + one endpoint covers DM / broadcast / schema-typed events
-- between agents, without bloating `task` with message-shaped rows.
--
-- The column set mirrors AgentMesh's `interactions` table 1:1 so the
-- same client protocol ({fromId, fromType, target.*, payload, metadata})
-- maps directly onto sqlc rows. Targets are mutually exclusive:
-- - to_agent_id     : direct message to one agent
-- - channel         : fan-out to a named channel (not yet wired; reserved)
-- - capability      : broadcast to every agent advertising the capability
-- - session_id      : linked to an existing collaboration session
--
-- Indexes are built around the two hot read paths:
-- 1. Inbox:     `WHERE to_agent_id = $1 AND created_at > $2 ORDER BY created_at`
-- 2. Sent mail: `WHERE from_id = $1 ORDER BY created_at DESC`

CREATE TABLE agent_interaction (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID REFERENCES workspace(id) ON DELETE CASCADE,

    -- Sender identity — `from_type` distinguishes agent vs user (who can
    -- send as themselves via attach / 附身).
    from_id       UUID NOT NULL,
    from_type     TEXT NOT NULL CHECK (from_type IN ('agent', 'user')),

    -- Target — exactly one of {to_agent_id, channel, capability, session_id}
    -- is set. Enforced at the handler layer rather than a CHECK so callers
    -- get a readable 400 rather than a constraint error.
    to_agent_id   UUID REFERENCES agent(id) ON DELETE SET NULL,
    channel       TEXT,
    capability    TEXT,
    session_id    UUID,

    -- Protocol fields mirrored from AgentMesh.
    type          TEXT NOT NULL
        CHECK (type IN ('message', 'task', 'query', 'event', 'broadcast')),
    content_type  TEXT NOT NULL DEFAULT 'text'
        CHECK (content_type IN ('text', 'json', 'file')),
    schema        TEXT,
    payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Delivery bookkeeping. `status` starts 'pending' and flips to
    -- 'delivered' once the WS push confirms or the inbox GET acks it.
    status        TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'delivered', 'read', 'failed')),
    delivered_at  TIMESTAMPTZ,
    read_at       TIMESTAMPTZ,

    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_interaction_to_agent_created
    ON agent_interaction (to_agent_id, created_at DESC)
    WHERE to_agent_id IS NOT NULL;

CREATE INDEX idx_agent_interaction_from
    ON agent_interaction (from_id, created_at DESC);

CREATE INDEX idx_agent_interaction_capability
    ON agent_interaction (capability)
    WHERE capability IS NOT NULL;

CREATE INDEX idx_agent_interaction_workspace_created
    ON agent_interaction (workspace_id, created_at DESC);

CREATE INDEX idx_agent_interaction_session
    ON agent_interaction (session_id)
    WHERE session_id IS NOT NULL;
