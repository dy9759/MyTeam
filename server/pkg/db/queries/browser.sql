-- name: ListBrowserContexts :many
SELECT * FROM browser_context
WHERE workspace_id = @workspace_id
ORDER BY last_used_at DESC, created_at DESC;

-- name: GetBrowserContext :one
SELECT * FROM browser_context
WHERE id = @id;

-- name: CreateBrowserContext :one
INSERT INTO browser_context (workspace_id, name, domain, status, created_by, shared_with)
VALUES (@workspace_id, @name, @domain, @status, @created_by, @shared_with)
RETURNING *;

-- name: TouchBrowserContext :exec
UPDATE browser_context
SET last_used_at = NOW()
WHERE id = @id;

-- name: DeleteBrowserContext :exec
DELETE FROM browser_context
WHERE id = @id;

-- name: ListBrowserTabs :many
SELECT * FROM browser_tab
WHERE workspace_id = @workspace_id
ORDER BY last_active_at DESC, created_at DESC;

-- name: GetBrowserTab :one
SELECT * FROM browser_tab
WHERE id = @id;

-- name: CreateBrowserTab :one
INSERT INTO browser_tab (
    workspace_id,
    url,
    title,
    status,
    created_by,
    shared_with,
    context_id,
    session_id,
    live_url,
    screenshot_url,
    conversation_id,
    project_id
)
VALUES (
    @workspace_id,
    @url,
    @title,
    @status,
    @created_by,
    @shared_with,
    @context_id,
    @session_id,
    @live_url,
    @screenshot_url,
    @conversation_id,
    @project_id
)
RETURNING *;

-- name: ReconnectBrowserTab :one
UPDATE browser_tab
SET session_id = @session_id,
    live_url = @live_url,
    screenshot_url = @screenshot_url,
    status = 'active',
    last_active_at = NOW()
WHERE id = @id
RETURNING *;

-- name: UpdateBrowserTabSharing :one
UPDATE browser_tab
SET shared_with = @shared_with,
    last_active_at = NOW()
WHERE id = @id
RETURNING *;

-- name: AttachBrowserTabContext :one
UPDATE browser_tab
SET context_id = @context_id,
    last_active_at = NOW()
WHERE id = @id
RETURNING *;

-- name: ClearBrowserTabContext :one
UPDATE browser_tab
SET context_id = NULL,
    last_active_at = NOW()
WHERE id = @id
RETURNING *;

-- name: CloseBrowserTab :exec
UPDATE browser_tab
SET status = 'closed',
    last_active_at = NOW()
WHERE id = @id;
