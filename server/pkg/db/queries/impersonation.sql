-- name: StartImpersonation :one
INSERT INTO impersonation_session (owner_id, agent_id, workspace_id, expires_at)
VALUES ($1, $2, $3, NOW() + INTERVAL '30 minutes')
RETURNING *;

-- name: EndImpersonation :exec
UPDATE impersonation_session SET ended_at = NOW()
WHERE agent_id = $1 AND ended_at IS NULL;

-- name: GetActiveImpersonation :one
SELECT * FROM impersonation_session
WHERE agent_id = $1 AND ended_at IS NULL AND expires_at > NOW()
LIMIT 1;

-- name: GetImpersonationByOwner :one
SELECT * FROM impersonation_session
WHERE owner_id = $1 AND ended_at IS NULL AND expires_at > NOW()
LIMIT 1;

-- name: ExpireStaleImpersonations :exec
UPDATE impersonation_session SET ended_at = NOW()
WHERE ended_at IS NULL AND expires_at <= NOW();
