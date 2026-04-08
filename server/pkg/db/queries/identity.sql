-- name: UpdateAgentIdentityCard :exec
UPDATE agent SET identity_card = $2, updated_at = NOW()
WHERE id = $1;

-- name: GetAgentIdentityCard :one
SELECT id, identity_card FROM agent
WHERE id = $1;

-- name: UpdateAgentOnlineStatus :exec
UPDATE agent SET online_status = $2, updated_at = NOW()
WHERE id = $1;

-- name: UpdateAgentWorkloadStatus :exec
UPDATE agent SET workload_status = $2, updated_at = NOW()
WHERE id = $1;

-- name: UpdateAgentLastActiveAt :exec
UPDATE agent SET last_active_at = NOW(), updated_at = NOW()
WHERE id = $1;

-- name: ListAgentsByType :many
SELECT * FROM agent
WHERE workspace_id = $1 AND agent_type = $2 AND archived_at IS NULL
ORDER BY created_at ASC;
