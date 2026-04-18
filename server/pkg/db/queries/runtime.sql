-- Common column list for agent_runtime SELECTs after Account Phase 2 drops:
-- excludes runtime_mode, last_seen_at. Reads of runtime_mode -> mode,
-- reads of last_seen_at -> last_heartbeat_at.

-- name: ListAgentRuntimes :many
SELECT
    id, workspace_id, daemon_id, name, provider, status,
    device_info, metadata, created_at, updated_at,
    server_host, working_dir, capabilities, last_heartbeat,
    readiness, concurrency_limit, current_load, lease_expires_at,
    last_heartbeat_at, mode
FROM agent_runtime
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetAgentRuntime :one
SELECT
    id, workspace_id, daemon_id, name, provider, status,
    device_info, metadata, created_at, updated_at,
    server_host, working_dir, capabilities, last_heartbeat,
    readiness, concurrency_limit, current_load, lease_expires_at,
    last_heartbeat_at, mode
FROM agent_runtime
WHERE id = $1;

-- name: GetAgentRuntimeForWorkspace :one
SELECT
    id, workspace_id, daemon_id, name, provider, status,
    device_info, metadata, created_at, updated_at,
    server_host, working_dir, capabilities, last_heartbeat,
    readiness, concurrency_limit, current_load, lease_expires_at,
    last_heartbeat_at, mode
FROM agent_runtime
WHERE id = $1 AND workspace_id = $2;

-- name: UpsertAgentRuntime :one
INSERT INTO agent_runtime (
    workspace_id,
    daemon_id,
    name,
    mode,
    provider,
    status,
    device_info,
    metadata,
    last_heartbeat_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
ON CONFLICT (workspace_id, daemon_id, provider)
DO UPDATE SET
    name = EXCLUDED.name,
    mode = EXCLUDED.mode,
    status = EXCLUDED.status,
    device_info = EXCLUDED.device_info,
    metadata = EXCLUDED.metadata,
    last_heartbeat_at = now(),
    updated_at = now()
RETURNING
    id, workspace_id, daemon_id, name, provider, status,
    device_info, metadata, created_at, updated_at,
    server_host, working_dir, capabilities, last_heartbeat,
    readiness, concurrency_limit, current_load, lease_expires_at,
    last_heartbeat_at, mode;

-- name: UpdateAgentRuntimeHeartbeat :one
UPDATE agent_runtime
SET status = 'online', last_heartbeat_at = now(), updated_at = now()
WHERE id = $1
RETURNING
    id, workspace_id, daemon_id, name, provider, status,
    device_info, metadata, created_at, updated_at,
    server_host, working_dir, capabilities, last_heartbeat,
    readiness, concurrency_limit, current_load, lease_expires_at,
    last_heartbeat_at, mode;

-- name: SetAgentRuntimeOffline :exec
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE id = $1;

-- name: MarkStaleRuntimesOffline :many
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE status = 'online'
  AND last_heartbeat_at < now() - make_interval(secs => @stale_seconds::double precision)
RETURNING id, workspace_id;

-- name: GetCloudRuntime :one
SELECT
    id, workspace_id, daemon_id, name, provider, status,
    device_info, metadata, created_at, updated_at,
    server_host, working_dir, capabilities, last_heartbeat,
    readiness, concurrency_limit, current_load, lease_expires_at,
    last_heartbeat_at, mode
FROM agent_runtime
WHERE workspace_id = $1 AND mode = 'cloud' AND provider = 'cloud_llm'
LIMIT 1;

-- name: ListCloudRuntimes :many
-- Lists all cloud-mode runtimes that are in a serviceable state.
-- Used by CloudExecutorService to fan out execution claims across
-- workspaces. Ordered by current_load ASC so lighter runtimes are
-- preferred when claims race.
SELECT
    id, workspace_id, daemon_id, name, provider, status,
    device_info, metadata, created_at, updated_at,
    server_host, working_dir, capabilities, last_heartbeat,
    readiness, concurrency_limit, current_load, lease_expires_at,
    last_heartbeat_at, mode
FROM agent_runtime
WHERE mode = 'cloud' AND status IN ('online', 'degraded')
ORDER BY current_load ASC NULLS LAST, created_at ASC;

-- name: EnsureCloudRuntime :one
INSERT INTO agent_runtime (
    workspace_id, daemon_id, name, mode, provider, status, device_info, metadata, last_heartbeat_at
) VALUES ($1, 'cloud', 'Cloud LLM', 'cloud', 'cloud_llm', 'online', 'cloud', '{}', now())
ON CONFLICT (workspace_id, daemon_id, provider)
DO UPDATE SET
    status = 'online',
    last_heartbeat_at = now(),
    updated_at = now()
RETURNING
    id, workspace_id, daemon_id, name, provider, status,
    device_info, metadata, created_at, updated_at,
    server_host, working_dir, capabilities, last_heartbeat,
    readiness, concurrency_limit, current_load, lease_expires_at,
    last_heartbeat_at, mode;

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

-- name: SetRuntimeMetadataKey :exec
-- Merges a single key into the runtime.metadata JSONB document.
-- Used to store per-agent or per-runtime config (e.g. cloud_llm_config)
-- alongside the runtime instead of on the agent row itself.
UPDATE agent_runtime
SET metadata   = jsonb_set(COALESCE(metadata, '{}'::jsonb), ARRAY[@key::TEXT], @value::JSONB, TRUE),
    updated_at = now()
WHERE id = @id;
