-- Plan 4 Task 5+6 rollback.
-- Reverse all changes from 054_cross_cutting_infra.up.sql in reverse order.

-- =====================================================================
-- 6) agent_task_queue cost_* columns
-- =====================================================================

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS cost_provider,
    DROP COLUMN IF EXISTS cost_usd,
    DROP COLUMN IF EXISTS cost_output_tokens,
    DROP COLUMN IF EXISTS cost_input_tokens;

-- =====================================================================
-- 5) workspace_quota
-- =====================================================================

DROP TABLE IF EXISTS workspace_quota;

-- =====================================================================
-- 4) workspace_secret
-- =====================================================================

DROP INDEX IF EXISTS idx_workspace_secret_workspace;
DROP TABLE IF EXISTS workspace_secret;

-- =====================================================================
-- 3) personal_access_token.scopes
-- =====================================================================

ALTER TABLE personal_access_token
    DROP COLUMN IF EXISTS scopes;

-- =====================================================================
-- 2) inbox_item extensions
-- =====================================================================

DROP INDEX IF EXISTS idx_inbox_item_recipient_active;

-- Restore original severity CHECK.
ALTER TABLE inbox_item DROP CONSTRAINT IF EXISTS inbox_item_severity_check;
ALTER TABLE inbox_item
    ADD CONSTRAINT inbox_item_severity_check
    CHECK (severity IN ('action_required', 'attention', 'info'));

ALTER TABLE inbox_item DROP CONSTRAINT IF EXISTS inbox_item_thread_id_fkey;
ALTER TABLE inbox_item DROP CONSTRAINT IF EXISTS inbox_item_channel_id_fkey;

ALTER TABLE inbox_item
    DROP COLUMN IF EXISTS resolution_by,
    DROP COLUMN IF EXISTS resolution,
    DROP COLUMN IF EXISTS resolved_at,
    DROP COLUMN IF EXISTS channel_id,
    DROP COLUMN IF EXISTS thread_id,
    DROP COLUMN IF EXISTS slot_id,
    DROP COLUMN IF EXISTS task_id,
    DROP COLUMN IF EXISTS plan_id;

-- =====================================================================
-- 1) activity_log rewrite
-- =====================================================================

DROP INDEX IF EXISTS idx_activity_log_event_type;
DROP INDEX IF EXISTS idx_activity_log_task;
DROP INDEX IF EXISTS idx_activity_log_project;
DROP INDEX IF EXISTS idx_activity_log_workspace_time;

ALTER TABLE activity_log DROP CONSTRAINT IF EXISTS activity_log_retention_class_check;

ALTER TABLE activity_log
    DROP COLUMN IF EXISTS retention_class,
    DROP COLUMN IF EXISTS payload,
    DROP COLUMN IF EXISTS related_runtime_id,
    DROP COLUMN IF EXISTS related_agent_id,
    DROP COLUMN IF EXISTS related_thread_id,
    DROP COLUMN IF EXISTS related_channel_id,
    DROP COLUMN IF EXISTS related_execution_id,
    DROP COLUMN IF EXISTS related_slot_id,
    DROP COLUMN IF EXISTS related_task_id,
    DROP COLUMN IF EXISTS related_plan_id,
    DROP COLUMN IF EXISTS related_project_id,
    DROP COLUMN IF EXISTS real_operator_type,
    DROP COLUMN IF EXISTS real_operator_id,
    DROP COLUMN IF EXISTS effective_actor_type,
    DROP COLUMN IF EXISTS effective_actor_id,
    DROP COLUMN IF EXISTS event_type;
