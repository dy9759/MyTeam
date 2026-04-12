package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// SourceRef identifies a conversation source for project context.
type SourceRef struct {
	Type string `json:"type"` // "channel", "dm", "thread"
	ID   string `json:"id"`   // UUID of the channel/dm/thread
}

// CreateProjectFromChatRequest is the request body for POST /api/projects/from-chat.
type CreateProjectFromChatRequest struct {
	Title        string      `json:"title"`
	SourceRefs   []SourceRef `json:"source_refs"`
	AgentIDs     []string    `json:"agent_ids"`
	ScheduleType string      `json:"schedule_type"`
	CronExpr     *string     `json:"cron_expr"`
}

// CreateProjectFromChatResponse is the response for project creation from chat.
type CreateProjectFromChatResponse struct {
	Project  ProjectResponse         `json:"project"`
	Plan     *CreateFromChatPlanResp `json:"plan"`
	Workflow *WorkflowResponse       `json:"workflow,omitempty"`
	Channel  map[string]any          `json:"channel"`
}

// CreateFromChatPlanResp is a plan response with additional project-specific fields.
type CreateFromChatPlanResp struct {
	PlanResponse
	TaskBrief      string          `json:"task_brief"`
	AssignedAgents json.RawMessage `json:"assigned_agents"`
	ApprovalStatus string          `json:"approval_status"`
}

// CreateProjectFromChat handles POST /api/projects/from-chat.
// Creates a project from conversation context with LLM-generated plan.
func (h *Handler) CreateProjectFromChat(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectFromChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate required fields
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(req.SourceRefs) == 0 {
		writeError(w, http.StatusBadRequest, "at least one source_ref is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	scheduleType := req.ScheduleType
	if scheduleType == "" {
		scheduleType = "one_time"
	}
	switch scheduleType {
	case "one_time", "scheduled", "recurring":
		// valid
	default:
		writeError(w, http.StatusBadRequest, "invalid schedule_type")
		return
	}

	if (scheduleType == "scheduled" || scheduleType == "recurring") && (req.CronExpr == nil || *req.CronExpr == "") {
		writeError(w, http.StatusBadRequest, "cron_expr is required for scheduled/recurring projects")
		return
	}

	ctx := r.Context()

	// Step 1: Validate source_refs exist and gather context
	var contextParts []string
	var sourceConversations []map[string]any

	for _, ref := range req.SourceRefs {
		switch ref.Type {
		case "channel":
			// Validate channel exists
			ch, err := h.Queries.GetChannel(ctx, parseUUID(ref.ID))
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("channel %s not found", ref.ID))
				return
			}

			// Verify user has access (is a member of the workspace that owns this channel)
			if uuidToString(ch.WorkspaceID) != workspaceID {
				writeError(w, http.StatusForbidden, fmt.Sprintf("channel %s is not in this workspace", ref.ID))
				return
			}

			// Fetch messages from channel (last 100)
			messages, err := h.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
				ChannelID: parseUUID(ref.ID),
				Limit:     100,
				Offset:    0,
			})
			if err != nil {
				slog.Warn("failed to fetch channel messages for project context", "channel_id", ref.ID, "error", err)
			} else {
				contextParts = append(contextParts, formatMessagesAsContext(messages, "channel", ch.Name))
			}

			sourceConversations = append(sourceConversations, map[string]any{
				"conversation_id": ref.ID,
				"type":            "full",
				"snapshot_at":     time.Now().UTC().Format(time.RFC3339),
			})

		case "dm":
			// For DM, use ListChannelMessages with the DM channel ID
			// DMs in the unified model are also channels with conversation_type = 'dm'
			messages, err := h.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
				ChannelID: parseUUID(ref.ID),
				Limit:     100,
				Offset:    0,
			})
			if err != nil {
				slog.Warn("failed to fetch DM messages for project context", "dm_id", ref.ID, "error", err)
			} else {
				contextParts = append(contextParts, formatMessagesAsContext(messages, "dm", ""))
			}

			sourceConversations = append(sourceConversations, map[string]any{
				"conversation_id": ref.ID,
				"type":            "full",
				"snapshot_at":     time.Now().UTC().Format(time.RFC3339),
			})

		case "thread":
			// Threads reference a parent message; fetch thread messages
			messages, err := h.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
				ChannelID: parseUUID(ref.ID),
				Limit:     100,
				Offset:    0,
			})
			if err != nil {
				slog.Warn("failed to fetch thread messages for project context", "thread_id", ref.ID, "error", err)
			} else {
				contextParts = append(contextParts, formatMessagesAsContext(messages, "thread", ""))
			}

			sourceConversations = append(sourceConversations, map[string]any{
				"conversation_id": ref.ID,
				"type":            "full",
				"snapshot_at":     time.Now().UTC().Format(time.RFC3339),
			})

		default:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid source_ref type: %s", ref.Type))
			return
		}
	}

	chatContext := strings.Join(contextParts, "\n\n---\n\n")

	// Step 2: Fetch identity cards for specified agents
	var agentIdentities []service.AgentIdentity
	var agentOwnerIDs []string

	for _, agentID := range req.AgentIDs {
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          parseUUID(agentID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("agent %s not found in workspace", agentID))
			return
		}

		// Parse tools from JSONB
		var tools []string
		if len(agent.Tools) > 0 {
			_ = json.Unmarshal(agent.Tools, &tools)
		}

		agentIdentities = append(agentIdentities, service.AgentIdentity{
			ID:           agentID,
			Name:         agent.Name,
			Capabilities: agent.Capabilities,
			Skills:       agent.Capabilities, // Capabilities serve as skills in the current model
			Tools:        tools,
		})

		// Track agent owners for channel membership
		ownerID := uuidToString(agent.OwnerID)
		if ownerID != "" {
			agentOwnerIDs = append(agentOwnerIDs, ownerID)
		}
	}

	// Step 3: Generate plan with context using PlanGenerator
	generatedPlan, err := h.PlanGenerator.GeneratePlanWithContext(ctx, chatContext, agentIdentities, workspaceID)
	if err != nil {
		slog.Error("failed to generate plan from chat context", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to generate plan")
		return
	}

	// Step 4: Create Plan record
	stepsJSON, _ := json.Marshal(generatedPlan.Steps)
	assignedAgentsJSON := buildAssignedAgentsJSON(generatedPlan.Steps)

	plan, err := h.Queries.CreatePlan(ctx, db.CreatePlanParams{
		WorkspaceID:    parseUUID(workspaceID),
		Title:          generatedPlan.Title,
		Description:    strToText(generatedPlan.Description),
		SourceType:     strToText("chat"),
		Constraints:    strToText(generatedPlan.Constraints),
		ExpectedOutput: strToText(""),
		Steps:          stepsJSON,
		CreatedBy:      parseUUID(userID),
	})
	if err != nil {
		slog.Error("failed to create plan from chat context", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create plan")
		return
	}

	// Step 5: Create Workflow from plan
	dagJSON := buildWorkflowDAG(generatedPlan.Steps)
	wf, err := h.Queries.CreateWorkflow(ctx, db.CreateWorkflowParams{
		PlanID:      plan.ID,
		WorkspaceID: parseUUID(workspaceID),
		Title:       generatedPlan.Title,
		Status:      "draft",
		Type:        scheduleType,
		CronExpr:    ptrToText(req.CronExpr),
		Dag:         dagJSON,
		CreatedBy:   parseUUID(userID),
	})
	if err != nil {
		slog.Error("failed to create workflow from plan", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create workflow")
		return
	}

	// Create workflow steps from plan steps
	for _, step := range generatedPlan.Steps {
		skills := step.RequiredSkills
		if skills == nil {
			skills = []string{}
		}

		_, stepErr := h.Queries.CreateWorkflowStep(ctx, db.CreateWorkflowStepParams{
			WorkflowID:       wf.ID,
			StepOrder:        int32(step.Order),
			Description:      step.Description,
			AgentID:          optionalUUID(&step.AssignedAgentID),
			FallbackAgentIds: nil,
			RequiredSkills:   skills,
		})
		if stepErr != nil {
			slog.Warn("failed to create workflow step", "step_order", step.Order, "error", stepErr)
		}
	}

	// Step 6: Auto-create project channel
	ch, err := h.createProjectChannel(r, workspaceID, userID, req.Title)
	if err != nil {
		slog.Error("failed to create project channel for from-chat project", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create project channel")
		return
	}

	// Add agents as channel members
	for _, agentID := range req.AgentIDs {
		_ = h.Queries.AddChannelMember(ctx, db.AddChannelMemberParams{
			ChannelID:  ch.ID,
			MemberID:   parseUUID(agentID),
			MemberType: "agent",
		})
	}

	// Add agents' owners as channel members
	for _, ownerID := range agentOwnerIDs {
		if ownerID != userID { // Creator is already added
			_ = h.Queries.AddChannelMember(ctx, db.AddChannelMemberParams{
				ChannelID:  ch.ID,
				MemberID:   parseUUID(ownerID),
				MemberType: "member",
			})
		}
	}

	// Step 7: Build source_conversations JSONB
	sourceConvsJSON, _ := json.Marshal(sourceConversations)

	// Step 8: Create Project record
	// TODO: Use h.Queries.CreateProject() once sqlc query is generated.
	// project, err := h.Queries.CreateProject(ctx, db.CreateProjectParams{
	//     WorkspaceID:         parseUUID(workspaceID),
	//     Title:               req.Title,
	//     Description:         strToText(generatedPlan.Description),
	//     Status:              "not_started",
	//     ScheduleType:        scheduleType,
	//     CronExpr:            ptrToText(req.CronExpr),
	//     SourceConversations: sourceConvsJSON,
	//     ChannelID:           ch.ID,
	//     CreatorOwnerID:      parseUUID(userID),
	// })

	// Step 9: Create initial ProjectVersion (version_number=1)
	// TODO: Use h.Queries.CreateProjectVersion() once sqlc query is generated.
	// version, err := h.Queries.CreateProjectVersion(ctx, db.CreateProjectVersionParams{
	//     ProjectID:     project.ID,
	//     VersionNumber: 1,
	//     VersionStatus: "active",
	//     PlanSnapshot:  stepsJSON,
	//     CreatedBy:     parseUUID(userID),
	// })

	channelIDStr := uuidToString(ch.ID)
	now := time.Now().UTC().Format(time.RFC3339)

	projectResp := ProjectResponse{
		ID:                  "", // TODO: from created project
		WorkspaceID:         workspaceID,
		Title:               req.Title,
		Description:         &generatedPlan.Description,
		Status:              "not_started",
		ScheduleType:        scheduleType,
		CronExpr:            req.CronExpr,
		SourceConversations: sourceConvsJSON,
		ChannelID:           &channelIDStr,
		CreatorOwnerID:      userID,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	planResp := &CreateFromChatPlanResp{
		PlanResponse:   planToResponse(plan),
		TaskBrief:      generatedPlan.TaskBrief,
		AssignedAgents: assignedAgentsJSON,
		ApprovalStatus: "draft",
	}

	wfResp := workflowToResponse(wf)

	resp := CreateProjectFromChatResponse{
		Project:  projectResp,
		Plan:     planResp,
		Workflow: &wfResp,
		Channel:  channelToResponse(ch),
	}

	// Step 10: Broadcast event
	h.publish(protocol.EventProjectCreated, workspaceID, "member", userID, map[string]any{
		"project":     projectResp,
		"plan_id":     uuidToString(plan.ID),
		"workflow_id": uuidToString(wf.ID),
	})

	slog.Info("project created from chat context",
		"title", req.Title,
		"plan_id", uuidToString(plan.ID),
		"workflow_id", uuidToString(wf.ID),
		"channel_id", channelIDStr,
		"source_refs", len(req.SourceRefs),
		"agents", len(req.AgentIDs),
	)

	// Step 11: Return complete project
	writeJSON(w, http.StatusCreated, resp)
}

// formatMessagesAsContext converts a list of messages into a human-readable context string.
func formatMessagesAsContext(messages []db.Message, sourceType, sourceName string) string {
	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	if sourceName != "" {
		fmt.Fprintf(&sb, "[%s: %s]\n", sourceType, sourceName)
	} else {
		fmt.Fprintf(&sb, "[%s]\n", sourceType)
	}

	for _, msg := range messages {
		senderID := uuidToString(msg.SenderID)
		timestamp := timestampToString(msg.CreatedAt)
		content := msg.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		fmt.Fprintf(&sb, "[%s] %s (%s): %s\n", timestamp, senderID, msg.SenderType, content)
	}

	return sb.String()
}

// buildAssignedAgentsJSON constructs the assigned_agents JSONB from plan steps.
func buildAssignedAgentsJSON(steps []service.PlanStep) json.RawMessage {
	type agentAssignment struct {
		TaskOrder int    `json:"task_order"`
		AgentID   string `json:"agent_id"`
	}

	var assignments []agentAssignment
	for _, step := range steps {
		if step.AssignedAgentID != "" {
			assignments = append(assignments, agentAssignment{
				TaskOrder: step.Order,
				AgentID:   step.AssignedAgentID,
			})
		}
	}

	if assignments == nil {
		return json.RawMessage("[]")
	}

	data, _ := json.Marshal(assignments)
	return data
}

// buildWorkflowDAG constructs a simple DAG JSON from plan steps.
func buildWorkflowDAG(steps []service.PlanStep) json.RawMessage {
	type dagNode struct {
		Order       int    `json:"order"`
		Description string `json:"description"`
		DependsOn   []int  `json:"depends_on"`
	}

	nodes := make([]dagNode, len(steps))
	for i, step := range steps {
		deps := step.DependsOn
		if deps == nil {
			deps = []int{}
		}
		nodes[i] = dagNode{
			Order:       step.Order,
			Description: step.Description,
			DependsOn:   deps,
		}
	}

	data, _ := json.Marshal(nodes)
	return data
}
