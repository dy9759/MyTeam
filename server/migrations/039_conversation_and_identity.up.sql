-- Extend agent table with identity and status fields
ALTER TABLE agent ADD COLUMN IF NOT EXISTS agent_type TEXT NOT NULL DEFAULT 'personal_agent';
ALTER TABLE agent ADD COLUMN IF NOT EXISTS online_status TEXT NOT NULL DEFAULT 'offline';
ALTER TABLE agent ADD COLUMN IF NOT EXISTS workload_status TEXT NOT NULL DEFAULT 'idle';
ALTER TABLE agent ADD COLUMN IF NOT EXISTS identity_card JSONB DEFAULT '{}';
ALTER TABLE agent ADD COLUMN IF NOT EXISTS accessible_files_scope JSONB DEFAULT '[]';
ALTER TABLE agent ADD COLUMN IF NOT EXISTS allowed_channels_scope JSONB DEFAULT '[]';
ALTER TABLE agent ADD COLUMN IF NOT EXISTS last_active_at TIMESTAMPTZ;

-- Thread table (thread ID = root message ID)
CREATE TABLE IF NOT EXISTS thread (
    id UUID PRIMARY KEY,
    channel_id UUID NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    title TEXT,
    reply_count INTEGER NOT NULL DEFAULT 0,
    last_reply_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_thread_channel ON thread(channel_id);

-- Extend channel table for conversation unification
ALTER TABLE channel ADD COLUMN IF NOT EXISTS conversation_type TEXT NOT NULL DEFAULT 'channel';
ALTER TABLE channel ADD COLUMN IF NOT EXISTS parent_conversation_id UUID REFERENCES channel(id);
ALTER TABLE channel ADD COLUMN IF NOT EXISTS invite_code TEXT;
ALTER TABLE channel ADD COLUMN IF NOT EXISTS reply_policy JSONB DEFAULT '{}';
ALTER TABLE channel ADD COLUMN IF NOT EXISTS auto_assignment_policy JSONB DEFAULT '{}';
ALTER TABLE channel ADD COLUMN IF NOT EXISTS project_id UUID;
ALTER TABLE channel ADD COLUMN IF NOT EXISTS linked_project_ids UUID[] DEFAULT '{}';

-- Extend message table with thread support
ALTER TABLE message ADD COLUMN IF NOT EXISTS thread_id UUID REFERENCES thread(id);

-- Note: is_impersonated already exists on message (migration 034)

-- Indexes
CREATE INDEX IF NOT EXISTS idx_agent_type ON agent(workspace_id, agent_type);
CREATE INDEX IF NOT EXISTS idx_agent_online ON agent(workspace_id, online_status);
CREATE INDEX IF NOT EXISTS idx_channel_conversation_type ON channel(workspace_id, conversation_type);
CREATE INDEX IF NOT EXISTS idx_channel_project ON channel(project_id);
CREATE INDEX IF NOT EXISTS idx_message_thread ON message(thread_id);
CREATE INDEX IF NOT EXISTS idx_channel_invite_code ON channel(invite_code) WHERE invite_code IS NOT NULL;
