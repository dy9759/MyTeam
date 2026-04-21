-- Archive state for conversations.
--
-- Channels archive globally via a nullable timestamp — archiving is
-- workspace-wide and affects all members. DMs archive per-user because
-- each participant manages their own inbox (one party archiving a chat
-- must not hide it for the other). dm_conversation_state holds the
-- per-user view: keyed by (user_id, peer_id, peer_type), with
-- archived_at nullable to leave room for future per-user flags (mute,
-- pin) without another migration.

ALTER TABLE channel
    ADD COLUMN archived_at TIMESTAMPTZ;

CREATE INDEX idx_channel_active
    ON channel (workspace_id)
    WHERE archived_at IS NULL;

CREATE TABLE dm_conversation_state (
    user_id UUID NOT NULL,
    peer_id UUID NOT NULL,
    peer_type TEXT NOT NULL CHECK (peer_type IN ('member', 'agent')),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    archived_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, peer_id, peer_type)
);

CREATE INDEX idx_dm_state_user_workspace
    ON dm_conversation_state (user_id, workspace_id);
