package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/errcode"
	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CreatePR opens a pull request on the project's source-control provider.
// No server-side implementation exists yet.
type CreatePR struct{}

func (CreatePR) Name() string { return "create_pr" }

func (CreatePR) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"project_id", "branch", "title", "body"},
		"properties": map[string]any{
			"project_id": map[string]string{"type": "string", "format": "uuid"},
			"branch":     map[string]string{"type": "string"},
			"title":      map[string]string{"type": "string"},
			"body":       map[string]string{"type": "string"},
		},
	}
}

func (CreatePR) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (CreatePR) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): no existing handler — needs SCM provider integration.
	return mcptool.Result{
		Stub:   true,
		Note:   "no implementation; planned: handler/scm.go CreatePR",
		Errors: []string{errcode.MCPToolNotAvailable.Code},
	}, nil
}
