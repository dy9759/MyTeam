package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llmclient"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CloudExecutorService claims and executes tasks for cloud-mode agents.
//
// It polls two queues in parallel:
//
//  1. agent_task_queue (legacy Issue-link tasks) via pollAndExecute.
//  2. execution        (Plan/Task/Run model tasks) via pollAndExecuteExecutions.
//
// Both paths share the same quota gate (workspace_quota.max_concurrent_cloud_exec
// + monthly USD ceiling). Completion of an execution-path task cascades back
// into SchedulerService so downstream tasks become ready.
type CloudExecutorService struct {
	Queries     *db.Queries
	Hub         *realtime.Hub
	Bus         *events.Bus
	TaskService *TaskService
	Quota       *QuotaService
	Scheduler   *SchedulerService
}

// NewCloudExecutorService creates a new CloudExecutorService.
//
// scheduler may be nil; the execution-table poll loop will still run but
// will not cascade completion back into the Plan/Run state machine. The
// agent_task_queue path is unaffected.
func NewCloudExecutorService(queries *db.Queries, hub *realtime.Hub, bus *events.Bus, taskService *TaskService, scheduler *SchedulerService) *CloudExecutorService {
	return &CloudExecutorService{
		Queries:     queries,
		Hub:         hub,
		Bus:         bus,
		TaskService: taskService,
		Quota:       NewQuotaService(queries),
		Scheduler:   scheduler,
	}
}

// Start subscribes to task:dispatch events and starts a poll loop for pending cloud tasks.
func (s *CloudExecutorService) Start(ctx context.Context) {
	// Subscribe to task:dispatch events.
	s.Bus.Subscribe(protocol.EventTaskDispatch, func(e events.Event) {
		go s.handleDispatch(ctx, e)
	})

	// Start poll loop.
	go s.pollLoop(ctx)

	slog.Info("[cloud-executor] started")
}

func (s *CloudExecutorService) handleDispatch(ctx context.Context, e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	taskIDStr, _ := payload["task_id"].(string)
	if taskIDStr == "" {
		return
	}

	taskID := util.ParseUUID(taskIDStr)
	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil {
		return
	}

	// Only handle dispatched tasks for cloud agents.
	if task.Status != "dispatched" {
		return
	}

	agentRow, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return
	}

	runtime, err := s.Queries.GetAgentRuntime(ctx, agentRow.RuntimeID)
	if err != nil {
		return
	}
	if runtime.Mode.String != "cloud" {
		return
	}

	// Quota gate before executing a dispatched task.
	if err := s.enforceQuota(ctx, task, runtime.WorkspaceID); err != nil {
		return
	}

	s.executeTask(ctx, task, agentRow, runtime)
}

func (s *CloudExecutorService) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("[cloud-executor] poll loop stopped")
			return
		case <-ticker.C:
			s.pollAndExecute(ctx)
			s.pollAndExecuteExecutions(ctx)
		}
	}
}

func (s *CloudExecutorService) pollAndExecute(ctx context.Context) {
	tasks, err := s.Queries.ListCloudPendingTasks(ctx)
	if err != nil {
		slog.Debug("[cloud-executor] poll error", "error", err)
		return
	}

	for _, task := range tasks {
		// Resolve workspace via the task's runtime so the quota check can
		// reference the correct workspace_quota row regardless of whether
		// the task is still queued.
		runtime, err := s.Queries.GetAgentRuntime(ctx, task.RuntimeID)
		if err != nil {
			continue
		}
		if runtime.Mode.String != "cloud" {
			continue
		}

		// Workspace-level quota gate per PRD §12: enforce monthly USD
		// ceiling and concurrent cloud-exec cap before consuming a slot.
		if err := s.enforceQuota(ctx, task, runtime.WorkspaceID); err != nil {
			continue
		}

		if task.Status == "queued" {
			// Claim the task first.
			claimed, err := s.TaskService.ClaimTask(ctx, task.AgentID)
			if err != nil || claimed == nil {
				continue
			}
			task = *claimed
		}

		agentRow, err := s.Queries.GetAgent(ctx, task.AgentID)
		if err != nil {
			continue
		}

		go s.executeTask(ctx, task, agentRow, runtime)
	}
}

// enforceQuota wraps QuotaService.CheckBeforeClaim and, on quota failure,
// fails the task with the corresponding error code so the queue does not
// stay forever pending. Returns nil on pass, non-nil if the caller should
// skip this task.
func (s *CloudExecutorService) enforceQuota(ctx context.Context, task db.AgentTaskQueue, workspaceID pgtype.UUID) error {
	if s.Quota == nil || !workspaceID.Valid {
		return nil
	}

	wsUUID, err := uuid.FromBytes(workspaceID.Bytes[:])
	if err != nil {
		return nil
	}

	inflight, err := s.Queries.CountInflightCloudExecutions(ctx, workspaceID)
	if err != nil {
		slog.Warn("[cloud-executor] count inflight failed", "ws", wsUUID.String(), "error", err)
		return nil
	}

	if err := s.Quota.CheckBeforeClaim(ctx, wsUUID, int(inflight)); err != nil {
		switch {
		case errors.Is(err, ErrQuotaExceeded):
			slog.Info("[cloud-executor] quota exceeded; failing task",
				"task_id", util.UUIDToString(task.ID),
				"workspace_id", wsUUID.String())
			s.failTaskForQuota(ctx, task, "QUOTA_EXCEEDED: "+err.Error())
		case errors.Is(err, ErrQuotaConcurrentLimit):
			// Concurrent-limit is transient — leave the task queued so a
			// future poll cycle picks it up once a slot frees. Log only.
			slog.Debug("[cloud-executor] concurrent limit reached; skipping",
				"task_id", util.UUIDToString(task.ID),
				"workspace_id", wsUUID.String(),
				"inflight", inflight)
		default:
			slog.Warn("[cloud-executor] quota check error",
				"task_id", util.UUIDToString(task.ID),
				"workspace_id", wsUUID.String(),
				"error", err)
		}
		return err
	}
	return nil
}

// failTaskForQuota fails a task that hit a hard quota limit so it doesn't
// stay perpetually queued. Best-effort; failures are logged.
func (s *CloudExecutorService) failTaskForQuota(ctx context.Context, task db.AgentTaskQueue, reason string) {
	// FailAgentTask only matches dispatched/running rows. For queued tasks we
	// have to claim first so the row enters the dispatched state, then fail.
	current := task
	if current.Status == "queued" {
		claimed, err := s.TaskService.ClaimTask(ctx, current.AgentID)
		if err != nil || claimed == nil {
			slog.Debug("[cloud-executor] could not claim task to fail-on-quota",
				"task_id", util.UUIDToString(current.ID), "err", err)
			return
		}
		current = *claimed
	}
	if _, err := s.TaskService.FailTask(ctx, current.ID, reason); err != nil {
		slog.Warn("[cloud-executor] fail-on-quota failed",
			"task_id", util.UUIDToString(current.ID), "err", err)
	}
}

func (s *CloudExecutorService) executeTask(ctx context.Context, task db.AgentTaskQueue, agentRow db.Agent, runtime db.AgentRuntime) {
	taskIDStr := util.UUIDToString(task.ID)
	slog.Info("[cloud-executor] executing task", "task_id", taskIDStr)

	// Start the task.
	_, err := s.TaskService.StartTask(ctx, task.ID)
	if err != nil {
		slog.Warn("[cloud-executor] start task failed", "task_id", taskIDStr, "error", err)
		return
	}

	// Load issue context.
	issue, err := s.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		s.TaskService.FailTask(ctx, task.ID, fmt.Sprintf("failed to load issue: %v", err))
		return
	}

	// Load recent comments.
	comments, _ := s.Queries.ListComments(ctx, db.ListCommentsParams{
		IssueID:     task.IssueID,
		WorkspaceID: issue.WorkspaceID,
	})

	// Build the prompt.
	prompt := buildCloudPrompt(issue, comments, task.TriggerCommentID)

	// Parse cloud LLM config from the runtime metadata.
	llmCfg := s.buildLLMConfig(runtime)

	// Create cloud backend and execute.
	backend := agent.NewCloudBackend(llmCfg)

	systemPrompt := fmt.Sprintf(
		"You are %s, an AI assistant. You are working on issue '%s'. "+
			"Analyze the issue and provide a helpful, concise response. "+
			"Focus on actionable suggestions.",
		agentRow.Name, issue.Title,
	)

	session, err := backend.Execute(ctx, prompt, agent.ExecOptions{
		SystemPrompt: systemPrompt,
	})
	if err != nil {
		s.TaskService.FailTask(ctx, task.ID, fmt.Sprintf("cloud execute failed: %v", err))
		return
	}

	// Drain messages.
	for range session.Messages {
	}

	// Wait for result.
	result := <-session.Result

	if result.Status == "failed" {
		s.TaskService.FailTask(ctx, task.ID, result.Error)
		return
	}

	resultJSON, _ := json.Marshal(protocol.TaskCompletedPayload{
		Output: result.Output,
	})

	s.TaskService.CompleteTask(ctx, task.ID, resultJSON, "", "")
	slog.Info("[cloud-executor] task completed", "task_id", taskIDStr)
}

func (s *CloudExecutorService) buildLLMConfig(runtime db.AgentRuntime) llmclient.Config {
	cloudCfg := cloudLLMConfigFromRuntime(runtime)

	cfg := llmclient.DashScopeFromEnv()

	if cloudCfg.BaseURL != "" {
		cfg.Endpoint = cloudCfg.BaseURL
	}
	if cloudCfg.APIKey != "" {
		cfg.APIKey = cloudCfg.APIKey
	}
	if cloudCfg.Model != "" {
		cfg.Model = cloudCfg.Model
	}

	return cfg
}

func buildCloudPrompt(issue db.Issue, comments []db.Comment, triggerCommentID pgtype.UUID) string {
	var prompt string

	prompt += fmt.Sprintf("## Issue: %s\n", issue.Title)
	prompt += fmt.Sprintf("Status: %s | Priority: %s\n\n", issue.Status, issue.Priority)

	if issue.Description.Valid && issue.Description.String != "" {
		prompt += fmt.Sprintf("### Description\n%s\n\n", issue.Description.String)
	}

	if len(comments) > 0 {
		prompt += "### Recent Comments\n"
		// Only include last 10 comments to avoid token overflow.
		start := 0
		if len(comments) > 10 {
			start = len(comments) - 10
		}
		for _, c := range comments[start:] {
			prompt += fmt.Sprintf("- [%s] %s\n", c.AuthorType, c.Content)
		}
		prompt += "\n"
	}

	// If triggered by a specific comment, highlight it.
	if triggerCommentID.Valid {
		for _, c := range comments {
			if c.ID == triggerCommentID {
				prompt += fmt.Sprintf("### Trigger Comment\n%s\n\n", c.Content)
				break
			}
		}
	}

	prompt += "Please analyze this issue and provide a helpful response."

	return prompt
}

// pollAndExecuteExecutions polls the new execution table for queued cloud
// executions and processes them. Companion to pollAndExecute (which polls
// the legacy agent_task_queue for Issue tasks). Per PRD §10.3.
//
// For each cloud-mode runtime that's online or degraded:
//
//  1. Quota gate via QuotaService.CheckBeforeClaim.
//  2. Atomic ClaimExecution (FOR UPDATE SKIP LOCKED).
//  3. Allocate SDK session + sandbox (placeholder UUIDs in this batch;
//     real allocation lands in plan5-d3-followup).
//  4. StartExecution + write context_ref with mode/sdk_session_id/sandbox_id.
//  5. Spawn runExecutionAsync goroutine to drive the execution to completion.
func (s *CloudExecutorService) pollAndExecuteExecutions(ctx context.Context) {
	cloudRuntimes, err := s.Queries.ListCloudRuntimes(ctx)
	if err != nil {
		slog.Warn("[cloud-executor] list cloud runtimes failed", "error", err)
		return
	}

	for _, rt := range cloudRuntimes {
		if !rt.WorkspaceID.Valid {
			continue
		}

		// Quota check before claim — skip the runtime if it can't take more
		// work. QuotaService logs the underlying reason.
		if s.Quota != nil {
			inflight, err := s.Queries.CountInflightExecutionsForRuntime(ctx, rt.ID)
			if err != nil {
				slog.Debug("[cloud-executor] count inflight failed",
					"runtime", uuidToStringSafe(rt.ID), "error", err)
				continue
			}
			wsUUID, err := uuid.FromBytes(rt.WorkspaceID.Bytes[:])
			if err != nil {
				continue
			}
			if err := s.Quota.CheckBeforeClaim(ctx, wsUUID, int(inflight)); err != nil {
				// Both ErrQuotaExceeded and ErrQuotaConcurrentLimit leave the
				// execution rows in 'queued' so they pick up on a future tick
				// once budget frees up. QuotaService already logs the cause.
				continue
			}
		}

		// Build the initial context_ref. The session/sandbox IDs are placeholders
		// here; the real values are written by StartExecution after they're
		// allocated below. We need a non-empty JSONB for ClaimExecution because
		// the column has NOT NULL semantics in PRD §10.3.
		initialCtxRef, _ := json.Marshal(map[string]any{
			"mode":                 "cloud",
			"sdk_session_id":       "",
			"sandbox_id":           "",
			"virtual_project_path": "/workspace",
		})

		e, err := s.Queries.ClaimExecution(ctx, db.ClaimExecutionParams{
			RuntimeID:  rt.ID,
			ContextRef: initialCtxRef,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue // No queued executions for this runtime; common case.
			}
			slog.Warn("[cloud-executor] claim execution failed",
				"runtime", uuidToStringSafe(rt.ID), "error", err)
			continue
		}

		// Allocate SDK session + sandbox. MVP: stub UUIDs. The real allocation
		// (SDK session creation + sandbox provisioning) lands in
		// plan5-d3-followup, which will replace these placeholders in
		// runExecutionAsync before the SDK call is issued.
		sessionID := uuid.New().String()
		sandboxID := uuid.New().String()

		updatedCtxRef, _ := json.Marshal(map[string]any{
			"mode":                 "cloud",
			"sdk_session_id":       sessionID,
			"sandbox_id":           sandboxID,
			"virtual_project_path": "/workspace",
		})

		if err := s.Queries.StartExecution(ctx, db.StartExecutionParams{
			ID:         e.ID,
			ContextRef: updatedCtxRef,
		}); err != nil {
			slog.Warn("[cloud-executor] start execution failed",
				"exec", uuidToStringSafe(e.ID), "error", err)
			// Best-effort: try to fail the row so it doesn't sit in 'claimed'
			// forever. If FailExecution also errors, the row will time out via
			// the scheduler's own watchdog (future work).
			s.failExecution(ctx, e, fmt.Sprintf("start execution: %v", err))
			continue
		}

		// Refresh the row so the goroutine sees the running status + ctx_ref.
		e.Status = "running"
		e.ContextRef = updatedCtxRef

		go s.runExecutionAsync(ctx, e, sessionID, sandboxID)
	}
}

// runExecutionAsync runs a single execution to completion. Best-effort: any
// failure inside the goroutine is converted into FailExecution +
// Scheduler.HandleTaskFailure so the task can retry or surface to a human.
//
// Uses a fresh ctx with a 30-minute deadline rather than inheriting the
// parent poll loop's ctx so a server shutdown cancels new claims but lets
// in-flight work finish.
//
// The Claude Agent SDK invocation is stubbed — the real integration lands in
// plan5-d3-followup. For now we record a stub result, mark the execution
// completed, and cascade to the scheduler.
func (s *CloudExecutorService) runExecutionAsync(parentCtx context.Context, e db.Execution, sessionID, sandboxID string) {
	_ = parentCtx // intentionally not propagated — see comment above.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	execIDStr := uuidToStringSafe(e.ID)

	// Resolve agent + runtime so we have everything needed for the SDK
	// invocation (and so a missing FK fails the execution rather than
	// silently no-op'ing).
	agentRow, err := s.Queries.GetAgent(ctx, e.AgentID)
	if err != nil {
		s.failExecution(ctx, e, fmt.Sprintf("get agent: %v", err))
		return
	}
	runtime, err := s.Queries.GetAgentRuntime(ctx, e.RuntimeID)
	if err != nil {
		s.failExecution(ctx, e, fmt.Sprintf("get runtime: %v", err))
		return
	}
	_ = agentRow
	_ = runtime

	// TODO(plan5-d3-followup): integrate Claude Agent SDK here.
	//   - Build SDK request from e.Payload (task title/description/AC + input
	//     context refs) and runtime.metadata (LLM config).
	//   - Call SDK Execute() against sessionID inside sandboxID.
	//   - Stream messages → optional WS publish.
	//   - Map SDK result → result map below; capture cost via CompleteExecution
	//     params.
	result := map[string]any{
		"stub":       true,
		"note":       "CloudExecutorService SDK invocation is stubbed; integration follows in plan5-d3-followup",
		"session_id": sessionID,
		"sandbox_id": sandboxID,
	}

	if err := s.Queries.CompleteExecution(ctx, db.CompleteExecutionParams{
		ID:     e.ID,
		Result: marshalResult(result),
	}); err != nil {
		slog.Warn("[cloud-executor] complete execution failed",
			"exec", execIDStr, "error", err)
		return
	}

	if s.Scheduler != nil && e.TaskID.Valid {
		taskUUID, err := uuid.FromBytes(e.TaskID.Bytes[:])
		if err == nil {
			execUUID, _ := uuid.FromBytes(e.ID.Bytes[:])
			if err := s.Scheduler.HandleTaskCompletion(ctx, taskUUID, execUUID, result); err != nil {
				slog.Warn("[cloud-executor] scheduler handle completion failed",
					"exec", execIDStr, "error", err)
			}
		}
	}

	slog.Info("[cloud-executor] execution completed", "exec", execIDStr)
}

// failExecution marks an execution row failed and cascades to
// Scheduler.HandleTaskFailure so the retry/fallback policy can run.
// Best-effort: errors are logged so a single failure path doesn't itself
// leave the row in an inconsistent state.
func (s *CloudExecutorService) failExecution(ctx context.Context, e db.Execution, errMsg string) {
	if err := s.Queries.FailExecution(ctx, db.FailExecutionParams{
		ID:     e.ID,
		Status: "failed",
		Error:  pgtype.Text{String: errMsg, Valid: true},
	}); err != nil {
		slog.Warn("[cloud-executor] fail execution failed",
			"exec", uuidToStringSafe(e.ID), "error", err)
	}
	if s.Scheduler != nil && e.TaskID.Valid {
		taskUUID, err := uuid.FromBytes(e.TaskID.Bytes[:])
		if err == nil {
			execUUID, _ := uuid.FromBytes(e.ID.Bytes[:])
			if err := s.Scheduler.HandleTaskFailure(ctx, taskUUID, execUUID, errMsg); err != nil {
				slog.Warn("[cloud-executor] scheduler handle failure failed",
					"exec", uuidToStringSafe(e.ID), "error", err)
			}
		}
	}
}

// marshalResult is a tiny helper so the calling code reads cleanly. Failures
// are swallowed because the input is a map[string]any built by us; if json
// can't marshal it, the bug is upstream.
func marshalResult(r map[string]any) []byte {
	b, _ := json.Marshal(r)
	return b
}

// uuidToStringSafe stringifies a pgtype.UUID without panicking on invalid
// values — useful for log lines where a missing UUID isn't a fatal bug.
func uuidToStringSafe(u pgtype.UUID) string {
	if !u.Valid {
		return "<invalid>"
	}
	return util.UUIDToString(u)
}
