package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/errcode"
	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CheckoutRepo clones or refreshes a project repo into the local daemon workspace.
// LOCAL ONLY — refused in cloud runtime.
type CheckoutRepo struct{}

func (CheckoutRepo) Name() string { return "checkout_repo" }

func (CheckoutRepo) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"project_id"},
		"properties": map[string]any{
			"project_id": map[string]string{"type": "string", "format": "uuid"},
		},
	}
}

func (CheckoutRepo) RuntimeModes() []string { return []string{mcptool.RuntimeLocal} }

func (CheckoutRepo) Exec(_ context.Context, _ *db.Queries, ws mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	if ws.RuntimeMode != mcptool.RuntimeLocal {
		return mcptool.Result{
			Stub:   true,
			Note:   "checkout_repo is local-only",
			Errors: []string{errcode.MCPToolNotAvailable.Code},
		}, nil
	}
	// TODO(plan4-followup): no existing handler — daemon-side git clone/refresh.
	return mcptool.Result{
		Stub:   true,
		Note:   "no implementation; planned: daemon/checkout.go CheckoutRepo",
		Errors: []string{errcode.MCPToolNotAvailable.Code},
	}, nil
}
