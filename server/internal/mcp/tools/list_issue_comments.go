package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ListIssueComments returns paginated comments for an issue.
type ListIssueComments struct{}

func (ListIssueComments) Name() string { return "list_issue_comments" }

func (ListIssueComments) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id"},
		"properties": map[string]any{
			"issue_id": map[string]string{"type": "string", "format": "uuid"},
			"limit":    map[string]string{"type": "integer"},
			"offset":   map[string]string{"type": "integer"},
		},
	}
}

func (ListIssueComments) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (ListIssueComments) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to server/internal/handler/comment.go ListComments
	return mcptool.Result{Stub: true, Note: "wire to handler/comment.go ListComments"}, nil
}
