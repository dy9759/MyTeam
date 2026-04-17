-- name: UpdateAgentIdentityCard :exec
UPDATE agent SET identity_card = $2, updated_at = NOW()
WHERE id = $1;

-- name: GetAgentIdentityCard :one
SELECT id, identity_card FROM agent
WHERE id = $1;

-- name: UpdateAgentLastActiveAt :exec
UPDATE agent SET last_active_at = NOW(), updated_at = NOW()
WHERE id = $1;

-- name: ListAgentsByType :many
SELECT
    id, workspace_id, name, avatar_url, visibility, status,
    max_concurrent_tasks, owner_id, created_at, updated_at, description,
    runtime_id, instructions, archived_at, archived_by,
    auto_reply_enabled, auto_reply_config, display_name, avatar, bio, tags,
    trigger_on_channel_mention, needs_attention, needs_attention_reason,
    agent_type, identity_card, last_active_at, scope, owner_type
FROM agent
WHERE workspace_id = $1 AND agent_type = $2 AND archived_at IS NULL
ORDER BY created_at ASC;
