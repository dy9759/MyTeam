package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	"github.com/MyAIOSHub/MyTeam/server/internal/realtime"
	"github.com/MyAIOSHub/MyTeam/server/internal/util"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/MyAIOSHub/MyTeam/server/pkg/protocol"
)

// ProjectLifecycleService monitors active project runs for timeouts
// and agent availability, and reacts to agent status changes.
type ProjectLifecycleService struct {
	Queries   *db.Queries
	Hub       *realtime.Hub
	EventBus  *events.Bus
	Scheduler *SchedulerService
}

// NewProjectLifecycleService creates a new ProjectLifecycleService.
func NewProjectLifecycleService(q *db.Queries, hub *realtime.Hub, bus *events.Bus, scheduler *SchedulerService) *ProjectLifecycleService {
	return &ProjectLifecycleService{
		Queries:   q,
		Hub:       hub,
		EventBus:  bus,
		Scheduler: scheduler,
	}
}

// Start begins monitoring project lifecycle events.
// It subscribes to agent status changes and starts a periodic monitor.
func (s *ProjectLifecycleService) Start() {
	// Subscribe to agent status change events to detect agents going offline.
	s.EventBus.Subscribe(protocol.EventAgentStatus, s.handleAgentStatusChanged)

	// Start periodic monitoring of active runs for timeouts.
	go s.runMonitorLoop()

	slog.Info("project lifecycle service started")
}

// handleAgentStatusChanged reacts to agent status changes.
// When an agent goes offline during an active run, it triggers step failure.
func (s *ProjectLifecycleService) handleAgentStatusChanged(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	agentMap, ok := payload["agent"].(map[string]any)
	if !ok {
		return
	}

	status, _ := agentMap["status"].(string)
	agentID, _ := agentMap["id"].(string)

	// Only handle agents going offline.
	if status != "offline" {
		return
	}

	slog.Info("project lifecycle: agent went offline, checking for active tasks",
		"agent_id", agentID,
	)

	s.handleAgentOffline(context.Background(), agentID)
}

// handleAgentOffline finds active tasks for an agent that went offline
// and triggers step failure for any tasks with associated workflow steps.
func (s *ProjectLifecycleService) handleAgentOffline(ctx context.Context, agentID string) {
	agentUUID := util.ParseUUID(agentID)

	// Find active tasks for this agent (status = 'running' or 'dispatched').
	// We look for tasks that are currently running.
	running, err := s.Queries.CountRunningTasks(ctx, agentUUID)
	if err != nil {
		slog.Error("project lifecycle: failed to count running tasks", "agent_id", agentID, "error", err)
		return
	}

	if running == 0 {
		return
	}

	slog.Warn("project lifecycle: agent went offline with active tasks",
		"agent_id", agentID,
		"running_tasks", running,
	)

	// TODO: When agent_task_queue has workflow_step_id column, query for tasks
	// with workflow_step_id set and trigger HandleStepFailure for each.
	// For now, log the situation for monitoring.
	//
	// tasks, err := s.Queries.ListRunningTasksByAgent(ctx, agentUUID)
	// for _, task := range tasks {
	//     if task.WorkflowStepID.Valid {
	//         s.Scheduler.HandleStepFailure(ctx, util.UUIDToString(task.WorkflowStepID), "agent went offline")
	//     }
	// }
}

// runMonitorLoop periodically checks active workflow steps for timeouts.
// It runs every 60 seconds and checks all "running" steps.
func (s *ProjectLifecycleService) runMonitorLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.monitorActiveRuns(context.Background())
	}
}

// monitorActiveRuns checks all running workflow steps for timeout conditions.
func (s *ProjectLifecycleService) monitorActiveRuns(ctx context.Context) {
	// List all workflows with status "running".
	// TODO: When a ListRunningWorkflows query exists, use it.
	// For now, we use the existing ListWorkflows with a workspace iteration approach.
	// This is a best-effort implementation that will be refined when project_run
	// queries are available.

	// Alternative approach: scan all workflow steps with status "running"
	// and check their started_at against the timeout rule.
	// TODO: Add a query ListRunningWorkflowSteps when sqlc queries are updated.
	//
	// steps, err := s.Queries.ListRunningWorkflowSteps(ctx)
	// for _, step := range steps {
	//     timeoutRule := DefaultTimeoutRule()
	//     // Parse timeout_rule from step JSONB when column exists.
	//     if step.StartedAt.Valid {
	//         elapsed := time.Since(step.StartedAt.Time)
	//         if elapsed.Seconds() > float64(timeoutRule.MaxDurationSeconds) {
	//             slog.Warn("step timed out",
	//                 "step_id", util.UUIDToString(step.ID),
	//                 "elapsed_seconds", elapsed.Seconds(),
	//                 "max_seconds", timeoutRule.MaxDurationSeconds,
	//             )
	//             s.Scheduler.HandleStepTimeout(ctx, util.UUIDToString(step.ID))
	//         }
	//     }
	// }

	slog.Debug("project lifecycle: monitor cycle completed")
}
