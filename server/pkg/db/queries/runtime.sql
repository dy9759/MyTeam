-- name: ListAgentRuntimes :many
SELECT * FROM agent_runtime
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetAgentRuntime :one
SELECT * FROM agent_runtime
WHERE id = $1;

-- name: GetAgentRuntimeForWorkspace :one
SELECT * FROM agent_runtime
WHERE id = $1 AND workspace_id = $2;

-- name: UpsertAgentRuntime :one
INSERT INTO agent_runtime (
    workspace_id,
    daemon_id,
    name,
    runtime_mode,
    provider,
    status,
    device_info,
    metadata,
    last_seen_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
ON CONFLICT (workspace_id, daemon_id, provider)
DO UPDATE SET
    name = EXCLUDED.name,
    runtime_mode = EXCLUDED.runtime_mode,
    status = EXCLUDED.status,
    device_info = EXCLUDED.device_info,
    metadata = EXCLUDED.metadata,
    last_seen_at = now(),
    updated_at = now()
RETURNING *;

-- name: UpdateAgentRuntimeHeartbeat :one
UPDATE agent_runtime
SET status = 'online', last_seen_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetAgentRuntimeOffline :exec
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE id = $1;

-- name: MarkStaleRuntimesOffline :many
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE status = 'online'
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
RETURNING id, workspace_id;

-- name: GetCloudRuntime :one
SELECT * FROM agent_runtime
WHERE workspace_id = $1 AND runtime_mode = 'cloud' AND provider = 'cloud_llm'
LIMIT 1;

-- name: EnsureCloudRuntime :one
INSERT INTO agent_runtime (
    workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
) VALUES ($1, 'cloud', 'Cloud LLM', 'cloud', 'cloud_llm', 'online', 'cloud', '{}', now())
ON CONFLICT (workspace_id, daemon_id, provider)
DO UPDATE SET
    status = 'online',
    last_seen_at = now(),
    updated_at = now()
RETURNING *;

-- name: FailTasksForOfflineRuntimes :many
-- Marks dispatched/running tasks as failed when their runtime is offline.
-- This cleans up orphaned tasks after a daemon crash or network partition.
UPDATE agent_task_queue
SET status = 'failed', completed_at = now(), error = 'runtime went offline'
WHERE status IN ('dispatched', 'running')
  AND runtime_id IN (
    SELECT id FROM agent_runtime WHERE status = 'offline'
  )
RETURNING id, agent_id, issue_id;

-- name: UpdateRuntimeHeartbeat :exec
UPDATE agent_runtime
SET last_heartbeat_at = now(),
    last_seen_at      = now(),
    status            = COALESCE(sqlc.narg('status'), status),
    updated_at        = now()
WHERE id = $1;

-- name: SetRuntimeLoad :exec
UPDATE agent_runtime
SET current_load = $2,
    updated_at   = now()
WHERE id = $1;

-- name: AcquireRuntimeLease :exec
UPDATE agent_runtime
SET lease_expires_at = $2,
    updated_at       = now()
WHERE id = $1;
