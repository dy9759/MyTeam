// Package service: scheduler.go — SchedulerService orchestrates the Task /
// ParticipantSlot / Execution state machines per Plan 5 PRD §9.3 + §5.
//
// Lifecycle:
//
//   ScheduleRun(plan,run): reset all tasks/slots, then ScheduleTask each
//   task with no unmet depends_on dependencies.
//
//   ScheduleTask(task): activate before_execution slots; if a blocking
//   human_input slot becomes ready the task transitions to needs_human and
//   waits for the human. Otherwise pick an available agent (primary →
//   fallback), create an Execution row, and put the task into queued.
//
//   HandleTaskCompletion(task,exec,result): mark agent_execution slot
//   submitted, persist a result Artifact, activate before_done slots; if a
//   human_review slot becomes ready the task moves to under_review and we
//   wait for ReviewService to drive it forward. Otherwise the task is
//   completed, downstream tasks whose deps are now satisfied are scheduled,
//   and the run is checked for terminal state.
//
//   HandleTaskFailure(task,exec,err): retry within retry_rule.max_retries,
//   then try the next fallback agent, then give up by setting the task to
//   needs_attention. HandleTaskTimeout is the same path with err=timeout.
//
// Dependency direction: scheduler depends on Slots / Artifacts / Reviews /
// Quota and publishes events through Bus + Hub. It does not call any of
// these owners back — they only flow into the scheduler via the public
// methods above.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Task status constants — mirror the CHECK constraint in migration 055.
// (TaskStatusRunning / TaskStatusCompleted / TaskStatusNeedsAttention live
// in review.go as the canonical home; we re-declare the rest here so the
// state machine stays grep-able from this file.)
const (
	TaskStatusDraft       = "draft"
	TaskStatusReady       = "ready"
	TaskStatusQueued      = "queued"
	TaskStatusAssigned    = "assigned"
	TaskStatusNeedsHuman  = "needs_human"
	TaskStatusUnderReview = "under_review"
	TaskStatusFailed      = "failed"
	TaskStatusCancelled   = "cancelled"
)

// TaskRetryRule mirrors task.retry_rule JSONB. Defaults match the SQL
// default in migration 055 (max_retries=2, retry_delay_seconds=30).
type TaskRetryRule struct {
	MaxRetries        int `json:"max_retries"`
	RetryDelaySeconds int `json:"retry_delay_seconds"`
}

// TaskTimeoutRule mirrors task.timeout_rule JSONB. Defaults match the SQL
// default (max_duration_seconds=1800, action="retry").
type TaskTimeoutRule struct {
	MaxDurationSeconds int    `json:"max_duration_seconds"`
	Action             string `json:"action"`
}

// TaskEscalationPolicy mirrors task.escalation_policy JSONB.
type TaskEscalationPolicy struct {
	EscalateAfterSeconds int    `json:"escalate_after_seconds"`
	EscalateTo           string `json:"escalate_to"`
}

// SchedulerService drives Task → Slot → Execution transitions.
type SchedulerService struct {
	Q         *db.Queries
	Slots     *SlotService
	Artifacts *ArtifactService
	Reviews   *ReviewService
	Quota     *QuotaService
	Bus       *events.Bus
	Hub       *realtime.Hub
}

// NewSchedulerService constructs a scheduler bound to its dependencies. All
// dependencies (except Bus/Hub) should be non-nil; the scheduler degrades
// gracefully when Bus or Hub is nil, but the slot/artifact/review/quota
// services are core to the Task lifecycle.
func NewSchedulerService(
	q *db.Queries,
	slots *SlotService,
	artifacts *ArtifactService,
	reviews *ReviewService,
	quota *QuotaService,
	bus *events.Bus,
	hub *realtime.Hub,
) *SchedulerService {
	return &SchedulerService{
		Q:         q,
		Slots:     slots,
		Artifacts: artifacts,
		Reviews:   reviews,
		Quota:     quota,
		Bus:       bus,
		Hub:       hub,
	}
}

// ScheduleRun kicks off a Plan execution: it resets all tasks/slots back
// to their initial state, then schedules each task whose depends_on are
// satisfied (vacuously true for tasks with no deps).
//
// Per-task scheduling failures are logged and the loop continues so a
// single bad task does not abort the whole run.
func (s *SchedulerService) ScheduleRun(ctx context.Context, planID, runID uuid.UUID) error {
	// 1. Reset all tasks for this plan to point at the new run + status=draft.
	if err := s.Q.ResetTaskForNewRun(ctx, db.ResetTaskForNewRunParams{
		RunID:  toPgUUID(runID),
		PlanID: toPgUUID(planID),
	}); err != nil {
		return fmt.Errorf("reset tasks: %w", err)
	}

	// 2. Reset all slots for those tasks back to waiting.
	tasks, err := s.Q.ListTasksByPlan(ctx, toPgUUID(planID))
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}
	taskIDs := make([]uuid.UUID, 0, len(tasks))
	for _, t := range tasks {
		if t.ID.Valid {
			taskIDs = append(taskIDs, uuid.UUID(t.ID.Bytes))
		}
	}
	if s.Slots != nil {
		if err := s.Slots.ResetForNewRun(ctx, taskIDs); err != nil {
			slog.Warn("scheduler: slot reset failed", "plan_id", planID, "err", err)
		}
	}

	// 3. Move tasks with no unmet deps from draft → ready, then schedule.
	for _, t := range tasks {
		deps := pgUUIDsToUUIDs(t.DependsOn)
		if !s.allDepsTerminal(tasks, deps) {
			continue // Stays draft; will be scheduled when deps complete.
		}
		ready, err := s.Q.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
			ID:     t.ID,
			Status: TaskStatusReady,
		})
		if err != nil {
			slog.Warn("scheduler: mark ready failed", "task", uuid.UUID(t.ID.Bytes), "err", err)
			continue
		}
		if err := s.ScheduleTask(ctx, ready); err != nil {
			slog.Warn("scheduler: ScheduleTask failed", "task", uuid.UUID(t.ID.Bytes), "err", err)
		}
	}
	return nil
}

// ScheduleTask drives one task through the pre-execution side of the
// state machine:
//
//  1. Activate before_execution slots (waiting → ready). If any blocking
//     human_input slot becomes ready, the task transitions to needs_human
//     and we return — the human's input will eventually trigger the next
//     ScheduleTask.
//  2. Find an available agent. If none, mark needs_attention.
//  3. Create an Execution row pre-populated with the task payload and put
//     the task into queued + assigned to the chosen agent.
//
// The actual execution.status transitions (claimed → running → completed)
// are owned by the daemon / cloud executor.
func (s *SchedulerService) ScheduleTask(ctx context.Context, task db.Task) error {
	// 1. Activate before_execution slots.
	var activated []db.ParticipantSlot
	if s.Slots != nil {
		var err error
		activated, err = s.Slots.ActivateBeforeExecution(ctx, uuid.UUID(task.ID.Bytes))
		if err != nil {
			return fmt.Errorf("activate before_execution: %w", err)
		}
	}

	// 2. If any blocking human_input slot is now ready, defer to the human.
	for _, slot := range activated {
		if slot.SlotType == SlotTypeHumanInput && slot.Blocking {
			if _, err := s.Q.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
				ID:     task.ID,
				Status: TaskStatusNeedsHuman,
			}); err != nil {
				return fmt.Errorf("set needs_human: %w", err)
			}
			s.publishTaskStatusChange(ctx, task, TaskStatusNeedsHuman)
			return nil
		}
	}

	// 3. Find an available agent.
	agentID, runtimeID, ok := s.findAvailableAgent(ctx, task)
	if !ok {
		if _, err := s.Q.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
			ID:     task.ID,
			Status: TaskStatusNeedsAttention,
		}); err != nil {
			return fmt.Errorf("set needs_attention: %w", err)
		}
		s.publishTaskStatusChange(ctx, task, TaskStatusNeedsAttention)
		return nil
	}

	// 4. Build the execution payload.
	payload, err := json.Marshal(map[string]any{
		"task_id":             uuid.UUID(task.ID.Bytes).String(),
		"title":               task.Title,
		"description":         valOrEmpty(task.Description),
		"acceptance_criteria": valOrEmpty(task.AcceptanceCriteria),
		"input_context_refs":  rawJSONOrNull(task.InputContextRefs),
	})
	if err != nil {
		return fmt.Errorf("marshal execution payload: %w", err)
	}

	if _, err := s.Q.CreateExecution(ctx, db.CreateExecutionParams{
		TaskID:    task.ID,
		RunID:     task.RunID,
		AgentID:   toPgUUID(agentID),
		RuntimeID: toPgUUID(runtimeID),
		Payload:   payload,
	}); err != nil {
		return fmt.Errorf("create execution: %w", err)
	}

	// 5. Assign agent + put task into queued.
	if err := s.Q.AssignTaskAgent(ctx, db.AssignTaskAgentParams{
		ID:            task.ID,
		ActualAgentID: toPgUUID(agentID),
	}); err != nil {
		return fmt.Errorf("assign agent: %w", err)
	}
	if _, err := s.Q.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
		ID:     task.ID,
		Status: TaskStatusQueued,
	}); err != nil {
		return fmt.Errorf("set queued: %w", err)
	}
	s.publishTaskStatusChange(ctx, task, TaskStatusQueued)
	return nil
}

// HandleTaskCompletion is invoked when an Execution finishes successfully.
// It assumes the caller has already marked the Execution row completed.
//
//  1. Mark the agent_execution slot submitted (it was in ready/in_progress).
//  2. Persist a result Artifact when result is non-empty.
//  3. Activate before_done slots; if a human_review slot becomes ready the
//     task transitions to under_review and we wait for ReviewService.
//  4. Otherwise the task is completed; cascade to downstream tasks whose
//     deps are now satisfied, and check whether the run itself is done.
func (s *SchedulerService) HandleTaskCompletion(ctx context.Context, taskID, executionID uuid.UUID, result map[string]any) error {
	task, err := s.Q.GetTask(ctx, toPgUUID(taskID))
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}

	// 1. Mark agent_execution slot submitted (best-effort).
	if s.Slots != nil {
		slots, err := s.Q.ListSlotsByTask(ctx, task.ID)
		if err != nil {
			slog.Warn("scheduler: list slots for completion failed", "task", taskID, "err", err)
		}
		for _, slot := range slots {
			if slot.SlotType != SlotTypeAgentExecution {
				continue
			}
			if slot.Status != SlotStatusReady && slot.Status != SlotStatusInProgress {
				continue
			}
			if _, err := s.Slots.MarkSubmitted(ctx, uuid.UUID(slot.ID.Bytes)); err != nil {
				slog.Warn("scheduler: mark agent_execution submitted failed",
					"slot", slot.ID, "err", err)
			}
		}
	}

	// 2. Persist a result Artifact if a payload was provided.
	if len(result) > 0 && s.Artifacts != nil {
		var createdByID uuid.UUID
		if task.ActualAgentID.Valid {
			createdByID = uuid.UUID(task.ActualAgentID.Bytes)
		}
		if _, err := s.Artifacts.CreateHeadless(ctx, CreateHeadlessRequest{
			TaskID:        taskID,
			ExecutionID:   executionID,
			RunID:         pgUUIDToUUID(task.RunID),
			ArtifactType:  ArtifactTypeReport,
			Title:         task.Title,
			Content:       result,
			CreatedByID:   createdByID,
			CreatedByType: "agent",
		}); err != nil {
			slog.Warn("scheduler: create artifact failed", "task", taskID, "err", err)
		}
	}

	// 3. Activate before_done slots. The activated slice tells us which
	//    slots were *flipped* this call — useful for logging/notifications
	//    but NOT for deciding whether the task can complete: a previous
	//    HandleTaskCompletion invocation may have already activated the
	//    blocking review slots, so a second call would see activated=[]
	//    and incorrectly fall through to TaskStatusCompleted, silently
	//    bypassing the human review gate. Query the slot table directly.
	if s.Slots != nil {
		_, _ = s.Slots.ActivateBeforeDone(ctx, taskID)
	}
	blockingReviewCount, err := s.Q.CountBlockingReviewSlots(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("count blocking review slots: %w", err)
	}

	newStatus := TaskStatusCompleted
	if blockingReviewCount > 0 {
		newStatus = TaskStatusUnderReview
	}
	if _, err := s.Q.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
		ID:     task.ID,
		Status: newStatus,
	}); err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	s.publishTaskStatusChange(ctx, task, newStatus)

	// 4. If the task is fully done, schedule downstream tasks + check run.
	if newStatus == TaskStatusCompleted {
		s.scheduleDownstreamReady(ctx, task)
		if err := s.checkRunCompletion(ctx, pgUUIDToUUID(task.RunID)); err != nil {
			slog.Warn("scheduler: run completion check failed",
				"run", task.RunID, "err", err)
		}
	}
	return nil
}

// HandleTaskFailure applies the retry policy when an Execution fails.
//
//  1. If current_retry < retry_rule.max_retries → IncrementTaskRetry and
//     re-ScheduleTask (with the same agent, since we haven't exhausted
//     retries on it yet).
//  2. Else if a fallback agent is available → switch to it and reset the
//     retry counter, then re-ScheduleTask.
//  3. Else mark the task needs_attention and persist the error message.
//
// TODO(plan5): create an inbox notification for the task owner when we
// fall through to needs_attention. Wired in a follow-up batch.
func (s *SchedulerService) HandleTaskFailure(ctx context.Context, taskID, executionID uuid.UUID, errorMsg string) error {
	_ = executionID // reserved for future per-execution audit; current path acts on Task.
	task, err := s.Q.GetTask(ctx, toPgUUID(taskID))
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}

	// Parse retry policy (default if JSON is empty/invalid).
	retryRule := TaskRetryRule{MaxRetries: 2, RetryDelaySeconds: 30}
	if len(task.RetryRule) > 0 {
		if err := json.Unmarshal(task.RetryRule, &retryRule); err != nil {
			slog.Warn("scheduler: parse retry_rule failed, using defaults",
				"task", taskID, "err", err)
		}
	}

	// 1. Retry on the same agent if we have budget left.
	if int(task.CurrentRetry) < retryRule.MaxRetries {
		if err := s.Q.IncrementTaskRetry(ctx, task.ID); err != nil {
			return fmt.Errorf("increment retry: %w", err)
		}
		// Re-schedule with the same agent (the existing primary/fallback set
		// will pick the same one). ScheduleTask creates a fresh Execution row.
		return s.ScheduleTask(ctx, task)
	}

	// 2. Try the next fallback agent (skipping the one that just failed).
	for _, fbBytes := range task.FallbackAgentIds {
		if !fbBytes.Valid {
			continue
		}
		fbID := uuid.UUID(fbBytes.Bytes)
		if task.ActualAgentID.Valid && fbBytes.Bytes == task.ActualAgentID.Bytes {
			continue
		}
		if err := s.Q.AssignTaskAgent(ctx, db.AssignTaskAgentParams{
			ID:            task.ID,
			ActualAgentID: toPgUUID(fbID),
		}); err != nil {
			slog.Warn("scheduler: assign fallback agent failed",
				"task", taskID, "fallback", fbID, "err", err)
			continue
		}
		// Re-fetch so we have the updated actual_agent_id when computing
		// candidates inside ScheduleTask.
		updated, getErr := s.Q.GetTask(ctx, task.ID)
		if getErr != nil {
			slog.Warn("scheduler: re-fetch task for fallback failed",
				"task", taskID, "err", getErr)
			updated = task
		}
		return s.ScheduleTask(ctx, updated)
	}

	// 3. Out of options → needs_attention.
	if err := s.Q.SetTaskError(ctx, db.SetTaskErrorParams{
		ID:     task.ID,
		Status: TaskStatusNeedsAttention,
		Error:  toPgNullText(errorMsg),
	}); err != nil {
		return fmt.Errorf("set task error: %w", err)
	}
	s.publishTaskStatusChange(ctx, task, TaskStatusNeedsAttention)
	// TODO(plan5): create an inbox notification for the task owner.
	return nil
}

// HandleTaskTimeout is the entry point for the lifecycle ticker when it
// detects a task whose execution has exceeded its timeout. Currently this
// is just HandleTaskFailure with a fixed error string; per PRD §5.4 the
// timeout_rule.action ("retry"|"fail"|"escalate") will eventually steer
// behaviour here.
func (s *SchedulerService) HandleTaskTimeout(ctx context.Context, taskID uuid.UUID) error {
	return s.HandleTaskFailure(ctx, taskID, uuid.Nil, "task timeout")
}

// findAvailableAgent walks the task's agent candidates and returns the
// first one whose Agent.status is idle/busy AND its runtime is online/
// degraded AND the runtime has spare capacity (current_load <
// concurrency_limit).
//
// Candidate order: actual_agent_id (when set, so a fallback chosen by
// HandleTaskFailure stays sticky on the next ScheduleTask), then the
// primary, then the fallback list. Duplicates are skipped.
//
// Returns (agentID, runtimeID, true) on success and (uuid.Nil, uuid.Nil,
// false) when no candidate qualifies.
func (s *SchedulerService) findAvailableAgent(ctx context.Context, task db.Task) (uuid.UUID, uuid.UUID, bool) {
	candidates := make([]pgtype.UUID, 0, 2+len(task.FallbackAgentIds))
	if task.ActualAgentID.Valid {
		candidates = append(candidates, task.ActualAgentID)
	}
	if task.PrimaryAssigneeID.Valid {
		candidates = append(candidates, task.PrimaryAssigneeID)
	}
	for _, fb := range task.FallbackAgentIds {
		if fb.Valid {
			candidates = append(candidates, fb)
		}
	}

	seen := make(map[[16]byte]bool, len(candidates))
	for _, candID := range candidates {
		if seen[candID.Bytes] {
			continue
		}
		seen[candID.Bytes] = true
		agent, err := s.Q.GetAgent(ctx, candID)
		if err != nil {
			continue
		}
		if agent.Status != "idle" && agent.Status != "busy" {
			continue
		}
		if !agent.RuntimeID.Valid {
			continue
		}
		runtime, err := s.Q.GetAgentRuntime(ctx, agent.RuntimeID)
		if err != nil {
			continue
		}
		if runtime.Status != "online" && runtime.Status != "degraded" {
			continue
		}
		if runtime.CurrentLoad >= runtime.ConcurrencyLimit {
			continue
		}
		return uuid.UUID(agent.ID.Bytes), uuid.UUID(runtime.ID.Bytes), true
	}
	return uuid.Nil, uuid.Nil, false
}

// scheduleDownstreamReady is called after a task completes to look at the
// task's run-mates and ScheduleTask any that were waiting on this task.
// It only considers tasks still in draft — tasks already in flight or
// terminal are left alone.
func (s *SchedulerService) scheduleDownstreamReady(ctx context.Context, completed db.Task) {
	if !completed.RunID.Valid {
		return
	}
	tasks, err := s.Q.ListTasksByRun(ctx, completed.RunID)
	if err != nil {
		slog.Warn("scheduler: list tasks for downstream check failed",
			"run", completed.RunID, "err", err)
		return
	}
	for _, t := range tasks {
		if t.Status != TaskStatusDraft {
			continue
		}
		deps := pgUUIDsToUUIDs(t.DependsOn)
		if !s.allDepsTerminal(tasks, deps) {
			continue
		}
		ready, err := s.Q.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
			ID:     t.ID,
			Status: TaskStatusReady,
		})
		if err != nil {
			slog.Warn("scheduler: mark downstream ready failed",
				"task", t.ID, "err", err)
			continue
		}
		if err := s.ScheduleTask(ctx, ready); err != nil {
			slog.Warn("scheduler: schedule downstream failed",
				"task", t.ID, "err", err)
		}
	}
}

// checkRunCompletion inspects every task in the run and, when all of them
// are in terminal state, transitions the ProjectRun to completed (when all
// tasks completed) or failed (when any task ended in failed/cancelled).
// While any task is non-terminal the run stays as it is.
func (s *SchedulerService) checkRunCompletion(ctx context.Context, runID uuid.UUID) error {
	if runID == uuid.Nil {
		return nil
	}
	tasks, err := s.Q.ListTasksByRun(ctx, toPgUUID(runID))
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}
	if len(tasks) == 0 {
		return nil
	}
	allTerminal := true
	anyFailed := false
	for _, t := range tasks {
		switch t.Status {
		case TaskStatusCompleted:
			// terminal-success
		case TaskStatusFailed, TaskStatusCancelled:
			anyFailed = true
		default:
			allTerminal = false
		}
	}
	if !allTerminal {
		return nil
	}
	newStatus := "completed"
	if anyFailed {
		newStatus = "failed"
	}
	if err := s.Q.UpdateProjectRunStatus(ctx, db.UpdateProjectRunStatusParams{
		ID:     toPgUUID(runID),
		Status: newStatus,
	}); err != nil {
		return fmt.Errorf("update run status: %w", err)
	}
	if s.Bus != nil {
		eventType := "run:completed"
		if anyFailed {
			eventType = "run:failed"
		}
		// Resolve workspace_id from run → project. AuditService and
		// ResultsReporterService both depend on event.WorkspaceID being set;
		// a missing value cascades into "audit log failed: null value in
		// column workspace_id" warnings and leaves the activity_log row
		// uncreated. Failure here is non-fatal — the event still publishes.
		var workspaceID string
		if run, runErr := s.Q.GetProjectRun(ctx, toPgUUID(runID)); runErr == nil {
			if project, projErr := s.Q.GetProject(ctx, run.ProjectID); projErr == nil {
				workspaceID = util.UUIDToString(project.WorkspaceID)
			} else {
				slog.Warn("scheduler: load project for run completion event failed",
					"run", runID, "err", projErr)
			}
		} else {
			slog.Warn("scheduler: load run for completion event failed",
				"run", runID, "err", runErr)
		}
		s.Bus.Publish(events.Event{
			Type:        eventType,
			WorkspaceID: workspaceID,
			ActorType:   "system",
			Payload: map[string]any{
				"run_id": runID.String(),
				"status": newStatus,
			},
		})
	}
	return nil
}

// allDepsTerminal returns true when every dep UUID in deps maps to a task
// in completed status. Failed/cancelled deps return false — those will be
// handled by checkRunCompletion's run-level rollup, not by skipping the
// downstream task.
func (s *SchedulerService) allDepsTerminal(allTasks []db.Task, deps []uuid.UUID) bool {
	if len(deps) == 0 {
		return true
	}
	byID := make(map[[16]byte]db.Task, len(allTasks))
	for _, t := range allTasks {
		if t.ID.Valid {
			byID[t.ID.Bytes] = t
		}
	}
	for _, d := range deps {
		t, ok := byID[d]
		if !ok || t.Status != TaskStatusCompleted {
			return false
		}
	}
	return true
}

// publishTaskStatusChange emits a task:status_changed event so the rest of
// the system can react (UI, notifiers, etc). Bus is optional — nil bus is
// a silent no-op so unit tests can run without wiring an event bus.
func (s *SchedulerService) publishTaskStatusChange(_ context.Context, task db.Task, newStatus string) {
	if s.Bus == nil {
		return
	}
	payload := map[string]any{
		"task_id": uuid.UUID(task.ID.Bytes).String(),
		"to":      newStatus,
	}
	if task.RunID.Valid {
		payload["run_id"] = uuid.UUID(task.RunID.Bytes).String()
	}
	workspaceID := ""
	if task.WorkspaceID.Valid {
		workspaceID = uuid.UUID(task.WorkspaceID.Bytes).String()
	}
	s.Bus.Publish(events.Event{
		Type:        "task:status_changed",
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload:     payload,
	})
}

// valOrEmpty returns the underlying string for a possibly-NULL pgtype.Text,
// using "" as the fallback. Used when serializing task fields into the
// execution payload.
func valOrEmpty(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

// rawJSONOrNull returns the underlying byte slice for a JSONB column when
// non-empty, or json.RawMessage("null") so the marshaled execution payload
// stays valid JSON when the column is empty.
func rawJSONOrNull(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}

// pgUUIDToUUID converts a pgtype.UUID to a uuid.UUID, returning uuid.Nil
// for NULL/invalid input.
func pgUUIDToUUID(p pgtype.UUID) uuid.UUID {
	if !p.Valid {
		return uuid.Nil
	}
	return uuid.UUID(p.Bytes)
}

// pgUUIDsToUUIDs converts a slice of pgtype.UUID to a slice of uuid.UUID,
// dropping NULL entries.
func pgUUIDsToUUIDs(ps []pgtype.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ps))
	for _, p := range ps {
		if p.Valid {
			out = append(out, uuid.UUID(p.Bytes))
		}
	}
	return out
}
