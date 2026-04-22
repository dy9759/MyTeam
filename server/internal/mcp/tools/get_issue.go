package tools

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// GetIssue fetches a single issue by id.
type GetIssue struct{}

func (GetIssue) Name() string { return "get_issue" }

func (GetIssue) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id"},
		"properties": map[string]any{
			"issue_id": map[string]string{"type": "string", "format": "uuid"},
		},
	}
}

func (GetIssue) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (GetIssue) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	if err := mcptool.RequireMember(ctx, ws); err != nil {
		return mcptool.Result{}, err
	}
	issueID, err := requireUUIDArg(args, "issue_id")
	if err != nil {
		return mcptool.Result{}, err
	}

	issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          uuidToPgtype(issueID),
		WorkspaceID: uuidToPgtype(ws.WorkspaceID),
	})
	if err != nil {
		return mcptool.Result{}, fmt.Errorf("issue not found: %w", err)
	}

	return mcptool.Result{Data: issueToMap(issue)}, nil
}

// uuidToPgtype converts a google/uuid.UUID into a pgtype.UUID for sqlc params.
func uuidToPgtype(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// requireUUIDArg pulls a required UUID-typed string argument from the args map.
func requireUUIDArg(args map[string]any, key string) (uuid.UUID, error) {
	raw, ok := args[key]
	if !ok {
		return uuid.Nil, fmt.Errorf("missing required argument: %s", key)
	}
	s, ok := raw.(string)
	if !ok {
		return uuid.Nil, fmt.Errorf("argument %s must be a string", key)
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid uuid for %s: %w", key, err)
	}
	return id, nil
}

// issueToMap renders an issue row as a JSON-serializable map. Mirrors the
// HTTP handler's IssueResponse fields so MCP callers see the same shape as
// REST callers.
func issueToMap(i db.Issue) map[string]any {
	out := map[string]any{
		"id":           uuidString(i.ID),
		"workspace_id": uuidString(i.WorkspaceID),
		"number":       i.Number,
		"title":        i.Title,
		"status":       i.Status,
		"priority":     i.Priority,
		"creator_type": i.CreatorType,
		"creator_id":   uuidString(i.CreatorID),
		"position":     i.Position,
		"created_at":   timestampString(i.CreatedAt),
		"updated_at":   timestampString(i.UpdatedAt),
	}
	if i.Description.Valid {
		out["description"] = i.Description.String
	}
	if i.AssigneeType.Valid {
		out["assignee_type"] = i.AssigneeType.String
	}
	if i.AssigneeID.Valid {
		out["assignee_id"] = uuidString(i.AssigneeID)
	}
	if i.ParentIssueID.Valid {
		out["parent_issue_id"] = uuidString(i.ParentIssueID)
	}
	if i.DueDate.Valid {
		out["due_date"] = i.DueDate.Time.Format("2006-01-02T15:04:05Z07:00")
	}
	return out
}


func timestampString(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format("2006-01-02T15:04:05.999999Z07:00")
}
