-- name: UpdateAgentProfile :exec
UPDATE agent SET
  display_name = COALESCE($2, display_name),
  avatar = COALESCE($3, avatar),
  bio = COALESCE($4, bio),
  tags = COALESCE($5, tags)
WHERE id = $1;

-- name: GetAgentProfile :one
SELECT id, name, display_name, avatar, bio, tags, status, description
FROM agent WHERE id = $1;

-- name: UpdateAgentAutoReply :exec
UPDATE agent SET
  auto_reply_enabled = $2,
  auto_reply_config = $3
WHERE id = $1;

-- name: GetAutoReplyAgents :many
SELECT id, name, auto_reply_enabled, auto_reply_config
FROM agent
WHERE workspace_id = $1 AND auto_reply_enabled = TRUE AND archived_at IS NULL;

-- name: GetAgentByName :one
SELECT
    id, workspace_id, name, avatar_url, visibility, status,
    max_concurrent_tasks, owner_id, created_at, updated_at, description,
    runtime_id, instructions, archived_at, archived_by,
    auto_reply_enabled, auto_reply_config, display_name, avatar, bio, tags,
    trigger_on_channel_mention, needs_attention, needs_attention_reason,
    agent_type, identity_card, last_active_at, scope, owner_type
FROM agent WHERE workspace_id = $1 AND name = $2 AND archived_at IS NULL;

-- name: GetSystemAgent :one
SELECT
    id, workspace_id, name, avatar_url, visibility, status,
    max_concurrent_tasks, owner_id, created_at, updated_at, description,
    runtime_id, instructions, archived_at, archived_by,
    auto_reply_enabled, auto_reply_config, display_name, avatar, bio, tags,
    trigger_on_channel_mention, needs_attention, needs_attention_reason,
    agent_type, identity_card, last_active_at, scope, owner_type
FROM agent
WHERE workspace_id = $1 AND agent_type = 'system_agent' AND scope IS NULL
LIMIT 1;

-- name: SetAgentNeedsAttention :exec
UPDATE agent SET needs_attention = $2, needs_attention_reason = $3 WHERE id = $1;

-- name: ListAllAgentsGlobal :many
SELECT
    id, workspace_id, name, avatar_url, visibility, status,
    max_concurrent_tasks, owner_id, created_at, updated_at, description,
    runtime_id, instructions, archived_at, archived_by,
    auto_reply_enabled, auto_reply_config, display_name, avatar, bio, tags,
    trigger_on_channel_mention, needs_attention, needs_attention_reason,
    agent_type, identity_card, last_active_at, scope, owner_type
FROM agent WHERE archived_at IS NULL ORDER BY created_at ASC;

-- name: CreateSystemAgent :one
INSERT INTO agent (workspace_id, name, description, status, owner_id, visibility, agent_type, runtime_id)
VALUES ($1, 'System Agent', 'Workspace system agent - manages defaults and automation', 'idle', $2, 'workspace', 'system_agent', $3)
RETURNING
    id, workspace_id, name, avatar_url, visibility, status,
    max_concurrent_tasks, owner_id, created_at, updated_at, description,
    runtime_id, instructions, archived_at, archived_by,
    auto_reply_enabled, auto_reply_config, display_name, avatar, bio, tags,
    trigger_on_channel_mention, needs_attention, needs_attention_reason,
    agent_type, identity_card, last_active_at, scope, owner_type;

-- name: CreatePageSystemAgent :one
INSERT INTO agent (workspace_id, name, description, instructions, status, owner_id, visibility, agent_type, scope, runtime_id)
VALUES ($1, $2, $3, $4, 'idle', $5, 'workspace', 'system_agent', $6, $7)
RETURNING
    id, workspace_id, name, avatar_url, visibility, status,
    max_concurrent_tasks, owner_id, created_at, updated_at, description,
    runtime_id, instructions, archived_at, archived_by,
    auto_reply_enabled, auto_reply_config, display_name, avatar, bio, tags,
    trigger_on_channel_mention, needs_attention, needs_attention_reason,
    agent_type, identity_card, last_active_at, scope, owner_type;

-- name: GetPageSystemAgent :one
SELECT
    id, workspace_id, name, avatar_url, visibility, status,
    max_concurrent_tasks, owner_id, created_at, updated_at, description,
    runtime_id, instructions, archived_at, archived_by,
    auto_reply_enabled, auto_reply_config, display_name, avatar, bio, tags,
    trigger_on_channel_mention, needs_attention, needs_attention_reason,
    agent_type, identity_card, last_active_at, scope, owner_type
FROM agent
WHERE workspace_id = $1 AND scope = $2 AND archived_at IS NULL
LIMIT 1;

-- name: ListPageSystemAgents :many
SELECT
    id, workspace_id, name, avatar_url, visibility, status,
    max_concurrent_tasks, owner_id, created_at, updated_at, description,
    runtime_id, instructions, archived_at, archived_by,
    auto_reply_enabled, auto_reply_config, display_name, avatar, bio, tags,
    trigger_on_channel_mention, needs_attention, needs_attention_reason,
    agent_type, identity_card, last_active_at, scope, owner_type
FROM agent
WHERE workspace_id = $1 AND agent_type = 'system_agent' AND scope IS NOT NULL AND archived_at IS NULL
ORDER BY scope ASC;

-- name: GetPersonalAgent :one
SELECT
    id, workspace_id, name, avatar_url, visibility, status,
    max_concurrent_tasks, owner_id, created_at, updated_at, description,
    runtime_id, instructions, archived_at, archived_by,
    auto_reply_enabled, auto_reply_config, display_name, avatar, bio, tags,
    trigger_on_channel_mention, needs_attention, needs_attention_reason,
    agent_type, identity_card, last_active_at, scope, owner_type
FROM agent
WHERE workspace_id = $1 AND owner_id = $2 AND agent_type = 'personal_agent' AND archived_at IS NULL
LIMIT 1;

-- name: CreatePersonalAgent :one
INSERT INTO agent (
    workspace_id, name, description,
    runtime_id, visibility, status, max_concurrent_tasks, owner_id,
    agent_type, auto_reply_enabled
) VALUES ($1, $2, $3, $4, 'private', 'idle', 1, $5, 'personal_agent', TRUE)
RETURNING
    id, workspace_id, name, avatar_url, visibility, status,
    max_concurrent_tasks, owner_id, created_at, updated_at, description,
    runtime_id, instructions, archived_at, archived_by,
    auto_reply_enabled, auto_reply_config, display_name, avatar, bio, tags,
    trigger_on_channel_mention, needs_attention, needs_attention_reason,
    agent_type, identity_card, last_active_at, scope, owner_type;
