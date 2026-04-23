# ProjectLinear Phase 2: Task Brief & Execution Engine

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement structured task briefs, context import from chat/channel/thread, plan approval UI, and complete the execution engine with stuck detection, auto-retry/reassign/skip cascade, and owner notification.

**Architecture:** Task brief becomes structured JSONB on the plan table. Context import snapshots conversation messages into a new project_context table. The SchedulerService gets a monitoring goroutine for stuck detection and implements the on_failure cascade. Frontend gets a DAG visualization component and execution dashboard.

**Tech Stack:** Go 1.26 (Chi router, sqlc, pgx), PostgreSQL 17, Next.js 16, TypeScript, Zustand, Tailwind CSS

**Depends on:** Phase 1 (migration 045, handler wiring)

---

## File Structure

### Backend
- **Create:** `server/migrations/046_task_brief_context.up.sql`
- **Create:** `server/migrations/046_task_brief_context.down.sql`
- **Create:** `server/pkg/db/queries/project_contexts.sql`
- **Create:** `server/internal/handler/project_context.go` -- Context import handler
- **Modify:** `server/pkg/db/queries/plans.sql` -- Update task_brief type
- **Modify:** `server/internal/service/scheduler.go` -- Stuck detection, on_failure cascade
- **Modify:** `server/internal/handler/workflow.go` -- StartWorkflow creates project_run
- **Modify:** `server/cmd/server/router.go` -- Context import route

### Frontend
- **Create:** `apps/web/features/projects/components/dag-view.tsx`
- **Create:** `apps/web/features/projects/components/context-import-dialog.tsx`
- **Create:** `apps/web/features/projects/components/task-brief-editor.tsx`
- **Modify:** `apps/web/shared/types/project.ts` -- Add ProjectContext, TaskBrief types
- **Modify:** `apps/web/shared/api/client.ts` -- Add context import method
- **Modify:** `apps/web/features/projects/store.ts` -- Add context state
- **Modify:** `apps/web/app/(dashboard)/projects/[id]/page.tsx` -- Wire DAG view, approval UI

---

### Task 1: Migration -- project_context table + task_brief JSONB

**Files:**
- Create: `server/migrations/046_task_brief_context.up.sql`
- Create: `server/migrations/046_task_brief_context.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- server/migrations/046_task_brief_context.up.sql

CREATE TABLE project_context (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  version_id UUID REFERENCES project_version(id),
  source_type TEXT NOT NULL,
  source_id UUID NOT NULL,
  source_name TEXT,
  message_range_start TIMESTAMPTZ,
  message_range_end TIMESTAMPTZ,
  snapshot_md TEXT NOT NULL,
  message_count INTEGER NOT NULL DEFAULT 0,
  imported_by UUID NOT NULL,
  imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_project_context_project ON project_context(project_id);

-- Convert task_brief from TEXT to JSONB
ALTER TABLE plan ALTER COLUMN task_brief TYPE JSONB USING
  CASE
    WHEN task_brief IS NULL THEN NULL
    WHEN task_brief::text = '' THEN NULL
    ELSE jsonb_build_object('goal', task_brief::text)
  END;

ALTER TABLE plan ALTER COLUMN task_brief SET DEFAULT '{}';
```

- [ ] **Step 2: Write the down migration**

```sql
-- server/migrations/046_task_brief_context.down.sql

ALTER TABLE plan ALTER COLUMN task_brief TYPE TEXT USING task_brief::text;
ALTER TABLE plan ALTER COLUMN task_brief SET DEFAULT NULL;
DROP TABLE IF EXISTS project_context;
```

- [ ] **Step 3: Run migration**

Run: `cd server && make migrate-up`
Expected: Migration 046 applies successfully.

- [ ] **Step 4: Commit**

```bash
git add server/migrations/046_task_brief_context.up.sql server/migrations/046_task_brief_context.down.sql
git commit -m "feat(server): add project_context table, convert task_brief to JSONB (migration 046)"
```

---

### Task 2: SQLC Queries for project_context

**Files:**
- Create: `server/pkg/db/queries/project_contexts.sql`

- [ ] **Step 1: Write queries**

```sql
-- server/pkg/db/queries/project_contexts.sql

-- name: CreateProjectContext :one
INSERT INTO project_context (project_id, version_id, source_type, source_id, source_name, message_range_start, message_range_end, snapshot_md, message_count, imported_by)
VALUES (@project_id, @version_id, @source_type, @source_id, @source_name, @message_range_start, @message_range_end, @snapshot_md, @message_count, @imported_by)
RETURNING *;

-- name: ListProjectContexts :many
SELECT * FROM project_context WHERE project_id = @project_id ORDER BY imported_at DESC;

-- name: GetProjectContext :one
SELECT * FROM project_context WHERE id = @id;

-- name: DeleteProjectContext :exec
DELETE FROM project_context WHERE id = @id;
```

- [ ] **Step 2: Regenerate sqlc**

Run: `cd server && make sqlc`
Expected: New file `project_contexts.sql.go` generated. Models updated with `ProjectContext` struct.

- [ ] **Step 3: Verify build**

Run: `cd server && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add server/pkg/db/queries/project_contexts.sql server/pkg/db/generated/
git commit -m "feat(server): add sqlc queries for project_context"
```

---

### Task 3: Context Import Handler

**Files:**
- Create: `server/internal/handler/project_context.go`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Write the handler**

```go
// server/internal/handler/project_context.go
package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ImportContextRequest is the request body for importing context into a project.
type ImportContextRequest struct {
	SourceType string  `json:"source_type"` // channel, dm, thread
	SourceID   string  `json:"source_id"`
	DateFrom   *string `json:"date_from"` // RFC3339
	DateTo     *string `json:"date_to"`   // RFC3339
}

// ImportProjectContext handles POST /api/projects/{projectID}/import-context
func (h *Handler) ImportProjectContext(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	var req ImportContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.SourceType == "" || req.SourceID == "" {
		writeError(w, http.StatusBadRequest, "source_type and source_id are required")
		return
	}

	switch req.SourceType {
	case "channel", "dm", "thread":
	default:
		writeError(w, http.StatusBadRequest, "source_type must be channel, dm, or thread")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Fetch messages based on source type
	var messages []db.Message
	var sourceName string
	var fetchErr error

	sourceUUID := parseUUID(req.SourceID)

	switch req.SourceType {
	case "channel":
		ch, err := h.Queries.GetChannel(r.Context(), sourceUUID)
		if err != nil {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		sourceName = ch.Name
		messages, fetchErr = h.Queries.ListMessagesByChannel(r.Context(), db.ListMessagesByChannelParams{
			ChannelID: sourceUUID,
			Limit:     500,
			Offset:    0,
		})
	case "dm":
		messages, fetchErr = h.Queries.ListMessagesByRecipient(r.Context(), db.ListMessagesByRecipientParams{
			SenderID:    parseUUID(userID),
			RecipientID: sourceUUID,
			Limit:       500,
			Offset:      0,
		})
		sourceName = "DM"
	case "thread":
		messages, fetchErr = h.Queries.ListThreadMessages(r.Context(), sourceUUID)
		sourceName = "Thread"
	}

	if fetchErr != nil {
		slog.Error("failed to fetch messages for context", "error", fetchErr)
		writeError(w, http.StatusInternalServerError, "failed to fetch messages")
		return
	}

	// Filter by date range if provided
	if req.DateFrom != nil || req.DateTo != nil {
		filtered := make([]db.Message, 0, len(messages))
		for _, m := range messages {
			if req.DateFrom != nil {
				from, _ := time.Parse(time.RFC3339, *req.DateFrom)
				if m.CreatedAt.Time.Before(from) {
					continue
				}
			}
			if req.DateTo != nil {
				to, _ := time.Parse(time.RFC3339, *req.DateTo)
				if m.CreatedAt.Time.After(to) {
					continue
				}
			}
			filtered = append(filtered, m)
		}
		messages = filtered
	}

	// Format messages as markdown snapshot
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Context Import: %s (%s)\n\n", sourceName, req.SourceType))
	sb.WriteString(fmt.Sprintf("Imported at: %s\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Messages: %d\n\n---\n\n", len(messages)))

	for _, m := range messages {
		content := m.Content
		if len(content) > 1000 {
			content = content[:1000] + "..."
		}
		sb.WriteString(fmt.Sprintf("**%s** (%s):\n%s\n\n",
			uuidToString(m.SenderID),
			m.CreatedAt.Time.Format("2006-01-02 15:04"),
			content,
		))
	}

	// Parse date range for DB storage
	var rangeStart, rangeEnd pgtype.Timestamptz
	if req.DateFrom != nil {
		if t, err := time.Parse(time.RFC3339, *req.DateFrom); err == nil {
			rangeStart = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}
	if req.DateTo != nil {
		if t, err := time.Parse(time.RFC3339, *req.DateTo); err == nil {
			rangeEnd = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}

	ctx, err := h.Queries.CreateProjectContext(r.Context(), db.CreateProjectContextParams{
		ProjectID:         parseUUID(projectID),
		SourceType:        req.SourceType,
		SourceID:          sourceUUID,
		SourceName:        strToText(sourceName),
		MessageRangeStart: rangeStart,
		MessageRangeEnd:   rangeEnd,
		SnapshotMd:        sb.String(),
		MessageCount:      int32(len(messages)),
		ImportedBy:        parseUUID(userID),
	})
	if err != nil {
		slog.Error("failed to save project context", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to import context")
		return
	}

	type ContextResponse struct {
		ID           string  `json:"id"`
		ProjectID    string  `json:"project_id"`
		SourceType   string  `json:"source_type"`
		SourceName   *string `json:"source_name"`
		MessageCount int32   `json:"message_count"`
		ImportedAt   string  `json:"imported_at"`
	}

	writeJSON(w, http.StatusCreated, ContextResponse{
		ID:           uuidToString(ctx.ID),
		ProjectID:    projectID,
		SourceType:   ctx.SourceType,
		SourceName:   textToPtr(ctx.SourceName),
		MessageCount: ctx.MessageCount,
		ImportedAt:   ctx.ImportedAt.Time.Format(time.RFC3339),
	})
}

// ListProjectContexts handles GET /api/projects/{projectID}/contexts
func (h *Handler) ListProjectContexts(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	contexts, err := h.Queries.ListProjectContexts(r.Context(), parseUUID(projectID))
	if err != nil {
		slog.Error("list contexts failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list contexts")
		return
	}

	type ContextResponse struct {
		ID           string  `json:"id"`
		ProjectID    string  `json:"project_id"`
		SourceType   string  `json:"source_type"`
		SourceName   *string `json:"source_name"`
		MessageCount int32   `json:"message_count"`
		ImportedAt   string  `json:"imported_at"`
	}

	result := make([]ContextResponse, 0, len(contexts))
	for _, c := range contexts {
		result = append(result, ContextResponse{
			ID:           uuidToString(c.ID),
			ProjectID:    uuidToString(c.ProjectID),
			SourceType:   c.SourceType,
			SourceName:   textToPtr(c.SourceName),
			MessageCount: c.MessageCount,
			ImportedAt:   c.ImportedAt.Time.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, result)
}
```

- [ ] **Step 2: Register routes**

In `server/cmd/server/router.go`, add to the project routes block:

```go
r.Post("/api/projects/{projectID}/import-context", h.ImportProjectContext)
r.Get("/api/projects/{projectID}/contexts", h.ListProjectContexts)
```

- [ ] **Step 3: Verify build**

Run: `cd server && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add server/internal/handler/project_context.go server/cmd/server/router.go
git commit -m "feat(server): add context import handler for projects"
```

---

### Task 4: Execution Engine -- Stuck Detection & on_failure Cascade

**Files:**
- Modify: `server/internal/service/scheduler.go`

- [ ] **Step 1: Add monitoring goroutine**

Add a `MonitorActiveSteps` method to `SchedulerService`:

```go
// MonitorActiveSteps periodically checks running steps for timeouts and stuck conditions.
// Call this in a goroutine: go scheduler.MonitorActiveSteps(ctx)
func (s *SchedulerService) MonitorActiveSteps(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkRunningSteps(ctx)
		}
	}
}

func (s *SchedulerService) checkRunningSteps(ctx context.Context) {
	// List all workflows in "running" status
	workflows, err := s.queries.ListWorkflowsByStatus(ctx, "running")
	if err != nil {
		slog.Error("monitor: failed to list running workflows", "error", err)
		return
	}

	for _, wf := range workflows {
		steps, err := s.queries.ListWorkflowSteps(ctx, wf.ID)
		if err != nil {
			continue
		}

		for _, step := range steps {
			if step.Status != "running" {
				continue
			}

			if !step.StartedAt.Valid {
				continue
			}

			elapsed := time.Since(step.StartedAt.Time)

			// Parse timeout rule
			var timeoutRule TimeoutRule
			if len(step.TimeoutRule) > 0 {
				_ = json.Unmarshal(step.TimeoutRule, &timeoutRule)
			}
			if timeoutRule.MaxDurationSeconds == 0 {
				timeoutRule = defaultTimeoutRule
			}

			if elapsed > time.Duration(timeoutRule.MaxDurationSeconds)*time.Second {
				slog.Warn("monitor: step timed out",
					"step_id", uuidToString(step.ID),
					"elapsed", elapsed,
					"timeout", timeoutRule.MaxDurationSeconds,
				)
				s.HandleStepTimeout(ctx, step.ID)
			}
		}
	}
}
```

- [ ] **Step 2: Implement on_failure cascade in HandleStepFailure**

Update `HandleStepFailure` to implement the cascade from the spec:

```go
func (s *SchedulerService) HandleStepFailure(ctx context.Context, stepID pgtype.UUID, errMsg string) {
	step, err := s.queries.GetWorkflowStep(ctx, stepID)
	if err != nil {
		slog.Error("HandleStepFailure: failed to get step", "error", err)
		return
	}

	onFailure := "block"
	if step.OnFailure != nil && *step.OnFailure != "" {
		onFailure = *step.OnFailure
	}

	var retryRule RetryRule
	if len(step.RetryRule) > 0 {
		_ = json.Unmarshal(step.RetryRule, &retryRule)
	}
	if retryRule.MaxRetries == 0 {
		retryRule = defaultRetryRule
	}

	slog.Info("HandleStepFailure",
		"step_id", uuidToString(stepID),
		"on_failure", onFailure,
		"current_retry", step.CurrentRetry,
		"max_retries", retryRule.MaxRetries,
	)

	switch onFailure {
	case "retry_once":
		if step.CurrentRetry < 1 {
			s.retryStep(ctx, step)
			return
		}
		s.failStep(ctx, step, errMsg)

	case "retry_n":
		if int(step.CurrentRetry) < retryRule.MaxRetries {
			s.retryStep(ctx, step)
			return
		}
		s.failStep(ctx, step, errMsg)

	case "reassign_then_retry":
		if s.tryReassign(ctx, step) {
			return
		}
		s.failStep(ctx, step, errMsg)

	case "skip":
		if step.Skippable {
			s.skipStep(ctx, step)
			return
		}
		s.failStep(ctx, step, errMsg)

	case "pause_and_notify_owner":
		s.pauseAndNotifyOwner(ctx, step, errMsg)

	default: // "block"
		s.failStep(ctx, step, errMsg)
	}
}

func (s *SchedulerService) retryStep(ctx context.Context, step db.WorkflowStep) {
	_ = s.queries.IncrementWorkflowStepRetry(ctx, step.ID)
	_ = s.queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
		ID:     step.ID,
		Status: "retrying",
	})

	// Exponential backoff
	var retryRule RetryRule
	if len(step.RetryRule) > 0 {
		_ = json.Unmarshal(step.RetryRule, &retryRule)
	}
	if retryRule.RetryDelaySeconds == 0 {
		retryRule.RetryDelaySeconds = 30
	}

	delay := time.Duration(retryRule.RetryDelaySeconds) * time.Second * time.Duration(1<<step.CurrentRetry)
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}

	time.AfterFunc(delay, func() {
		_ = s.queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
			ID:     step.ID,
			Status: "pending",
		})
		s.ScheduleStep(ctx, step, step.RunID)
	})

	s.broadcastStepEvent(ctx, protocol.EventWorkflowStepFailed, step, map[string]any{
		"action": "retrying",
		"retry":  step.CurrentRetry + 1,
	})
}

func (s *SchedulerService) tryReassign(ctx context.Context, step db.WorkflowStep) bool {
	for _, candidateID := range step.CandidateAgentIds {
		cUUID := pgtype.UUID{Bytes: candidateID.Bytes, Valid: true}
		agent, err := s.queries.GetAgent(ctx, cUUID)
		if err != nil || agent.ArchivedAt.Valid {
			continue
		}
		// Reassign
		_ = s.queries.UpdateWorkflowStepActualAgent(ctx, db.UpdateWorkflowStepActualAgentParams{
			ID:           step.ID,
			ActualAgentID: cUUID,
		})
		_ = s.queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
			ID:     step.ID,
			Status: "pending",
		})
		s.ScheduleStep(ctx, step, step.RunID)

		s.broadcastStepEvent(ctx, "workflow:step:reassigned", step, map[string]any{
			"new_agent_id": uuidToString(cUUID),
		})
		return true
	}
	return false
}

func (s *SchedulerService) skipStep(ctx context.Context, step db.WorkflowStep) {
	_ = s.queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
		ID:     step.ID,
		Status: "skipped",
	})

	s.broadcastStepEvent(ctx, "workflow:step:skipped", step, nil)

	// Unblock dependents -- treat skipped like completed
	s.unblockDependents(ctx, step)
}

func (s *SchedulerService) pauseAndNotifyOwner(ctx context.Context, step db.WorkflowStep, errMsg string) {
	_ = s.queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
		ID:     step.ID,
		Status: "blocked",
	})

	// Pause the run
	if step.RunID.Valid {
		_ = s.queries.UpdateProjectRunStatus(ctx, db.UpdateProjectRunStatusParams{
			ID:     step.RunID,
			Status: "paused",
		})
	}

	// Create inbox item for owner
	wf, _ := s.queries.GetWorkflow(ctx, step.WorkflowID)
	if wf.ID.Valid {
		// Find project via plan
		// Notify via event bus
		s.eventBus.Publish(protocol.EventProjectStatusChanged, map[string]any{
			"workflow_id": uuidToString(step.WorkflowID),
			"step_id":     uuidToString(step.ID),
			"action":      "paused_for_owner",
			"error":       errMsg,
		})
	}
}

// unblockDependents checks and schedules steps that depend on the completed/skipped step.
func (s *SchedulerService) unblockDependents(ctx context.Context, completedStep db.WorkflowStep) {
	steps, err := s.queries.ListWorkflowSteps(ctx, completedStep.WorkflowID)
	if err != nil {
		return
	}

	completedID := uuidToString(completedStep.ID)

	for _, step := range steps {
		if step.Status != "pending" && step.Status != "blocked" {
			continue
		}

		dependsOnThis := false
		for _, depID := range step.DependsOn {
			if uuidToString(pgtype.UUID{Bytes: depID.Bytes, Valid: true}) == completedID {
				dependsOnThis = true
				break
			}
		}

		if !dependsOnThis {
			continue
		}

		// Check if ALL dependencies are now completed or skipped
		allDepsResolved := true
		for _, depID := range step.DependsOn {
			depUUID := pgtype.UUID{Bytes: depID.Bytes, Valid: true}
			depStep, err := s.queries.GetWorkflowStep(ctx, depUUID)
			if err != nil {
				allDepsResolved = false
				break
			}
			if depStep.Status != "completed" && depStep.Status != "skipped" {
				allDepsResolved = false
				break
			}
		}

		if allDepsResolved {
			_ = s.queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
				ID:     step.ID,
				Status: "ready",
			})
			s.ScheduleStep(ctx, step, completedStep.RunID)
		}
	}
}
```

- [ ] **Step 3: Add ListWorkflowsByStatus query**

Add to `server/pkg/db/queries/workflows.sql`:

```sql
-- name: ListWorkflowsByStatus :many
SELECT * FROM workflow WHERE status = @status;
```

- [ ] **Step 4: Regenerate sqlc and verify build**

Run: `cd server && make sqlc && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 5: Commit**

```bash
git add server/internal/service/scheduler.go server/pkg/db/queries/workflows.sql server/pkg/db/generated/
git commit -m "feat(server): add stuck detection, on_failure cascade, and dependency resolution"
```

---

### Task 5: Wire StartWorkflow to Create project_run

**Files:**
- Modify: `server/internal/handler/workflow.go`

- [ ] **Step 1: Update StartWorkflow handler**

In the `StartWorkflow` handler, before calling `s.Scheduler.ScheduleWorkflow()`, add project_run creation:

```go
// In StartWorkflow handler, after getting the workflow:

// Create project_run if this workflow has a plan linked to a project
var runID pgtype.UUID
if wf.PlanID.Valid {
	plan, planErr := h.Queries.GetPlan(r.Context(), wf.PlanID)
	if planErr == nil && plan.ProjectID.Valid {
		run, runErr := h.Queries.CreateProjectRun(r.Context(), db.CreateProjectRunParams{
			PlanID:    wf.PlanID,
			ProjectID: plan.ProjectID,
			Status:    "pending",
		})
		if runErr != nil {
			slog.Error("failed to create project run", "error", runErr)
		} else {
			runID = run.ID
			// Update project status to running
			_ = h.Queries.UpdateProjectStatus(r.Context(), db.UpdateProjectStatusParams{
				ID:     plan.ProjectID,
				Status: "running",
			})
		}
	}
}

if err := h.Scheduler.ScheduleWorkflow(r.Context(), wf.ID, runID); err != nil {
	slog.Error("failed to schedule workflow", "error", err)
	writeError(w, http.StatusInternalServerError, "failed to start workflow")
	return
}
```

- [ ] **Step 2: Verify build**

Run: `cd server && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 3: Commit**

```bash
git add server/internal/handler/workflow.go
git commit -m "feat(server): create project_run when starting workflow execution"
```

---

### Task 6: Frontend Types for Context and Task Brief

**Files:**
- Modify: `apps/web/shared/types/project.ts`
- Modify: `apps/web/shared/api/client.ts`
- Modify: `apps/web/features/projects/store.ts`

- [ ] **Step 1: Add types**

Add to `apps/web/shared/types/project.ts`:

```typescript
export interface ProjectContext {
  id: string;
  project_id: string;
  source_type: 'channel' | 'dm' | 'thread';
  source_name?: string;
  message_count: number;
  imported_at: string;
}

export interface TaskBrief {
  goal?: string;
  background?: string;
  referenced_files?: { file_id: string; file_name: string; description?: string }[];
  constraints?: string[];
  participant_scope?: string;
  deliverables?: { name: string; description: string; type: string }[];
  acceptance_criteria?: string[];
  timeline?: string;
}
```

- [ ] **Step 2: Add API methods**

Add to `apps/web/shared/api/client.ts`:

```typescript
async importProjectContext(projectId: string, data: {
  source_type: string;
  source_id: string;
  date_from?: string;
  date_to?: string;
}): Promise<ProjectContext> {
  return this.fetch(`/api/projects/${projectId}/import-context`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

async listProjectContexts(projectId: string): Promise<ProjectContext[]> {
  return this.fetch(`/api/projects/${projectId}/contexts`);
}
```

- [ ] **Step 3: Add store state**

Add to `apps/web/features/projects/store.ts`:

```typescript
// State
contexts: ProjectContext[];

// Actions
fetchContexts: async (projectId: string) => {
  const contexts = await api.listProjectContexts(projectId);
  set({ contexts });
},

importContext: async (projectId: string, data: {
  source_type: string;
  source_id: string;
  date_from?: string;
  date_to?: string;
}) => {
  const ctx = await api.importProjectContext(projectId, data);
  set((s) => ({ contexts: [ctx, ...s.contexts] }));
  return ctx;
},
```

Initialize `contexts: []` in the store creator.

- [ ] **Step 4: Verify typecheck**

Run: `pnpm typecheck`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/web/shared/types/project.ts apps/web/shared/api/client.ts apps/web/features/projects/store.ts
git commit -m "feat(web): add project context types, API methods, and store actions"
```

---

### Task 7: DAG View Component

**Files:**
- Create: `apps/web/features/projects/components/dag-view.tsx`

- [ ] **Step 1: Create the DAG visualization component**

```tsx
// apps/web/features/projects/components/dag-view.tsx
"use client";

import { useMemo } from "react";
import type { WorkflowStep } from "@/shared/types/workflow";

const STATUS_COLORS: Record<string, string> = {
  pending: "bg-muted text-muted-foreground",
  ready: "bg-blue-500/10 text-blue-400 border-blue-500/30",
  queued: "bg-blue-500/10 text-blue-400",
  assigned: "bg-blue-500/20 text-blue-400",
  running: "bg-blue-500/30 text-blue-300 animate-pulse",
  waiting_input: "bg-yellow-500/20 text-yellow-300",
  blocked: "bg-orange-500/20 text-orange-300",
  retrying: "bg-orange-500/30 text-orange-300 animate-pulse",
  timeout: "bg-red-500/20 text-red-300",
  completed: "bg-green-500/20 text-green-300",
  failed: "bg-red-500/20 text-red-300",
  cancelled: "bg-muted text-muted-foreground line-through",
  skipped: "bg-muted text-muted-foreground italic",
};

interface DagViewProps {
  steps: WorkflowStep[];
  onSelectStep?: (step: WorkflowStep) => void;
  selectedStepId?: string;
}

export function DagView({ steps, onSelectStep, selectedStepId }: DagViewProps) {
  // Compute layers for layout: steps with no deps first, then their dependents, etc.
  const layers = useMemo(() => {
    const stepMap = new Map(steps.map((s) => [s.id, s]));
    const placed = new Set<string>();
    const result: WorkflowStep[][] = [];

    // Iteratively find steps whose deps are all placed
    let remaining = [...steps];
    while (remaining.length > 0) {
      const layer: WorkflowStep[] = [];
      const nextRemaining: WorkflowStep[] = [];

      for (const step of remaining) {
        const depsResolved = step.depends_on.every((d) => placed.has(d));
        if (depsResolved) {
          layer.push(step);
        } else {
          nextRemaining.push(step);
        }
      }

      if (layer.length === 0) {
        // Circular dependency or orphan -- dump remaining
        result.push(nextRemaining);
        break;
      }

      for (const s of layer) placed.add(s.id);
      result.push(layer);
      remaining = nextRemaining;
    }

    return result;
  }, [steps]);

  if (steps.length === 0) {
    return (
      <div className="rounded-xl border border-dashed border-border/70 bg-background/50 p-8 text-center text-sm text-muted-foreground">
        No workflow steps defined.
      </div>
    );
  }

  return (
    <div className="space-y-4 overflow-x-auto">
      {layers.map((layer, layerIdx) => (
        <div key={layerIdx} className="flex flex-wrap gap-3">
          {layerIdx > 0 && (
            <div className="mb-1 w-full border-t border-dashed border-border/40" />
          )}
          <span className="w-full text-[10px] uppercase tracking-wider text-muted-foreground">
            Stage {layerIdx + 1}
          </span>
          {layer.map((step) => (
            <button
              key={step.id}
              type="button"
              onClick={() => onSelectStep?.(step)}
              className={`flex min-w-[200px] flex-col gap-1 rounded-xl border p-3 text-left text-sm transition ${
                selectedStepId === step.id
                  ? "border-primary ring-1 ring-primary"
                  : "border-border/70 hover:border-border"
              } ${STATUS_COLORS[step.status] ?? ""}`}
            >
              <div className="flex items-center justify-between">
                <span className="font-medium">
                  {step.title || step.description.slice(0, 40)}
                </span>
                <span className="rounded-full px-1.5 py-0.5 text-[10px] font-bold">
                  {step.status}
                </span>
              </div>
              {step.agent_id && (
                <span className="text-xs opacity-70">
                  Agent: {step.actual_agent_id || step.agent_id}
                </span>
              )}
              {step.depends_on.length > 0 && (
                <span className="text-[10px] opacity-50">
                  depends on {step.depends_on.length} step(s)
                </span>
              )}
            </button>
          ))}
        </div>
      ))}
    </div>
  );
}
```

- [ ] **Step 2: Export from index**

Add to `apps/web/features/projects/index.ts`:

```typescript
export { DagView } from "./components/dag-view";
```

- [ ] **Step 3: Verify typecheck**

Run: `pnpm typecheck`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add apps/web/features/projects/components/dag-view.tsx apps/web/features/projects/index.ts
git commit -m "feat(web): add DAG view component for workflow step visualization"
```

---

### Task 8: Verify Full Build

- [ ] **Step 1: Run Go tests**

Run: `cd server && go test ./... -count=1`

- [ ] **Step 2: Run TypeScript checks**

Run: `pnpm typecheck && pnpm test`

- [ ] **Step 3: Fix any issues and commit**

```bash
git add -A && git commit -m "fix: resolve Phase 2 build issues"
```
