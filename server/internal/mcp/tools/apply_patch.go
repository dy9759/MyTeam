package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/errcode"
	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ApplyPatch applies a unified-diff patch to a project working tree.
// No server-side implementation exists yet.
type ApplyPatch struct{}

func (ApplyPatch) Name() string { return "apply_patch" }

func (ApplyPatch) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"project_id", "patch"},
		"properties": map[string]any{
			"project_id": map[string]string{"type": "string", "format": "uuid"},
			"patch":      map[string]string{"type": "string"},
		},
	}
}

func (ApplyPatch) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (ApplyPatch) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): no existing handler — needs new patch-application service.
	return mcptool.Result{
		Stub:   true,
		Note:   "no implementation; planned: handler/patch.go ApplyPatch",
		Errors: []string{errcode.MCPToolNotAvailable.Code},
	}, nil
}
