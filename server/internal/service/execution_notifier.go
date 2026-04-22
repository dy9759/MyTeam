package service

import (
	"log/slog"

	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	"github.com/MyAIOSHub/MyTeam/server/internal/realtime"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/MyAIOSHub/MyTeam/server/pkg/protocol"
)

// ExecutionNotifierService listens to execution events and creates
// notifications (inbox items, channel messages) for relevant stakeholders.
type ExecutionNotifierService struct {
	Queries  *db.Queries
	Hub      *realtime.Hub
	EventBus *events.Bus
}

// NewExecutionNotifierService creates a new ExecutionNotifierService.
func NewExecutionNotifierService(q *db.Queries, hub *realtime.Hub, bus *events.Bus) *ExecutionNotifierService {
	return &ExecutionNotifierService{
		Queries:  q,
		Hub:      hub,
		EventBus: bus,
	}
}

// Start subscribes to execution-related events on the event bus.
func (s *ExecutionNotifierService) Start() {
	s.EventBus.Subscribe(protocol.EventWorkflowStepFailed, s.handleStepFailed)
	s.EventBus.Subscribe(protocol.EventWorkflowStepCompleted, s.handleStepCompleted)
	s.EventBus.Subscribe(protocol.EventWorkflowCompleted, s.handleWorkflowCompleted)
	s.EventBus.Subscribe(protocol.EventRunCompleted, s.handleRunCompleted)
	s.EventBus.Subscribe(protocol.EventRunFailed, s.handleRunFailed)

	slog.Info("execution notifier service started")
}

// handleStepFailed notifies relevant stakeholders when a workflow step fails.
func (s *ExecutionNotifierService) handleStepFailed(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	stepID, _ := payload["step_id"].(string)
	errMsg, _ := payload["error"].(string)
	action, _ := payload["action"].(string)
	isFinal, _ := payload["final"].(bool)

	slog.Info("execution notifier: step failed",
		"step_id", stepID,
		"error", errMsg,
		"action", action,
		"final", isFinal,
		"workspace_id", e.WorkspaceID,
	)

	// Determine severity based on whether this is a final failure.
	severity := "warning"
	if isFinal {
		severity = "critical"
	}

	// TODO: Look up the step's project and channel.
	// TODO: Send message to project channel: "Step X failed: {error}. Agent: {name}"
	// TODO: Create inbox item for the agent's owner with action_required=true, action_type="retry"
	//       severity = "warning" for retryable, "critical" for final failure
	_ = severity
}

// handleStepCompleted processes step completion notifications.
func (s *ExecutionNotifierService) handleStepCompleted(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	stepID, _ := payload["step_id"].(string)
	slog.Debug("execution notifier: step completed", "step_id", stepID, "workspace_id", e.WorkspaceID)

	// Step completions are generally informational; no special notification needed
	// unless it's the last step (handled by workflow completed).
}

// handleWorkflowCompleted processes workflow completion notifications.
func (s *ExecutionNotifierService) handleWorkflowCompleted(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	workflowID, _ := payload["workflow_id"].(string)
	status, _ := payload["status"].(string)

	slog.Info("execution notifier: workflow completed",
		"workflow_id", workflowID,
		"status", status,
		"workspace_id", e.WorkspaceID,
	)

	// TODO: Send summary message to project channel.
	// TODO: Create inbox notification for project participants.
	// TODO: Update project status based on workflow outcome.
}

// handleRunCompleted notifies stakeholders when a project run completes successfully.
func (s *ExecutionNotifierService) handleRunCompleted(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	runID, _ := payload["run_id"].(string)

	slog.Info("execution notifier: run completed",
		"run_id", runID,
		"workspace_id", e.WorkspaceID,
	)

	// TODO: Send summary message to project channel.
	// TODO: Send inbox notification to all project participants.
	// TODO: Update project status to "completed".
}

// handleRunFailed notifies stakeholders when a project run fails.
func (s *ExecutionNotifierService) handleRunFailed(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	runID, _ := payload["run_id"].(string)
	reason, _ := payload["failure_reason"].(string)

	slog.Warn("execution notifier: run failed",
		"run_id", runID,
		"reason", reason,
		"workspace_id", e.WorkspaceID,
	)

	// TODO: Send message to project channel with failure details.
	// TODO: Create critical inbox item for project creator.
	// TODO: Update project status to "failed".
}
