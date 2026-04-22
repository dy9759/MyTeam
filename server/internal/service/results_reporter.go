// Package service: results_reporter.go — listens to run completion events,
// composes a summary by walking the run's Executions, and delivers the
// summary to the project's channel + each member's inbox.
//
// Per-task data is read from the execution table (one row per Task
// attempt). The per-task breakdown lists task_id, status, and attempt so
// participants can see which tasks completed and which retried/failed.

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/MyAIOSHub/MyTeam/server/internal/events"
	"github.com/MyAIOSHub/MyTeam/server/internal/realtime"
	"github.com/MyAIOSHub/MyTeam/server/internal/util"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/MyAIOSHub/MyTeam/server/pkg/protocol"
)

// ResultsReporterService listens to workflow and run completion events,
// composes summary messages, and delivers them to project channels,
// source conversations, and participant inboxes.
type ResultsReporterService struct {
	Queries  *db.Queries
	Hub      *realtime.Hub
	EventBus *events.Bus
}

// NewResultsReporterService creates a new ResultsReporterService.
func NewResultsReporterService(q *db.Queries, hub *realtime.Hub, bus *events.Bus) *ResultsReporterService {
	return &ResultsReporterService{
		Queries:  q,
		Hub:      hub,
		EventBus: bus,
	}
}

// Start subscribes to workflow.completed and run:completed events.
func (s *ResultsReporterService) Start() {
	s.EventBus.Subscribe(protocol.EventWorkflowCompleted, s.handleWorkflowCompleted)
	s.EventBus.Subscribe(protocol.EventRunCompleted, s.handleRunCompleted)

	slog.Info("results reporter service started")
}

// handleWorkflowCompleted handles workflow completion by logging the event.
// The actual summary is deferred to run completion since a workflow may be
// part of a larger project run.
func (s *ResultsReporterService) handleWorkflowCompleted(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	workflowID, _ := payload["workflow_id"].(string)
	status, _ := payload["status"].(string)

	slog.Info("results reporter: workflow completed",
		"workflow_id", workflowID,
		"status", status,
		"workspace_id", e.WorkspaceID,
	)

	// Workflow completion alone does not trigger a full report.
	// The run:completed event triggers the summary.
}

// handleRunCompleted produces and delivers the run completion summary.
func (s *ResultsReporterService) handleRunCompleted(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}

	runID, _ := payload["run_id"].(string)
	if runID == "" {
		return
	}

	slog.Info("results reporter: run completed, generating summary",
		"run_id", runID,
		"workspace_id", e.WorkspaceID,
	)

	ctx := context.Background()
	s.reportRunCompletion(ctx, e.WorkspaceID, runID)
}

// reportRunCompletion composes and delivers the run summary.
//
// The summary lists per-task execution rows (latest attempt only when
// multiple are present, since ListExecutionsByRun orders by created_at
// DESC and we deduplicate by task_id).
func (s *ResultsReporterService) reportRunCompletion(ctx context.Context, workspaceID, runID string) {
	// Step 1: Get the project run.
	run, err := s.Queries.GetProjectRun(ctx, util.ParseUUID(runID))
	if err != nil {
		slog.Error("results reporter: failed to get project run", "run_id", runID, "error", err)
		return
	}
	projectID := util.UUIDToString(run.ProjectID)

	// Step 2: pull per-task execution rows for the run and synthesize a
	// summary block. Failures here are non-fatal — the run-level header
	// still gets posted.
	executions, exErr := s.Queries.ListExecutionsByRun(ctx, util.ParseUUID(runID))
	if exErr != nil {
		slog.Warn("results reporter: list executions failed", "run_id", runID, "error", exErr)
	}
	summary := s.composeSummary(runID, executions)

	slog.Info("results reporter: summary composed",
		"run_id", runID,
		"project_id", projectID,
		"summary_length", len(summary),
	)

	// Step 3-4: Post summary to the project's channel.
	project, projErr := s.Queries.GetProject(ctx, util.ParseUUID(projectID))
	if projErr == nil && project.ChannelID.Valid {
		s.postSummaryToChannel(ctx, workspaceID, util.UUIDToString(project.ChannelID), summary)
	}

	// Step 6: Notify project participants via inbox.
	if projErr == nil {
		s.notifyProjectParticipants(ctx, workspaceID, projectID, runID, summary)
	}

	// Step 7: Update project status. Without per-step status data we treat
	// the run as completed; failure paths will be reinstated when Task /
	// Execution status is available (Batch C3/D).
	s.Queries.UpdateProjectStatus(ctx, db.UpdateProjectStatusParams{
		ID:     util.ParseUUID(projectID),
		Status: "completed",
	})

	// Broadcast a project status change event.
	s.EventBus.Publish(events.Event{
		Type:        protocol.EventProjectStatusChanged,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"run_id": runID,
			"status": "completed",
		},
	})
}

// composeSummary builds the run summary. The per-task block is
// deduplicated by task_id, keeping the latest attempt (executions arrive
// newest-first from ListExecutionsByRun).
func (s *ResultsReporterService) composeSummary(runID string, executions []db.Execution) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Run Completed: %s\n\n", runID))
	sb.WriteString(fmt.Sprintf("**Completed at:** %s\n\n", time.Now().UTC().Format(time.RFC3339)))
	if len(executions) == 0 {
		sb.WriteString("No execution records were produced for this run.\n")
		return sb.String()
	}

	sb.WriteString("### Tasks\n\n")
	seen := make(map[[16]byte]bool, len(executions))
	for _, e := range executions {
		if !e.TaskID.Valid || seen[e.TaskID.Bytes] {
			continue
		}
		seen[e.TaskID.Bytes] = true
		sb.WriteString(fmt.Sprintf("- task=%s status=%s attempt=%d\n",
			util.UUIDToString(e.TaskID), e.Status, e.Attempt))
	}
	return sb.String()
}

// postSummaryToChannel creates a message record and broadcasts it.
func (s *ResultsReporterService) postSummaryToChannel(ctx context.Context, workspaceID, channelID, summary string) {
	// Create a message record in the database.
	_, msgErr := s.Queries.CreateMessage(ctx, db.CreateMessageParams{
		WorkspaceID: util.ParseUUID(workspaceID),
		ChannelID:   util.ParseUUID(channelID),
		SenderType:  "system",
		SenderID:    pgtype.UUID{},
		Content:     summary,
		ContentType: "text",
	})
	if msgErr != nil {
		slog.Error("results reporter: failed to post summary", "channel_id", channelID, "error", msgErr)
	}

	// Broadcast via WebSocket.
	data, err := json.Marshal(map[string]any{
		"type": "message:created",
		"payload": map[string]any{
			"channel_id":  channelID,
			"content":     summary,
			"sender_type": "system",
		},
	})
	if err != nil {
		slog.Error("results reporter: failed to marshal WS message", "error", err)
		return
	}

	s.Hub.BroadcastToWorkspace(workspaceID, data)
}

// notifyProjectParticipants creates inbox items for all participants.
func (s *ResultsReporterService) notifyProjectParticipants(ctx context.Context, workspaceID, projectID, runID, summary string) {
	// Get project and its channel members, create inbox items.
	project, projErr := s.Queries.GetProject(ctx, util.ParseUUID(projectID))
	if projErr != nil {
		slog.Warn("results reporter: project not found for inbox", "project_id", projectID)
		return
	}
	if !project.ChannelID.Valid {
		return
	}
	members, memErr := s.Queries.ListChannelMembers(ctx, project.ChannelID)
	if memErr != nil {
		slog.Warn("results reporter: failed to list channel members", "error", memErr)
		return
	}
	for _, m := range members {
		if m.MemberType != "member" {
			continue
		}
		s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   util.ParseUUID(workspaceID),
			RecipientType: "member",
			RecipientID:   m.MemberID,
			Type:          "run_completed",
			Title:         fmt.Sprintf("Project run completed: %s", runID[:8]),
			Body:          pgtype.Text{String: summary, Valid: true},
			Severity:      "info",
		})
	}

	slog.Debug("results reporter: inbox notifications created",
		"project_id", projectID,
		"run_id", runID,
	)
}
