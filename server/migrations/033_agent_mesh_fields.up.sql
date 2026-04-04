-- Add AgentMesh communication fields to agents
ALTER TABLE agent ADD COLUMN capabilities TEXT[] DEFAULT '{}';
ALTER TABLE agent ADD COLUMN auto_reply_enabled BOOLEAN DEFAULT FALSE;
ALTER TABLE agent ADD COLUMN auto_reply_config JSONB;
ALTER TABLE agent ADD COLUMN display_name TEXT;
ALTER TABLE agent ADD COLUMN avatar TEXT;
ALTER TABLE agent ADD COLUMN bio TEXT;
ALTER TABLE agent ADD COLUMN tags TEXT[] DEFAULT '{}';
ALTER TABLE agent ADD COLUMN agent_metadata JSONB;

-- Add channel mention trigger
ALTER TABLE agent ADD COLUMN trigger_on_channel_mention BOOLEAN DEFAULT TRUE;
