package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ReadFile reads the contents of a project-indexed file.
type ReadFile struct{}

func (ReadFile) Name() string { return "read_file" }

func (ReadFile) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"project_id"},
		"properties": map[string]any{
			"project_id":    map[string]string{"type": "string", "format": "uuid"},
			"file_index_id": map[string]string{"type": "string", "format": "uuid"},
			"path":          map[string]string{"type": "string"},
		},
		"oneOf": []map[string]any{
			{"required": []string{"file_index_id"}},
			{"required": []string{"path"}},
		},
	}
}

func (ReadFile) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (ReadFile) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to server/internal/handler/file_index.go GetFileIndexContent
	return mcptool.Result{Stub: true, Note: "wire to handler/file_index.go GetFileIndexContent"}, nil
}
