// Package service: scheduler.go — SchedulerService orchestrates the Task /
// ParticipantSlot / Execution state machines per Plan 5 PRD §9.3 + §5.
//
// Lifecycle:
//
//	ScheduleRun(plan,run): reset all tasks/slots, then ScheduleTask each
//	task with no unmet depends_on dependencies.
//
//	ScheduleTask(task): activate before_execution slots; if a blocking
//	human_input slot becomes ready the task transitions to needs_human and
//	waits for the human. Otherwise pick an available agent (primary →
//	fallback), create an Execution row, and put the task into queued.
//
//	HandleTaskCompletion(task,exec,result): mark agent_execution slot
//	submitted, persist a result Artifact, activate before_done slots; if a
//	human_review slot becomes ready the task moves to under_review and we
//	wait for ReviewService to drive it forward. Otherwise the task is
//	completed, downstream tasks whose deps are now satisfied are scheduled,
//	and the run is checked for terminal state.
//
//	HandleTaskFailure(task,exec,err): retry within retry_rule.max_retries,
//	then try the next fallback agent, then give up by setting the task to
//	needs_attention. HandleTaskTimeout is the same path with err=timeout.
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
	"strings"
	"time"

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
		// 5. Post a per-task completion message into the plan's
		// companion thread. Requirement: "each task completion posts
		// execution info into the plan thread, @ the executing agent
		// and the agent's owner". Failure is non-fatal — the state
		// machine already committed.
		if err := s.postTaskCompletionToPlanThread(ctx, task, result); err != nil {
			slog.Warn("scheduler: post task completion to plan thread failed",
				"task", taskID, "err", err)
		}
	}
	return nil
}

// postTaskCompletionToPlanThread writes a message into the companion
// thread of the plan that owns `task`. The message @-mentions the
// executing agent and that agent's owner so both get notified in the
// channel sidebar. Pure convenience — no state on failure.
func (s *SchedulerService) postTaskCompletionToPlanThread(
	ctx context.Context,
	task db.Task,
	result map[string]any,
) error {
	if !task.PlanID.Valid {
		return nil
	}
	plan, err := s.Q.GetPlan(ctx, task.PlanID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}
	if !plan.ThreadID.Valid {
		return nil // plan predates the thread feature; nothing to post into
	}

	// Resolve executing agent (actual_agent_id wins over primary_assignee_id;
	// falls back to primary when the scheduler didn't rewrite it).
	var agentUUID pgtype.UUID
	if task.ActualAgentID.Valid {
		agentUUID = task.ActualAgentID
	} else if task.PrimaryAssigneeID.Valid {
		agentUUID = task.PrimaryAssigneeID
	}

	var (
		agentMention string
		ownerMention string
		senderID     pgtype.UUID
		senderType   = "agent"
	)
	if agentUUID.Valid {
		if agent, err := s.Q.GetAgent(ctx, agentUUID); err == nil {
			agentMention = fmt.Sprintf("@%s", agent.Name)
			senderID = agent.ID
			if agent.OwnerID.Valid {
				if owner, err := s.Q.GetUser(ctx, agent.OwnerID); err == nil {
					ownerMention = fmt.Sprintf("@%s", owner.Name)
				}
			}
		}
	}
	// If no agent was resolved (edge case), fall back to the plan
	// creator so the message still persists with a valid sender.
	if !senderID.Valid {
		senderID = plan.CreatedBy
		senderType = "member"
	}

	summary := summarizeResultForThread(result)
	mentionLine := strings.TrimSpace(strings.Join(
		filterNonEmpty([]string{agentMention, ownerMention}),
		" ",
	))
	var content string
	if mentionLine != "" {
		content = fmt.Sprintf("✅ 任务完成: **%s**\n%s\n%s", task.Title, mentionLine, summary)
	} else {
		content = fmt.Sprintf("✅ 任务完成: **%s**\n%s", task.Title, summary)
	}

	meta, _ := json.Marshal(map[string]any{
		"kind":      "task_completed",
		"task_id":   pgUUIDStr(task.ID),
		"plan_id":   pgUUIDStr(plan.ID),
		"agent_id":  pgUUIDStr(agentUUID),
		"mentions":  []string{pgUUIDStr(agentUUID)},
	})

	_, err = s.Q.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID: plan.WorkspaceID,
		SenderID:    senderID,
		SenderType:  senderType,
		ChannelID:   pgtype.UUID{}, // thread-scoped message; channel pulled from thread row on the client
		ThreadID:    plan.ThreadID,
		Content:     content,
		ContentType: "text",
		Type:        "channel",
		Metadata:    meta,
	})
	return err
}

func summarizeResultForThread(result map[string]any) string {
	if result == nil {
		return "(无额外输出)"
	}
	if s, ok := result["summary"].(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	if s, ok := result["output"].(string); ok && strings.TrimSpace(s) != "" {
		if len(s) > 300 {
			return s[:300] + "…"
		}
		return s
	}
	b, err := json.Marshal(result)
	if err != nil {
		return "(结果无法序列化)"
	}
	if len(b) > 300 {
		return string(b[:300]) + "…"
	}
	return string(b)
}

func filterNonEmpty(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func pgUUIDStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

// HandleTaskFailure applies the retry policy when an Execution fails.
//
//  1. If current_retry < retry_rule.max_retries → IncrementTaskRetry and
//     re-ScheduleTask (with the same agent, since we haven't exhausted
//     retries on it yet).
//  2. Else if a fallback agent is available → switch to it and reset the
//     retry counter, then re-ScheduleTask.
//  3. Else mark the task needs_attention, persist the error message, and
//     notify the task owner via inbox so a human can intervene.
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
		// AssignTaskFallbackAgent both swaps the agent AND resets
		// current_retry to 0 in a single statement. If we used AssignTaskAgent
		// here the fallback would inherit the exhausted retry budget and
		// surface as needs_attention on its first failure (codex IMPORTANT #2).
		if err := s.Q.AssignTaskFallbackAgent(ctx, db.AssignTaskFallbackAgentParams{
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
	s.notifyTaskAttention(ctx, task, errorMsg)
	return nil
}

// HandleTaskTimeout is the entry point for the lifecycle ticker when it
// detects a task whose execution has exceeded its timeout. Per PRD §5.4 the
// timeout_rule.action steers the response:
//
//	retry    → run the standard retry/fallback policy (HandleTaskFailure).
//	fail     → mark the task failed without consuming the retry budget.
//	escalate → park the task in needs_attention and notify the owner.
//
// Defaults to retry when timeout_rule is missing or unparseable so unknown
// policies degrade safely rather than dropping the timeout silently.
func (s *SchedulerService) HandleTaskTimeout(ctx context.Context, taskID uuid.UUID) error {
	const errMsg = "task timeout"

	task, err := s.Q.GetTask(ctx, toPgUUID(taskID))
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}

	timeoutRule := TaskTimeoutRule{MaxDurationSeconds: 1800, Action: "retry"}
	if len(task.TimeoutRule) > 0 {
		if err := json.Unmarshal(task.TimeoutRule, &timeoutRule); err != nil {
			slog.Warn("scheduler: parse timeout_rule failed, defaulting to retry",
				"task", taskID, "err", err)
			timeoutRule.Action = "retry"
		}
	}
	if timeoutRule.Action == "" {
		timeoutRule.Action = "retry"
	}

	switch timeoutRule.Action {
	case "fail":
		if err := s.Q.SetTaskError(ctx, db.SetTaskErrorParams{
			ID:     task.ID,
			Status: TaskStatusFailed,
			Error:  toPgNullText(errMsg),
		}); err != nil {
			return fmt.Errorf("set task failed: %w", err)
		}
		s.publishTaskStatusChange(ctx, task, TaskStatusFailed)
		return nil
	case "escalate":
		if err := s.Q.SetTaskError(ctx, db.SetTaskErrorParams{
			ID:     task.ID,
			Status: TaskStatusNeedsAttention,
			Error:  toPgNullText(errMsg),
		}); err != nil {
			return fmt.Errorf("set task needs_attention: %w", err)
		}
		s.publishTaskStatusChange(ctx, task, TaskStatusNeedsAttention)
		s.notifyTaskAttention(ctx, task, errMsg)
		return nil
	default: // "retry" + any unknown value
		return s.HandleTaskFailure(ctx, taskID, uuid.Nil, errMsg)
	}
}

// notifyTaskAttention writes inbox items so a human knows the task is stuck.
// Best-effort: errors are logged but do not fail the caller, since we have
// already updated task.status to needs_attention by this point and that's
// the primary signal.
//
// Recipient resolution (first non-empty wins):
//  1. task.actual_agent_id → agent.owner_id (user-owned agents).
//  2. task.run_id → project_run.project_id → project.creator_owner_id
//     (system agents have no owner, so we fall back to the project creator).
//  3. workspace owners + admins (no project link, or project lookup failed).
//
// Step 3 fans out: every admin gets their own inbox row.
func (s *SchedulerService) notifyTaskAttention(ctx context.Context, task db.Task, reason string) {
	body := reason
	if body == "" {
		body = "Task requires attention"
	}
	title := fmt.Sprintf("Task needs attention: %s", task.Title)

	// 1) Try the agent's owner.
	if task.ActualAgentID.Valid {
		agent, err := s.Q.GetAgent(ctx, task.ActualAgentID)
		if err != nil {
			slog.Warn("scheduler: load agent for inbox notification failed",
				"task", task.ID, "agent", task.ActualAgentID, "err", err)
		} else if agent.OwnerID.Valid {
			s.writeAttentionInbox(ctx, task, agent.OwnerID, title, body)
			return
		}
	}

	// 2) Fall back to the project creator (per-project owner).
	if task.RunID.Valid {
		run, err := s.Q.GetProjectRun(ctx, task.RunID)
		if err != nil {
			slog.Warn("scheduler: load project_run for inbox fallback failed",
				"task", task.ID, "run", task.RunID, "err", err)
		} else {
			project, err := s.Q.GetProject(ctx, run.ProjectID)
			if err != nil {
				slog.Warn("scheduler: load project for inbox fallback failed",
					"task", task.ID, "project", run.ProjectID, "err", err)
			} else if project.CreatorOwnerID.Valid {
				s.writeAttentionInbox(ctx, task, project.CreatorOwnerID, title, body)
				return
			}
		}
	}

	// 3) Last resort: notify every workspace owner/admin so something with
	// human eyes on it sees the parked task.
	admins, err := s.Q.ListWorkspaceAdmins(ctx, task.WorkspaceID)
	if err != nil {
		slog.Warn("scheduler: list workspace admins for inbox fallback failed",
			"task", task.ID, "workspace", task.WorkspaceID, "err", err)
		return
	}
	if len(admins) == 0 {
		slog.Warn("scheduler: no recipient found for task_attention_needed inbox",
			"task", task.ID, "workspace", task.WorkspaceID)
		return
	}
	for _, m := range admins {
		s.writeAttentionInbox(ctx, task, m.UserID, title, body)
	}
}

// writeAttentionInbox is the shared writer for notifyTaskAttention so the
// CreateTaskAttentionInboxItem payload stays in one place. Errors are
// logged and swallowed because the caller treats notification as
// best-effort.
func (s *SchedulerService) writeAttentionInbox(ctx context.Context, task db.Task, recipient pgtype.UUID, title, body string) {
	if _, err := s.Q.CreateTaskAttentionInboxItem(ctx, db.CreateTaskAttentionInboxItemParams{
		WorkspaceID:    task.WorkspaceID,
		RecipientID:    recipient,
		RecipientType:  "member",
		Type:           "task_attention_needed",
		Severity:       "warning",
		Title:          title,
		Body:           toPgNullText(body),
		ActionRequired: true,
		TaskID:         task.ID,
	}); err != nil {
		slog.Warn("scheduler: create inbox notification failed",
			"task", task.ID, "recipient", recipient, "err", err)
	}
}

// findAvailableAgent picks the best candidate agent for a task using the
// weighted scoring model from cross-cutting PRD §9.2:
//
//	skill 0.35 + load 0.20 + freshness 0.10 + primary 0.25 + history 0.10
//
// Stickiness for retries: if task.actual_agent_id is set (a fallback already
// chosen by HandleTaskFailure), prefer that agent unconditionally. This keeps
// the retry sticky on the same runtime so warm caches/auth survive.
//
// For initial scheduling we collect every viable candidate (primary +
// fallbacks), score each, and return the highest. Viability gates are
// unchanged: agent.status idle/busy, runtime online/degraded, runtime has
// spare capacity.
//
// Returns (agentID, runtimeID, true) on success and (uuid.Nil, uuid.Nil,
// false) when no candidate qualifies.
func (s *SchedulerService) findAvailableAgent(ctx context.Context, task db.Task) (uuid.UUID, uuid.UUID, bool) {
	if task.ActualAgentID.Valid {
		if agentID, runtimeID, ok := s.tryViableAgent(ctx, task.ActualAgentID); ok {
			return agentID, runtimeID, true
		}
	}

	candidates := make([]pgtype.UUID, 0, 1+len(task.FallbackAgentIds))
	if task.PrimaryAssigneeID.Valid {
		candidates = append(candidates, task.PrimaryAssigneeID)
	}
	for _, fb := range task.FallbackAgentIds {
		if fb.Valid {
			candidates = append(candidates, fb)
		}
	}

	type scored struct {
		agentID, runtimeID uuid.UUID
		score              float64
	}
	picks := make([]scored, 0, len(candidates))
	seen := make(map[[16]byte]bool, len(candidates))
	for _, candID := range candidates {
		if seen[candID.Bytes] {
			continue
		}
		seen[candID.Bytes] = true
		agent, runtime, ok := s.fetchViableAgent(ctx, candID)
		if !ok {
			continue
		}
		isPrimary := task.PrimaryAssigneeID.Valid && task.PrimaryAssigneeID.Bytes == agent.ID.Bytes
		picks = append(picks, scored{
			agentID:   uuid.UUID(agent.ID.Bytes),
			runtimeID: uuid.UUID(runtime.ID.Bytes),
			score:     scoreAgentForTask(agent, runtime, task, isPrimary),
		})
	}
	if len(picks) == 0 {
		return uuid.Nil, uuid.Nil, false
	}

	best := picks[0]
	for _, p := range picks[1:] {
		if p.score > best.score {
			best = p
		}
	}
	return best.agentID, best.runtimeID, true
}

// tryViableAgent is the boolean form used for the actual_agent_id stickiness
// path — we only need (id,id,ok) without the underlying rows.
func (s *SchedulerService) tryViableAgent(ctx context.Context, id pgtype.UUID) (uuid.UUID, uuid.UUID, bool) {
	agent, runtime, ok := s.fetchViableAgent(ctx, id)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return uuid.UUID(agent.ID.Bytes), uuid.UUID(runtime.ID.Bytes), true
}

// fetchViableAgent applies the same gate as before (agent status, runtime
// status, capacity) and returns the underlying rows so callers can score
// without re-fetching.
func (s *SchedulerService) fetchViableAgent(ctx context.Context, id pgtype.UUID) (db.Agent, db.AgentRuntime, bool) {
	agent, err := s.Q.GetAgent(ctx, id)
	if err != nil {
		return db.Agent{}, db.AgentRuntime{}, false
	}
	if agent.Status != "idle" && agent.Status != "busy" {
		return db.Agent{}, db.AgentRuntime{}, false
	}
	if !agent.RuntimeID.Valid {
		return db.Agent{}, db.AgentRuntime{}, false
	}
	runtime, err := s.Q.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return db.Agent{}, db.AgentRuntime{}, false
	}
	if runtime.Status != "online" && runtime.Status != "degraded" {
		return db.Agent{}, db.AgentRuntime{}, false
	}
	if runtime.CurrentLoad >= runtime.ConcurrencyLimit {
		return db.Agent{}, db.AgentRuntime{}, false
	}
	return agent, runtime, true
}

// scoreAgentForTask implements the cross-cutting PRD §9.2 weighted score.
// All factors are normalized to [0,1] before weighting; total stays in [0,1].
//
//	skill (0.35)     fraction of task.required_skills also present in agent.tags
//	load  (0.20)     1 - current_load / concurrency_limit
//	fresh (0.10)     1 if last_active_at within 1h, decaying linearly to 0 at 24h
//	prim  (0.25)     1 if this candidate is the primary_assignee_id, else 0
//	hist  (0.10)     placeholder constant 0.5 — no per-agent success-rate table yet
//
// Tie-breaks fall through to the candidate iteration order (primary first,
// then fallbacks in declared order), which matches the legacy first-fit
// behavior when all factors are equal.
func scoreAgentForTask(agent db.Agent, runtime db.AgentRuntime, task db.Task, isPrimary bool) float64 {
	const (
		wSkill   = 0.35
		wLoad    = 0.20
		wFresh   = 0.10
		wPrimary = 0.25
		wHistory = 0.10
	)

	var skill float64
	if len(task.RequiredSkills) > 0 {
		have := make(map[string]bool, len(agent.Tags))
		for _, t := range agent.Tags {
			have[t] = true
		}
		matched := 0
		for _, req := range task.RequiredSkills {
			if have[req] {
				matched++
			}
		}
		skill = float64(matched) / float64(len(task.RequiredSkills))
	} else {
		// No required skills declared → don't punish; treat as full match.
		skill = 1.0
	}

	if runtime.ConcurrencyLimit <= 0 {
		slog.Error("scheduler invariant violated: runtime concurrency_limit must be positive",
			"runtime_id", uuid.UUID(runtime.ID.Bytes),
			"concurrency_limit", runtime.ConcurrencyLimit,
			"current_load", runtime.CurrentLoad)
		panic(fmt.Sprintf("scheduler invariant violated: runtime concurrency_limit must be positive (runtime_id=%s concurrency_limit=%d current_load=%d)",
			uuid.UUID(runtime.ID.Bytes), runtime.ConcurrencyLimit, runtime.CurrentLoad))
	}

	var load float64
	load = 1.0 - float64(runtime.CurrentLoad)/float64(runtime.ConcurrencyLimit)
	if load < 0 {
		load = 0
	}

	// Brand-new agents (no last_active_at yet) score neutral, not worst —
	// otherwise a never-pinged agent gets penalized harder than one that
	// went stale 23 hours ago, which inverts the freshness intent.
	fresh := 0.5
	if agent.LastActiveAt.Valid {
		ageHours := time.Since(agent.LastActiveAt.Time).Hours()
		switch {
		case ageHours <= 1:
			fresh = 1.0
		case ageHours >= 24:
			fresh = 0.0
		default:
			fresh = 1.0 - (ageHours-1)/23.0
		}
	}

	primary := 0.0
	if isPrimary {
		primary = 1.0
	}

	// History placeholder — neutral 0.5 until per-agent success rate is tracked.
	history := 0.5

	return wSkill*skill + wLoad*load + wFresh*fresh + wPrimary*primary + wHistory*history
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
