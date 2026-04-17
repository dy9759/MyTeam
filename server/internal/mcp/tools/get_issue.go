package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

func (GetIssue) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to server/internal/handler/issue.go GetIssue
	return mcptool.Result{Stub: true, Note: "wire to handler/issue.go GetIssue"}, nil
}
