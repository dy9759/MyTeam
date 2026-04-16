# ProjectLinear -- Full Design Spec

## Overview

ProjectLinear is the project management page for Multica, modeled after GitHub + Linear. It manages task briefs, plans, versions, execution, branching, and results. Each project has an embedded "ProjectLinear system agent" responsible for plan generation, execution orchestration, scheduling, exception handling, and owner notification.

## Terminology Mapping (Spec -> Existing Schema)

The user spec defines a 5-layer hierarchy: `repo -> branch -> version -> run -> result`. We map this onto the existing schema incrementally:

| Spec Term | DB Entity | Notes |
|-----------|-----------|-------|
| repo | `project` | Already exists. Top-level container. |
| branch | `project_branch` (NEW) | Currently `branch_name` on `project_version`; promote to first-class entity. |
| version | `project_version` | Already exists. Add `branch_id` FK, enforce immutability. |
| run | `project_run` | Already exists. Expand status enum. |
| result | `project_result` (NEW) | New table for run outputs/artifacts. |

Additional entities needed:
- `project_share` (NEW) -- Permission sharing (viewer/editor)
- `project_pr` (NEW) -- Pull request for branch merging
- `project_context` (NEW) -- Imported context snapshots from chat/channel/thread

---

## Phase 1: Core Completion

### 1.1 Database Migrations

**Migration 045: project_branch + schema alignment**

New table `project_branch`:
```sql
CREATE TABLE project_branch (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  parent_branch_id UUID REFERENCES project_branch(id),
  is_default BOOLEAN NOT NULL DEFAULT FALSE,
  status TEXT NOT NULL DEFAULT 'active', -- active, merged, archived
  created_by UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(project_id, name)
);
```

New table `project_result`:
```sql
CREATE TABLE project_result (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES project_run(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  version_id UUID REFERENCES project_version(id),
  summary TEXT,
  artifacts JSONB DEFAULT '[]',
  deliverables JSONB DEFAULT '[]',
  acceptance_status TEXT NOT NULL DEFAULT 'pending', -- pending, accepted, rejected
  accepted_by UUID,
  accepted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Alter existing tables:
```sql
-- project: add new columns + update status enum
ALTER TABLE project ADD COLUMN IF NOT EXISTS default_branch_id UUID REFERENCES project_branch(id);
ALTER TABLE project ADD COLUMN IF NOT EXISTS max_runs INTEGER;
ALTER TABLE project ADD COLUMN IF NOT EXISTS end_time TIMESTAMPTZ;
ALTER TABLE project ADD COLUMN IF NOT EXISTS consecutive_failure_threshold INTEGER DEFAULT 3;
ALTER TABLE project ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ; -- for scheduled_once projects
ALTER TABLE project ADD COLUMN IF NOT EXISTS plan_visibility TEXT NOT NULL DEFAULT 'owner_only'; -- owner_only, shared

-- project_version: add branch_id
ALTER TABLE project_version ADD COLUMN IF NOT EXISTS branch_id UUID REFERENCES project_branch(id);

-- workflow_step: add sub-task fields from spec
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS title TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS goal TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS priority TEXT DEFAULT 'medium';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS candidate_agent_ids UUID[] DEFAULT '{}';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS owner_reviewer_id UUID;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS context_md_path TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS instruction_md_path TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS worktree_path TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS expected_outputs JSONB DEFAULT '[]';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS actual_outputs JSONB DEFAULT '[]';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS skippable BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS requires_human_review BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS acceptance_checks JSONB DEFAULT '[]';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS done_definition TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS error_code TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS error_summary TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS on_failure TEXT DEFAULT 'block';
```

### 1.2 Backend Handler Completion

Complete the stubbed handlers in `project.go`:

- **GetProject** -- Query `project` by ID, join latest `plan` + active `project_run`, return full `ProjectResponse`
- **UpdateProject** -- Validate status transitions, call `UpdateProject` sqlc query, broadcast event
- **DeleteProject** -- Soft-delete or hard-delete, broadcast event
- **ForkProject** -- Load current project + plan + workflow, snapshot them into a new `project_version` with `plan_snapshot` and `workflow_snapshot` JSONB
- **ListProjectVersions** -- Query `project_version` by `project_id` ordered by `version_number DESC`
- **GetProjectRuns** -- Query `project_run` by `project_id` ordered by `created_at DESC`

Add new handlers:
- **GetProjectBranches** -- List branches for a project
- **CreateProjectBranch** -- Create a new branch (fork from existing)
- **GetProjectResult** -- Get result for a specific run
- **StartProjectExecution** -- Create `project_run`, transition project to `running`, call `ScheduleWorkflow`

New sqlc queries needed:
- `ListProjectBranches(project_id)` 
- `CreateProjectBranch(id, project_id, name, parent_branch_id, is_default, created_by)`
- `CreateProjectResult(id, run_id, project_id, version_id)`
- `GetProjectResult(run_id)`
- `UpdateProjectResult(id, summary, artifacts, deliverables, acceptance_status)`

### 1.3 Status Enum Alignment

**Project status** -- Expand valid values:
- Current: `not_started, running, paused, completed, failed, archived`
- Add: `draft, scheduled, stopped`
- Transition map:
  - `draft` -> `scheduled, running, archived`
  - `scheduled` -> `running, paused, archived`
  - `running` -> `paused, completed, failed, stopped`
  - `paused` -> `running, stopped, archived`
  - `completed` -> `archived`
  - `failed` -> `draft, archived`
  - `stopped` -> `draft, archived`
  - `archived` -> (terminal)

**Project schedule_type** -- Add `scheduled_once`:
- `one_time, scheduled_once, recurring`

**ProjectRun status** -- Expand:
- Current: `pending, running, completed, failed`
- Add: `queued, blocked, paused, success, partial_success, cancelled`
- Rename: `completed` -> `success` (migration update existing rows)

**ProjectVersion status** -- Expand:
- Current: `active, archived`
- Add: `ready, running, completed, failed, cancelled`

**WorkflowStep status** -- Already mostly aligned. Ensure `skipped` is added.

### 1.4 Frontend: Project Detail Page

Enhance `apps/web/app/(dashboard)/projects/[id]/page.tsx` with:

**Header section:**
- Project title (editable inline)
- Status badge with color
- Schedule type indicator
- Action buttons: Start Execution, Pause, Archive

**Tab navigation:**
- Overview (task brief + plan summary)
- Plan (plan editor with steps)
- Execution (live DAG + step cards)
- Versions (version tree)
- Files (project files from file index)
- Settings (schedule, permissions)

**Overview tab:**
- Task brief display (structured fields)
- Plan summary with step count, agent count
- Active run status (if any)
- Recent activity timeline

**Plan tab:**
- Existing `plan-editor.tsx` component
- Approval status banner (draft/approved/rejected)
- Approve/Reject buttons for project owner

**Execution tab:**
- DAG visualization showing step dependencies
- `execution-step-card.tsx` for each step
- Real-time status updates via WebSocket
- Run history selector

**Versions tab:**
- Existing `version-tree.tsx` enhanced with branch grouping
- Fork button per version
- Version detail panel (plan snapshot, workflow snapshot)

**Files tab:**
- Reuse file list component from existing file infrastructure
- Filter by version/run

### 1.5 TypeScript Type Updates

Update `apps/web/shared/types/project.ts`:
- Add `ProjectBranch` interface
- Add `ProjectResult` interface
- Expand `ProjectStatus` with `draft`, `scheduled`, `stopped`
- Add `scheduled_once` to `ProjectScheduleType`
- Expand `RunStatus` with `queued`, `blocked`, `success`, `partial_success`

Update `apps/web/shared/types/workflow.ts`:
- Add `title`, `goal`, `priority`, `on_failure`, `skippable`, `done_definition` etc. to `WorkflowStep`
- Add `skipped` to `WorkflowStepStatus`

### 1.6 API Client Updates

Add to `apps/web/shared/api/client.ts`:
- `listProjectBranches(projectId)`
- `createProjectBranch(projectId, data)`
- `getProjectResult(projectId, runId)`

Add to `packages/client-core/src/desktop-api-client.ts`:
- All project methods (currently only `listProjects` and `getProject` exist)

### 1.7 Store Updates

Add to `apps/web/features/projects/store.ts`:
- `branches: ProjectBranch[]`
- `currentResult: ProjectResult | null`
- `fetchBranches(projectId)`
- `createBranch(projectId, name, parentBranchId)`
- `startExecution(projectId)`
- `fetchResult(projectId, runId)`

---

## Phase 2: Task Brief & Execution

### 2.1 Structured Task Brief

Restructure `plan.task_brief` from TEXT to JSONB with these fields:

```typescript
interface TaskBrief {
  goal: string;                    // Project objective
  background: string;             // Context and motivation  
  referenced_files: FileRef[];    // Attached files
  constraints: string[];          // Limitations and rules
  participant_scope: string;      // Who can participate
  deliverables: Deliverable[];    // Expected outputs
  acceptance_criteria: string[];  // How to verify completion
  timeline: string;               // Time requirements
}

interface FileRef {
  file_id: string;
  file_name: string;
  description?: string;
}

interface Deliverable {
  name: string;
  description: string;
  type: 'code' | 'document' | 'file' | 'report' | 'other';
}
```

**Migration:** `ALTER TABLE plan ALTER COLUMN task_brief TYPE JSONB USING task_brief::jsonb`

### 2.2 Context Import from Chat/Channel/Thread

New table `project_context`:
```sql
CREATE TABLE project_context (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  version_id UUID REFERENCES project_version(id),
  source_type TEXT NOT NULL, -- channel, dm, thread
  source_id UUID NOT NULL,
  source_name TEXT,
  message_range_start TIMESTAMPTZ,
  message_range_end TIMESTAMPTZ,
  snapshot_md TEXT NOT NULL, -- Full markdown snapshot
  message_count INTEGER NOT NULL DEFAULT 0,
  imported_by UUID NOT NULL,
  imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**Backend endpoint:** `POST /api/projects/{projectID}/import-context`
- Request: `{ source_type, source_id, date_from?, date_to? }`
- Fetches messages from the source within date range
- Formats as markdown snapshot
- Saves to `project_context` table
- Returns the context record

**Frontend:** "Import Context" button in project overview that opens a dialog to:
1. Select source type (channel/DM/thread)
2. Pick the specific channel/conversation/thread
3. Optional date range filter
4. Preview the snapshot
5. Confirm import

### 2.3 Plan Approval Flow

The backend already has `ApprovePlan` and `RejectPlan` handlers. Frontend needs:

- Approval banner on plan tab showing current status (draft/approved/rejected)
- "Approve Plan" button -- calls `POST /api/projects/{projectID}/approve`
- "Reject Plan" button with reason textarea -- calls `POST /api/projects/{projectID}/reject`
- After approval, "Start Execution" button becomes active
- WebSocket event `plan:approved` / `plan:rejected` for real-time updates

### 2.4 Execution Engine Completion

The `SchedulerService` exists but needs completion:

**Stuck detection** -- Add a periodic monitoring goroutine:
```go
func (s *SchedulerService) MonitorActiveSteps(ctx context.Context) {
  // Every 30 seconds, check all running steps:
  // 1. If step exceeds timeout_rule.max_duration_seconds -> HandleStepTimeout
  // 2. If step has no heartbeat for 2x timeout -> mark as stuck
  // 3. If agent is offline -> trigger failure
}
```

**Auto-handling cascade** (from spec 3.10.3):
```
1. retry (if retries remaining)
2. reassign (try candidate_agent_ids)  
3. skip (if skippable=true)
4. pause_and_notify_owner
```

**on_failure strategies** -- Implement in `HandleStepFailure`:
- `block` -- Block downstream steps, fail workflow
- `retry_once` -- Retry once then block
- `retry_n` -- Retry N times (from retry_rule.max_retries)
- `reassign_then_retry` -- Try candidate agents, then retry
- `skip` -- Mark skipped, unblock dependents (if skippable)
- `pause_and_notify_owner` -- Pause run, send inbox item to owner

**Dependency resolution** -- When a step completes:
1. Find all steps where `depends_on` includes this step
2. For each dependent, check if ALL dependencies are now `completed` or `skipped`
3. If yes, transition to `ready` then schedule

**Owner notification** -- When stuck/failed:
1. Create `inbox_item` with `action_required=true`, `action_type='agent_stuck'`
2. Send chat message to project channel
3. Set `deadline` for owner response
4. If no response by deadline, pause the run

### 2.5 Frontend Execution Dashboard

**DAG Visualization** (`apps/web/features/projects/components/dag-view.tsx`):
- Render workflow steps as nodes in a directed graph
- Color-code by status (gray=pending, blue=running, green=success, red=failed, orange=retrying, yellow=blocked)
- Draw edges for `depends_on` relationships
- Animate running steps (pulse effect)
- Click a node to expand step detail card

**Step Detail Card** (enhance `execution-step-card.tsx`):
- Agent name + avatar
- Status with duration
- Input/output refs as clickable links
- Error message (if failed)
- Action buttons: Retry, Replace Agent, Skip (if skippable)
- Log viewer for step_logs

**Run Controls:**
- Pause/Resume run
- Cancel run
- Retry failed steps (bulk)

---

## Phase 3: Branching & PR

### 3.1 New Table: project_pr

```sql
CREATE TABLE project_pr (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  source_branch_id UUID NOT NULL REFERENCES project_branch(id),
  target_branch_id UUID NOT NULL REFERENCES project_branch(id),
  source_version_id UUID NOT NULL REFERENCES project_version(id),
  title TEXT NOT NULL,
  description TEXT,
  status TEXT NOT NULL DEFAULT 'open', -- open, merged, closed, needs_review
  has_conflicts BOOLEAN NOT NULL DEFAULT FALSE,
  merged_version_id UUID REFERENCES project_version(id), -- version created on merge
  created_by UUID NOT NULL,
  merged_by UUID,
  merged_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 3.2 PR Content

A PR packages changes from source branch for review:
- Plan file diffs (plan_snapshot comparison)
- Context md files
- Code/document/file artifacts from runs
- Result summaries

### 3.3 Backend Endpoints

New routes under `/api/projects/{projectID}`:
- `POST /prs` -- Create PR (source_branch_id, target_branch_id, source_version_id, title, description)
- `GET /prs` -- List PRs for project
- `GET /prs/{prID}` -- Get PR detail with diff
- `PATCH /prs/{prID}` -- Update PR (title, description)
- `POST /prs/{prID}/merge` -- Merge PR (creates new version on target branch)
- `POST /prs/{prID}/close` -- Close PR without merge

**Merge logic:**
1. Load source version's plan_snapshot + workflow_snapshot
2. Load target branch's HEAD version
3. Check for conflicts (same field modified in both)
4. If no conflicts: create new `project_version` on target branch with merged snapshots
5. If conflicts: set `has_conflicts=true`, return conflict details, require owner resolution
6. V1: no auto-merge; owner resolves conflicts manually via UI

### 3.4 Fork Flow

`POST /api/projects/{projectID}/fork`:
1. Load current project + latest version on specified branch
2. Create new `project_branch` with `parent_branch_id`
3. Create new `project_version` on the new branch, copying plan_snapshot + workflow_snapshot
4. Return the new branch + version

### 3.5 Frontend: Version Tree Enhancement

Enhance `version-tree.tsx`:
- Group versions by branch
- Show branch relationships (fork arrows)
- PR indicators on branches (open PRs badge)
- Fork button on any version
- Create PR button on non-default branches

**PR Dialog** (`apps/web/features/projects/components/pr-dialog.tsx`):
- Source/target branch selectors
- Title + description
- Diff preview (plan changes, file changes)
- Submit button

**PR Review Page** (`apps/web/features/projects/components/pr-review.tsx`):
- Side-by-side diff of plan snapshots
- File change list
- Merge/Close buttons
- Conflict resolution UI (v1: manual text editing)

---

## Phase 4: Permissions & Sharing

### 4.1 New Table: project_share

```sql
CREATE TABLE project_share (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  owner_id UUID NOT NULL,
  role TEXT NOT NULL DEFAULT 'viewer', -- viewer, editor
  can_merge_pr BOOLEAN NOT NULL DEFAULT FALSE,
  granted_by UUID NOT NULL,
  granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(project_id, owner_id)
);
```

### 4.2 Permission Model

**Project Owner** (creator_owner_id):
- Full control: create/edit/delete project, branches, versions
- Start/pause/cancel runs
- Replace agents
- Share project, manage permissions
- Create/review/merge PRs
- Archive project

**Shared Owner (viewer)**:
- View project, authorized plans, files, results
- Participate in project channel
- Cannot: modify plans, start runs, merge PRs

**Shared Owner (editor)**:
- Everything viewer can do
- Modify branch draft plans
- Create PRs
- Review PRs
- Cannot (by default): merge PRs, modify permissions, delete/archive

**Assigned Agent**:
- View own assigned tasks
- View completed files and code
- View context needed for own tasks
- Write to own worktree and artifact directories
- Cannot: view full global plan (unless authorized), modify permissions, merge PRs

**System Agent**:
- Generate task briefs and plan drafts
- Schedule runs
- Auto retry/reassign/notify
- Generate PR drafts
- Cannot: directly merge PRs, modify owner permissions

### 4.3 Backend: Permission Middleware

Add permission check middleware for project routes:
```go
func (h *Handler) requireProjectAccess(minRole string) func(http.Handler) http.Handler
```

- Checks `project.creator_owner_id == currentUser` (full access)
- Or checks `project_share` for role >= minRole
- Routes guarded: all `/api/projects/{projectID}/*` routes

### 4.4 Backend Endpoints

- `POST /api/projects/{projectID}/share` -- Share with owner (owner_id, role)
- `DELETE /api/projects/{projectID}/share/{ownerID}` -- Remove share
- `GET /api/projects/{projectID}/shares` -- List shares
- `PATCH /api/projects/{projectID}/share/{ownerID}` -- Update role/can_merge_pr

### 4.5 Frontend: Sharing Dialog

`apps/web/features/projects/components/share-dialog.tsx`:
- Search/select owners to share with
- Role picker (viewer/editor)
- Toggle: can merge PRs
- List current shares with remove button

### 4.6 Plan Visibility

- Project owner controls whether full plan is visible to shared owners
- Add `plan_visibility` column to `project`: `owner_only` (default) or `shared`
- Agents see only their assigned tasks via filtered queries
- Agents always see completed files and code (actual_outputs)

---

## Phase 5: Recurring & Scheduling

### 5.1 Scheduling Infrastructure

The system agent handles scheduling. Implementation:

**Cron Trigger Service** (`server/internal/service/project_scheduler.go`):
```go
type ProjectSchedulerService struct {
  queries  *db.Queries
  scheduler *SchedulerService
  ticker    *time.Ticker
}

func (s *ProjectSchedulerService) Start(ctx context.Context) {
  // Every 60 seconds:
  // 1. List projects with schedule_type in (scheduled_once, recurring) and status = scheduled
  // 2. For recurring: check cron_expr against current time
  // 3. For scheduled_once: check if scheduled time has passed
  // 4. If trigger: create new version from HEAD of default branch, create run, start execution
}
```

### 5.2 Recurring Project Rules

On each trigger:
1. Get latest version from default branch HEAD
2. Create new `project_version` (snapshot current plan + workflow)
3. Create new `project_run` linked to that version
4. Check all assigned agents are online
5. If critical agents offline: skip this trigger, notify owner
6. If agents available: start execution via `ScheduleWorkflow`

**Stop conditions** (checked before each trigger):
- `end_time` reached -> transition to `stopped`
- `max_runs` reached -> transition to `stopped`
- Consecutive failures >= `consecutive_failure_threshold` -> transition to `stopped`, notify owner

### 5.3 Scheduled Once

- Project has a `scheduled_at` timestamp
- On trigger: create version + run, execute, then transition to `completed` when done
- If execution fails, transition to `failed` (not `completed`)

### 5.4 Frontend: Schedule Settings

In project settings tab:
- Schedule type picker (one_time / scheduled_once / recurring)
- For scheduled_once: date/time picker
- For recurring: cron expression builder (presets: daily, weekly, monthly + custom)
- End time picker (optional)
- Max runs input (optional)
- Failure threshold input (default: 3)

---

## Phase 6: Desktop App Support

### 6.1 Desktop API Client

Add all project methods to `packages/client-core/src/desktop-api-client.ts`:
- `listProjects()`, `getProject(id)`, `createProject(data)`
- `createProjectFromChat(data)`, `updateProject(id, data)`, `deleteProject(id)`
- `listProjectBranches(projectId)`, `createProjectBranch(projectId, data)`
- `listProjectVersions(projectId)`, `listProjectRuns(projectId)`
- `getProjectResult(projectId, runId)`
- `startProjectExecution(projectId)`
- `approvePlan(projectId)`, `rejectPlan(projectId, reason)`
- `listProjectPRs(projectId)`, `createProjectPR(projectId, data)`, `mergeProjectPR(projectId, prId)`
- `listProjectShares(projectId)`, `shareProject(projectId, data)`

### 6.2 Desktop Project Route

New route `apps/desktop/src/routes/project-route.tsx`:
- Project list sidebar (same pattern as session-route)
- Project detail panel with tabs (overview, plan, execution, versions, files)
- Simplified UI compared to web (no PR review, no sharing management -- those stay web-only for v1)

### 6.3 Desktop Store

Zustand store for desktop project state:
- Projects list, current project, versions, runs, branches
- CRUD actions mirroring web store
- WebSocket sync for real-time execution updates

---

## Cross-Cutting Concerns

### WebSocket Events

New events to add to `server/pkg/protocol/events.go`:
```
project:branch_created
project:version_created
project:run_started
project:run_completed
project:run_failed
project:result_created
project:pr_created
project:pr_merged
project:pr_closed
project:shared
project:context_imported
workflow:step:stuck
workflow:step:skipped
workflow:step:reassigned
```

### Project <-> Session Relationship

Per spec 3.13:
- Each project auto-creates a channel (already implemented in `createProjectChannel`)
- Channel messages do NOT auto-enter project context
- Explicit "Import to project context" action required
- Fork creates a new thread in the project channel for branch discussion

### Project <-> File Relationship

Per spec 3.16:
- Project files (context, plans, results, artifacts) are indexed in `file_index` table
- `file_index` already has `source_type` and `source_id` columns
- Use `source_type='project'` + `source_id=project_id` for project files
- Additional metadata: version_id, run_id, generating_agent_id stored in file_index metadata

### Project <-> Account Relationship

Per spec 3.16:
- Completed projects displayed on owner profile/account page
- Only successful projects with results count
- Cross-owner collaborative projects shown on both profiles

---

## Implementation Order

1. **Phase 1** (Core): Migrations, handler completion, status alignment, basic project detail page
2. **Phase 2** (Task Brief & Execution): Structured task brief, context import, approval flow, execution engine, DAG view
3. **Phase 3** (Branching & PR): project_branch promotion, PR CRUD, merge logic, version tree UI
4. **Phase 4** (Permissions): project_share, permission middleware, sharing UI, plan visibility
5. **Phase 5** (Scheduling): ProjectSchedulerService, recurring triggers, stop conditions, schedule UI
6. **Phase 6** (Desktop): API client methods, desktop route, desktop store

Each phase is independently deployable and testable. Later phases build on earlier ones but each produces a working increment.
