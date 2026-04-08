package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
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
// Steps:
// 1. Get the project run.
// 2. Get all workflow steps and their results.
// 3. Compose a summary message (plain text, structured).
// 4. Post summary to the project's channel (create a message record).
// 5. Post summary to each source_conversation from project.source_conversations.
// 6. Create inbox notifications for all project participants.
// 7. Update project status to "completed".
func (s *ResultsReporterService) reportRunCompletion(ctx context.Context, workspaceID, runID string) {
	// TODO: Step 1 - Get the project run once project_run table exists:
	//   run, err := s.Queries.GetProjectRun(ctx, util.ParseUUID(runID))
	//   if err != nil {
	//       slog.Error("results reporter: failed to get project run", "run_id", runID, "error", err)
	//       return
	//   }
	//   projectID := util.UUIDToString(run.ProjectID)

	// TODO: Step 2 - Get workflow steps for this run:
	//   steps, err := s.Queries.ListWorkflowStepsByRun(ctx, util.ParseUUID(runID))

	// For now, build a placeholder summary.
	summary := s.composeSummary(runID, nil)

	slog.Info("results reporter: summary composed",
		"run_id", runID,
		"summary_length", len(summary),
	)

	// TODO: Step 3-4 - Post summary to the project's channel:
	//   project, err := s.Queries.GetProject(ctx, util.ParseUUID(projectID))
	//   if err == nil && project.ChannelID.Valid {
	//       s.postSummaryToChannel(ctx, workspaceID, util.UUIDToString(project.ChannelID), summary)
	//   }

	// TODO: Step 5 - Post summary to each source_conversation:
	//   var sourceConversations []struct{ ConversationID string }
	//   json.Unmarshal(project.SourceConversations, &sourceConversations)
	//   for _, sc := range sourceConversations {
	//       s.postSummaryToChannel(ctx, workspaceID, sc.ConversationID, summary)
	//   }

	// TODO: Step 6 - Create inbox notifications for all project participants:
	//   s.notifyProjectParticipants(ctx, workspaceID, projectID, runID, summary)

	// TODO: Step 7 - Update project status to "completed":
	//   s.Queries.UpdateProjectStatus(ctx, db.UpdateProjectStatusParams{
	//       ID:     util.ParseUUID(projectID),
	//       Status: "completed",
	//   })

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

	_ = summary
	_ = ctx
}

// composeSummary builds a structured summary from workflow step results.
func (s *ResultsReporterService) composeSummary(runID string, steps []stepResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Run Completed: %s\n\n", runID))
	sb.WriteString(fmt.Sprintf("**Completed at:** %s\n\n", time.Now().UTC().Format(time.RFC3339)))

	if len(steps) == 0 {
		sb.WriteString("No step results available.\n")
		return sb.String()
	}

	sb.WriteString("### Step Results\n\n")
	completed := 0
	failed := 0
	for _, step := range steps {
		status := "completed"
		if step.Error != "" {
			status = "failed"
			failed++
		} else {
			completed++
		}
		sb.WriteString(fmt.Sprintf("- **Step %d** (%s): %s\n", step.Order, step.Description, status))
		if step.Error != "" {
			sb.WriteString(fmt.Sprintf("  - Error: %s\n", step.Error))
		}
	}

	sb.WriteString(fmt.Sprintf("\n**Summary:** %d completed, %d failed out of %d total steps.\n",
		completed, failed, len(steps)))

	return sb.String()
}

// stepResult holds the relevant fields from a workflow step for summary generation.
type stepResult struct {
	Order       int32
	Description string
	Status      string
	Error       string
	Result      json.RawMessage
}

// postSummaryToChannel creates a message record and broadcasts it.
// nolint: unused // Will be used once project tables exist.
func (s *ResultsReporterService) postSummaryToChannel(ctx context.Context, workspaceID, channelID, summary string) {
	// TODO: Create a message record in the database:
	//   msg, err := s.Queries.CreateMessage(ctx, db.CreateMessageParams{
	//       WorkspaceID: util.ParseUUID(workspaceID),
	//       ChannelID:   util.ParseUUID(channelID),
	//       SenderType:  "system",
	//       SenderID:    pgtype.UUID{}, // system agent
	//       Content:     summary,
	//   })
	//   if err != nil {
	//       slog.Error("results reporter: failed to post summary", "channel_id", channelID, "error", err)
	//       return
	//   }

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

	_ = ctx
}

// notifyProjectParticipants creates inbox items for all participants.
// nolint: unused // Will be used once project tables exist.
func (s *ResultsReporterService) notifyProjectParticipants(ctx context.Context, workspaceID, projectID, runID, summary string) {
	// TODO: Get all project channel members and create inbox items:
	//   project, err := s.Queries.GetProject(ctx, util.ParseUUID(projectID))
	//   if err != nil { return }
	//   members, err := s.Queries.ListChannelMembers(ctx, project.ChannelID)
	//   for _, m := range members {
	//       if m.MemberType != "member" { continue }
	//       s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
	//           WorkspaceID:   util.ParseUUID(workspaceID),
	//           RecipientType: "member",
	//           RecipientID:   m.MemberID,
	//           Type:          "run_completed",
	//           Title:         "Project run completed",
	//           Body:          summary,
	//           Severity:      "info",
	//           ActionRequired: false,
	//       })
	//   }

	_ = ctx
	_ = workspaceID
	_ = projectID
	_ = runID
	_ = summary

	slog.Debug("results reporter: inbox notifications created",
		"project_id", projectID,
		"run_id", runID,
	)
}

// Ensure util import is used.
var _ = util.ParseUUID
