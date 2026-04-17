-- Plan 4 Task 5+6: Cross-cutting infra schema (PRD §3.1, §8.1).
-- Combined migration covering activity_log rewrite, inbox_item extensions,
-- personal_access_token scopes, workspace_secret/quota tables, and forward-
-- compatible cost_* columns on agent_task_queue.

-- =====================================================================
-- 1) activity_log rewrite (transitional: keeps legacy `action` column)
-- =====================================================================

-- Add the new event_type column (initially nullable so backfill is safe).
ALTER TABLE activity_log
    ADD COLUMN IF NOT EXISTS event_type            TEXT,
    ADD COLUMN IF NOT EXISTS effective_actor_id    UUID,
    ADD COLUMN IF NOT EXISTS effective_actor_type  TEXT,
    ADD COLUMN IF NOT EXISTS real_operator_id      UUID,
    ADD COLUMN IF NOT EXISTS real_operator_type    TEXT,
    ADD COLUMN IF NOT EXISTS related_project_id    UUID,
    ADD COLUMN IF NOT EXISTS related_plan_id       UUID,
    ADD COLUMN IF NOT EXISTS related_task_id       UUID,
    ADD COLUMN IF NOT EXISTS related_slot_id       UUID,
    ADD COLUMN IF NOT EXISTS related_execution_id  UUID,
    ADD COLUMN IF NOT EXISTS related_channel_id    UUID,
    ADD COLUMN IF NOT EXISTS related_thread_id     UUID,
    ADD COLUMN IF NOT EXISTS related_agent_id      UUID,
    ADD COLUMN IF NOT EXISTS related_runtime_id    UUID,
    ADD COLUMN IF NOT EXISTS payload               JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS retention_class       TEXT  NOT NULL DEFAULT 'permanent';

-- Backfill event_type from legacy action so existing rows have the new field set.
UPDATE activity_log
SET event_type = action
WHERE event_type IS NULL;

-- Promote event_type to NOT NULL after backfill.
ALTER TABLE activity_log
    ALTER COLUMN event_type SET NOT NULL;

-- Constraint: retention_class must be one of the supported tiers.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'activity_log_retention_class_check'
    ) THEN
        ALTER TABLE activity_log
            ADD CONSTRAINT activity_log_retention_class_check
            CHECK (retention_class IN ('permanent', 'ttl', 'temp'));
    END IF;
END $$;

-- New indexes for workspace-scoped lookups by time, project, task, and event_type.
CREATE INDEX IF NOT EXISTS idx_activity_log_workspace_time
    ON activity_log (workspace_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_activity_log_project
    ON activity_log (related_project_id)
    WHERE related_project_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_activity_log_task
    ON activity_log (related_task_id)
    WHERE related_task_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_activity_log_event_type
    ON activity_log (workspace_id, event_type, created_at DESC);

-- =====================================================================
-- 2) inbox_item extensions (additive)
-- =====================================================================

ALTER TABLE inbox_item
    ADD COLUMN IF NOT EXISTS plan_id        UUID,
    ADD COLUMN IF NOT EXISTS task_id        UUID,
    ADD COLUMN IF NOT EXISTS slot_id        UUID,
    ADD COLUMN IF NOT EXISTS thread_id      UUID,
    ADD COLUMN IF NOT EXISTS channel_id     UUID,
    ADD COLUMN IF NOT EXISTS resolved_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS resolution     TEXT,
    ADD COLUMN IF NOT EXISTS resolution_by  UUID;

-- FKs for thread / channel (SET NULL on delete to keep historical inbox rows).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'inbox_item_thread_id_fkey'
    ) THEN
        ALTER TABLE inbox_item
            ADD CONSTRAINT inbox_item_thread_id_fkey
            FOREIGN KEY (thread_id) REFERENCES thread(id) ON DELETE SET NULL;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'inbox_item_channel_id_fkey'
    ) THEN
        ALTER TABLE inbox_item
            ADD CONSTRAINT inbox_item_channel_id_fkey
            FOREIGN KEY (channel_id) REFERENCES channel(id) ON DELETE SET NULL;
    END IF;
END $$;

-- Widen severity CHECK to include legacy values + PRD §3.1 levels.
-- Original CHECK: ('action_required','attention','info').
-- New CHECK adds 'warning' and 'critical'; legacy mapping handled in code.
ALTER TABLE inbox_item DROP CONSTRAINT IF EXISTS inbox_item_severity_check;
ALTER TABLE inbox_item
    ADD CONSTRAINT inbox_item_severity_check
    CHECK (severity IN ('action_required', 'attention', 'info', 'warning', 'critical'));

-- Partial index for active (unresolved) inbox items by recipient.
CREATE INDEX IF NOT EXISTS idx_inbox_item_recipient_active
    ON inbox_item (recipient_id, created_at DESC)
    WHERE resolved_at IS NULL;

-- =====================================================================
-- 3) personal_access_token.scopes
-- =====================================================================

ALTER TABLE personal_access_token
    ADD COLUMN IF NOT EXISTS scopes TEXT[] NOT NULL DEFAULT '{}';

-- =====================================================================
-- 4) workspace_secret (new)
-- =====================================================================

CREATE TABLE IF NOT EXISTS workspace_secret (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    key             TEXT NOT NULL,
    value_encrypted BYTEA NOT NULL,
    created_by      UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at      TIMESTAMPTZ,
    UNIQUE (workspace_id, key)
);

CREATE INDEX IF NOT EXISTS idx_workspace_secret_workspace
    ON workspace_secret (workspace_id);

-- =====================================================================
-- 5) workspace_quota (new)
-- =====================================================================

CREATE TABLE IF NOT EXISTS workspace_quota (
    workspace_id              UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    max_monthly_usd           NUMERIC(10,2) NOT NULL DEFAULT 100.00,
    max_concurrent_cloud_exec INTEGER       NOT NULL DEFAULT 10,
    max_monthly_plan_gen      INTEGER       NOT NULL DEFAULT 200,
    current_monthly_usd       NUMERIC(10,2) NOT NULL DEFAULT 0,
    current_month             DATE          NOT NULL DEFAULT date_trunc('month', now())::DATE,
    updated_at                TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- =====================================================================
-- 6) agent_task_queue cost_* columns (forward-compat with Plan 5 execution)
-- =====================================================================

ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS cost_input_tokens  INTEGER       NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cost_output_tokens INTEGER       NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cost_usd           NUMERIC(10,4) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cost_provider      TEXT;
