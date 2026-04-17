package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ListProjectFiles enumerates indexed files for a project.
type ListProjectFiles struct{}

func (ListProjectFiles) Name() string { return "list_project_files" }

func (ListProjectFiles) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"project_id"},
		"properties": map[string]any{
			"project_id":  map[string]string{"type": "string", "format": "uuid"},
			"path_prefix": map[string]string{"type": "string"},
		},
	}
}

func (ListProjectFiles) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (ListProjectFiles) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to server/internal/handler/file_index.go ListFileIndex
	return mcptool.Result{Stub: true, Note: "wire to handler/file_index.go ListFileIndex"}, nil
}
