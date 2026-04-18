-- Plan 5: drop legacy planning artifacts now superseded by Task / Slot / Execution.
-- DESTRUCTIVE: data in dropped JSONB columns is lost.

-- Drop workflow_step first (FK target if any)
DROP TABLE IF EXISTS workflow_step CASCADE;

-- Drop workflow
DROP TABLE IF EXISTS workflow CASCADE;

-- Drop plan.steps JSONB (Task table now carries it)
ALTER TABLE plan DROP COLUMN IF EXISTS steps;

-- Drop project_version snapshots
ALTER TABLE project_version
  DROP COLUMN IF EXISTS plan_snapshot,
  DROP COLUMN IF EXISTS workflow_snapshot;

-- Drop project_run.retry_count (Task.current_retry + Execution.attempt take over)
ALTER TABLE project_run DROP COLUMN IF EXISTS retry_count;
