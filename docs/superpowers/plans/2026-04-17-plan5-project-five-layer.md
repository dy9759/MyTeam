# Project Module Restructure — Five-Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Land the Project module restructure described in PRD §3-§13. Establish the `Project → Version → Plan → Task → Slot` five-layer model with `Execution`, `Artifact`, and `Review` as orthogonal records, drop the legacy `workflow / workflow_step / plan.steps / *_snapshot` surface, rewrite `SchedulerService` around `Task`, expose new Daemon endpoints + `CloudExecutorService` integration with `execution`, and ship a list-based UI for the new model.

**Architecture:**
- **Migrations 053-058** add `task`, `participant_slot`, `execution`, `artifact`, `review`, the `plan.thread_id` FK and the destructive drops of `workflow / workflow_step / plan.steps / project_version.{plan_snapshot, workflow_snapshot} / project_run.retry_count`.
- **sqlc queries** for all five new tables (`task.sql`, `participant_slot.sql`, `execution.sql`, `artifact.sql`, `review.sql`) and the rewrite of `plans.sql` / `project_runs.sql` / `project_versions.sql` to drop dead columns.
- **Services:** new `SlotService`, `ArtifactService`, `ReviewService`. Rewrite `SchedulerService` so `ScheduleRun / ScheduleTask / HandleTaskCompletion / HandleTaskFailure / HandleTaskTimeout` operate on `task` + `slot` + `execution` instead of `workflow_step + agent_task_queue`. Update `PlanGeneratorService` to emit `Task + Slot` rows.
- **HTTP / Daemon:** new `/api/daemon/runtimes/{id}/executions/*` endpoints in parallel with the existing `/tasks/*` endpoints. New REST endpoints for `task / slot / artifact / review`. `CloudExecutorService` polls `execution` where `runtime.mode='cloud'` with `FOR UPDATE SKIP LOCKED` and writes `context_ref`.
- **Realtime:** publish `task:status_changed`, `slot:activated/submitted/decision`, `execution:claimed/started/completed/failed/progress`, `artifact:created`, `review:submitted`, `plan:approval_changed`, `run:started/completed/failed/cancelled`.
- **Frontend:** extend `features/projects/` with `task / slot / execution / artifact / review` sub-modules, new pages for project DAG (list view), plan detail with task list, task detail with slot timeline + artifact list + review action.

**Tech Stack:** Same as Plan 1/2/3/4 — Go 1.26, PostgreSQL 17 (pgvector), Chi router, sqlc, gorilla/websocket, Next.js 16, TypeScript strict, Zustand, shadcn.

**Reference PRD:** `/Users/chauncey2025/Documents/Obsidian Vault/2026-04-16-project-restructure-prd.md` §3 (object model), §4 (object definitions), §5 (state machines), §6 (core flows), §8 (mapping), §9 (services), §10 (Execution integration), §11 (data lifecycle), §12 (MVP scope).

**Plan dependencies:**
- Plan 1 (Account Phase 1) — `agent_runtime.mode`, `identity_card`. Required for `findAvailableAgent` filters.
- Plan 3 (Session Phase 4) — `thread.id` independent UUID. Required for `plan.thread_id` FK.
- Plan 4 (Quota & Cost) — `workspace_quota`, `execution.cost_*`, `activity_log` event types. Required for cost tracking and Slot/Task event audit.

**Assumption:** Live database has no production rows in `workflow / workflow_step` (these tables shipped recently and the product is not yet in customer use). No data migration from `workflow_step → task` is included; if any worktree DB has real rows, they are dropped together with the tables. This is consistent with the CLAUDE.md rule "do not add compatibility layers, fallback paths, or legacy adapters".

---

## File Structure

### New
| File | Responsibility |
|---|---|
| `server/migrations/053_task.up.sql` | Create `task` table + indexes + CHECK constraints |
| `server/migrations/053_task.down.sql` | Drop `task` |
| `server/migrations/054_participant_slot.up.sql` | Create `participant_slot` |
| `server/migrations/054_participant_slot.down.sql` | Drop `participant_slot` |
| `server/migrations/055_execution.up.sql` | Create `execution` (Project) |
| `server/migrations/055_execution.down.sql` | Drop `execution` |
| `server/migrations/056_artifact_review.up.sql` | Create `artifact` and `review` |
| `server/migrations/056_artifact_review.down.sql` | Drop both |
| `server/migrations/057_plan_thread_inbox.up.sql` | Add `plan.thread_id` FK + extend `inbox_item` (`task_id`, `slot_id`, `plan_id`) |
| `server/migrations/057_plan_thread_inbox.down.sql` | Reverse FKs and columns |
| `server/migrations/058_drop_legacy.up.sql` | DESTRUCTIVE — drop `workflow`, `workflow_step`, `plan.steps`, `project_version.{plan_snapshot, workflow_snapshot}`, `project_run.retry_count` |
| `server/migrations/058_drop_legacy.down.sql` | Best-effort restore |
| `server/pkg/db/queries/task.sql` | sqlc queries for `task` |
| `server/pkg/db/queries/participant_slot.sql` | sqlc queries for `participant_slot` |
| `server/pkg/db/queries/execution.sql` | sqlc queries for `execution` |
| `server/pkg/db/queries/artifact.sql` | sqlc queries for `artifact` |
| `server/pkg/db/queries/review.sql` | sqlc queries for `review` |
| `server/internal/service/slot.go` | `SlotService` |
| `server/internal/service/slot_test.go` | Unit tests |
| `server/internal/service/artifact.go` | `ArtifactService` |
| `server/internal/service/artifact_test.go` | Unit tests |
| `server/internal/service/review.go` | `ReviewService` |
| `server/internal/service/review_test.go` | Unit tests |
| `server/internal/handler/task.go` | REST endpoints for `task` |
| `server/internal/handler/slot.go` | REST endpoints for `participant_slot` |
| `server/internal/handler/artifact.go` | REST endpoints for `artifact` |
| `server/internal/handler/review.go` | REST endpoints for `review` |
| `server/internal/handler/execution.go` | REST + Daemon endpoints for `execution` |
| `apps/web/features/projects/task/` | Task store, components |
| `apps/web/features/projects/slot/` | Slot store, components |
| `apps/web/features/projects/execution/` | Execution components |
| `apps/web/features/projects/artifact/` | Artifact components |
| `apps/web/features/projects/review/` | Review components |
| `apps/web/app/(dashboard)/projects/[id]/page.tsx` | Project DAG (list view) |
| `apps/web/app/(dashboard)/projects/[id]/plans/[planId]/page.tsx` | Plan detail with task list |
| `apps/web/app/(dashboard)/projects/[id]/tasks/[taskId]/page.tsx` | Task detail with slot timeline + artifacts |

### Modified
| File | Why |
|---|---|
| `server/pkg/db/queries/plans.sql` | Drop `steps` from `CreatePlan` / `UpdatePlanSteps`; add `thread_id` writes |
| `server/pkg/db/queries/project_runs.sql` | Drop `retry_count` write in `FailProjectRun`; add `run_number` |
| `server/pkg/db/queries/project_versions.sql` | Drop `plan_snapshot` / `workflow_snapshot` from `CreateProjectVersion` |
| `server/pkg/db/queries/inbox.sql` | Surface new FKs (`task_id`, `slot_id`, `plan_id`) |
| `server/internal/service/scheduler.go` | Full rewrite — `Schedule(Workflow|Step)` → `Schedule(Run|Task)`, drop `agent_task_queue` writes, write `execution` rows |
| `server/internal/service/plan_generator.go` | Emit `Task + Slot` structs in addition to `GeneratedPlan` |
| `server/internal/service/cloud_executor.go` | Add `pollExecutions` loop, claim from `execution`, write `context_ref` |
| `server/internal/handler/handler.go` | Wire `SlotService`, `ArtifactService`, `ReviewService` |
| `server/internal/handler/daemon.go` | New `/api/daemon/runtimes/{id}/executions/*` routes (in parallel with `/tasks/*`) |
| `server/internal/handler/plan.go` | Drop `steps` JSON in/out, add `thread_id`; expose new approval endpoints |
| `server/internal/handler/project.go` | Tighten DAG read; drop `plan_snapshot` / `workflow_snapshot` writes |
| `server/internal/handler/workflow.go` | DELETE the file (workflow surface dropped). All callers go away |
| `server/cmd/server/router.go` | Wire new handlers + drop workflow routes |
| `apps/web/shared/types/index.ts` | Add `Task`, `Slot`, `Execution`, `Artifact`, `Review` types |
| `apps/web/features/projects/store.ts` | Drop `plan_snapshot` reads; add `tasks`, `slots`, `runs[].id`, etc. |
| `apps/web/features/projects/index.ts` | Export new sub-modules |
| `apps/web/features/inbox/store.ts` | Handle new types `human_input_needed`, `review_needed`, `task_attention_needed` |
| `apps/web/features/workflow/` | DELETE the directory (replaced by Project sub-modules) |

### Deleted
| File | Why |
|---|---|
| `server/pkg/db/queries/workflows.sql` | Workflow surface dropped |
| `server/internal/handler/workflow.go` | Workflow surface dropped |
| `apps/web/features/workflow/` | Replaced by `features/projects/task/` etc. |

---

### Task 1: Migration 053 — `task` table

**Files:**
- Create: `server/migrations/053_task.up.sql`
- Create: `server/migrations/053_task.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- Project Plan 5 - task table.
-- Replaces workflow_step. Carries planning + step execution state in one row.

CREATE TABLE IF NOT EXISTS task (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID NOT NULL REFERENCES plan(id) ON DELETE CASCADE,
    run_id UUID REFERENCES project_run(id),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,

    -- Planning fields
    title TEXT NOT NULL,
    description TEXT,
    step_order INTEGER NOT NULL DEFAULT 0,
    depends_on UUID[] NOT NULL DEFAULT '{}',
    primary_assignee_id UUID REFERENCES agent(id),
    fallback_agent_ids UUID[] NOT NULL DEFAULT '{}',
    required_skills TEXT[] NOT NULL DEFAULT '{}',
    collaboration_mode TEXT NOT NULL DEFAULT 'agent_exec_human_review',
    acceptance_criteria TEXT,

    -- Execution state (current Run only)
    status TEXT NOT NULL DEFAULT 'draft',
    actual_agent_id UUID REFERENCES agent(id),
    current_retry INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    result JSONB,
    error TEXT,

    -- Policy
    timeout_rule JSONB NOT NULL DEFAULT '{"max_duration_seconds":1800,"action":"retry"}',
    retry_rule JSONB NOT NULL DEFAULT '{"max_retries":2,"retry_delay_seconds":30}',
    escalation_policy JSONB NOT NULL DEFAULT '{"escalate_after_seconds":600}',

    -- Context
    input_context_refs JSONB NOT NULL DEFAULT '[]',
    output_refs JSONB NOT NULL DEFAULT '[]',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT task_status_check CHECK (status IN (
        'draft','ready','queued','assigned','running',
        'needs_human','under_review','needs_attention',
        'completed','failed','cancelled'
    )),
    CONSTRAINT task_collab_mode_check CHECK (collaboration_mode IN (
        'agent_exec_human_review',
        'human_input_agent_exec',
        'agent_prepare_human_action',
        'mixed'
    ))
);

CREATE INDEX IF NOT EXISTS idx_task_plan_order ON task(plan_id, step_order);
CREATE INDEX IF NOT EXISTS idx_task_run ON task(run_id);
CREATE INDEX IF NOT EXISTS idx_task_status ON task(workspace_id, status);
CREATE INDEX IF NOT EXISTS idx_task_depends_on ON task USING GIN (depends_on);
CREATE INDEX IF NOT EXISTS idx_task_primary_assignee ON task(primary_assignee_id) WHERE primary_assignee_id IS NOT NULL;
```

- [ ] **Step 2: Write the down migration**

```sql
DROP INDEX IF EXISTS idx_task_primary_assignee;
DROP INDEX IF EXISTS idx_task_depends_on;
DROP INDEX IF EXISTS idx_task_status;
DROP INDEX IF EXISTS idx_task_run;
DROP INDEX IF EXISTS idx_task_plan_order;
DROP TABLE IF EXISTS task;
```

- [ ] **Step 3: Apply + verify**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make migrate-up
psql "$DATABASE_URL" -c "\d task"
```

Expected: 30+ columns, two CHECK constraints, five indexes.

- [ ] **Step 4: Commit**

```bash
git add server/migrations/053_task.up.sql server/migrations/053_task.down.sql
git commit -m "feat(db): add task table (project plan 5)"
```

---

### Task 2: Migration 054 — `participant_slot` table

**Files:**
- Create: `server/migrations/054_participant_slot.up.sql`
- Create: `server/migrations/054_participant_slot.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- Project Plan 5 - participant_slot table.

CREATE TABLE IF NOT EXISTS participant_slot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES task(id) ON DELETE CASCADE,
    slot_type TEXT NOT NULL,
    slot_order INTEGER NOT NULL DEFAULT 0,
    participant_id UUID,
    participant_type TEXT NOT NULL,
    responsibility TEXT,
    trigger TEXT NOT NULL,
    blocking BOOLEAN NOT NULL DEFAULT TRUE,
    required BOOLEAN NOT NULL DEFAULT TRUE,
    expected_output TEXT,
    status TEXT NOT NULL DEFAULT 'waiting',
    timeout_seconds INTEGER,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT slot_type_check CHECK (slot_type IN (
        'human_input','agent_execution','human_review'
    )),
    CONSTRAINT slot_participant_type_check CHECK (participant_type IN (
        'member','agent'
    )),
    CONSTRAINT slot_trigger_check CHECK (trigger IN (
        'before_execution','during_execution','before_done'
    )),
    CONSTRAINT slot_status_check CHECK (status IN (
        'waiting','ready','in_progress','submitted',
        'approved','revision_requested','rejected',
        'expired','skipped'
    ))
);

CREATE INDEX IF NOT EXISTS idx_slot_task_order ON participant_slot(task_id, slot_order);
CREATE INDEX IF NOT EXISTS idx_slot_status ON participant_slot(status);
CREATE INDEX IF NOT EXISTS idx_slot_participant ON participant_slot(participant_id) WHERE participant_id IS NOT NULL;
```

- [ ] **Step 2: Write the down migration**

```sql
DROP INDEX IF EXISTS idx_slot_participant;
DROP INDEX IF EXISTS idx_slot_status;
DROP INDEX IF EXISTS idx_slot_task_order;
DROP TABLE IF EXISTS participant_slot;
```

- [ ] **Step 3: Apply + verify**

```bash
make migrate-up
psql "$DATABASE_URL" -c "\d participant_slot"
```

Expected: four CHECK constraints, three indexes.

- [ ] **Step 4: Commit**

```bash
git add server/migrations/054_participant_slot.up.sql server/migrations/054_participant_slot.down.sql
git commit -m "feat(db): add participant_slot table"
```

---

### Task 3: Migration 055 — `execution` table

**Files:**
- Create: `server/migrations/055_execution.up.sql`
- Create: `server/migrations/055_execution.down.sql`

This is the Project-link execution table. The existing `agent_task_queue` continues to serve the Issue link unchanged.

- [ ] **Step 1: Write the up migration**

```sql
-- Project Plan 5 - execution table (Project link).
-- agent_task_queue continues to serve Issue link.

CREATE TABLE IF NOT EXISTS execution (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES task(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES project_run(id) ON DELETE CASCADE,
    slot_id UUID REFERENCES participant_slot(id),
    agent_id UUID NOT NULL REFERENCES agent(id),
    runtime_id UUID REFERENCES agent_runtime(id),

    attempt INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'queued',
    priority INTEGER NOT NULL DEFAULT 50,

    payload JSONB NOT NULL DEFAULT '{}',
    result JSONB,
    error TEXT,

    -- Physical execution context (per PRD §4.7)
    context_ref JSONB NOT NULL DEFAULT '{}',

    -- Log retention (Plan 4 cost/lifecycle integration)
    log_retention_policy TEXT NOT NULL DEFAULT '90d',
    logs_expires_at TIMESTAMPTZ,

    claimed_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT execution_status_check CHECK (status IN (
        'queued','claimed','running','completed','failed','cancelled','timed_out'
    )),
    CONSTRAINT execution_log_retention_check CHECK (log_retention_policy IN (
        '7d','30d','90d','permanent'
    ))
);

-- Claim loop hot path: filter by runtime + queued, sort by priority desc then FIFO.
CREATE INDEX IF NOT EXISTS idx_execution_claim
    ON execution(runtime_id, priority DESC, created_at ASC)
    WHERE status = 'queued';

CREATE INDEX IF NOT EXISTS idx_execution_task ON execution(task_id, attempt);
CREATE INDEX IF NOT EXISTS idx_execution_run ON execution(run_id);
CREATE INDEX IF NOT EXISTS idx_execution_agent ON execution(agent_id);
CREATE INDEX IF NOT EXISTS idx_execution_logs_expires ON execution(logs_expires_at) WHERE logs_expires_at IS NOT NULL;
```

- [ ] **Step 2: Write the down migration**

```sql
DROP INDEX IF EXISTS idx_execution_logs_expires;
DROP INDEX IF EXISTS idx_execution_agent;
DROP INDEX IF EXISTS idx_execution_run;
DROP INDEX IF EXISTS idx_execution_task;
DROP INDEX IF EXISTS idx_execution_claim;
DROP TABLE IF EXISTS execution;
```

- [ ] **Step 3: Apply + verify**

```bash
make migrate-up
psql "$DATABASE_URL" -c "\d execution"
```

Expected: claim loop index has the `WHERE status='queued'` predicate.

- [ ] **Step 4: Commit**

```bash
git add server/migrations/055_execution.up.sql server/migrations/055_execution.down.sql
git commit -m "feat(db): add execution table for project link"
```

---

### Task 4: Migration 056 — `artifact` and `review` tables

**Files:**
- Create: `server/migrations/056_artifact_review.up.sql`
- Create: `server/migrations/056_artifact_review.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- Project Plan 5 - artifact + review tables.

CREATE TABLE IF NOT EXISTS artifact (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES task(id) ON DELETE CASCADE,
    slot_id UUID REFERENCES participant_slot(id),
    execution_id UUID REFERENCES execution(id),
    run_id UUID NOT NULL REFERENCES project_run(id),

    artifact_type TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    title TEXT,
    summary TEXT,
    content JSONB,

    file_index_id UUID REFERENCES file_index(id),
    file_snapshot_id UUID REFERENCES file_snapshot(id),

    retention_class TEXT NOT NULL DEFAULT 'permanent',
    created_by_id UUID,
    created_by_type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT artifact_type_check CHECK (artifact_type IN (
        'document','design','code_patch','report','file','plan_doc'
    )),
    CONSTRAINT artifact_creator_type_check CHECK (created_by_type IN (
        'member','agent'
    )),
    CONSTRAINT artifact_retention_check CHECK (retention_class IN (
        'permanent','ttl','temp'
    )),
    -- headless artifact: when no file_index_id, content must be present.
    CONSTRAINT artifact_headless_or_file CHECK (
        file_index_id IS NOT NULL OR content IS NOT NULL
    )
);

CREATE INDEX IF NOT EXISTS idx_artifact_task_version ON artifact(task_id, version DESC);
CREATE INDEX IF NOT EXISTS idx_artifact_run ON artifact(run_id);
CREATE INDEX IF NOT EXISTS idx_artifact_slot ON artifact(slot_id) WHERE slot_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_artifact_file_index ON artifact(file_index_id) WHERE file_index_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS review (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES task(id) ON DELETE CASCADE,
    artifact_id UUID NOT NULL REFERENCES artifact(id) ON DELETE CASCADE,
    slot_id UUID NOT NULL REFERENCES participant_slot(id),
    reviewer_id UUID NOT NULL,
    reviewer_type TEXT NOT NULL,
    decision TEXT NOT NULL,
    comment TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT review_decision_check CHECK (decision IN (
        'approve','request_changes','reject'
    )),
    CONSTRAINT review_reviewer_type_check CHECK (reviewer_type IN (
        'member','agent'
    ))
);

CREATE INDEX IF NOT EXISTS idx_review_task ON review(task_id);
CREATE INDEX IF NOT EXISTS idx_review_artifact ON review(artifact_id);
CREATE INDEX IF NOT EXISTS idx_review_slot ON review(slot_id);
```

- [ ] **Step 2: Write the down migration**

```sql
DROP INDEX IF EXISTS idx_review_slot;
DROP INDEX IF EXISTS idx_review_artifact;
DROP INDEX IF EXISTS idx_review_task;
DROP TABLE IF EXISTS review;

DROP INDEX IF EXISTS idx_artifact_file_index;
DROP INDEX IF EXISTS idx_artifact_slot;
DROP INDEX IF EXISTS idx_artifact_run;
DROP INDEX IF EXISTS idx_artifact_task_version;
DROP TABLE IF EXISTS artifact;
```

- [ ] **Step 3: Apply + verify**

```bash
make migrate-up
psql "$DATABASE_URL" -c "\d artifact"
psql "$DATABASE_URL" -c "\d review"
```

Expected: `artifact_headless_or_file` CHECK present.

- [ ] **Step 4: Commit**

```bash
git add server/migrations/056_artifact_review.up.sql server/migrations/056_artifact_review.down.sql
git commit -m "feat(db): add artifact and review tables"
```

---

### Task 5: Migration 057 — `plan.thread_id` FK + extend `inbox_item`

**Files:**
- Create: `server/migrations/057_plan_thread_inbox.up.sql`
- Create: `server/migrations/057_plan_thread_inbox.down.sql`

Depends on Plan 3 having decoupled `thread.id` from `root_message_id`. If Plan 3 is not merged yet, the FK still works against the existing `thread` table — only the semantics on the Plan 3 side change.

- [ ] **Step 1: Write the up migration**

```sql
-- Project Plan 5 - plan.thread_id FK + inbox_item extension.

ALTER TABLE plan
    ADD COLUMN IF NOT EXISTS thread_id UUID REFERENCES thread(id);

CREATE INDEX IF NOT EXISTS idx_plan_thread ON plan(thread_id) WHERE thread_id IS NOT NULL;

-- inbox_item extension: link to task / slot / plan.
ALTER TABLE inbox_item
    ADD COLUMN IF NOT EXISTS task_id UUID REFERENCES task(id),
    ADD COLUMN IF NOT EXISTS slot_id UUID REFERENCES participant_slot(id),
    ADD COLUMN IF NOT EXISTS plan_id UUID REFERENCES plan(id);

CREATE INDEX IF NOT EXISTS idx_inbox_task ON inbox_item(task_id) WHERE task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_inbox_slot ON inbox_item(slot_id) WHERE slot_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_inbox_plan ON inbox_item(plan_id) WHERE plan_id IS NOT NULL;
```

- [ ] **Step 2: Write the down migration**

```sql
DROP INDEX IF EXISTS idx_inbox_plan;
DROP INDEX IF EXISTS idx_inbox_slot;
DROP INDEX IF EXISTS idx_inbox_task;
ALTER TABLE inbox_item
    DROP COLUMN IF EXISTS plan_id,
    DROP COLUMN IF EXISTS slot_id,
    DROP COLUMN IF EXISTS task_id;
DROP INDEX IF EXISTS idx_plan_thread;
ALTER TABLE plan DROP COLUMN IF EXISTS thread_id;
```

- [ ] **Step 3: Apply + verify**

```bash
make migrate-up
psql "$DATABASE_URL" -c "\d plan" | grep thread
psql "$DATABASE_URL" -c "\d inbox_item" | grep -E 'task_id|slot_id|plan_id'
```

- [ ] **Step 4: Commit**

```bash
git add server/migrations/057_plan_thread_inbox.up.sql server/migrations/057_plan_thread_inbox.down.sql
git commit -m "feat(db): plan.thread_id FK and inbox_item project link columns"
```

---

### Task 6: sqlc queries for the five new tables

**Files:**
- Create: `server/pkg/db/queries/task.sql`
- Create: `server/pkg/db/queries/participant_slot.sql`
- Create: `server/pkg/db/queries/execution.sql`
- Create: `server/pkg/db/queries/artifact.sql`
- Create: `server/pkg/db/queries/review.sql`

- [ ] **Step 1: Write `task.sql`**

```sql
-- name: CreateTask :one
INSERT INTO task (
    plan_id, workspace_id, title, description,
    step_order, depends_on, primary_assignee_id, fallback_agent_ids,
    required_skills, collaboration_mode, acceptance_criteria,
    timeout_rule, retry_rule, escalation_policy,
    input_context_refs
) VALUES (
    @plan_id, @workspace_id, @title, @description,
    @step_order, @depends_on, @primary_assignee_id, @fallback_agent_ids,
    @required_skills, @collaboration_mode, @acceptance_criteria,
    @timeout_rule, @retry_rule, @escalation_policy,
    @input_context_refs
)
RETURNING *;

-- name: GetTask :one
SELECT * FROM task WHERE id = $1;

-- name: ListTasksByPlan :many
SELECT * FROM task WHERE plan_id = $1 ORDER BY step_order ASC;

-- name: ListReadyTasksByRun :many
SELECT * FROM task WHERE run_id = $1 AND status = 'ready' ORDER BY step_order ASC;

-- name: ListTasksByRun :many
SELECT * FROM task WHERE run_id = $1 ORDER BY step_order ASC;

-- name: UpdateTaskStatus :exec
UPDATE task SET
    status = @status,
    started_at = CASE WHEN @status = 'running' AND started_at IS NULL THEN NOW() ELSE started_at END,
    completed_at = CASE WHEN @status IN ('completed','failed','cancelled') THEN NOW() ELSE completed_at END,
    updated_at = NOW()
WHERE id = @id;

-- name: UpdateTaskRunBinding :exec
UPDATE task SET
    run_id = @run_id,
    status = 'draft',
    actual_agent_id = NULL,
    current_retry = 0,
    started_at = NULL,
    completed_at = NULL,
    result = NULL,
    error = NULL,
    updated_at = NOW()
WHERE plan_id = @plan_id;

-- name: UpdateTaskActualAgent :exec
UPDATE task SET actual_agent_id = @actual_agent_id, updated_at = NOW() WHERE id = @id;

-- name: IncrementTaskRetry :exec
UPDATE task SET current_retry = current_retry + 1, updated_at = NOW() WHERE id = @id;

-- name: ResetTaskRetry :exec
UPDATE task SET current_retry = 0, updated_at = NOW() WHERE id = @id;

-- name: UpdateTaskResult :exec
UPDATE task SET result = @result, error = @error, updated_at = NOW() WHERE id = @id;

-- name: ListDownstreamTasks :many
SELECT * FROM task WHERE plan_id = @plan_id AND @upstream_id = ANY(depends_on);

-- name: DeleteTask :exec
DELETE FROM task WHERE id = $1;
```

- [ ] **Step 2: Write `participant_slot.sql`**

```sql
-- name: CreateSlot :one
INSERT INTO participant_slot (
    task_id, slot_type, slot_order,
    participant_id, participant_type, responsibility,
    trigger, blocking, required, expected_output, timeout_seconds
) VALUES (
    @task_id, @slot_type, @slot_order,
    @participant_id, @participant_type, @responsibility,
    @trigger, @blocking, @required, @expected_output, @timeout_seconds
)
RETURNING *;

-- name: GetSlot :one
SELECT * FROM participant_slot WHERE id = $1;

-- name: ListSlotsByTask :many
SELECT * FROM participant_slot WHERE task_id = $1 ORDER BY slot_order ASC;

-- name: ListSlotsByTrigger :many
SELECT * FROM participant_slot WHERE task_id = @task_id AND trigger = @trigger ORDER BY slot_order ASC;

-- name: UpdateSlotStatus :exec
UPDATE participant_slot SET
    status = @status,
    started_at = CASE WHEN @status IN ('ready','in_progress') AND started_at IS NULL THEN NOW() ELSE started_at END,
    completed_at = CASE WHEN @status IN ('submitted','approved','revision_requested','rejected','expired','skipped') THEN NOW() ELSE completed_at END,
    updated_at = NOW()
WHERE id = @id;

-- name: ResetSlotsForTask :exec
UPDATE participant_slot SET
    status = 'waiting',
    started_at = NULL,
    completed_at = NULL,
    updated_at = NOW()
WHERE task_id = @task_id;

-- name: ListExpiredSlots :many
SELECT * FROM participant_slot
WHERE status IN ('ready','in_progress')
  AND timeout_seconds IS NOT NULL
  AND started_at IS NOT NULL
  AND started_at + (timeout_seconds * INTERVAL '1 second') < NOW();
```

- [ ] **Step 3: Write `execution.sql`**

```sql
-- name: CreateExecution :one
INSERT INTO execution (
    task_id, run_id, slot_id, agent_id, runtime_id,
    attempt, priority, payload, log_retention_policy
) VALUES (
    @task_id, @run_id, @slot_id, @agent_id, @runtime_id,
    @attempt, @priority, @payload, @log_retention_policy
)
RETURNING *;

-- name: GetExecution :one
SELECT * FROM execution WHERE id = $1;

-- name: ListPendingExecutionsForRuntime :many
SELECT * FROM execution
WHERE runtime_id = @runtime_id AND status = 'queued'
ORDER BY priority DESC, created_at ASC
LIMIT @limit_count;

-- name: ClaimExecution :one
-- Atomic claim using FOR UPDATE SKIP LOCKED.
UPDATE execution SET
    status = 'claimed',
    claimed_at = NOW(),
    context_ref = @context_ref,
    updated_at = NOW()
WHERE id = (
    SELECT id FROM execution
    WHERE runtime_id = @runtime_id AND status = 'queued'
    ORDER BY priority DESC, created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ClaimCloudExecution :one
-- Cloud variant: claim by mode='cloud' filter via runtime join.
UPDATE execution SET
    status = 'claimed',
    claimed_at = NOW(),
    context_ref = @context_ref,
    updated_at = NOW()
WHERE id = (
    SELECT e.id FROM execution e
    JOIN agent_runtime r ON r.id = e.runtime_id
    WHERE e.status = 'queued' AND r.mode = 'cloud'
    ORDER BY e.priority DESC, e.created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: StartExecution :exec
UPDATE execution SET status = 'running', started_at = NOW(), updated_at = NOW() WHERE id = @id;

-- name: CompleteExecution :exec
UPDATE execution SET
    status = 'completed', result = @result, completed_at = NOW(), updated_at = NOW()
WHERE id = @id;

-- name: FailExecution :exec
UPDATE execution SET
    status = 'failed', error = @error, completed_at = NOW(), updated_at = NOW()
WHERE id = @id;

-- name: CancelExecution :exec
UPDATE execution SET status = 'cancelled', completed_at = NOW(), updated_at = NOW() WHERE id = @id;

-- name: TimeOutExecution :exec
UPDATE execution SET status = 'timed_out', completed_at = NOW(), updated_at = NOW() WHERE id = @id;

-- name: UpdateExecutionContextRef :exec
UPDATE execution SET context_ref = @context_ref, updated_at = NOW() WHERE id = @id;

-- name: ListExecutionsByTask :many
SELECT * FROM execution WHERE task_id = $1 ORDER BY attempt DESC;

-- name: ListExecutionsByRun :many
SELECT * FROM execution WHERE run_id = $1 ORDER BY created_at ASC;
```

- [ ] **Step 4: Write `artifact.sql`**

```sql
-- name: CreateArtifact :one
INSERT INTO artifact (
    task_id, slot_id, execution_id, run_id,
    artifact_type, version, title, summary, content,
    file_index_id, file_snapshot_id,
    retention_class, created_by_id, created_by_type
) VALUES (
    @task_id, @slot_id, @execution_id, @run_id,
    @artifact_type, @version, @title, @summary, @content,
    @file_index_id, @file_snapshot_id,
    @retention_class, @created_by_id, @created_by_type
)
RETURNING *;

-- name: GetArtifact :one
SELECT * FROM artifact WHERE id = $1;

-- name: ListArtifactsByTask :many
SELECT * FROM artifact WHERE task_id = $1 ORDER BY version DESC;

-- name: GetLatestArtifactVersion :one
SELECT COALESCE(MAX(version), 0)::INTEGER AS max_version FROM artifact WHERE task_id = @task_id;

-- name: ListArtifactsByRun :many
SELECT * FROM artifact WHERE run_id = $1 ORDER BY created_at DESC;
```

- [ ] **Step 5: Write `review.sql`**

```sql
-- name: CreateReview :one
INSERT INTO review (
    task_id, artifact_id, slot_id,
    reviewer_id, reviewer_type, decision, comment
) VALUES (
    @task_id, @artifact_id, @slot_id,
    @reviewer_id, @reviewer_type, @decision, @comment
)
RETURNING *;

-- name: GetReview :one
SELECT * FROM review WHERE id = $1;

-- name: ListReviewsByArtifact :many
SELECT * FROM review WHERE artifact_id = $1 ORDER BY created_at DESC;

-- name: ListReviewsByTask :many
SELECT * FROM review WHERE task_id = $1 ORDER BY created_at DESC;
```

- [ ] **Step 6: Regenerate sqlc**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make sqlc
go build ./pkg/db/...
```

Expected: clean build for the generated package.

- [ ] **Step 7: Commit**

```bash
git add server/pkg/db/queries/task.sql server/pkg/db/queries/participant_slot.sql \
        server/pkg/db/queries/execution.sql server/pkg/db/queries/artifact.sql \
        server/pkg/db/queries/review.sql server/pkg/db/generated/
git commit -m "feat(sqlc): queries for task/slot/execution/artifact/review"
```

---

### Task 7: Modify existing sqlc queries — drop dead columns and add `thread_id`

**Files:**
- Modify: `server/pkg/db/queries/plans.sql`
- Modify: `server/pkg/db/queries/project_runs.sql`
- Modify: `server/pkg/db/queries/project_versions.sql`
- Modify: `server/pkg/db/queries/inbox.sql`

This task LANDS THE QUERY EDITS but does not yet drop the columns from the DB (Task 14 is destructive). The build will break in handlers — that is expected and fixed in Tasks 9-12.

- [ ] **Step 1: Edit `plans.sql`**

In `CreatePlan`, drop `steps` from the INSERT and add `thread_id`, `version_id`:

```sql
-- name: CreatePlan :one
INSERT INTO plan (
    workspace_id, title, description, source_type, source_ref_id,
    constraints, expected_output, task_brief, context_snapshot,
    thread_id, version_id, project_id, created_by
) VALUES (
    @workspace_id, @title, @description, @source_type, @source_ref_id,
    @constraints, @expected_output, @task_brief, @context_snapshot,
    @thread_id, @version_id, @project_id, @created_by
)
RETURNING *;
```

DELETE the `UpdatePlanSteps` query (the `steps` column is gone in Task 14).

Add a `GetPlanByThread` query:

```sql
-- name: GetPlanByThread :one
SELECT * FROM plan WHERE thread_id = $1 ORDER BY created_at DESC LIMIT 1;
```

- [ ] **Step 2: Edit `project_runs.sql`**

In `FailProjectRun`, drop the `retry_count = retry_count + 1` write:

```sql
-- name: FailProjectRun :exec
UPDATE project_run SET
    status = 'failed', end_at = NOW(),
    failure_reason = @failure_reason, updated_at = NOW()
WHERE id = @id;
```

Add `run_number` auto-numbered query:

```sql
-- name: NextRunNumber :one
SELECT COALESCE(MAX(run_number), 0) + 1 AS next_number FROM project_run WHERE plan_id = @plan_id;
```

- [ ] **Step 3: Edit `project_versions.sql`**

In `CreateProjectVersion`, drop `plan_snapshot` and `workflow_snapshot` from the INSERT column list and parameter list. The migration in Task 14 drops the columns; until then nullable defaults keep the row valid.

```sql
-- name: CreateProjectVersion :one
INSERT INTO project_version (
    project_id, parent_version_id, version_number,
    branch_name, fork_reason, created_by
) VALUES (
    @project_id, @parent_version_id, @version_number,
    @branch_name, @fork_reason, @created_by
)
RETURNING *;
```

- [ ] **Step 4: Edit `inbox.sql`**

Add `task_id`, `slot_id`, `plan_id` parameters to the `CreateInboxItem` query (or to the existing `CreateInboxItemForX` queries, whichever the file uses), plus a `ListInboxItemsByTask` and `ListInboxItemsByPlan`. Inspect the existing queries first; copy the existing INSERT structure and add the three new optional columns.

- [ ] **Step 5: Regenerate sqlc**

```bash
make sqlc
go build ./pkg/db/...
```

Expected: clean build for the generated package. Handler code will break — addressed in Tasks 9-12.

- [ ] **Step 6: Commit**

```bash
git add server/pkg/db/queries/plans.sql server/pkg/db/queries/project_runs.sql \
        server/pkg/db/queries/project_versions.sql server/pkg/db/queries/inbox.sql \
        server/pkg/db/generated/
git commit -m "refactor(sqlc): drop steps/snapshot/retry columns from plan/project queries; add thread_id/inbox project FKs"
```

---

### Task 8: `SlotService`, `ArtifactService`, `ReviewService`

**Files:**
- Create: `server/internal/service/slot.go`
- Create: `server/internal/service/slot_test.go`
- Create: `server/internal/service/artifact.go`
- Create: `server/internal/service/artifact_test.go`
- Create: `server/internal/service/review.go`
- Create: `server/internal/service/review_test.go`

- [ ] **Step 1: `SlotService` skeleton**

```go
// server/internal/service/slot.go
package service

import (
    "context"
    "fmt"

    "github.com/jackc/pgx/v5/pgtype"
    "github.com/multica-ai/multica/server/internal/events"
    "github.com/multica-ai/multica/server/internal/realtime"
    db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type SlotService struct {
    Queries *db.Queries
    Hub     *realtime.Hub
    Bus     *events.Bus
}

func NewSlotService(q *db.Queries, hub *realtime.Hub, bus *events.Bus) *SlotService {
    return &SlotService{Queries: q, Hub: hub, Bus: bus}
}

// Activate transitions a slot from waiting -> ready and updates the parent task
// (e.g. blocking human_input slot pushes Task to needs_human).
func (s *SlotService) Activate(ctx context.Context, slotID pgtype.UUID) error {
    slot, err := s.Queries.GetSlot(ctx, slotID)
    if err != nil { return fmt.Errorf("get slot: %w", err) }

    if err := s.Queries.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
        ID: slotID, Status: "ready",
    }); err != nil {
        return err
    }
    s.publishSlot("slot:activated", slot)
    return s.cascadeTaskFromSlot(ctx, slot, "ready")
}

// Submit transitions ready/in_progress -> submitted; cascades Task state.
func (s *SlotService) Submit(ctx context.Context, slotID pgtype.UUID) error {
    slot, err := s.Queries.GetSlot(ctx, slotID)
    if err != nil { return err }
    if err := s.Queries.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
        ID: slotID, Status: "submitted",
    }); err != nil { return err }
    s.publishSlot("slot:submitted", slot)
    return s.cascadeTaskFromSlot(ctx, slot, "submitted")
}

// Decide records a review-driven decision (approve/revision_requested/rejected).
func (s *SlotService) Decide(ctx context.Context, slotID pgtype.UUID, status string) error {
    if status != "approved" && status != "revision_requested" && status != "rejected" {
        return fmt.Errorf("invalid decision status: %s", status)
    }
    slot, err := s.Queries.GetSlot(ctx, slotID)
    if err != nil { return err }
    if err := s.Queries.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
        ID: slotID, Status: status,
    }); err != nil { return err }
    s.publishSlot("slot:decision", slot)
    return s.cascadeTaskFromSlot(ctx, slot, status)
}

// CheckTimeouts is called periodically (by SchedulerService) to expire stuck slots.
func (s *SlotService) CheckTimeouts(ctx context.Context) error {
    expired, err := s.Queries.ListExpiredSlots(ctx)
    if err != nil { return err }
    for _, slot := range expired {
        target := "expired"
        if !slot.Required {
            target = "skipped"
        }
        _ = s.Queries.UpdateSlotStatus(ctx, db.UpdateSlotStatusParams{
            ID: slot.ID, Status: target,
        })
        s.publishSlot("slot:expired", slot)
        _ = s.cascadeTaskFromSlot(ctx, slot, target)
    }
    return nil
}

// cascadeTaskFromSlot maps slot transitions onto Task status per PRD §4.6 table.
func (s *SlotService) cascadeTaskFromSlot(ctx context.Context, slot db.ParticipantSlot, slotStatus string) error {
    // Implementation: load task, switch on (slot.SlotType, slotStatus, slot.Trigger, slot.Blocking)
    // and call s.Queries.UpdateTaskStatus accordingly. See PRD §4.6 "Slot 状态与 Task 状态联动".
    // Errors here should be returned so callers can decide retry policy.
    return nil
}

func (s *SlotService) publishSlot(eventType string, slot db.ParticipantSlot) {
    if s.Bus == nil { return }
    s.Bus.Publish(events.Event{
        Type: eventType,
        Payload: map[string]any{
            "slot_id": slot.ID, "task_id": slot.TaskID,
            "slot_type": slot.SlotType, "status": slot.Status,
        },
    })
}
```

Implement `cascadeTaskFromSlot` per the PRD §4.6 table. Each branch loads the task, calls `UpdateTaskStatus`, and publishes `task:status_changed`.

- [ ] **Step 2: `ArtifactService` skeleton**

```go
// server/internal/service/artifact.go
package service

import (
    "context"
    "fmt"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgtype"
    db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type CreateArtifactWithFileReq struct {
    TaskID, SlotID, ExecutionID, RunID, WorkspaceID, ProjectID pgtype.UUID
    ArtifactType, Title, Summary, Path                          string
    Content                                                     []byte
    CreatedByID                                                 pgtype.UUID
    CreatedByType                                               string
    RetentionClass                                              string
}

type CreateHeadlessReq struct {
    TaskID, SlotID, ExecutionID, RunID pgtype.UUID
    ArtifactType, Title, Summary       string
    Content                            []byte // JSONB-marshalled content
    CreatedByID                        pgtype.UUID
    CreatedByType                      string
    RetentionClass                     string
}

type ArtifactService struct {
    Queries *db.Queries
    Pool    interface { // satisfied by *pgxpool.Pool
        Begin(ctx context.Context) (pgx.Tx, error)
    }
    FileService *FileService // existing service; injected from handler
}

func NewArtifactService(q *db.Queries, pool interface {
    Begin(ctx context.Context) (pgx.Tx, error)
}, fs *FileService) *ArtifactService {
    return &ArtifactService{Queries: q, Pool: pool, FileService: fs}
}

// CreateWithFile transactionally creates FileIndex+Snapshot and Artifact, validating access_scope.
func (s *ArtifactService) CreateWithFile(ctx context.Context, req CreateArtifactWithFileReq) (db.Artifact, error) {
    tx, err := s.Pool.Begin(ctx)
    if err != nil { return db.Artifact{}, err }
    defer tx.Rollback(ctx)

    qtx := s.Queries.WithTx(tx)

    // 1. Upsert FileIndex (access_scope='project', project_id matches)
    fi, err := s.FileService.UpsertProjectIndex(ctx, qtx, req.WorkspaceID, req.ProjectID, req.Path)
    if err != nil { return db.Artifact{}, fmt.Errorf("upsert file_index: %w", err) }

    // 2. Create FileSnapshot
    fs, err := s.FileService.CreateSnapshot(ctx, qtx, fi.ID, req.Content)
    if err != nil { return db.Artifact{}, fmt.Errorf("create snapshot: %w", err) }

    // 3. Determine next version
    next, err := qtx.GetLatestArtifactVersion(ctx, req.TaskID)
    if err != nil { return db.Artifact{}, err }

    // 4. Create artifact row
    art, err := qtx.CreateArtifact(ctx, db.CreateArtifactParams{
        TaskID:         req.TaskID,
        SlotID:         req.SlotID,
        ExecutionID:    req.ExecutionID,
        RunID:          req.RunID,
        ArtifactType:   req.ArtifactType,
        Version:        next + 1,
        Title:          req.Title,
        Summary:        req.Summary,
        FileIndexID:    fi.ID,
        FileSnapshotID: fs.ID,
        RetentionClass: req.RetentionClass,
        CreatedByID:    req.CreatedByID,
        CreatedByType:  req.CreatedByType,
    })
    if err != nil { return db.Artifact{}, err }

    if err := tx.Commit(ctx); err != nil { return db.Artifact{}, err }
    return art, nil
}

// CreateHeadless creates a content-only Artifact. Per the CHECK constraint,
// content must be non-NULL when file_index_id is NULL.
func (s *ArtifactService) CreateHeadless(ctx context.Context, req CreateHeadlessReq) (db.Artifact, error) {
    next, err := s.Queries.GetLatestArtifactVersion(ctx, req.TaskID)
    if err != nil { return db.Artifact{}, err }
    return s.Queries.CreateArtifact(ctx, db.CreateArtifactParams{
        TaskID:         req.TaskID,
        SlotID:         req.SlotID,
        ExecutionID:    req.ExecutionID,
        RunID:          req.RunID,
        ArtifactType:   req.ArtifactType,
        Version:        next + 1,
        Title:          req.Title,
        Summary:        req.Summary,
        Content:        req.Content,
        RetentionClass: req.RetentionClass,
        CreatedByID:    req.CreatedByID,
        CreatedByType:  req.CreatedByType,
    })
}
```

`FileService.UpsertProjectIndex` is a thin wrapper over the existing `file_index` queries. If your `FileService` does not yet have an `UpsertProjectIndex(ctx, tx, ws, project, path)` helper, add one inline next to the existing file-index code.

- [ ] **Step 3: `ReviewService` skeleton**

```go
// server/internal/service/review.go
package service

import (
    "context"

    "github.com/jackc/pgx/v5/pgtype"
    "github.com/multica-ai/multica/server/internal/events"
    "github.com/multica-ai/multica/server/internal/realtime"
    db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ReviewService struct {
    Queries *db.Queries
    Hub     *realtime.Hub
    Bus     *events.Bus
    Slots   *SlotService
}

func NewReviewService(q *db.Queries, hub *realtime.Hub, bus *events.Bus, slots *SlotService) *ReviewService {
    return &ReviewService{Queries: q, Hub: hub, Bus: bus, Slots: slots}
}

type CreateReviewReq struct {
    TaskID, ArtifactID, SlotID pgtype.UUID
    ReviewerID                 pgtype.UUID
    ReviewerType               string
    Decision                   string
    Comment                    string
}

// Create writes the review row, then cascades through SlotService to update the
// human_review slot and the task.
func (s *ReviewService) Create(ctx context.Context, req CreateReviewReq) (db.Review, error) {
    review, err := s.Queries.CreateReview(ctx, db.CreateReviewParams{
        TaskID: req.TaskID, ArtifactID: req.ArtifactID, SlotID: req.SlotID,
        ReviewerID: req.ReviewerID, ReviewerType: req.ReviewerType,
        Decision: req.Decision, Comment: req.Comment,
    })
    if err != nil { return db.Review{}, err }

    targetSlot := map[string]string{
        "approve": "approved", "request_changes": "revision_requested", "reject": "rejected",
    }[req.Decision]
    _ = s.Slots.Decide(ctx, req.SlotID, targetSlot)

    if s.Bus != nil {
        s.Bus.Publish(events.Event{Type: "review:submitted",
            Payload: map[string]any{"review_id": review.ID, "task_id": req.TaskID, "decision": req.Decision}})
    }
    return review, nil
}
```

- [ ] **Step 4: Tests**

Write minimal table-driven tests:

- `slot_test.go`: each PRD §4.6 row in the "Slot 状态与 Task 状态联动" table. Use a fake `db.Queries` (or a tiny in-memory implementation of just the calls used).
- `artifact_test.go`: `CreateHeadless` returns version 1 then 2 for the same task; `CreateWithFile` rolls back on FileIndex failure.
- `review_test.go`: each `decision -> slot status` mapping calls `SlotService.Decide` with the right value.

- [ ] **Step 5: Build + test**

```bash
cd server
go build ./internal/service/...
go test ./internal/service/ -run TestSlot -run TestArtifact -run TestReview
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/service/slot.go server/internal/service/slot_test.go \
        server/internal/service/artifact.go server/internal/service/artifact_test.go \
        server/internal/service/review.go server/internal/service/review_test.go
git commit -m "feat(service): SlotService, ArtifactService, ReviewService"
```

---

### Task 9: Rewrite `SchedulerService`

**Files:**
- Modify: `server/internal/service/scheduler.go`

The current `SchedulerService` is built around `workflow + workflow_step + agent_task_queue`. The rewrite swaps to `task + participant_slot + execution`, and removes all workflow surface.

- [ ] **Step 1: New top-level methods**

Replace the file's exported methods. Keep the `RetryRule / TimeoutRule / OwnerEscalationPolicy` helpers and their `Default*` factories — they stay valid for `task.timeout_rule` / `task.retry_rule` / `task.escalation_policy`.

```go
type SchedulerService struct {
    Queries *db.Queries
    Hub     *realtime.Hub
    Bus     *events.Bus
    Slots   *SlotService
}

func NewSchedulerService(q *db.Queries, hub *realtime.Hub, bus *events.Bus, slots *SlotService) *SchedulerService {
    return &SchedulerService{Queries: q, Hub: hub, Bus: bus, Slots: slots}
}

// ScheduleRun starts a Run by binding all tasks to it, then dispatching tasks
// with no unmet dependencies.
func (s *SchedulerService) ScheduleRun(ctx context.Context, planID, runID pgtype.UUID) error {
    if err := s.Queries.UpdateTaskRunBinding(ctx, db.UpdateTaskRunBindingParams{
        PlanID: planID, RunID: runID,
    }); err != nil {
        return fmt.Errorf("bind tasks to run: %w", err)
    }
    if err := s.Queries.StartProjectRun(ctx, runID); err != nil { return err }

    tasks, err := s.Queries.ListTasksByRun(ctx, runID)
    if err != nil { return err }
    for _, t := range tasks {
        if len(t.DependsOn) == 0 {
            // Slots also reset by trigger of UpdateTaskRunBinding (see below).
            _ = s.Queries.ResetSlotsForTask(ctx, t.ID)
            go s.ScheduleTask(ctx, t, runID)
        }
    }
    return nil
}

// ScheduleTask attempts to advance a task from draft/ready to a running execution
// (or to needs_human if a blocking before_execution human_input slot is present).
func (s *SchedulerService) ScheduleTask(ctx context.Context, t db.Task, runID pgtype.UUID) {
    // 1. Load before_execution slots.
    before, err := s.Queries.ListSlotsByTrigger(ctx, db.ListSlotsByTriggerParams{
        TaskID: t.ID, Trigger: "before_execution",
    })
    if err != nil { slog.Error("list before slots", "err", err); return }
    for _, sl := range before {
        if sl.SlotType == "human_input" && sl.Blocking {
            _ = s.Slots.Activate(ctx, sl.ID) // cascades Task -> needs_human
            return
        }
    }

    // 2. Find a usable agent (primary -> fallback).
    agent, runtime, ok := s.findAvailableAgent(ctx, t)
    if !ok {
        _ = s.Queries.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{ID: t.ID, Status: "needs_attention"})
        s.publishTaskStatus(t.ID, "needs_attention")
        return
    }

    _ = s.Queries.UpdateTaskActualAgent(ctx, db.UpdateTaskActualAgentParams{ID: t.ID, ActualAgentID: agent.ID})

    payload, _ := json.Marshal(map[string]any{
        "title": t.Title, "description": t.Description,
        "input_context_refs": t.InputContextRefs,
        "acceptance_criteria": t.AcceptanceCriteria,
    })
    exec, err := s.Queries.CreateExecution(ctx, db.CreateExecutionParams{
        TaskID: t.ID, RunID: runID, AgentID: agent.ID, RuntimeID: runtime.ID,
        Attempt: int32(t.CurrentRetry + 1), Priority: 50, Payload: payload,
        LogRetentionPolicy: "90d",
    })
    if err != nil {
        _ = s.Queries.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{ID: t.ID, Status: "needs_attention"})
        return
    }
    _ = s.Queries.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{ID: t.ID, Status: "queued"})
    s.publishExecution("execution:created", exec)
    s.publishTaskStatus(t.ID, "queued")
}

// HandleTaskCompletion is called when an Execution returns success.
// Creates an Artifact (if result indicates one), activates review slot (if any),
// otherwise transitions task -> completed and walks downstream.
func (s *SchedulerService) HandleTaskCompletion(ctx context.Context, taskID pgtype.UUID, result []byte) error {
    t, err := s.Queries.GetTask(ctx, taskID)
    if err != nil { return err }
    _ = s.Queries.UpdateTaskResult(ctx, db.UpdateTaskResultParams{ID: taskID, Result: result})

    // Find before_done slots (review).
    reviews, err := s.Queries.ListSlotsByTrigger(ctx, db.ListSlotsByTriggerParams{
        TaskID: taskID, Trigger: "before_done",
    })
    if err != nil { return err }
    var reviewSlot *db.ParticipantSlot
    for i, sl := range reviews {
        if sl.SlotType == "human_review" && sl.Blocking {
            reviewSlot = &reviews[i]
            break
        }
    }
    if reviewSlot != nil {
        if err := s.Slots.Activate(ctx, reviewSlot.ID); err != nil { return err }
        _ = s.Queries.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{ID: taskID, Status: "under_review"})
        s.publishTaskStatus(taskID, "under_review")
        return nil
    }

    _ = s.Queries.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{ID: taskID, Status: "completed"})
    s.publishTaskStatus(taskID, "completed")
    return s.scheduleDownstream(ctx, t)
}

// HandleTaskFailure runs the retry/fallback ladder; ends in needs_attention if exhausted.
func (s *SchedulerService) HandleTaskFailure(ctx context.Context, taskID pgtype.UUID, errMsg string) error {
    t, err := s.Queries.GetTask(ctx, taskID)
    if err != nil { return err }
    rule, _ := decodeRetryRule(t.RetryRule) // helper that decodes JSONB into RetryRule
    if int(t.CurrentRetry) < rule.MaxRetries {
        _ = s.Queries.IncrementTaskRetry(ctx, taskID)
        time.AfterFunc(time.Duration(rule.RetryDelaySeconds)*time.Second, func() {
            s.ScheduleTask(context.Background(), t, t.RunID)
        })
        return nil
    }
    if len(t.FallbackAgentIds) > 0 {
        _ = s.Queries.ResetTaskRetry(ctx, taskID)
        // findAvailableAgent will pick from fallback list.
        s.ScheduleTask(ctx, t, t.RunID)
        return nil
    }
    _ = s.Queries.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{ID: taskID, Status: "needs_attention"})
    s.publishTaskStatus(taskID, "needs_attention")
    return nil
}

// HandleTaskTimeout is invoked by the timeout watcher.
func (s *SchedulerService) HandleTaskTimeout(ctx context.Context, taskID pgtype.UUID) error {
    return s.HandleTaskFailure(ctx, taskID, "timed_out")
}

// scheduleDownstream walks tasks that depend on the just-completed task and
// schedules any whose dependencies are now satisfied. Also detects whole-Run
// completion.
func (s *SchedulerService) scheduleDownstream(ctx context.Context, completed db.Task) error {
    downstream, err := s.Queries.ListDownstreamTasks(ctx, db.ListDownstreamTasksParams{
        PlanID: completed.PlanID, UpstreamID: completed.ID,
    })
    if err != nil { return err }
    for _, d := range downstream {
        if s.allDependenciesMet(ctx, d) {
            _ = s.Queries.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{ID: d.ID, Status: "ready"})
            go s.ScheduleTask(ctx, d, completed.RunID)
        }
    }
    return s.checkRunCompletion(ctx, completed.RunID)
}

func (s *SchedulerService) findAvailableAgent(ctx context.Context, t db.Task) (db.Agent, db.AgentRuntime, bool) {
    // Order: primary_assignee_id then fallback_agent_ids.
    // For each candidate, load agent + runtime; allow if:
    //   agent.status IN ('idle','busy')
    //   runtime.status = 'online'
    //   runtime.current_load < runtime.concurrency_limit (if both fields populated by Plan 4)
    // Returns the first usable pair.
    return db.Agent{}, db.AgentRuntime{}, false // implement
}
```

Helpers (`decodeRetryRule`, `allDependenciesMet`, `publishTaskStatus`, `publishExecution`, `checkRunCompletion`) follow the same pattern as the existing `scheduler.go`. Replace every reference to `workflow_step`, `agent_task_queue`, and `RetryCount on project_run` with the new equivalents.

- [ ] **Step 2: Drop dead code**

Remove every method whose name starts with `ScheduleWorkflow`, `ScheduleStep`, `HandleStepCompletion`, `HandleStepFailure`, `HandleStepTimeout`, `checkWorkflowFailure`, `findAvailableAgentForStep`. Their replacements are above.

Stop calling `agent_task_queue` queries from `scheduler.go`. The Issue link continues to use `TaskService` independently.

- [ ] **Step 3: Build**

```bash
cd server
go build ./internal/service/scheduler.go
```

If the file pulls in symbols from `workflow.go`, fix those references in this commit. Handler-side cleanup is Task 10.

- [ ] **Step 4: Commit**

```bash
git add server/internal/service/scheduler.go
git commit -m "refactor(scheduler): rewrite around task/slot/execution"
```

---

### Task 10: Update `PlanGeneratorService` to emit Task + Slot rows

**Files:**
- Modify: `server/internal/service/plan_generator.go`

- [ ] **Step 1: New output types**

Replace `PlanStep` with task-and-slot pairs:

```go
type GeneratedSlot struct {
    SlotType        string `json:"slot_type"`
    SlotOrder       int    `json:"slot_order"`
    ParticipantType string `json:"participant_type"`
    ParticipantID   string `json:"participant_id,omitempty"`
    Trigger         string `json:"trigger"`
    Blocking        bool   `json:"blocking"`
    Required        bool   `json:"required"`
    Responsibility  string `json:"responsibility"`
    ExpectedOutput  string `json:"expected_output"`
}

type GeneratedTask struct {
    Title              string          `json:"title"`
    Description        string          `json:"description"`
    StepOrder          int             `json:"step_order"`
    DependsOnIndices   []int           `json:"depends_on_indices"` // resolved to UUIDs after insert
    PrimaryAssigneeID  string          `json:"primary_assignee_id,omitempty"`
    FallbackAgentIDs   []string        `json:"fallback_agent_ids,omitempty"`
    RequiredSkills     []string        `json:"required_skills"`
    CollaborationMode  string          `json:"collaboration_mode"`
    AcceptanceCriteria string          `json:"acceptance_criteria"`
    Slots              []GeneratedSlot `json:"slots"`
}

type GeneratedPlan struct {
    Title       string          `json:"title"`
    Description string          `json:"description"`
    TaskBrief   string          `json:"task_brief"`
    Constraints string          `json:"constraints"`
    Tasks       []GeneratedTask `json:"tasks"`
}
```

- [ ] **Step 2: Update `GeneratePlanFromText` and `GeneratePlanFromThread`**

Update the LLM prompt to produce the new schema (PRD §6.2). Drop the old `Steps []PlanStep` field. Strip any "old PlanStep" handling.

The fallback path (no LLM) should produce a single task with one `agent_execution` slot:

```go
return &GeneratedPlan{
    Title: truncate(input, 60), Description: input,
    Tasks: []GeneratedTask{{
        Title: input, StepOrder: 1, RequiredSkills: []string{},
        CollaborationMode: "agent_exec_human_review",
        Slots: []GeneratedSlot{
            {SlotType: "agent_execution", SlotOrder: 1, ParticipantType: "agent",
                Trigger: "during_execution", Blocking: true, Required: true},
            {SlotType: "human_review", SlotOrder: 2, ParticipantType: "member",
                Trigger: "before_done", Blocking: true, Required: true},
        },
    }},
}, nil
```

- [ ] **Step 3: Persistence helper**

Add `PersistPlan(ctx, planID, runIDOptional, gp *GeneratedPlan)` that:

1. Inserts Task rows in `step_order` ascending, capturing the returned IDs.
2. Resolves `DependsOnIndices` → UUIDs against the captured ID slice.
3. Updates each task's `depends_on` (a follow-up `UPDATE task SET depends_on=...`; add a `SetTaskDependsOn` query if not present).
4. Inserts Slot rows.

- [ ] **Step 4: Build**

```bash
cd server
go build ./internal/service/plan_generator.go
```

- [ ] **Step 5: Commit**

```bash
git add server/internal/service/plan_generator.go
git commit -m "feat(plan-generator): emit task+slot rows"
```

---

### Task 11: HTTP handlers for task / slot / artifact / review / execution

**Files:**
- Create: `server/internal/handler/task.go`
- Create: `server/internal/handler/slot.go`
- Create: `server/internal/handler/artifact.go`
- Create: `server/internal/handler/review.go`
- Create: `server/internal/handler/execution.go`
- Modify: `server/internal/handler/handler.go` — wire `Slots`, `Artifacts`, `Reviews` services

- [ ] **Step 1: Wire services into `Handler`**

```go
type Handler struct {
    // existing fields...
    Slots     *service.SlotService
    Artifacts *service.ArtifactService
    Reviews   *service.ReviewService
}
```

In `New()`, construct them after `TaskService`:

```go
h.Slots = service.NewSlotService(queries, hub, bus)
h.Reviews = service.NewReviewService(queries, hub, bus, h.Slots)
// ArtifactService needs the pool for transactions; pass txStarter (which is *pgxpool.Pool in production).
h.Artifacts = service.NewArtifactService(queries, txStarter, fileService /* existing */)
```

- [ ] **Step 2: `task.go` endpoints**

| Method | Path | Action |
|---|---|---|
| `GET` | `/api/projects/{projectId}/tasks` | List by run (active) |
| `GET` | `/api/plans/{planId}/tasks` | List by plan |
| `GET` | `/api/tasks/{id}` | Get one |
| `PATCH` | `/api/tasks/{id}` | Update planning fields (admin or plan creator only) |
| `POST` | `/api/tasks/{id}/cancel` | Cancel |

Each handler reads the workspace via `resolveWorkspaceID`, loads the task with `h.Queries.GetTask`, verifies the workspace matches, then maps the row through a `taskToResponse(db.Task) TaskResponse` helper.

- [ ] **Step 3: `slot.go` endpoints**

| Method | Path | Action |
|---|---|---|
| `GET` | `/api/tasks/{taskId}/slots` | List slots |
| `POST` | `/api/slots/{id}/submit` | Mark `submitted` (used by human_input slot) — body: optional payload reference |
| `POST` | `/api/slots/{id}/skip` | Owner-skip an optional slot |

`POST /submit` calls `h.Slots.Submit(ctx, slotID)`.

- [ ] **Step 4: `artifact.go` endpoints**

| Method | Path | Action |
|---|---|---|
| `GET` | `/api/tasks/{taskId}/artifacts` | List versions |
| `GET` | `/api/artifacts/{id}` | Get one |
| `POST` | `/api/tasks/{taskId}/artifacts` | Create headless or file artifact (multipart for file) |

For `POST` with a `file_index_id` body, call `h.Artifacts.CreateWithFile`. Otherwise call `h.Artifacts.CreateHeadless`.

- [ ] **Step 5: `review.go` endpoints**

| Method | Path | Action |
|---|---|---|
| `POST` | `/api/artifacts/{artifactId}/reviews` | Body: `{slot_id, decision, comment}` |
| `GET` | `/api/tasks/{taskId}/reviews` | List for a task |

`POST` calls `h.Reviews.Create`.

- [ ] **Step 6: `execution.go` endpoints (REST)**

| Method | Path | Action |
|---|---|---|
| `GET` | `/api/tasks/{taskId}/executions` | List attempts |
| `GET` | `/api/executions/{id}` | Get one |
| `POST` | `/api/executions/{id}/cancel` | Cancel |

- [ ] **Step 7: `execution.go` endpoints (Daemon side)**

| Method | Path | Action |
|---|---|---|
| `GET` | `/api/daemon/runtimes/{runtimeId}/executions/pending` | List queued for runtime |
| `POST` | `/api/daemon/runtimes/{runtimeId}/executions/claim` | `ClaimExecution`; body provides `{daemon_id, working_dir}` to populate `context_ref` |
| `POST` | `/api/daemon/executions/{id}/start` | `StartExecution`; optional `context_ref` update |
| `POST` | `/api/daemon/executions/{id}/progress` | Stream progress; broadcasts `execution:progress` only |
| `POST` | `/api/daemon/executions/{id}/complete` | `CompleteExecution`; calls `Scheduler.HandleTaskCompletion` |
| `POST` | `/api/daemon/executions/{id}/fail` | `FailExecution`; calls `Scheduler.HandleTaskFailure` |
| `POST` | `/api/daemon/executions/{id}/messages` | Append messages (uses existing `task_message` table) |

The claim handler must compose `context_ref` for `local` mode:

```go
contextRef, _ := json.Marshal(map[string]any{
    "mode":        "local",
    "working_dir": req.WorkingDir,
    "daemon_id":   req.DaemonID,
})
exec, err := h.Queries.ClaimExecution(r.Context(), db.ClaimExecutionParams{
    RuntimeID:  parseUUID(runtimeID),
    ContextRef: contextRef,
})
```

These run side-by-side with the existing `/api/daemon/runtimes/{id}/tasks/*` endpoints, which continue to serve the Issue link.

- [ ] **Step 8: Wire routes**

In `cmd/server/router.go`, register the new routes inside the protected and daemon route groups. Drop any `workflow` routes (Task 12).

- [ ] **Step 9: Build + test**

```bash
cd server
go build ./...
go test ./internal/handler/ -run TestExecution -run TestTask -run TestArtifact -run TestSlot -run TestReview
```

- [ ] **Step 10: Commit**

```bash
git add server/internal/handler/task.go server/internal/handler/slot.go \
        server/internal/handler/artifact.go server/internal/handler/review.go \
        server/internal/handler/execution.go server/internal/handler/handler.go \
        server/cmd/server/router.go
git commit -m "feat(handler): task/slot/artifact/review/execution REST + daemon endpoints"
```

---

### Task 12: Update `CloudExecutorService` to drive `execution`

**Files:**
- Modify: `server/internal/service/cloud_executor.go`

- [ ] **Step 1: Add execution poll loop**

Keep the existing `agent_task_queue` poll loop (it serves the Issue link). Add a parallel loop that claims `execution` rows for cloud runtimes:

```go
func (s *CloudExecutorService) pollExecutions(ctx context.Context) {
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            s.claimAndRunOne(ctx)
        }
    }
}

func (s *CloudExecutorService) claimAndRunOne(ctx context.Context) {
    ctxRef, _ := json.Marshal(map[string]any{"mode": "cloud"}) // sdk fields filled after allocate.
    exec, err := s.Queries.ClaimCloudExecution(ctx, db.ClaimCloudExecutionParams{ContextRef: ctxRef})
    if err != nil {
        if !errors.Is(err, pgx.ErrNoRows) { slog.Error("claim cloud exec", "err", err) }
        return
    }

    // Allocate SDK session + sandbox (stubs are acceptable for this plan; wire to AgentSDK later).
    sessionID := uuid.NewString()
    sandboxID := uuid.NewString()
    virtualPath := "/workspace/" + uuidToString(exec.RunID)

    updated, _ := json.Marshal(map[string]any{
        "mode": "cloud",
        "sdk_session_id": sessionID,
        "sandbox_id": sandboxID,
        "virtual_project_path": virtualPath,
    })
    _ = s.Queries.UpdateExecutionContextRef(ctx, db.UpdateExecutionContextRefParams{
        ID: exec.ID, ContextRef: updated,
    })
    _ = s.Queries.StartExecution(ctx, exec.ID)
    s.publish("execution:started", exec)

    // Run the agent. Defer the task-completion callback to the existing TaskService
    // pipeline by emitting events the Scheduler subscribes to.
    // Implementation sketch only; details depend on AgentSDK wiring.
}
```

- [ ] **Step 2: Subscribe scheduler to completion events**

Inside `SchedulerService` (Task 9), subscribe to `execution:completed` / `execution:failed` events and route to `HandleTaskCompletion` / `HandleTaskFailure`.

- [ ] **Step 3: Start the new loop in `Start`**

```go
go s.pollExecutions(ctx)
```

- [ ] **Step 4: Commit**

```bash
git add server/internal/service/cloud_executor.go server/internal/service/scheduler.go
git commit -m "feat(cloud-executor): claim execution rows and write context_ref"
```

---

### Task 13: Frontend — types, stores, pages

**Files:**
- Modify: `apps/web/shared/types/index.ts`
- Create: `apps/web/features/projects/task/{store.ts,index.ts,components/}`
- Create: `apps/web/features/projects/slot/`
- Create: `apps/web/features/projects/execution/`
- Create: `apps/web/features/projects/artifact/`
- Create: `apps/web/features/projects/review/`
- Modify: `apps/web/features/projects/index.ts`
- Modify: `apps/web/features/projects/store.ts`
- Modify: `apps/web/features/inbox/store.ts`
- Create: `apps/web/app/(dashboard)/projects/[id]/page.tsx`
- Create: `apps/web/app/(dashboard)/projects/[id]/plans/[planId]/page.tsx`
- Create: `apps/web/app/(dashboard)/projects/[id]/tasks/[taskId]/page.tsx`
- DELETE: `apps/web/features/workflow/`

- [ ] **Step 1: Add new types in `shared/types/index.ts`**

```typescript
export type TaskStatus =
  | "draft" | "ready" | "queued" | "assigned" | "running"
  | "needs_human" | "under_review" | "needs_attention"
  | "completed" | "failed" | "cancelled";

export type CollaborationMode =
  | "agent_exec_human_review"
  | "human_input_agent_exec"
  | "agent_prepare_human_action"
  | "mixed";

export interface Task {
  id: string;
  plan_id: string;
  run_id: string | null;
  workspace_id: string;
  title: string;
  description: string | null;
  step_order: number;
  depends_on: string[];
  primary_assignee_id: string | null;
  fallback_agent_ids: string[];
  required_skills: string[];
  collaboration_mode: CollaborationMode;
  acceptance_criteria: string | null;
  status: TaskStatus;
  actual_agent_id: string | null;
  current_retry: number;
  started_at: string | null;
  completed_at: string | null;
  result: unknown;
  error: string | null;
  timeout_rule: { max_duration_seconds: number; action: string };
  retry_rule: { max_retries: number; retry_delay_seconds: number };
  escalation_policy: { escalate_after_seconds: number };
  input_context_refs: unknown;
  output_refs: unknown;
  created_at: string;
  updated_at: string;
}

export type SlotType = "human_input" | "agent_execution" | "human_review";
export type SlotTrigger = "before_execution" | "during_execution" | "before_done";
export type SlotStatus =
  | "waiting" | "ready" | "in_progress" | "submitted"
  | "approved" | "revision_requested" | "rejected"
  | "expired" | "skipped";

export interface ParticipantSlot {
  id: string;
  task_id: string;
  slot_type: SlotType;
  slot_order: number;
  participant_id: string | null;
  participant_type: "member" | "agent";
  responsibility: string | null;
  trigger: SlotTrigger;
  blocking: boolean;
  required: boolean;
  expected_output: string | null;
  status: SlotStatus;
  timeout_seconds: number | null;
  started_at: string | null;
  completed_at: string | null;
}

export type ExecutionStatus =
  | "queued" | "claimed" | "running" | "completed"
  | "failed" | "cancelled" | "timed_out";

export interface Execution {
  id: string;
  task_id: string;
  run_id: string;
  slot_id: string | null;
  agent_id: string;
  runtime_id: string | null;
  attempt: number;
  status: ExecutionStatus;
  priority: number;
  payload: unknown;
  result: unknown;
  error: string | null;
  context_ref: Record<string, unknown>;
  log_retention_policy: "7d" | "30d" | "90d" | "permanent";
  logs_expires_at: string | null;
  claimed_at: string | null;
  started_at: string | null;
  completed_at: string | null;
}

export type ArtifactType =
  "document" | "design" | "code_patch" | "report" | "file" | "plan_doc";

export interface Artifact {
  id: string;
  task_id: string;
  slot_id: string | null;
  execution_id: string | null;
  run_id: string;
  artifact_type: ArtifactType;
  version: number;
  title: string | null;
  summary: string | null;
  content: unknown;
  file_index_id: string | null;
  file_snapshot_id: string | null;
  retention_class: "permanent" | "ttl" | "temp";
  created_by_id: string | null;
  created_by_type: "member" | "agent";
  created_at: string;
}

export type ReviewDecision = "approve" | "request_changes" | "reject";

export interface Review {
  id: string;
  task_id: string;
  artifact_id: string;
  slot_id: string;
  reviewer_id: string;
  reviewer_type: "member" | "agent";
  decision: ReviewDecision;
  comment: string | null;
  created_at: string;
}
```

Also add WS event types to whatever file declares them (`shared/types/realtime.ts` or similar):

```typescript
export type ProjectWSEvent =
  | { type: "task:status_changed"; task_id: string; status: TaskStatus }
  | { type: "slot:activated" | "slot:submitted" | "slot:decision"; slot_id: string; task_id: string; status: SlotStatus }
  | { type: "execution:claimed" | "execution:started" | "execution:completed" | "execution:failed" | "execution:progress"; execution_id: string; task_id: string }
  | { type: "artifact:created"; artifact_id: string; task_id: string }
  | { type: "review:submitted"; review_id: string; task_id: string; decision: ReviewDecision }
  | { type: "plan:approval_changed"; plan_id: string; status: string }
  | { type: "run:started" | "run:completed" | "run:failed" | "run:cancelled"; run_id: string };
```

- [ ] **Step 2: Stores under `features/projects/{task,slot,execution,artifact,review}/store.ts`**

Each follows the pattern of the existing `features/projects/store.ts` (Zustand). Minimum surface:

`task/store.ts`:
```typescript
interface TaskState { tasksByPlan: Record<string, Task[]>; loading: boolean; }
interface TaskActions {
  fetchByPlan(planId: string): Promise<void>;
  fetchOne(id: string): Promise<Task>;
  cancel(id: string): Promise<void>;
}
```

`slot/store.ts`:
```typescript
interface SlotState { slotsByTask: Record<string, ParticipantSlot[]>; }
interface SlotActions {
  fetchByTask(taskId: string): Promise<void>;
  submit(slotId: string): Promise<void>;
  skip(slotId: string): Promise<void>;
}
```

`execution/store.ts`, `artifact/store.ts`, `review/store.ts` follow the same pattern.

- [ ] **Step 3: Update `features/projects/store.ts`**

Drop reads of `plan_snapshot` / `workflow_snapshot` (gone in 058). The current store imports `Project, ProjectVersion, ProjectRun` only — fine. Remove any code that reads `runs[].retry_count` (also gone in 058).

Update `features/projects/index.ts` to re-export the new sub-modules:

```typescript
export * from "./store";
export * from "./task";
export * from "./slot";
export * from "./execution";
export * from "./artifact";
export * from "./review";
```

- [ ] **Step 4: Pages**

`/projects/[id]/page.tsx` — list of plans for the project + a flat task list per active run, grouped by plan. Use a simple `<Card>` + `<Table>` from shadcn. Show task status with a badge.

`/projects/[id]/plans/[planId]/page.tsx` — task list ordered by `step_order`. Show DAG dependencies as text ("depends on: Task 1, Task 3"). Allow inline status badge.

`/projects/[id]/tasks/[taskId]/page.tsx` — three sections:
1. Slot timeline (vertical list, ordered by `slot_order`, badge per status).
2. Artifact list (newest version first, link to file or render content).
3. Review panel (button row: Approve / Request changes / Reject) — only enabled if there is an active `human_review` slot in `ready` and the current user is the slot's `participant_id`.

This is the LIST view per "Out of Scope" — DAG graph rendering is post-MVP.

- [ ] **Step 5: Inbox handlers**

In `features/inbox/store.ts`, extend the type discriminator and rendering for the three new types:

```typescript
type InboxItemType =
  | "human_input_needed"
  | "review_needed"
  | "task_attention_needed"
  | "plan_approval_needed"
  | "run_completed"
  | "run_failed"
  | /* existing types */;
```

Each renders a CTA that deep-links to the right URL (`/projects/{project_id}/tasks/{task_id}` for the first three).

- [ ] **Step 6: Delete `features/workflow/`**

```bash
rm -rf apps/web/features/workflow
```

Search-and-fix any imports:

```bash
grep -rln "features/workflow" apps/web
```

Replace each import with the matching `features/projects/*` module.

- [ ] **Step 7: Typecheck**

```bash
pnpm typecheck
```

If you can run it. Otherwise eye-check.

- [ ] **Step 8: Commit**

```bash
git add apps/web/shared/types/ apps/web/features/projects/ apps/web/app/\(dashboard\)/projects/ apps/web/features/inbox/store.ts
git rm -r apps/web/features/workflow
git commit -m "feat(web): project task/slot/execution/artifact/review modules + pages"
```

---

### Task 14: Migration 058 — DESTRUCTIVE drops

**Files:**
- Create: `server/migrations/058_drop_legacy.up.sql`
- Create: `server/migrations/058_drop_legacy.down.sql`
- Delete: `server/pkg/db/queries/workflows.sql`
- Delete: `server/internal/handler/workflow.go`

**WARNING — DESTRUCTIVE — apply last.** This task drops `workflow`, `workflow_step`, `plan.steps`, `project_version.{plan_snapshot, workflow_snapshot}`, and `project_run.retry_count`. Apply ONLY after Tasks 7-13 land and the build is clean.

Per the plan's assumption: live database has no production rows in `workflow / workflow_step`. If your worktree DB has rows, they will be deleted. That is by design.

- [ ] **Step 1: Write the up migration**

```sql
-- Project Plan 5 - destructive drops.
-- Pre-conditions: callers updated to read task/execution/etc. instead.

-- workflow + workflow_step gone (job moved to plan + task).
DROP TABLE IF EXISTS workflow_step CASCADE;
DROP TABLE IF EXISTS workflow CASCADE;

-- agent_task_queue still serves Issue link, but the FK to workflow_step is gone.
ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS workflow_step_id;

-- plan: drop the steps JSONB.
ALTER TABLE plan
    DROP COLUMN IF EXISTS steps;

-- project_version: drop snapshots.
ALTER TABLE project_version
    DROP COLUMN IF EXISTS plan_snapshot,
    DROP COLUMN IF EXISTS workflow_snapshot;

-- project_run: retry_count moved to task.current_retry + execution.attempt.
ALTER TABLE project_run
    DROP COLUMN IF EXISTS retry_count;
```

- [ ] **Step 2: Write the down migration**

Best-effort restore (data lost):

```sql
ALTER TABLE plan ADD COLUMN IF NOT EXISTS steps JSONB DEFAULT '[]';
ALTER TABLE project_version
    ADD COLUMN IF NOT EXISTS plan_snapshot JSONB,
    ADD COLUMN IF NOT EXISTS workflow_snapshot JSONB;
ALTER TABLE project_run ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS workflow_step_id UUID;
-- workflow / workflow_step tables not restored automatically — they were full schemas.
```

- [ ] **Step 3: Delete the workflow query and handler files**

```bash
rm server/pkg/db/queries/workflows.sql
rm server/internal/handler/workflow.go
```

Re-run `make sqlc` so the generated code drops the workflow types.

- [ ] **Step 4: Drop `workflow` route registrations in `cmd/server/router.go`** (if they were not already removed in Task 11).

- [ ] **Step 5: Apply + round-trip**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make migrate-up
make migrate-down  # restores best-effort columns; tables stay gone.
make migrate-up    # re-applies cleanly.
```

- [ ] **Step 6: Final build**

```bash
cd server
go build ./...
```

Fix any remaining references to the dropped types (likely none if Tasks 9 and 11 were complete).

- [ ] **Step 7: Commit**

```bash
git add server/migrations/058_drop_legacy.up.sql server/migrations/058_drop_legacy.down.sql \
        server/pkg/db/generated/ server/cmd/server/router.go
git rm server/pkg/db/queries/workflows.sql server/internal/handler/workflow.go
git commit -m "feat(db): drop legacy workflow/steps/snapshots (DESTRUCTIVE)

Applied after task/slot/execution callers replaced workflow surface."
```

---

### Task 15: WebSocket events + activity_log entries

**Files:**
- Modify: `server/internal/service/scheduler.go` (already publishes; verify all events)
- Modify: `server/internal/service/slot.go`, `artifact.go`, `review.go` (verify publishes)
- Modify: `server/internal/service/audit.go` (subscribe to new events, write `activity_log`)

- [ ] **Step 1: Audit publish call sites**

For each event in PRD §10.4, confirm publication:

| Event | Publisher |
|---|---|
| `task:status_changed` | `SchedulerService.publishTaskStatus`, `SlotService.cascadeTaskFromSlot` |
| `slot:activated` / `slot:submitted` / `slot:decision` | `SlotService.publishSlot` |
| `execution:claimed` / `started` / `completed` / `failed` / `progress` | `execution.go` daemon endpoints + `cloud_executor.go` |
| `artifact:created` | `artifact.go` POST handler |
| `review:submitted` | `ReviewService.Create` |
| `plan:approval_changed` | `plan.go` approval handler |
| `run:started` / `completed` / `failed` / `cancelled` | `project_runs` flow in `project.go` |

- [ ] **Step 2: Audit subscriptions**

`audit.go` subscribes to `events.Bus` and writes `activity_log`. Add subscriptions for:

```go
bus.Subscribe("task:status_changed", a.onTaskStatusChanged)
bus.Subscribe("slot:activated", a.onSlotEvent)
bus.Subscribe("slot:submitted", a.onSlotEvent)
bus.Subscribe("slot:decision", a.onSlotEvent)
bus.Subscribe("artifact:created", a.onArtifactCreated)
bus.Subscribe("review:submitted", a.onReviewSubmitted)
```

Each handler builds an `activity_log` row with `event_type`, `related_project_id`, `related_run_id`, `actor_*`, and a payload JSONB. Plan 4 should have already added the `event_type` enum values needed.

- [ ] **Step 3: Hub broadcasts**

Verify `realtime.Hub.Broadcast` receives the same events for WS subscribers. The existing `Hub` already broadcasts whatever the bus publishes — confirm by `grep -n "Subscribe" internal/realtime/hub.go`.

- [ ] **Step 4: Build + test**

```bash
cd server
go build ./...
go test ./internal/service/ -run TestScheduler -run TestAudit
```

- [ ] **Step 5: Commit**

```bash
git add server/internal/service/audit.go server/internal/service/scheduler.go \
        server/internal/service/slot.go server/internal/service/artifact.go \
        server/internal/service/review.go server/internal/handler/execution.go \
        server/internal/handler/artifact.go server/internal/handler/review.go
git commit -m "feat(events): publish project events to bus + activity_log"
```

---

### Task 16: Final verification

- [ ] **Step 1: Full migration round-trip**

```bash
cd /Users/chauncey2025/Documents/GitHub/MyTeam/.claude/worktrees/suspicious-gauss-d2c691
make migrate-down  # to before 053
make migrate-up    # all five
make migrate-down
make migrate-up
```

- [ ] **Step 2: `make check`**

```bash
make check
```

This runs `pnpm typecheck`, `pnpm test`, `go test ./...`, and Playwright E2E. Note pre-existing failures separately and re-run individual targets if needed:

```bash
pnpm typecheck
pnpm test
make test
```

- [ ] **Step 3: Smoke test the new daemon endpoints**

Boot the server and post against `/api/daemon/runtimes/.../executions/pending` with no claimed work, confirming an empty array. Insert a fake `execution` row directly via psql, claim it, then start/complete via the new endpoints. Confirm `task.status` advances and a `task:status_changed` event broadcasts on the WS hub.

- [ ] **Step 4: Smoke test the UI**

`pnpm dev:web` and walk through:
1. Create a project from a chat thread.
2. Generate a Plan; confirm the LLM produces tasks with slots.
3. Approve the plan; start a Run.
4. Confirm the task list shows status transitions in real time.
5. Create a headless Artifact via the API; confirm it appears on the task page.
6. Approve the Artifact via the Review panel; confirm task moves to `completed`.

- [ ] **Step 5: Commit any final fixes**

```bash
git add -A
git commit -m "fix(plan5): final adjustments after smoke tests"
```

---

## Self-Review Checklist

- [ ] All five new tables (`task`, `participant_slot`, `execution`, `artifact`, `review`) created with the CHECK constraints listed in PRD §4.
- [ ] `idx_execution_claim` includes `WHERE status='queued'` predicate.
- [ ] `artifact_headless_or_file` CHECK enforces headless rule.
- [ ] `plan.thread_id` FK references `thread(id)`.
- [ ] `inbox_item` extended with `task_id`, `slot_id`, `plan_id`.
- [ ] sqlc queries exist for: create/get/list/update on each new table.
- [ ] `SchedulerService` does not reference `workflow`, `workflow_step`, or `agent_task_queue` for the Project link.
- [ ] `PlanGeneratorService` produces `Tasks []GeneratedTask` (each with `Slots []GeneratedSlot`).
- [ ] `SlotService.cascadeTaskFromSlot` covers every row in PRD §4.6 "Slot 状态与 Task 状态联动".
- [ ] `ArtifactService.CreateWithFile` uses a transaction and rolls back on error.
- [ ] `ReviewService.Create` maps `decision → slot status` per PRD §4.9.
- [ ] Daemon endpoints `/api/daemon/runtimes/{id}/executions/{pending,claim}` and `/api/daemon/executions/{id}/{start,progress,complete,fail,messages}` exist alongside the legacy `/tasks/*` endpoints.
- [ ] Claim handlers populate `context_ref` per PRD §4.7 schemas (local vs cloud).
- [ ] `CloudExecutorService.pollExecutions` claims via `ClaimCloudExecution` with `FOR UPDATE SKIP LOCKED`.
- [ ] WS events from PRD §10.4 each have a publish call.
- [ ] `audit.go` writes `activity_log` for the new events.
- [ ] Migration 058 drops `workflow`, `workflow_step`, `plan.steps`, `project_version.{plan_snapshot, workflow_snapshot}`, `project_run.retry_count`.
- [ ] Migration 058 is marked DESTRUCTIVE — apply last and only after callers updated.
- [ ] `apps/web/features/workflow/` deleted; no `features/workflow` imports remain.
- [ ] `apps/web/features/projects/{task,slot,execution,artifact,review}/` populated with stores and components.
- [ ] Three new pages exist: project DAG list, plan detail, task detail.
- [ ] Inbox handles `human_input_needed`, `review_needed`, `task_attention_needed`.
- [ ] No "TBD", "TODO", or "see above" markers in completed code.
- [ ] No tasks reference tables not yet created earlier in the plan.
- [ ] `make check` passes (or only pre-existing failures remain).

---

## Out of Scope

These are explicitly deferred per PRD §12 and the plan's user instructions:

- **Cross-Owner Agent sharing.** Plan-internal Agent assignment is restricted to the Plan creator's Owner. Multi-owner sharing is post-MVP.
- **Real-time DAG editor UI.** Task list view only. Visual DAG renderer is post-MVP.
- **Multi-person review chains.** A `human_review` slot has a single approver; multi-step review (legal → engineering → owner) is post-MVP.
- **Artifact turn-based multi-editor.** Artifacts are written by one Agent at a time; concurrent artifact editing is post-MVP.
- **Recurring / scheduled Plan auto-generation.** Plans are created manually for now; cron-driven Plan creation is post-MVP.
- **`workflow` → `task` data migration.** Live database has no `workflow` / `workflow_step` rows. Migration 058 drops the tables outright. If a worktree DB has rows, they are dropped. (Stated assumption — verify with `psql` before applying 058 in a non-worktree environment.)
- **Background log GC.** `log_retention_policy` and `logs_expires_at` are populated but the cleanup job that honours them is post-MVP per PRD §11.4.
- **`access_scope` enforcement audit.** `ArtifactService.CreateWithFile` validates `access_scope='project'` and `project_id` alignment, but a full RBAC sweep across `file_index` is in scope for the Account / RBAC plans, not this one.
- **System Agent driving reviews.** Reviews are member-only at MVP. The `reviewer_type='agent'` enum exists for future automation.
