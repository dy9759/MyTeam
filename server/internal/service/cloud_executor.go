package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	"github.com/MyAIOSHub/MyTeam/server/internal/realtime"
	"github.com/MyAIOSHub/MyTeam/server/internal/util"
	"github.com/MyAIOSHub/MyTeam/server/pkg/agent_runner"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/MyAIOSHub/MyTeam/server/pkg/protocol"
)

// CloudExecutorService claims and executes tasks for cloud-mode agents.
//
// It polls two queues in parallel:
//
//  1. agent_task_queue (legacy Issue-link tasks) via pollAndExecute.
//  2. execution        (Plan/Task/Run model tasks) via pollAndExecuteExecutions.
//
// Both paths share the same quota gate (workspace_quota.max_concurrent_cloud_exec
// + monthly USD ceiling) and the same Claude Agent SDK Runner — that's the
// architecture the platform standardizes on for every cloud-mode agent
// (system, personal, project). Completion of an execution-path task
// cascades back into SchedulerService so downstream tasks become ready.
type CloudExecutorService struct {
	Queries     *db.Queries
	Hub         *realtime.Hub
	Bus         *events.Bus
	TaskService *TaskService
	Quota       *QuotaService
	Scheduler   *SchedulerService

	// Runner spawns the Python claude-agent-sdk child process. The same
	// Runner instance is shared with AutoReplyService so the platform has a
	// single SDK invocation path for every cloud-mode agent. Tests inject a
	// fake to avoid hitting a real LLM endpoint.
	Runner agent_runner.AgentRunner
}

// NewCloudExecutorService creates a new CloudExecutorService.
//
// scheduler may be nil; the execution-table poll loop will still run but
// will not cascade completion back into the Plan/Run state machine. The
// agent_task_queue path is unaffected.
//
// runner must be non-nil for either poll loop to actually invoke the LLM.
// Pass agent_runner.NewRunner() for production, or share the Runner with
// AutoReplyService so both paths converge on the same SDK installation.
func NewCloudExecutorService(queries *db.Queries, hub *realtime.Hub, bus *events.Bus, taskService *TaskService, scheduler *SchedulerService, runner agent_runner.AgentRunner) *CloudExecutorService {
	return &CloudExecutorService{
		Queries:     queries,
		Hub:         hub,
		Bus:         bus,
		TaskService: taskService,
		Quota:       NewQuotaService(queries),
		Scheduler:   scheduler,
		Runner:      runner,
	}
}

// runnerConfigFromRuntime maps the agent runtime metadata into the SDK
// Runner's per-invocation config. Mirrors the conversion used by
// auto_reply.go so a single agent gets the same config no matter which
// path invokes it.
func runnerConfigFromRuntime(runtime db.AgentRuntime, systemPrompt string) agent_runner.Config {
	cloud := cloudLLMConfigFromRuntime(runtime)
	cfg := agent_runner.Config{
		Kernel:       cloud.Kernel,
		BaseURL:      cloud.BaseURL,
		APIKey:       cloud.APIKey,
		Model:        cloud.Model,
		SystemPrompt: systemPrompt,
	}
	if cfg.Kernel == "" {
		cfg.Kernel = "anthropic"
	}
	return cfg
}

// orphanLeaseDuration is the time after claimed_at past which a 'claimed'
// execution row is considered orphaned (i.e. the server crashed between
// ClaimExecution and StartExecution). Five minutes comfortably exceeds the
// normal claim-to-running gap, which is sub-second.
const orphanLeaseDuration = 5 * time.Minute

// orphanRecoveryStartupDelay defers the orphan scan until after the database
// pool is definitely up. Per PRD §6.4 the scan must not race the rest of
// startup; cheaper than coordinating with a readiness signal.
var orphanRecoveryStartupDelay = 5 * time.Second

// Start subscribes to task:dispatch events and starts a poll loop for pending cloud tasks.
func (s *CloudExecutorService) Start(ctx context.Context) {
	// Subscribe to task:dispatch events.
	s.Bus.Subscribe(protocol.EventTaskDispatch, func(e events.Event) {
		go s.handleDispatch(ctx, e)
	})

	// Crash recovery: after a brief delay (so the DB pool is definitely
	// up) scan for executions stranded in 'claimed' from a previous
	// process. Best-effort; failures are logged and the poll loop still
	// runs. Per cross-cutting PRD §6.4.
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(orphanRecoveryStartupDelay):
		}
		s.recoverOrphanedExecutions(ctx)
	}()

	// Start poll loop.
	go s.pollLoop(ctx)

	slog.Info("[cloud-executor] started")
}

// recoverOrphanedExecutions fails every execution stuck in 'claimed' whose
// claimed_at is older than orphanLeaseDuration so SchedulerService.HandleTaskFailure
// can run its retry policy. Best-effort: any per-row error is logged and the
// scan continues with the next row.
func (s *CloudExecutorService) recoverOrphanedExecutions(ctx context.Context) {
	cutoff := pgtype.Timestamptz{Time: time.Now().Add(-orphanLeaseDuration), Valid: true}
	orphans, err := s.Queries.ListOrphanedClaimedExecutions(ctx, cutoff)
	if err != nil {
		slog.Warn("[cloud-executor] orphan recovery scan failed", "error", err)
		return
	}
	if len(orphans) == 0 {
		slog.Info("[cloud-executor] orphan recovery: no stranded executions")
		return
	}
	for _, exec := range orphans {
		s.failExecution(ctx, exec, "lease_expired_orphan")
	}
	slog.Info("[cloud-executor] orphan recovery complete", "recovered", len(orphans))
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

	systemPrompt := fmt.Sprintf(
		"You are %s, an AI assistant. You are working on issue '%s'. "+
			"Analyze the issue and provide a helpful, concise response. "+
			"Focus on actionable suggestions.",
		agentRow.Name, issue.Title,
	)

	// Invoke via the Claude Agent SDK Runner — same path AutoReplyService
	// uses, so a single agent's cloud_llm_config controls every cloud path.
	if s.Runner == nil {
		s.TaskService.FailTask(ctx, task.ID, "cloud-executor: runner not configured")
		return
	}
	output, err := s.Runner.Run(ctx, prompt, runnerConfigFromRuntime(runtime, systemPrompt))
	if err != nil {
		s.TaskService.FailTask(ctx, task.ID, fmt.Sprintf("cloud execute failed: %v", err))
		return
	}

	resultJSON, _ := json.Marshal(protocol.TaskCompletedPayload{
		Output: output,
	})

	s.TaskService.CompleteTask(ctx, task.ID, resultJSON, "", "")
	slog.Info("[cloud-executor] task completed", "task_id", taskIDStr)
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
// The LLM call goes through the same agent_runner.Runner that
// AutoReplyService uses (Python claude-agent-sdk subprocess), so every
// cloud-mode agent — system, personal, project — converges on a single
// SDK invocation path. Tests inject a fake Runner to avoid spawning the
// child process.
func (s *CloudExecutorService) runExecutionAsync(parentCtx context.Context, e db.Execution, sessionID, sandboxID string) {
	_ = parentCtx // intentionally not propagated — see comment above.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	execIDStr := uuidToStringSafe(e.ID)

	// Belt-and-suspenders panic recovery: a panic in payload parsing or
	// the runner subprocess plumbing would otherwise leave the execution
	// stuck in 'running' forever. Convert it into failExecution so
	// Scheduler.HandleTaskFailure can run the retry/fallback policy.
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("[cloud-executor] panic in runExecutionAsync",
				"exec", execIDStr, "recover", rec)
			s.failExecution(ctx, e, fmt.Sprintf("panic: %v", rec))
		}
	}()

	if s.Runner == nil {
		s.failExecution(ctx, e, "cloud-executor: runner not configured")
		return
	}

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

	// Decode the payload SchedulerService.ScheduleTask wrote when it created
	// this execution row. A malformed payload is fatal — the scheduler is the
	// only writer, so a parse error means the schema is out of sync.
	var payload struct {
		TaskID             string          `json:"task_id"`
		Title              string          `json:"title"`
		Description        string          `json:"description"`
		AcceptanceCriteria string          `json:"acceptance_criteria"`
		InputContextRefs   json.RawMessage `json:"input_context_refs"`
	}
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			s.failExecution(ctx, e, fmt.Sprintf("parse payload: %v", err))
			return
		}
	}
	if payload.Title == "" {
		s.failExecution(ctx, e, "execution payload missing title")
		return
	}

	systemPrompt := buildExecutionSystemPrompt(agentRow.Name)
	prompt := buildExecutionPrompt(payload.Title, payload.Description, payload.AcceptanceCriteria)
	startedAt := time.Now()

	output, err := s.Runner.Run(ctx, prompt, runnerConfigFromRuntime(runtime, systemPrompt))
	if err != nil {
		s.failExecution(ctx, e, fmt.Sprintf("runner: %v", err))
		return
	}

	result := map[string]any{
		"output":      output,
		"session_id":  sessionID,
		"sandbox_id":  sandboxID,
		"duration_ms": time.Since(startedAt).Milliseconds(),
	}

	// TODO(plan5-d4-followup): pull real token usage + USD from the runner
	// once agent_runner surfaces a Result struct (today Run() returns only
	// the reply string). Until then we record zeros so the QuotaService /
	// execution.cost_* wiring is in place — when the runner gains usage
	// data we only need to populate these three locals.
	var (
		costUSD          float64
		costInputTokens  int64
		costOutputTokens int64
	)

	rowsAffected, err := s.Queries.CompleteExecution(ctx, db.CompleteExecutionParams{
		ID:               e.ID,
		Result:           marshalResult(result),
		CostInputTokens:  pgtype.Int4{Int32: int32(costInputTokens), Valid: true},
		CostOutputTokens: pgtype.Int4{Int32: int32(costOutputTokens), Valid: true},
		CostUsd:          float64ToNumeric(costUSD),
	})
	if err != nil {
		slog.Warn("[cloud-executor] complete execution failed",
			"exec", execIDStr, "error", err)
		return
	}
	// Idempotency guard: the WHERE status='running' clause means a second
	// concurrent completion (e.g. the daemon also POSTed /complete) returns
	// 0 rows. Skip the cascade so HandleTaskCompletion does not run twice
	// and silently bypass a human_review slot.
	if rowsAffected == 0 {
		slog.Info("[cloud-executor] execution already completed by another path; skipping cascade",
			"exec", execIDStr)
		return
	}

	// Workspace-level monthly cost roll-up (PRD §12). Best-effort so a quota
	// failure cannot undo the completion that already landed.
	if s.Quota != nil && runtime.WorkspaceID.Valid {
		if wsUUID, wsErr := uuid.FromBytes(runtime.WorkspaceID.Bytes[:]); wsErr == nil {
			s.Quota.RecordCost(ctx, wsUUID, costUSD, costInputTokens, costOutputTokens)
		}
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

// buildExecutionSystemPrompt assembles the system prompt sent to the LLM
// for an execution-table task. Kept as a separate function so prompt tuning
// is one obvious place rather than buried in the goroutine.
func buildExecutionSystemPrompt(agentName string) string {
	return fmt.Sprintf(
		"You are %s, an AI agent on the MyTeam platform. Complete the task described below. "+
			"Respond with the final result in plain text. If the task is unclear or you cannot "+
			"proceed, say so explicitly so the human reviewer can intervene.",
		agentName,
	)
}

// buildExecutionPrompt formats the task fields into the prompt body. Empty
// fields are omitted so the LLM doesn't see headings followed by nothing.
func buildExecutionPrompt(title, description, acceptanceCriteria string) string {
	var sb strings.Builder
	sb.WriteString("## Task: ")
	sb.WriteString(title)
	sb.WriteString("\n\n")
	if description != "" {
		sb.WriteString("### Description\n")
		sb.WriteString(description)
		sb.WriteString("\n\n")
	}
	if acceptanceCriteria != "" {
		sb.WriteString("### Acceptance Criteria\n")
		sb.WriteString(acceptanceCriteria)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Complete the task and provide the result.")
	return sb.String()
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
