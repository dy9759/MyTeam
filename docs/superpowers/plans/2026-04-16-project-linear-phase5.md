# ProjectLinear Phase 5: Recurring & Scheduling

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement cron-based recurring project execution, scheduled_once triggers, stop conditions, and agent availability checks before execution.

**Architecture:** A new `ProjectSchedulerService` runs a ticker goroutine that checks scheduled/recurring projects every 60 seconds. On trigger, it creates a new version from HEAD, creates a run, checks agent availability, and starts execution. Stop conditions (max_runs, end_time, consecutive failures) are checked before each trigger.

**Tech Stack:** Go 1.26, cron expression parsing (gorhill/cronexpr or simple custom), PostgreSQL 17

**Depends on:** Phase 1 (project columns: max_runs, end_time, consecutive_failure_threshold, scheduled_at), Phase 2 (execution engine)

---

## File Structure

### Backend
- **Create:** `server/internal/service/project_scheduler.go`
- **Modify:** `server/pkg/db/queries/projects.sql` -- Add scheduling queries
- **Modify:** `server/cmd/server/main.go` -- Start scheduler goroutine
- **Modify:** `server/internal/handler/project.go` -- scheduled_once support

### Frontend
- **Create:** `apps/web/features/projects/components/schedule-settings.tsx`
- **Modify:** `apps/web/app/(dashboard)/projects/[id]/page.tsx` -- Wire schedule settings

---

### Task 1: Scheduling Queries

**Files:**
- Modify: `server/pkg/db/queries/projects.sql`

- [ ] **Step 1: Add scheduling queries**

Append to `server/pkg/db/queries/projects.sql`:

```sql
-- name: ListScheduledProjects :many
SELECT * FROM project
WHERE status = 'scheduled'
  AND schedule_type IN ('scheduled_once', 'recurring')
ORDER BY created_at ASC;

-- name: ListRunningRecurringProjects :many
SELECT * FROM project
WHERE status = 'running'
  AND schedule_type = 'recurring'
ORDER BY created_at ASC;

-- name: CountProjectRuns :one
SELECT COUNT(*) FROM project_run WHERE project_id = @project_id;

-- name: CountConsecutiveFailedRuns :one
SELECT COUNT(*) FROM (
  SELECT status FROM project_run
  WHERE project_id = @project_id
  ORDER BY created_at DESC
  LIMIT @limit_count
) sub
WHERE sub.status = 'failed';
```

- [ ] **Step 2: Regenerate sqlc**

Run: `cd server && make sqlc`

- [ ] **Step 3: Verify build**

Run: `cd server && go build ./...`

- [ ] **Step 4: Commit**

```bash
git add server/pkg/db/queries/projects.sql server/pkg/db/generated/
git commit -m "feat(server): add scheduling-related sqlc queries"
```

---

### Task 2: ProjectSchedulerService

**Files:**
- Create: `server/internal/service/project_scheduler.go`

- [ ] **Step 1: Write the service**

```go
// server/internal/service/project_scheduler.go
package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ProjectSchedulerService handles cron-based project triggering.
type ProjectSchedulerService struct {
	queries   *db.Queries
	scheduler *SchedulerService
	eventBus  *EventBus
}

// NewProjectSchedulerService creates a new project scheduler.
func NewProjectSchedulerService(queries *db.Queries, scheduler *SchedulerService, eventBus *EventBus) *ProjectSchedulerService {
	return &ProjectSchedulerService{
		queries:   queries,
		scheduler: scheduler,
		eventBus:  eventBus,
	}
}

// Start begins the scheduling loop. Call in a goroutine.
func (s *ProjectSchedulerService) Start(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	slog.Info("project scheduler started")

	for {
		select {
		case <-ctx.Done():
			slog.Info("project scheduler stopped")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *ProjectSchedulerService) tick(ctx context.Context) {
	// Check scheduled projects
	scheduled, err := s.queries.ListScheduledProjects(ctx)
	if err != nil {
		slog.Error("project scheduler: failed to list scheduled projects", "error", err)
		return
	}

	now := time.Now()

	for _, project := range scheduled {
		switch project.ScheduleType {
		case "scheduled_once":
			if project.ScheduledAt.Valid && now.After(project.ScheduledAt.Time) {
				s.triggerProject(ctx, project)
			}
		case "recurring":
			if s.shouldTriggerRecurring(project, now) {
				s.triggerProject(ctx, project)
			}
		}
	}
}

func (s *ProjectSchedulerService) shouldTriggerRecurring(project db.Project, now time.Time) bool {
	if !project.CronExpr.Valid || project.CronExpr.String == "" {
		return false
	}

	// Check stop conditions
	if project.EndTime.Valid && now.After(project.EndTime.Time) {
		_ = s.queries.UpdateProjectStatus(context.Background(), db.UpdateProjectStatusParams{
			ID:     project.ID,
			Status: "stopped",
		})
		slog.Info("project stopped: end_time reached", "project_id", uuidToString(project.ID))
		return false
	}

	if project.MaxRuns.Valid {
		count, err := s.queries.CountProjectRuns(context.Background(), project.ID)
		if err == nil && count >= int64(project.MaxRuns.Int32) {
			_ = s.queries.UpdateProjectStatus(context.Background(), db.UpdateProjectStatusParams{
				ID:     project.ID,
				Status: "stopped",
			})
			slog.Info("project stopped: max_runs reached", "project_id", uuidToString(project.ID))
			return false
		}
	}

	if project.ConsecutiveFailureThreshold.Valid {
		threshold := project.ConsecutiveFailureThreshold.Int32
		failCount, err := s.queries.CountConsecutiveFailedRuns(context.Background(), db.CountConsecutiveFailedRunsParams{
			ProjectID:  project.ID,
			LimitCount: threshold,
		})
		if err == nil && failCount >= int64(threshold) {
			_ = s.queries.UpdateProjectStatus(context.Background(), db.UpdateProjectStatusParams{
				ID:     project.ID,
				Status: "stopped",
			})
			slog.Info("project stopped: consecutive failures", "project_id", uuidToString(project.ID), "failures", failCount)
			return false
		}
	}

	// Simple cron matching: check if current minute matches the cron expression
	// Format: "minute hour day-of-month month day-of-week"
	return matchCron(project.CronExpr.String, now)
}

func (s *ProjectSchedulerService) triggerProject(ctx context.Context, project db.Project) {
	slog.Info("triggering project", "project_id", uuidToString(project.ID), "type", project.ScheduleType)

	// Get latest version from default branch
	latestVersion, err := s.queries.GetLatestProjectVersion(ctx, project.ID)
	if err != nil {
		slog.Error("trigger: no versions found", "project_id", uuidToString(project.ID))
		return
	}

	// Create new version snapshot
	newVersion, err := s.queries.CreateProjectVersion(ctx, db.CreateProjectVersionParams{
		ProjectID:        project.ID,
		ParentVersionID:  latestVersion.ID,
		VersionNumber:    latestVersion.VersionNumber + 1,
		PlanSnapshot:     latestVersion.PlanSnapshot,
		WorkflowSnapshot: latestVersion.WorkflowSnapshot,
	})
	if err != nil {
		slog.Error("trigger: failed to create version", "error", err)
		return
	}

	// Find associated plan to create run
	plan, err := s.queries.GetPlanByProject(ctx, project.ID)
	if err != nil {
		slog.Error("trigger: no plan found", "project_id", uuidToString(project.ID))
		return
	}

	// Create run
	run, err := s.queries.CreateProjectRun(ctx, db.CreateProjectRunParams{
		PlanID:    plan.ID,
		ProjectID: project.ID,
		Status:    "pending",
	})
	if err != nil {
		slog.Error("trigger: failed to create run", "error", err)
		return
	}

	// Update project status to running
	_ = s.queries.UpdateProjectStatus(ctx, db.UpdateProjectStatusParams{
		ID:     project.ID,
		Status: "running",
	})

	// Find workflow and start execution
	// The workflow is linked via plan_id
	workflows, _ := s.queries.ListWorkflows(ctx, db.ListWorkflowsParams{
		WorkspaceID: project.WorkspaceID,
		Limit:       100,
		Offset:      0,
	})

	for _, wf := range workflows {
		if wf.PlanID == plan.ID {
			if err := s.scheduler.ScheduleWorkflow(ctx, wf.ID, run.ID); err != nil {
				slog.Error("trigger: failed to schedule workflow", "error", err)
			}
			break
		}
	}

	s.eventBus.Publish(protocol.EventProjectRunStarted, map[string]any{
		"project_id": uuidToString(project.ID),
		"run_id":     uuidToString(run.ID),
		"version_id": uuidToString(newVersion.ID),
	})

	// For scheduled_once, mark as running (it will complete/fail after execution)
	if project.ScheduleType == "scheduled_once" {
		// The project transitions to completed/failed when the run finishes
		// This is handled by the execution engine callbacks
		slog.Info("scheduled_once project triggered", "project_id", uuidToString(project.ID))
	}
}

// matchCron does basic cron expression matching.
// Format: "minute hour dom month dow" (5 fields, * means any)
func matchCron(expr string, t time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}

	checks := []struct {
		field string
		value int
	}{
		{fields[0], t.Minute()},
		{fields[1], t.Hour()},
		{fields[2], t.Day()},
		{fields[3], int(t.Month())},
		{fields[4], int(t.Weekday())},
	}

	for _, c := range checks {
		if c.field == "*" {
			continue
		}
		// Simple numeric match (does not handle ranges/steps)
		var expected int
		if _, err := fmt.Sscanf(c.field, "%d", &expected); err != nil {
			return false
		}
		if c.value != expected {
			return false
		}
	}

	return true
}
```

- [ ] **Step 2: Add missing import**

Add `"fmt"` to the import block.

- [ ] **Step 3: Verify build**

Run: `cd server && go build ./...`

- [ ] **Step 4: Commit**

```bash
git add server/internal/service/project_scheduler.go
git commit -m "feat(server): add ProjectSchedulerService for recurring/scheduled execution"
```

---

### Task 3: Start Scheduler in Server Main

**Files:**
- Modify: `server/cmd/server/main.go`

- [ ] **Step 1: Add scheduler startup**

In the server startup, after creating the handler and starting the HTTP server, add:

```go
// Start project scheduler
projectScheduler := service.NewProjectSchedulerService(queries, scheduler, eventBus)
go projectScheduler.Start(ctx)

// Start step monitor
go scheduler.MonitorActiveSteps(ctx)
```

- [ ] **Step 2: Verify build**

Run: `cd server && go build ./...`

- [ ] **Step 3: Commit**

```bash
git add server/cmd/server/main.go
git commit -m "feat(server): start project scheduler and step monitor on server boot"
```

---

### Task 4: Frontend Schedule Settings Component

**Files:**
- Create: `apps/web/features/projects/components/schedule-settings.tsx`

- [ ] **Step 1: Write the component**

```tsx
// apps/web/features/projects/components/schedule-settings.tsx
"use client";

import { useState } from "react";
import type { Project, ProjectScheduleType } from "@/shared/types/project";

interface ScheduleSettingsProps {
  project: Project;
  onUpdate: (data: Partial<Project>) => void;
}

const CRON_PRESETS = [
  { label: "Every hour", value: "0 * * * *" },
  { label: "Daily at 9am", value: "0 9 * * *" },
  { label: "Weekly (Mon 9am)", value: "0 9 * * 1" },
  { label: "Monthly (1st, 9am)", value: "0 9 1 * *" },
];

export function ScheduleSettings({ project, onUpdate }: ScheduleSettingsProps) {
  const [scheduleType, setScheduleType] = useState<ProjectScheduleType>(project.schedule_type);
  const [cronExpr, setCronExpr] = useState(project.cron_expr ?? "");

  const handleSave = () => {
    onUpdate({
      schedule_type: scheduleType,
      cron_expr: cronExpr || undefined,
    });
  };

  return (
    <div className="space-y-4">
      <div>
        <label className="text-sm font-medium text-foreground">Schedule Type</label>
        <div className="mt-2 flex gap-2">
          {(["one_time", "scheduled_once", "recurring"] as const).map((t) => (
            <button
              key={t}
              type="button"
              onClick={() => setScheduleType(t)}
              className={`rounded-lg px-3 py-1.5 text-sm transition ${
                scheduleType === t
                  ? "bg-primary text-primary-foreground"
                  : "bg-secondary text-secondary-foreground hover:bg-secondary/80"
              }`}
            >
              {t === "one_time" ? "One-time" : t === "scheduled_once" ? "Scheduled" : "Recurring"}
            </button>
          ))}
        </div>
      </div>

      {(scheduleType === "scheduled_once" || scheduleType === "recurring") && (
        <div>
          <label className="text-sm font-medium text-foreground">
            {scheduleType === "scheduled_once" ? "Run at" : "Cron Expression"}
          </label>

          {scheduleType === "recurring" && (
            <div className="mt-2 flex flex-wrap gap-1">
              {CRON_PRESETS.map((preset) => (
                <button
                  key={preset.value}
                  type="button"
                  onClick={() => setCronExpr(preset.value)}
                  className={`rounded-md px-2 py-1 text-xs transition ${
                    cronExpr === preset.value
                      ? "bg-primary/20 text-primary"
                      : "bg-secondary text-muted-foreground hover:text-foreground"
                  }`}
                >
                  {preset.label}
                </button>
              ))}
            </div>
          )}

          <input
            type={scheduleType === "scheduled_once" ? "datetime-local" : "text"}
            value={cronExpr}
            onChange={(e) => setCronExpr(e.target.value)}
            placeholder={scheduleType === "recurring" ? "0 9 * * 1-5" : ""}
            className="mt-2 w-full rounded-lg border border-border bg-background px-3 py-2 text-sm"
          />
        </div>
      )}

      <button
        type="button"
        onClick={handleSave}
        className="rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground"
      >
        Save Schedule
      </button>
    </div>
  );
}
```

- [ ] **Step 2: Verify typecheck**

Run: `pnpm typecheck`

- [ ] **Step 3: Commit**

```bash
git add apps/web/features/projects/components/schedule-settings.tsx
git commit -m "feat(web): add schedule settings component for project scheduling"
```

---

### Task 5: Verify Full Build

- [ ] **Step 1: Run all checks**

Run: `cd server && go build ./... && go test ./... -count=1`
Run: `pnpm typecheck && pnpm test`

- [ ] **Step 2: Commit fixes if needed**

```bash
git add -A && git commit -m "fix: resolve Phase 5 build issues"
```
