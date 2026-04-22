package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// RequestApproval surfaces a task slot to a human reviewer for a go/no-go
// decision. Creates an inbox_item of type "human_input_needed" addressed to
// the task owner (resolved task → run → project → creator_owner_id).
//
// Auth: caller agent must be the task's actual_agent_id OR primary_assignee_id
// (cross-cutting PRD §7.2).
type RequestApproval struct{}

func (RequestApproval) Name() string { return "request_approval" }

func (RequestApproval) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"task_id", "slot_id", "context"},
		"properties": map[string]any{
			"task_id": map[string]string{"type": "string", "format": "uuid"},
			"slot_id": map[string]string{"type": "string", "format": "uuid"},
			"context": map[string]string{"type": "string"},
		},
	}
}

func (RequestApproval) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (RequestApproval) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	taskID, err := uuidArg(args, "task_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	slotID, err := optionalUUIDArg(args, "slot_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	contextStr := stringArg(args, "context")

	task, deny, err := ensureAgentOnTask(ctx, q, ws, taskID)
	if err != nil {
		return mcptool.Result{}, err
	}
	if deny.Note != "" {
		return deny, nil
	}

	// Resolve task owner via run → project → creator_owner_id.
	if !task.RunID.Valid {
		return mcptool.Result{Errors: []string{"TASK_HAS_NO_RUN"}, Note: "task has no run_id; cannot resolve owner"}, nil
	}
	run, err := q.GetProjectRun(ctx, task.RunID)
	if err != nil {
		return mcptool.Result{}, fmt.Errorf("get project run: %w", err)
	}
	project, err := q.GetProject(ctx, run.ProjectID)
	if err != nil {
		return mcptool.Result{}, fmt.Errorf("get project: %w", err)
	}
	if !sameUUID(project.WorkspaceID, ws.WorkspaceID) {
		return permissionDenied("project does not belong to caller workspace"), nil
	}
	if !project.CreatorOwnerID.Valid {
		return mcptool.Result{Errors: []string{"PROJECT_HAS_NO_OWNER"}, Note: "project missing creator_owner_id"}, nil
	}

	// Build the inbox details payload — slot_id, context, requesting agent.
	details, _ := json.Marshal(map[string]any{
		"slot_id":             uuidStringIfValid(slotID),
		"context":             contextStr,
		"requesting_agent_id": uuidStringOrEmpty(ws.AgentID),
		"requesting_user_id":  uuidStringOrEmpty(ws.UserID),
		"task_id":             taskID.String(),
	})

	title := "Approval requested: " + task.Title
	body := contextStr
	if body == "" {
		body = "An agent has requested approval to proceed."
	}

	item, err := q.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   task.WorkspaceID,
		RecipientType: "member",
		RecipientID:   project.CreatorOwnerID,
		Type:          "human_input_needed",
		Severity:      "action_required",
		IssueID:       pgUUID(uuid.Nil),
		Title:         title,
		Body:          toPgNullText(body),
		ActorType:     toPgNullText(actorTypeFromCtx(ws)),
		ActorID:       pgUUID(actorIDFromCtx(ws)),
		Details:       details,
	})
	if err != nil {
		return mcptool.Result{}, fmt.Errorf("create inbox item: %w", err)
	}

	return mcptool.Result{Data: map[string]any{
		"inbox_item_id": uuid.UUID(item.ID.Bytes).String(),
		"recipient_id":  uuid.UUID(item.RecipientID.Bytes).String(),
		"task_id":       taskID.String(),
		"slot_id":       uuidStringIfValid(slotID),
	}}, nil
}

// uuidStringIfValid returns the canonical string for a uuid, or "" when nil.
// Lower-case nil prevents propagating the all-zero UUID into JSON payloads.
func uuidStringIfValid(u uuid.UUID) string {
	if u == uuid.Nil {
		return ""
	}
	return u.String()
}

// uuidStringOrEmpty is identical in behavior to uuidStringIfValid; kept as
// a separate name so call-sites read intent-first.
func uuidStringOrEmpty(u uuid.UUID) string { return uuidStringIfValid(u) }

// actorTypeFromCtx picks the inbox actor_type to attribute the request to.
// Agents are first-class actors (PRD §3.1) so we prefer the agent id when
// the call originated from an execution; otherwise fall through to member.
func actorTypeFromCtx(ws mcptool.Context) string {
	if ws.AgentID != uuid.Nil {
		return "agent"
	}
	return "member"
}

// actorIDFromCtx returns the matching id for actorTypeFromCtx.
func actorIDFromCtx(ws mcptool.Context) uuid.UUID {
	if ws.AgentID != uuid.Nil {
		return ws.AgentID
	}
	return ws.UserID
}
