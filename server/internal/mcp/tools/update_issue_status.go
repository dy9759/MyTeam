package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// UpdateIssueStatus mutates the workflow status of an issue.
type UpdateIssueStatus struct{}

func (UpdateIssueStatus) Name() string { return "update_issue_status" }

func (UpdateIssueStatus) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id", "status"},
		"properties": map[string]any{
			"issue_id": map[string]string{"type": "string", "format": "uuid"},
			"status":   map[string]string{"type": "string"},
		},
	}
}

func (UpdateIssueStatus) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (UpdateIssueStatus) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	if err := mcptool.RequireMember(ctx, ws); err != nil {
		return mcptool.Result{}, err
	}
	issueID, err := requireUUIDArg(args, "issue_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	status, _ := args["status"].(string)
	if status == "" {
		return mcptool.Result{}, errors.New("status is required")
	}

	// Workspace boundary check before mutating: ensure the issue belongs to
	// the caller's workspace, otherwise UpdateIssueStatus would silently
	// touch another tenant's row.
	if _, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          uuidToPgtype(issueID),
		WorkspaceID: uuidToPgtype(ws.WorkspaceID),
	}); err != nil {
		return mcptool.Result{}, fmt.Errorf("issue not found: %w", err)
	}

	updated, err := q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     uuidToPgtype(issueID),
		Status: status,
	})
	if err != nil {
		return mcptool.Result{}, fmt.Errorf("update status: %w", err)
	}

	return mcptool.Result{Data: issueToMap(updated)}, nil
}
