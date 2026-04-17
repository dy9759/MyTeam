package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SearchProjectContext runs a semantic search against the project context store.
type SearchProjectContext struct{}

func (SearchProjectContext) Name() string { return "search_project_context" }

func (SearchProjectContext) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"project_id", "query"},
		"properties": map[string]any{
			"project_id": map[string]string{"type": "string", "format": "uuid"},
			"query":      map[string]string{"type": "string"},
		},
	}
}

func (SearchProjectContext) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (SearchProjectContext) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to server/internal/handler/search.go (project-scoped semantic search)
	return mcptool.Result{Stub: true, Note: "wire to handler/search.go project context search"}, nil
}
