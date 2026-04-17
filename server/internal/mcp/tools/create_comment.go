package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CreateComment creates a new comment on an issue.
type CreateComment struct{}

func (CreateComment) Name() string { return "create_comment" }

func (CreateComment) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"issue_id", "body"},
		"properties": map[string]any{
			"issue_id": map[string]string{"type": "string", "format": "uuid"},
			"body":     map[string]string{"type": "string"},
		},
	}
}

func (CreateComment) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (CreateComment) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to server/internal/handler/comment.go CreateComment
	return mcptool.Result{Stub: true, Note: "wire to handler/comment.go CreateComment"}, nil
}
