-- name: GetOrInitWorkspaceQuota :one
INSERT INTO workspace_quota (workspace_id)
VALUES (@workspace_id)
ON CONFLICT (workspace_id) DO UPDATE
SET updated_at = workspace_quota.updated_at
RETURNING *;

-- name: GetWorkspaceQuota :one
SELECT * FROM workspace_quota WHERE workspace_id = @workspace_id;

-- name: AddWorkspaceCostUSD :exec
UPDATE workspace_quota
SET current_monthly_usd = current_monthly_usd + @amount,
    updated_at = now()
WHERE workspace_id = @workspace_id;

-- name: ResetMonthlyQuota :exec
UPDATE workspace_quota
SET current_monthly_usd = 0,
    current_month = date_trunc('month', now())::DATE,
    updated_at = now()
WHERE workspace_id = @workspace_id
  AND current_month < date_trunc('month', now())::DATE;
