package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GetProject fetches a project by id.
type GetProject struct{}

func (GetProject) Name() string { return "get_project" }

func (GetProject) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"project_id"},
		"properties": map[string]any{
			"project_id": map[string]string{"type": "string", "format": "uuid"},
		},
	}
}

func (GetProject) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (GetProject) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to server/internal/handler/project.go GetProject
	return mcptool.Result{Stub: true, Note: "wire to handler/project.go GetProject"}, nil
}
