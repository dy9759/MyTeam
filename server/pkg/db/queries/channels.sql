-- name: CreateChannel :one
INSERT INTO channel (workspace_id, name, description, created_by, created_by_type)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetChannel :one
SELECT * FROM channel WHERE id = $1;

-- name: GetChannelByName :one
SELECT * FROM channel WHERE workspace_id = $1 AND name = $2;

-- name: ListChannels :many
SELECT * FROM channel WHERE workspace_id = $1 ORDER BY created_at ASC;

-- name: DeleteChannel :exec
DELETE FROM channel WHERE id = $1;

-- name: AddChannelMember :exec
INSERT INTO channel_member (channel_id, member_id, member_type)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: RemoveChannelMember :exec
DELETE FROM channel_member WHERE channel_id = $1 AND member_id = $2 AND member_type = $3;

-- name: ListChannelMembers :many
SELECT * FROM channel_member WHERE channel_id = $1;

-- name: GetChannelsForMember :many
SELECT c.* FROM channel c
JOIN channel_member cm ON c.id = cm.channel_id
WHERE cm.member_id = $1 AND cm.member_type = $2;

-- name: ListChannelsByCategory :many
SELECT * FROM channel WHERE workspace_id = $1 AND category = $2 ORDER BY created_at ASC;

-- name: UpdateChannelVisibility :exec
UPDATE channel SET visibility = $2 WHERE id = $1;

-- name: UpdateChannelCategory :exec
UPDATE channel SET category = $2 WHERE id = $1;

-- name: ListPublicChannels :many
SELECT * FROM channel WHERE workspace_id = $1 AND visibility = 'public' ORDER BY created_at ASC;

-- name: GetChannelByInviteCode :one
SELECT * FROM channel WHERE invite_code = $1;

-- name: UpdateChannelConversationType :exec
UPDATE channel SET conversation_type = $2 WHERE id = $1;

-- name: ListChannelsByConversationType :many
SELECT * FROM channel
WHERE workspace_id = $1 AND conversation_type = $2
ORDER BY created_at ASC;

-- name: UpdateChannelProject :exec
UPDATE channel SET project_id = $2 WHERE id = $1;
