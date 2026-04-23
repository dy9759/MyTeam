package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	"github.com/MyAIOSHub/MyTeam/server/internal/util"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/MyAIOSHub/MyTeam/server/pkg/protocol"
)

// ProjectSchedulerService periodically checks for scheduled and recurring projects
// and triggers execution when their schedule conditions are met.
type ProjectSchedulerService struct {
	queries   *db.Queries
	scheduler *SchedulerService
	bus       *events.Bus
}

// NewProjectSchedulerService creates a new ProjectSchedulerService.
func NewProjectSchedulerService(queries *db.Queries, scheduler *SchedulerService, bus *events.Bus) *ProjectSchedulerService {
	return &ProjectSchedulerService{
		queries:   queries,
		scheduler: scheduler,
		bus:       bus,
	}
}

// Start runs the scheduler loop until ctx is cancelled.
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

// tick runs one scheduling cycle.
func (s *ProjectSchedulerService) tick(ctx context.Context) {
	projects, err := s.queries.ListScheduledProjects(ctx)
	if err != nil {
		slog.Error("project scheduler: failed to list scheduled projects", "error", err)
		return
	}

	now := time.Now().UTC()
	for _, p := range projects {
		projectID := util.UUIDToString(p.ID)
		switch p.ScheduleType {
		case "scheduled_once":
			// Trigger if scheduled_at is in the past.
			if p.ScheduledAt.Valid && now.After(p.ScheduledAt.Time) {
				slog.Info("project scheduler: triggering scheduled_once project", "project_id", projectID)
				if err := s.triggerProject(ctx, p); err != nil {
					slog.Error("project scheduler: failed to trigger scheduled_once project",
						"project_id", projectID, "error", err)
				}
			}

		case "recurring":
			// Check stop conditions before triggering.
			if s.shouldStopRecurring(ctx, p) {
				slog.Info("project scheduler: stopping recurring project (stop condition met)", "project_id", projectID)
				if err := s.queries.UpdateProjectStatus(ctx, db.UpdateProjectStatusParams{
					ID:     p.ID,
					Status: "stopped",
				}); err != nil {
					slog.Error("project scheduler: failed to stop recurring project", "project_id", projectID, "error", err)
				}
				continue
			}

			// Check cron match.
			if p.CronExpr.Valid && matchCron(p.CronExpr.String, now) {
				slog.Info("project scheduler: triggering recurring project", "project_id", projectID)
				if err := s.triggerProject(ctx, p); err != nil {
					slog.Error("project scheduler: failed to trigger recurring project",
						"project_id", projectID, "error", err)
				}
			}
		}
	}
}

// shouldStopRecurring checks whether a recurring project has met any stop condition.
func (s *ProjectSchedulerService) shouldStopRecurring(ctx context.Context, p db.Project) bool {
	projectID := util.UUIDToString(p.ID)

	// Stop if end_time has passed.
	if p.EndTime.Valid && time.Now().UTC().After(p.EndTime.Time) {
		slog.Info("project scheduler: recurring project end_time reached", "project_id", projectID)
		return true
	}

	// Stop if max_runs reached.
	if p.MaxRuns.Valid {
		count, err := s.queries.CountProjectRuns(ctx, p.ID)
		if err != nil {
			slog.Error("project scheduler: failed to count project runs", "project_id", projectID, "error", err)
		} else if count >= int64(p.MaxRuns.Int32) {
			slog.Info("project scheduler: recurring project max_runs reached",
				"project_id", projectID, "runs", count, "max", p.MaxRuns.Int32)
			return true
		}
	}

	// Stop if consecutive failure threshold reached.
	threshold := int32(3) // default
	if p.ConsecutiveFailureThreshold.Valid {
		threshold = p.ConsecutiveFailureThreshold.Int32
	}
	if threshold > 0 {
		failCount, err := s.queries.CountConsecutiveFailedRuns(ctx, db.CountConsecutiveFailedRunsParams{
			ProjectID:  p.ID,
			LimitCount: threshold,
		})
		if err != nil {
			slog.Error("project scheduler: failed to count consecutive failures", "project_id", projectID, "error", err)
		} else if failCount >= int64(threshold) {
			slog.Info("project scheduler: recurring project consecutive failure threshold reached",
				"project_id", projectID, "failures", failCount, "threshold", threshold)
			return true
		}
	}

	return false
}

// triggerProject creates a run and kicks off execution for a project.
func (s *ProjectSchedulerService) triggerProject(ctx context.Context, p db.Project) error {
	projectID := util.UUIDToString(p.ID)

	// Get latest project version.
	version, err := s.queries.GetLatestProjectVersion(ctx, p.ID)
	if err != nil {
		return fmt.Errorf("get latest version: %w", err)
	}

	// Find associated plan.
	plan, err := s.queries.GetPlanByProject(ctx, p.ID)
	if err != nil {
		return fmt.Errorf("get plan by project: %w", err)
	}

	// Only trigger plans that are approved.
	if plan.ApprovalStatus != "approved" {
		return fmt.Errorf("plan not approved (status=%s)", plan.ApprovalStatus)
	}

	// Create a project run.
	run, err := s.queries.CreateProjectRun(ctx, db.CreateProjectRunParams{
		PlanID:    plan.ID,
		ProjectID: p.ID,
		Status:    "pending",
	})
	if err != nil {
		return fmt.Errorf("create project run: %w", err)
	}

	runID := util.UUIDToString(run.ID)

	// Update project status to running.
	if err := s.queries.UpdateProjectStatus(ctx, db.UpdateProjectStatusParams{
		ID:     p.ID,
		Status: "running",
	}); err != nil {
		return fmt.Errorf("update project status: %w", err)
	}

	// Kick off execution via the current task/run scheduler rather than the
	// legacy workflow model that main has already removed.
	if err := s.scheduler.ScheduleRun(
		ctx,
		uuid.UUID(plan.ID.Bytes),
		uuid.UUID(run.ID.Bytes),
	); err != nil {
		return fmt.Errorf("schedule run: %w", err)
	}

	slog.Info("project scheduler: project triggered successfully",
		"project_id", projectID,
		"run_id", runID,
		"version_id", util.UUIDToString(version.ID),
	)

	// Broadcast project run started event.
	if s.bus != nil {
		s.bus.Publish(events.Event{
			Type:        protocol.EventProjectRunStarted,
			WorkspaceID: util.UUIDToString(p.WorkspaceID),
			ActorType:   "system",
			ActorID:     "",
			Payload: map[string]any{
				"project_id": projectID,
				"run_id":     runID,
			},
		})
	}

	return nil
}

// matchCron returns true if the given 5-field cron expression matches the provided time.
// Supported: numeric values and '*' (any). Does not support ranges, steps, or lists.
func matchCron(expr string, t time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}

	// Fields: minute hour dom month dow
	values := []int{t.Minute(), t.Hour(), t.Day(), int(t.Month()), int(t.Weekday())}

	for i, field := range fields {
		if field == "*" {
			continue
		}
		n, err := strconv.Atoi(field)
		if err != nil {
			return false
		}
		if n != values[i] {
			return false
		}
	}

	return true
}
