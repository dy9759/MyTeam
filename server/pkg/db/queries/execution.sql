-- name: CreateExecution :one
INSERT INTO execution (
    task_id, run_id, slot_id, agent_id, runtime_id, attempt,
    priority, payload, log_retention_policy
) VALUES (
    @task_id, @run_id, sqlc.narg('slot_id'), @agent_id, @runtime_id,
    COALESCE(sqlc.narg('attempt')::int, 1),
    COALESCE(sqlc.narg('priority')::int, 0),
    COALESCE(sqlc.narg('payload')::jsonb, '{}'),
    COALESCE(sqlc.narg('log_retention_policy')::text, '90d')
)
RETURNING *;

-- name: GetExecution :one
SELECT * FROM execution WHERE id = $1;

-- name: ListExecutionsByTask :many
SELECT * FROM execution WHERE task_id = @task_id ORDER BY attempt DESC;

-- name: ListExecutionsByRun :many
SELECT * FROM execution WHERE run_id = @run_id ORDER BY created_at DESC;

-- name: ClaimExecution :one
-- Atomic claim with FOR UPDATE SKIP LOCKED — used by daemon + cloud executor.
UPDATE execution SET
    status = 'claimed',
    claimed_at = now(),
    context_ref = @context_ref,
    updated_at = now()
WHERE id = (
    SELECT e.id FROM execution e
    WHERE e.runtime_id = @runtime_id AND e.status = 'queued'
    ORDER BY e.priority DESC, e.created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: StartExecution :exec
UPDATE execution SET
    status = 'running',
    started_at = now(),
    context_ref = COALESCE(sqlc.narg('context_ref')::jsonb, context_ref),
    updated_at = now()
WHERE id = $1;

-- name: CompleteExecution :exec
UPDATE execution SET
    status = 'completed',
    result = @result,
    cost_input_tokens = COALESCE(sqlc.narg('cost_input_tokens')::int, cost_input_tokens),
    cost_output_tokens = COALESCE(sqlc.narg('cost_output_tokens')::int, cost_output_tokens),
    cost_usd = COALESCE(sqlc.narg('cost_usd')::numeric, cost_usd),
    cost_provider = COALESCE(sqlc.narg('cost_provider')::text, cost_provider),
    completed_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: FailExecution :exec
UPDATE execution SET
    status = @status,
    error = @error,
    completed_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: CountInflightExecutionsForRuntime :one
SELECT COUNT(*) FROM execution
WHERE runtime_id = @runtime_id AND status IN ('claimed','running');
