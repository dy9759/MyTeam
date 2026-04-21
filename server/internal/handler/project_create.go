package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// SourceRef identifies a conversation source for project context.
//
// MessageIDs (optional) narrows the source to a specific subset of messages
// inside the channel/dm/thread. When omitted the handler loads the most
// recent 100 messages — the legacy whole-conversation behavior. The UI uses
// this to let a user multi-select messages and turn just those into a
// project (PRD §7 "selected chat → todolist").
//
// PeerType is required when Type == "dm". DM messages are stored with
// recipient_id/recipient_type, so we must know whether the peer is a member
// or an agent to query the right rows via ListDMMessages.
type SourceRef struct {
	Type       string   `json:"type"`                  // "channel", "dm", "thread"
	ID         string   `json:"id"`                    // UUID of the channel/dm/thread peer
	MessageIDs []string `json:"message_ids,omitempty"` // optional subset filter
	PeerType   string   `json:"peer_type,omitempty"`   // "member" | "agent" — required for dm
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
//
// Plan 5 C4: Tasks now carries the IDs of the rows materialized by
// PlanGenerator → CreateTask. Slots are reachable via
// /api/tasks/{id}/slots in Batch D; here we just expose how many were
// created so clients can confirm the plan landed end-to-end.
type CreateProjectFromChatResponse struct {
	Project  ProjectResponse         `json:"project"`
	Plan     *CreateFromChatPlanResp `json:"plan"`
	Channel  map[string]any          `json:"channel"`
	Tasks    []TaskRefResponse       `json:"tasks"`
	Warnings []string                `json:"warnings,omitempty"`
}

// TaskRefResponse is the slim task surface returned by from-chat — full
// task fields will move into a dedicated /api/tasks endpoint in Batch D.
type TaskRefResponse struct {
	ID                string `json:"id"`
	LocalID           string `json:"local_id"`
	Title             string `json:"title"`
	StepOrder         int    `json:"step_order"`
	CollaborationMode string `json:"collaboration_mode"`
	SlotCount         int    `json:"slot_count"`
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
			ch, err := h.Queries.GetChannel(ctx, parseUUID(ref.ID))
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("channel %s not found", ref.ID))
				return
			}
			if uuidToString(ch.WorkspaceID) != workspaceID {
				writeError(w, http.StatusForbidden, fmt.Sprintf("channel %s is not in this workspace", ref.ID))
				return
			}
			messages, err := h.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
				ChannelID: parseUUID(ref.ID),
				Limit:     100,
				Offset:    0,
			})
			if err != nil {
				slog.Warn("failed to fetch channel messages for project context", "channel_id", ref.ID, "error", err)
			} else {
				messages = filterMessagesByID(messages, ref.MessageIDs)
				contextParts = append(contextParts, formatMessagesAsContext(messages, "channel", ch.Name))
			}
			sourceConversations = append(sourceConversations, sourceConversationEntry(ref))

		case "dm":
			peerType := ref.PeerType
			if peerType == "" {
				peerType = "member"
			}
			if peerType != "member" && peerType != "agent" {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid peer_type: %s", peerType))
				return
			}
			messages, err := h.Queries.ListDMMessages(ctx, db.ListDMMessagesParams{
				WorkspaceID: parseUUID(workspaceID),
				SelfID:      parseUUID(userID),
				SelfType:    "member",
				PeerID:      parseUUID(ref.ID),
				PeerType:    strToText(peerType),
				LimitCount:  100,
				OffsetCount: 0,
			})
			if err != nil {
				slog.Warn("failed to fetch DM messages for project context", "peer_id", ref.ID, "error", err)
			} else {
				messages = filterMessagesByID(messages, ref.MessageIDs)
				contextParts = append(contextParts, formatMessagesAsContext(messages, "dm", ""))
			}
			sourceConversations = append(sourceConversations, sourceConversationEntry(ref))

		case "thread":
			messages, err := h.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
				ChannelID: parseUUID(ref.ID),
				Limit:     100,
				Offset:    0,
			})
			if err != nil {
				slog.Warn("failed to fetch thread messages for project context", "thread_id", ref.ID, "error", err)
			} else {
				messages = filterMessagesByID(messages, ref.MessageIDs)
				contextParts = append(contextParts, formatMessagesAsContext(messages, "thread", ""))
			}
			sourceConversations = append(sourceConversations, sourceConversationEntry(ref))

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

		// Pull capabilities/tools from identity_card (post Account Phase 2:
		// no standalone agent.tools / agent.capabilities columns).
		caps := capabilitiesFromIdentityCard(agent.IdentityCard)
		tools := toolsFromIdentityCard(agent.IdentityCard)

		agentIdentities = append(agentIdentities, service.AgentIdentity{
			ID:           agentID,
			Name:         agent.Name,
			Capabilities: caps,
			Skills:       caps, // Capabilities serve as skills in the current model
			Tools:        tools,
		})

		// Track agent owners for channel membership
		ownerID := uuidToString(agent.OwnerID)
		if ownerID != "" {
			agentOwnerIDs = append(agentOwnerIDs, ownerID)
		}
	}

	// Step 3: Generate plan with context using PlanGenerator
	genResult, err := h.PlanGenerator.GeneratePlanWithContext(ctx, chatContext, agentIdentities, workspaceID)
	if err != nil {
		slog.Error("failed to generate plan from chat context", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to generate plan")
		return
	}

	// Step 4: Create Plan record
	assignedAgentsJSON := buildAssignedAgentsJSON(genResult.Tasks)

	plan, err := h.Queries.CreatePlan(ctx, db.CreatePlanParams{
		WorkspaceID:    parseUUID(workspaceID),
		Title:          genResult.Plan.Title,
		Description:    strToText(genResult.Plan.Description),
		SourceType:     strToText("chat"),
		Constraints:    strToText(genResult.Plan.Constraints),
		ExpectedOutput: strToText(genResult.Plan.ExpectedOutput),
		CreatedBy:      parseUUID(userID),
	})
	if err != nil {
		slog.Error("failed to create plan from chat context", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create plan")
		return
	}

	// Step 5: Materialize Tasks and Slots from drafts.
	taskRefs, materializeWarnings, err := h.materializePlanDrafts(ctx, plan.ID, parseUUID(workspaceID), genResult)
	if err != nil {
		slog.Error("failed to materialize plan drafts", "error", err, "plan_id", uuidToString(plan.ID))
		writeError(w, http.StatusInternalServerError, "failed to materialize plan tasks")
		return
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

	// Step 8: Create Project record and link Plan → Project so the plan-by-
	// project lookups (RejectPlan, ApprovePlan, MediationService) can resolve.
	project, err := h.Queries.CreateProject(ctx, db.CreateProjectParams{
		WorkspaceID:         parseUUID(workspaceID),
		Title:               req.Title,
		Description:         strToText(genResult.Plan.Description),
		Status:              "not_started",
		ScheduleType:        scheduleType,
		CronExpr:            ptrToText(req.CronExpr),
		SourceConversations: sourceConvsJSON,
		ChannelID:           ch.ID,
		CreatorOwnerID:      parseUUID(userID),
	})
	if err != nil {
		slog.Error("create project (from-chat) failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	// Link the freshly-created plan to this project. version_id is left null
	// here — ProjectVersion is post-MVP per Plan 5 §13. Without this link, a
	// later RejectPlan / ApprovePlan call (which uses GetPlanByProject) cannot
	// find the plan.
	if err := h.Queries.UpdatePlanProject(ctx, db.UpdatePlanProjectParams{
		ID:        plan.ID,
		ProjectID: project.ID,
	}); err != nil {
		slog.Warn("link plan to project failed", "error", err, "plan_id", uuidToString(plan.ID), "project_id", uuidToString(project.ID))
	}

	channelIDStr := uuidToString(ch.ID)
	projectResp := projectToResponse(project)
	// from-chat returns the channel id at the response level for convenience —
	// the helper sets it from project.ChannelID, but make sure it's there even
	// when the row write skipped the channel link for any reason.
	if projectResp.ChannelID == nil {
		projectResp.ChannelID = &channelIDStr
	}

	planResp := &CreateFromChatPlanResp{
		PlanResponse:   planToResponse(plan),
		TaskBrief:      genResult.Plan.TaskBrief,
		AssignedAgents: assignedAgentsJSON,
		ApprovalStatus: plan.ApprovalStatus,
	}

	combinedWarnings := append([]string{}, genResult.Warnings...)
	combinedWarnings = append(combinedWarnings, materializeWarnings...)

	resp := CreateProjectFromChatResponse{
		Project:  projectResp,
		Plan:     planResp,
		Channel:  channelToResponse(ch),
		Tasks:    taskRefs,
		Warnings: combinedWarnings,
	}

	// Step 9: Broadcast event
	h.publish(protocol.EventProjectCreated, workspaceID, "member", userID, map[string]any{
		"project": projectResp,
		"plan_id": uuidToString(plan.ID),
		"tasks":   taskRefs,
	})

	slog.Info("project created from chat context",
		"title", req.Title,
		"plan_id", uuidToString(plan.ID),
		"channel_id", channelIDStr,
		"source_refs", len(req.SourceRefs),
		"agents", len(req.AgentIDs),
		"tasks", len(taskRefs),
		"warnings", len(combinedWarnings),
	)

	writeJSON(w, http.StatusCreated, resp)
}

// materializePlanDrafts inserts Task and ParticipantSlot rows for the
// plan, translating LocalIDs to real UUIDs. It runs in three passes:
//
//  1. INSERT every Task (in step_order) and capture the LocalID → UUID map
//  2. UPDATE depends_on on each Task by translating its DependsOnLocal
//     list against that map (entries whose LocalID is unknown are
//     dropped and surfaced as a warning, but never fail the call)
//  3. INSERT every Slot, mapping TaskLocalID to its newly created
//     Task UUID; slots whose TaskLocalID can't be resolved are skipped
//     with a warning
//
// Returns the slim TaskRefResponse list (one per inserted task), any
// warnings raised during materialization, and a hard error only if the
// underlying DB calls fail.
func (h *Handler) materializePlanDrafts(
	ctx context.Context,
	planID, workspaceID pgtype.UUID,
	gen *service.GeneratePlanResult,
) ([]TaskRefResponse, []string, error) {
	if gen == nil || len(gen.Tasks) == 0 {
		return nil, nil, nil
	}

	idMap := make(map[string]pgtype.UUID, len(gen.Tasks))
	type createdTask struct {
		row   db.Task
		local string
	}
	created := make([]createdTask, 0, len(gen.Tasks))
	var warnings []string

	// Pass 1: insert tasks in step_order so the resulting rows read top-to-bottom.
	tasksOrdered := append([]service.TaskDraft{}, gen.Tasks...)
	sortTaskDrafts(tasksOrdered)

	for _, t := range tasksOrdered {
		params := db.CreateTaskParams{
			PlanID:             planID,
			WorkspaceID:        workspaceID,
			Title:              firstNonEmpty(t.Title, "Untitled task"),
			Description:        strToText(t.Description),
			StepOrder:          pgtype.Int4{Int32: int32(t.StepOrder), Valid: true},
			RequiredSkills:     append([]string{}, t.RequiredSkills...),
			CollaborationMode:  strToText(t.CollaborationMode),
			AcceptanceCriteria: strToText(t.AcceptanceCriteria),
		}
		// PrimaryAssigneeID is optional; only set when the LLM gave us
		// something parseable.
		if id := strings.TrimSpace(t.PrimaryAssigneeAgentID); id != "" {
			params.PrimaryAssigneeID = parseUUID(id)
		}
		// Fallback agents — drop empty / unparseable strings silently;
		// validation happens at FK check time.
		for _, fa := range t.FallbackAgentIDs {
			fa = strings.TrimSpace(fa)
			if fa == "" {
				continue
			}
			params.FallbackAgentIds = append(params.FallbackAgentIds, parseUUID(fa))
		}

		row, err := h.Queries.CreateTask(ctx, params)
		if err != nil {
			return nil, warnings, fmt.Errorf("create task %q: %w", t.LocalID, err)
		}
		idMap[t.LocalID] = row.ID
		created = append(created, createdTask{row: row, local: t.LocalID})
	}

	// Pass 2: wire depends_on. Drafts use LocalIDs; we resolve them to
	// the UUIDs from pass 1 and write back in a second statement so we
	// don't have to defer the FK array until both rows exist.
	for _, t := range tasksOrdered {
		if len(t.DependsOnLocal) == 0 {
			continue
		}
		taskID, ok := idMap[t.LocalID]
		if !ok {
			continue
		}
		var deps []pgtype.UUID
		var unresolved []string
		for _, dep := range t.DependsOnLocal {
			depID, ok := idMap[dep]
			if !ok {
				unresolved = append(unresolved, dep)
				continue
			}
			deps = append(deps, depID)
		}
		if len(unresolved) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"%s:%s unknown depends_on %s",
				service.WarnSlotMissingTask, t.LocalID, strings.Join(unresolved, ","),
			))
		}
		if len(deps) == 0 {
			continue
		}
		if err := h.Queries.UpdateTaskDependsOn(ctx, db.UpdateTaskDependsOnParams{
			ID:        taskID,
			DependsOn: deps,
		}); err != nil {
			return nil, warnings, fmt.Errorf("update depends_on for task %q: %w", t.LocalID, err)
		}
	}

	// Pass 3: insert slots. Group counts by TaskLocalID so we can build
	// TaskRefResponse with the slot_count later.
	slotCounts := make(map[string]int, len(gen.Tasks))
	for _, sl := range gen.Slots {
		taskID, ok := idMap[sl.TaskLocalID]
		if !ok {
			warnings = append(warnings, fmt.Sprintf(
				"%s:%s slot %s skipped (no task)",
				service.WarnSlotMissingTask, sl.TaskLocalID, sl.SlotType,
			))
			continue
		}
		params := db.CreateParticipantSlotParams{
			TaskID:          taskID,
			SlotType:        sl.SlotType,
			SlotOrder:       pgtype.Int4{Int32: int32(sl.SlotOrder), Valid: true},
			ParticipantType: strToText(sl.ParticipantType),
			Responsibility:  strToText(sl.Responsibility),
			Trigger:         strToText(sl.Trigger),
			Blocking:        pgtype.Bool{Bool: sl.Blocking, Valid: true},
			Required:        pgtype.Bool{Bool: sl.Required, Valid: true},
			ExpectedOutput:  strToText(sl.ExpectedOutput),
		}
		if id := strings.TrimSpace(sl.ParticipantID); id != "" {
			params.ParticipantID = parseUUID(id)
		}
		if _, err := h.Queries.CreateParticipantSlot(ctx, params); err != nil {
			return nil, warnings, fmt.Errorf("create slot for task %q: %w", sl.TaskLocalID, err)
		}
		slotCounts[sl.TaskLocalID]++
	}

	// Build the slim response in the original draft order.
	refs := make([]TaskRefResponse, 0, len(created))
	for _, c := range created {
		refs = append(refs, TaskRefResponse{
			ID:                uuidToString(c.row.ID),
			LocalID:           c.local,
			Title:             c.row.Title,
			StepOrder:         int(c.row.StepOrder),
			CollaborationMode: c.row.CollaborationMode,
			SlotCount:         slotCounts[c.local],
		})
	}

	return refs, warnings, nil
}

// sortTaskDrafts sorts in-place by StepOrder ascending, falling back to
// LocalID for stability when two tasks share a step_order.
func sortTaskDrafts(tasks []service.TaskDraft) {
	for i := 1; i < len(tasks); i++ {
		for j := i; j > 0; j-- {
			if tasks[j].StepOrder < tasks[j-1].StepOrder ||
				(tasks[j].StepOrder == tasks[j-1].StepOrder && tasks[j].LocalID < tasks[j-1].LocalID) {
				tasks[j], tasks[j-1] = tasks[j-1], tasks[j]
				continue
			}
			break
		}
	}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// filterMessagesByID narrows the message slice to the IDs in keep. When keep
// is empty the original slice is returned unchanged — callers want the full
// recent window in that case (legacy behavior). Order is preserved so the
// LLM sees the conversation in chronological order.
func filterMessagesByID(messages []db.Message, keep []string) []db.Message {
	if len(keep) == 0 {
		return messages
	}
	want := make(map[string]struct{}, len(keep))
	for _, id := range keep {
		want[id] = struct{}{}
	}
	out := make([]db.Message, 0, len(keep))
	for _, m := range messages {
		if _, ok := want[uuidToString(m.ID)]; ok {
			out = append(out, m)
		}
	}
	return out
}

// sourceConversationEntry builds the audit JSON written to project.source_conversations.
// When MessageIDs is set the entry records both the conversation_id and the
// specific message subset so a later operator can reproduce the exact context
// the LLM saw.
func sourceConversationEntry(ref SourceRef) map[string]any {
	entry := map[string]any{
		"conversation_id": ref.ID,
		"type":            "full",
		"snapshot_at":     time.Now().UTC().Format(time.RFC3339),
	}
	if len(ref.MessageIDs) > 0 {
		entry["type"] = "message_subset"
		entry["message_ids"] = ref.MessageIDs
	}
	return entry
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

// buildAssignedAgentsJSON constructs the assigned_agents JSONB from task drafts.
func buildAssignedAgentsJSON(tasks []service.TaskDraft) json.RawMessage {
	type agentAssignment struct {
		LocalID string `json:"local_id"`
		AgentID string `json:"agent_id"`
	}

	var assignments []agentAssignment
	for _, t := range tasks {
		if id := strings.TrimSpace(t.PrimaryAssigneeAgentID); id != "" {
			assignments = append(assignments, agentAssignment{
				LocalID: t.LocalID,
				AgentID: id,
			})
		}
	}

	if assignments == nil {
		return json.RawMessage("[]")
	}

	data, _ := json.Marshal(assignments)
	return data
}

// capabilitiesFromIdentityCard extracts the "capabilities" array from an
// agent's identity_card JSONB. Returns an empty slice if the card is empty
// or the field is missing/malformed. Mirrors the helper in the service
// package so handlers can read identity_card without crossing the boundary.
func capabilitiesFromIdentityCard(raw []byte) []string {
	return stringSliceFromIdentityCardKey(raw, "capabilities")
}

// toolsFromIdentityCard extracts the "tools" array from identity_card JSONB.
func toolsFromIdentityCard(raw []byte) []string {
	return stringSliceFromIdentityCardKey(raw, "tools")
}

func stringSliceFromIdentityCardKey(raw []byte, key string) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var card map[string]any
	if err := json.Unmarshal(raw, &card); err != nil {
		return []string{}
	}
	arr, ok := card[key].([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
