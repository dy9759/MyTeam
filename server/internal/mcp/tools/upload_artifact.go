package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// UploadArtifact uploads a deliverable artifact for a task slot execution.
type UploadArtifact struct{}

func (UploadArtifact) Name() string { return "upload_artifact" }

func (UploadArtifact) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"task_id", "slot_id", "execution_id", "artifact_type", "title", "summary"},
		"properties": map[string]any{
			"task_id":       map[string]string{"type": "string", "format": "uuid"},
			"slot_id":       map[string]string{"type": "string", "format": "uuid"},
			"execution_id":  map[string]string{"type": "string", "format": "uuid"},
			"artifact_type": map[string]string{"type": "string"},
			"title":         map[string]string{"type": "string"},
			"summary":       map[string]string{"type": "string"},
			"content":       map[string]string{"type": "string"},
			"file":          map[string]string{"type": "string"},
		},
		"oneOf": []map[string]any{
			{"required": []string{"content"}},
			{"required": []string{"file"}},
		},
	}
}

func (UploadArtifact) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (UploadArtifact) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to artifact upload handler (to be added under handler/artifact.go)
	return mcptool.Result{Stub: true, Note: "wire to handler/artifact.go UploadArtifact (TBD)"}, nil
}
