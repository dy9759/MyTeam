package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// RetryRule defines the retry behaviour for a workflow step.
type RetryRule struct {
	MaxRetries        int `json:"max_retries"`
	RetryDelaySeconds int `json:"retry_delay_seconds"`
}

// DefaultRetryRule returns the default retry rule per the spec.
func DefaultRetryRule() RetryRule {
	return RetryRule{MaxRetries: 2, RetryDelaySeconds: 30}
}

// TimeoutRule defines the timeout behaviour for a workflow step.
type TimeoutRule struct {
	MaxDurationSeconds int    `json:"max_duration_seconds"`
	Action             string `json:"action"` // "retry", "fail", or "escalate"
}

// DefaultTimeoutRule returns the default timeout rule per the spec.
func DefaultTimeoutRule() TimeoutRule {
	return TimeoutRule{MaxDurationSeconds: 1800, Action: "retry"}
}

// OwnerEscalationPolicy defines how to escalate to the owner.
type OwnerEscalationPolicy struct {
	EscalateAfterSeconds int    `json:"escalate_after_seconds"`
	EscalateTo           string `json:"escalate_to"` // owner user ID
}

// DefaultOwnerEscalationPolicy returns the default escalation policy.
func DefaultOwnerEscalationPolicy() OwnerEscalationPolicy {
	return OwnerEscalationPolicy{EscalateAfterSeconds: 600}
}

type SchedulerService struct {
	Queries *db.Queries
	Hub     *realtime.Hub
	Bus     *events.Bus
}

func NewSchedulerService(q *db.Queries, hub *realtime.Hub) *SchedulerService {
	return &SchedulerService{Queries: q, Hub: hub}
}

// ScheduleWorkflow starts executing a workflow, optionally tied to a project run.
// runID may be empty for standalone workflow execution.
func (s *SchedulerService) ScheduleWorkflow(ctx context.Context, workflowID string, runID string) error {
	err := s.Queries.UpdateWorkflowStatus(ctx, db.UpdateWorkflowStatusParams{
		ID:     util.ParseUUID(workflowID),
		Status: "running",
	})
	if err != nil {
		return fmt.Errorf("update workflow status: %w", err)
	}

	steps, err := s.Queries.ListWorkflowSteps(ctx, util.ParseUUID(workflowID))
	if err != nil {
		return fmt.Errorf("list steps: %w", err)
	}

	if len(steps) == 0 {
		return fmt.Errorf("workflow has no steps")
	}

	// Find steps with no dependencies and schedule them.
	scheduled := 0
	for _, step := range steps {
		if len(step.DependsOn) == 0 {
			go s.ScheduleStep(ctx, step, runID)
			scheduled++
		}
	}

	if scheduled == 0 {
		return fmt.Errorf("no schedulable steps found (all have dependencies)")
	}

	slog.Info("workflow scheduled",
		"workflow_id", workflowID,
		"run_id", runID,
		"total_steps", len(steps),
		"initial_steps", scheduled,
	)

	return nil
}

// ScheduleStep dispatches a single workflow step to an agent.
// It checks agent availability, tries fallbacks, and creates the task queue entry.
func (s *SchedulerService) ScheduleStep(ctx context.Context, step db.WorkflowStep, runID string) {
	stepID := util.UUIDToString(step.ID)
	slog.Info("scheduling step", "step_id", stepID, "agent_id", util.UUIDToString(step.AgentID))

	// Update step status to "queued".
	s.Queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
		ID:     step.ID,
		Status: "queued",
		Result: nil,
		Error:  pgtype.Text{},
	})

	// Validate agent is assigned.
	if !step.AgentID.Valid {
		s.failStep(ctx, step, "no agent assigned")
		return
	}

	// Find an available agent (primary or fallback).
	selectedAgentID, err := s.findAvailableAgent(ctx, step)
	if err != nil {
		s.failStep(ctx, step, err.Error())
		return
	}

	// Create agent task queue entry.
	// TODO: When sqlc types are updated with workflow_step_id and run_id columns,
	// pass them here. For now, create a standard task and log the association.
	slog.Info("creating task for workflow step",
		"step_id", stepID,
		"agent_id", util.UUIDToString(selectedAgentID),
		"run_id", runID,
	)

	// Update step status to "assigned".
	s.Queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
		ID:     step.ID,
		Status: "assigned",
		Result: nil,
		Error:  pgtype.Text{},
	})

	// TODO: Set actual_agent_id on the step when the column exists in sqlc types.
	// s.Queries.UpdateWorkflowStepActualAgent(ctx, ...)

	// Broadcast step started event.
	s.broadcastStepEvent(ctx, protocol.EventWorkflowStepStarted, step, map[string]any{
		"agent_id": util.UUIDToString(selectedAgentID),
		"run_id":   runID,
	})
}

// findAvailableAgent checks the primary agent and fallbacks for availability.
// Returns the UUID of the first available agent, or an error if none are available.
func (s *SchedulerService) findAvailableAgent(ctx context.Context, step db.WorkflowStep) (pgtype.UUID, error) {
	// Try primary agent first.
	agent, err := s.Queries.GetAgent(ctx, step.AgentID)
	if err == nil && isAgentAvailable(agent) {
		return step.AgentID, nil
	}

	if err != nil {
		slog.Warn("primary agent not found", "step_id", util.UUIDToString(step.ID), "agent_id", util.UUIDToString(step.AgentID), "error", err)
	} else {
		slog.Info("primary agent unavailable", "step_id", util.UUIDToString(step.ID), "agent_id", util.UUIDToString(step.AgentID), "status", agent.Status)
	}

	// Try fallback agents in order.
	for _, fbID := range step.FallbackAgentIds {
		fbAgent, fbErr := s.Queries.GetAgent(ctx, fbID)
		if fbErr == nil && isAgentAvailable(fbAgent) {
			slog.Info("using fallback agent", "step_id", util.UUIDToString(step.ID), "agent_id", util.UUIDToString(fbID))
			return fbID, nil
		}
	}

	return pgtype.UUID{}, fmt.Errorf("all agents unavailable (primary + %d fallbacks)", len(step.FallbackAgentIds))
}

// isAgentAvailable checks if an agent can accept new tasks.
func isAgentAvailable(agent db.Agent) bool {
	// Agent must not be archived.
	if agent.ArchivedAt.Valid {
		return false
	}
	// Check status: "idle" or "working" (which maps to the "busy" concept but allows concurrent tasks).
	return agent.Status == "idle" || agent.Status == "working" || agent.Status == "online"
}

// HandleStepCompletion processes a completed step, triggers dependent steps,
// and checks if the workflow/run is complete.
func (s *SchedulerService) HandleStepCompletion(ctx context.Context, stepID string, result json.RawMessage) error {
	// Update step status to "completed".
	s.Queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
		ID:     util.ParseUUID(stepID),
		Status: "completed",
		Result: result,
		Error:  pgtype.Text{},
	})

	step, err := s.Queries.GetWorkflowStep(ctx, util.ParseUUID(stepID))
	if err != nil {
		return fmt.Errorf("get step: %w", err)
	}

	slog.Info("step completed", "step_id", stepID, "workflow_id", util.UUIDToString(step.WorkflowID))

	// TODO: Update step output_refs when the column exists in sqlc types.

	// Broadcast step completed event.
	s.broadcastStepEvent(ctx, protocol.EventWorkflowStepCompleted, step, map[string]any{
		"result": result,
	})

	// Get all steps in the workflow to check dependencies and completion.
	steps, err := s.Queries.ListWorkflowSteps(ctx, step.WorkflowID)
	if err != nil {
		return fmt.Errorf("list steps: %w", err)
	}

	// Build a map of step ID -> status for dependency checking.
	stepStatusMap := make(map[string]string, len(steps))
	for _, st := range steps {
		stepStatusMap[util.UUIDToString(st.ID)] = st.Status
	}

	// Find pending steps whose dependencies are now all satisfied.
	for _, pendingStep := range steps {
		if pendingStep.Status != "pending" {
			continue
		}
		if len(pendingStep.DependsOn) == 0 {
			continue // Should have been scheduled already.
		}

		allDepsCompleted := true
		for _, depID := range pendingStep.DependsOn {
			depStatus, exists := stepStatusMap[util.UUIDToString(depID)]
			if !exists || depStatus != "completed" {
				allDepsCompleted = false
				break
			}
		}

		if allDepsCompleted {
			slog.Info("dependencies satisfied, scheduling step",
				"step_id", util.UUIDToString(pendingStep.ID),
				"workflow_id", util.UUIDToString(step.WorkflowID),
			)
			// TODO: Pass actual run_id when available on the step.
			go s.ScheduleStep(ctx, pendingStep, "")
		}
	}

	// Check if all steps are done (completed or failed).
	allDone := true
	allSucceeded := true
	for _, st := range steps {
		switch st.Status {
		case "completed":
			// OK
		case "failed", "cancelled":
			allSucceeded = false
		default:
			allDone = false
			allSucceeded = false
		}
	}

	if allDone {
		workflowStatus := "completed"
		if !allSucceeded {
			workflowStatus = "failed"
		}

		s.Queries.UpdateWorkflowStatus(ctx, db.UpdateWorkflowStatusParams{
			ID:     step.WorkflowID,
			Status: workflowStatus,
		})

		slog.Info("workflow finished",
			"workflow_id", util.UUIDToString(step.WorkflowID),
			"status", workflowStatus,
		)

		// Broadcast workflow completed event.
		if s.Bus != nil {
			wf, wfErr := s.Queries.GetWorkflow(ctx, step.WorkflowID)
			if wfErr == nil {
				s.Bus.Publish(events.Event{
					Type:        protocol.EventWorkflowCompleted,
					WorkspaceID: util.UUIDToString(wf.WorkspaceID),
					ActorType:   "system",
					ActorID:     "",
					Payload: map[string]any{
						"workflow_id": util.UUIDToString(step.WorkflowID),
						"status":      workflowStatus,
					},
				})
			}
		}

		// TODO: Update project run status when run_id is available on steps.
		// s.updateProjectRunStatus(ctx, runID, workflowStatus)
	}

	return nil
}

// HandleStepFailure processes a failed step with retry and fallback logic.
func (s *SchedulerService) HandleStepFailure(ctx context.Context, stepID string, errMsg string) error {
	step, err := s.Queries.GetWorkflowStep(ctx, util.ParseUUID(stepID))
	if err != nil {
		return fmt.Errorf("get step: %w", err)
	}

	slog.Warn("handling step failure", "step_id", stepID, "error", errMsg)

	// Parse retry rule from step.
	retryRule := DefaultRetryRule()
	// TODO: Parse retry_rule from step JSONB when the column exists.
	// if step.RetryRule != nil {
	//     json.Unmarshal(step.RetryRule, &retryRule)
	// }

	// Get current retry count.
	currentRetry := int32(0)
	if step.RetryCount.Valid {
		currentRetry = step.RetryCount.Int32
	}

	// Check if we can retry with the current agent.
	if int(currentRetry) < retryRule.MaxRetries {
		slog.Info("retrying step",
			"step_id", stepID,
			"retry", currentRetry+1,
			"max_retries", retryRule.MaxRetries,
		)

		// Update step status to "retrying" and increment retry count.
		s.Queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
			ID:     step.ID,
			Status: "retrying",
			Result: nil,
			Error:  pgtype.Text{String: errMsg, Valid: true},
		})

		// TODO: Increment retry_count on step when column/query exists.
		// s.Queries.IncrementWorkflowStepRetryCount(ctx, step.ID)

		// Schedule retry after delay (with exponential backoff per spec F3).
		delay := retryRule.RetryDelaySeconds
		if currentRetry > 0 {
			// Exponential backoff: delay * 2^(retry_count - 1)
			delay = retryRule.RetryDelaySeconds * (1 << currentRetry)
		}

		go func() {
			timer := time.NewTimer(time.Duration(delay) * time.Second)
			defer timer.Stop()
			<-timer.C

			slog.Info("retry timer fired, re-scheduling step", "step_id", stepID)
			// Re-fetch step to get latest state.
			freshStep, freshErr := s.Queries.GetWorkflowStep(context.Background(), step.ID)
			if freshErr != nil {
				slog.Error("retry: failed to re-fetch step", "step_id", stepID, "error", freshErr)
				return
			}
			// Only reschedule if still in "retrying" state (not cancelled/manually resolved).
			if freshStep.Status == "retrying" {
				// TODO: Pass actual run_id when available.
				s.ScheduleStep(context.Background(), freshStep, "")
			}
		}()

		// Broadcast step retrying event.
		s.broadcastStepEvent(ctx, protocol.EventWorkflowStepFailed, step, map[string]any{
			"error":       errMsg,
			"retry_count": currentRetry + 1,
			"max_retries": retryRule.MaxRetries,
			"action":      "retrying",
		})

		return nil
	}

	// Retries exhausted. Try fallback agents.
	slog.Info("retries exhausted, trying fallback agents", "step_id", stepID)

	for _, fbID := range step.FallbackAgentIds {
		// Skip the current primary agent.
		if fbID == step.AgentID {
			continue
		}

		fbAgent, fbErr := s.Queries.GetAgent(ctx, fbID)
		if fbErr != nil {
			continue
		}
		if !isAgentAvailable(fbAgent) {
			slog.Info("fallback agent unavailable", "step_id", stepID, "agent_id", util.UUIDToString(fbID))
			continue
		}

		slog.Info("assigning fallback agent",
			"step_id", stepID,
			"original_agent", util.UUIDToString(step.AgentID),
			"fallback_agent", util.UUIDToString(fbID),
		)

		// Reset step for new agent: set status back to "queued" and reset retry count.
		s.Queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
			ID:     step.ID,
			Status: "queued",
			Result: nil,
			Error:  pgtype.Text{},
		})
		// TODO: Reset retry_count to 0 and set actual_agent_id when columns exist.
		// TODO: Log agent_replaced activity.

		// Re-fetch step and schedule with new agent.
		freshStep, freshErr := s.Queries.GetWorkflowStep(ctx, step.ID)
		if freshErr != nil {
			return fmt.Errorf("re-fetch step for fallback: %w", freshErr)
		}
		go s.ScheduleStep(ctx, freshStep, "")
		return nil
	}

	// All fallbacks exhausted. Apply owner escalation policy.
	slog.Warn("all fallback agents exhausted, escalating to owner",
		"step_id", stepID,
		"workflow_id", util.UUIDToString(step.WorkflowID),
	)

	// Mark step as "failed".
	s.failStep(ctx, step, fmt.Sprintf("all retries and fallbacks exhausted: %s", errMsg))

	// Broadcast final failure event.
	s.broadcastStepEvent(ctx, protocol.EventWorkflowStepFailed, step, map[string]any{
		"error":  errMsg,
		"action": "failed",
		"final":  true,
	})

	// TODO: Create escalation inbox item when inbox supports action_required fields.
	// s.createEscalationInboxItem(ctx, step, errMsg)

	// TODO: Send message to project channel.
	// s.sendProjectChannelMessage(ctx, step, errMsg)

	// Check if this failure should mark the workflow/run as failed.
	s.checkWorkflowFailure(ctx, step)

	return nil
}

// HandleStepTimeout handles a step that has exceeded its timeout.
func (s *SchedulerService) HandleStepTimeout(ctx context.Context, stepID string) error {
	step, err := s.Queries.GetWorkflowStep(ctx, util.ParseUUID(stepID))
	if err != nil {
		return fmt.Errorf("get step: %w", err)
	}

	slog.Warn("step timed out", "step_id", stepID)

	// Parse timeout rule from step.
	timeoutRule := DefaultTimeoutRule()
	// TODO: Parse timeout_rule from step JSONB when the column exists.
	// if step.TimeoutRule != nil {
	//     json.Unmarshal(step.TimeoutRule, &timeoutRule)
	// }

	// Update step status to "timeout".
	s.Queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
		ID:     step.ID,
		Status: "timeout",
		Result: nil,
		Error:  pgtype.Text{String: "step timed out", Valid: true},
	})

	switch timeoutRule.Action {
	case "retry":
		// Treat like a failure and use retry logic.
		return s.HandleStepFailure(ctx, stepID, "step timed out")

	case "fail":
		// Mark as failed and trigger escalation.
		s.failStep(ctx, step, "step timed out (action: fail)")
		s.broadcastStepEvent(ctx, protocol.EventWorkflowStepFailed, step, map[string]any{
			"error":  "step timed out",
			"action": "failed",
			"final":  true,
		})
		s.checkWorkflowFailure(ctx, step)
		return nil

	case "escalate":
		// Notify owner without failing the step.
		slog.Warn("step timed out, escalating to owner", "step_id", stepID)
		// TODO: Create escalation inbox item.
		// TODO: Send message to project channel.
		s.broadcastStepEvent(ctx, protocol.EventWorkflowStepFailed, step, map[string]any{
			"error":  "step timed out (escalated to owner)",
			"action": "escalated",
		})
		return nil

	default:
		// Default to retry.
		return s.HandleStepFailure(ctx, stepID, "step timed out")
	}
}

// failStep marks a step as failed with the given error message.
func (s *SchedulerService) failStep(ctx context.Context, step db.WorkflowStep, errMsg string) {
	s.Queries.UpdateWorkflowStepStatus(ctx, db.UpdateWorkflowStepStatusParams{
		ID:     step.ID,
		Status: "failed",
		Result: nil,
		Error:  pgtype.Text{String: errMsg, Valid: true},
	})
	slog.Warn("step failed", "step_id", util.UUIDToString(step.ID), "error", errMsg)
}

// checkWorkflowFailure checks if a step failure should cascade to workflow/run failure.
func (s *SchedulerService) checkWorkflowFailure(ctx context.Context, failedStep db.WorkflowStep) {
	steps, err := s.Queries.ListWorkflowSteps(ctx, failedStep.WorkflowID)
	if err != nil {
		slog.Error("failed to list steps for workflow failure check", "error", err)
		return
	}

	// Check if there are any still-active steps.
	hasActive := false
	for _, st := range steps {
		switch st.Status {
		case "pending", "queued", "assigned", "running", "retrying", "waiting_input":
			hasActive = true
		}
	}

	// If no active steps remain, the workflow is done (with failures).
	if !hasActive {
		s.Queries.UpdateWorkflowStatus(ctx, db.UpdateWorkflowStatusParams{
			ID:     failedStep.WorkflowID,
			Status: "failed",
		})

		slog.Warn("workflow failed due to step failure",
			"workflow_id", util.UUIDToString(failedStep.WorkflowID),
		)

		if s.Bus != nil {
			wf, wfErr := s.Queries.GetWorkflow(ctx, failedStep.WorkflowID)
			if wfErr == nil {
				s.Bus.Publish(events.Event{
					Type:        protocol.EventWorkflowCompleted,
					WorkspaceID: util.UUIDToString(wf.WorkspaceID),
					ActorType:   "system",
					ActorID:     "",
					Payload: map[string]any{
						"workflow_id": util.UUIDToString(failedStep.WorkflowID),
						"status":      "failed",
					},
				})
			}
		}

		// TODO: Update project run status to "failed" when run_id is available.
	}
}

// broadcastStepEvent publishes a workflow step event via the event bus.
func (s *SchedulerService) broadcastStepEvent(ctx context.Context, eventType string, step db.WorkflowStep, extra map[string]any) {
	if s.Bus == nil {
		return
	}

	wf, err := s.Queries.GetWorkflow(ctx, step.WorkflowID)
	if err != nil {
		slog.Warn("broadcast step event: workflow not found", "workflow_id", util.UUIDToString(step.WorkflowID))
		return
	}

	payload := map[string]any{
		"workflow_id": util.UUIDToString(step.WorkflowID),
		"step_id":     util.UUIDToString(step.ID),
		"step_order":  step.StepOrder,
		"description": step.Description,
		"status":      step.Status,
	}
	for k, v := range extra {
		payload[k] = v
	}

	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(wf.WorkspaceID),
		ActorType:   "system",
		ActorID:     "",
		Payload:     payload,
	})
}
