ALTER TABLE project_version
    DROP COLUMN IF EXISTS context_imports;

DROP INDEX IF EXISTS idx_browser_usage_workspace;
DROP TABLE IF EXISTS browser_usage;

DROP INDEX IF EXISTS idx_browser_tab_workspace;
DROP TABLE IF EXISTS browser_tab;

DROP INDEX IF EXISTS idx_browser_context_workspace;
DROP TABLE IF EXISTS browser_context;

DROP INDEX IF EXISTS idx_workspace_collaborator_workspace;
DROP TABLE IF EXISTS workspace_collaborator;

ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS readiness,
    DROP COLUMN IF EXISTS last_heartbeat,
    DROP COLUMN IF EXISTS capabilities,
    DROP COLUMN IF EXISTS working_dir,
    DROP COLUMN IF EXISTS server_host;
