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
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llmclient"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CloudExecutorService claims and executes tasks for cloud-mode agents.
type CloudExecutorService struct {
	Queries     *db.Queries
	Hub         *realtime.Hub
	Bus         *events.Bus
	TaskService *TaskService
}

// NewCloudExecutorService creates a new CloudExecutorService.
func NewCloudExecutorService(queries *db.Queries, hub *realtime.Hub, bus *events.Bus, taskService *TaskService) *CloudExecutorService {
	return &CloudExecutorService{
		Queries:     queries,
		Hub:         hub,
		Bus:         bus,
		TaskService: taskService,
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

	if agentRow.RuntimeMode != "cloud" {
		return
	}

	s.executeTask(ctx, task, agentRow)
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

		if agentRow.RuntimeMode != "cloud" {
			continue
		}

		go s.executeTask(ctx, task, agentRow)
	}
}

func (s *CloudExecutorService) executeTask(ctx context.Context, task db.AgentTaskQueue, agentRow db.Agent) {
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

	// Parse cloud LLM config.
	llmCfg := s.buildLLMConfig(agentRow)

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

func (s *CloudExecutorService) buildLLMConfig(agentRow db.Agent) llmclient.Config {
	var cloudCfg CloudLLMConfig
	if len(agentRow.CloudLlmConfig) > 0 {
		json.Unmarshal(agentRow.CloudLlmConfig, &cloudCfg)
	}

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
