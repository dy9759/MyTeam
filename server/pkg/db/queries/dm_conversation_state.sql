-- name: UpsertDMArchiveState :exec
-- Sets (or updates) a user's per-peer archived_at. Pass NULL to unarchive.
INSERT INTO dm_conversation_state (user_id, peer_id, peer_type, workspace_id, archived_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, peer_id, peer_type)
DO UPDATE SET archived_at = EXCLUDED.archived_at, updated_at = NOW();

-- name: ListDMArchivedPeers :many
-- Returns (peer_id, peer_type) pairs the user has archived in this workspace.
-- Frontend uses this to split conversations into active/archived sections.
SELECT peer_id, peer_type FROM dm_conversation_state
WHERE user_id = $1 AND workspace_id = $2 AND archived_at IS NOT NULL;
