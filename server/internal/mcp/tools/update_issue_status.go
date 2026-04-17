package tools

import (
	"context"

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

func (UpdateIssueStatus) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to server/internal/handler/issue.go UpdateIssue (status field)
	return mcptool.Result{Stub: true, Note: "wire to handler/issue.go UpdateIssue"}, nil
}
