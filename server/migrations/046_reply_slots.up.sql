CREATE TABLE IF NOT EXISTS reply_slot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id UUID NOT NULL REFERENCES message(id) ON DELETE CASCADE,
    channel_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    slot_index INTEGER NOT NULL DEFAULT 0,
    content_summary TEXT,
    assigned_agent_id UUID REFERENCES agent(id),
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '30 seconds',
    replied_at TIMESTAMPTZ,
    reply_message_id UUID REFERENCES message(id)
);

CREATE INDEX IF NOT EXISTS idx_reply_slot_message ON reply_slot(message_id);
CREATE INDEX IF NOT EXISTS idx_reply_slot_status ON reply_slot(workspace_id, status) WHERE status = 'pending';
