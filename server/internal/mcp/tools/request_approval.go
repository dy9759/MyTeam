package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RequestApproval surfaces a task slot to a human reviewer for a go/no-go decision.
type RequestApproval struct{}

func (RequestApproval) Name() string { return "request_approval" }

func (RequestApproval) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"task_id", "slot_id", "context"},
		"properties": map[string]any{
			"task_id": map[string]string{"type": "string", "format": "uuid"},
			"slot_id": map[string]string{"type": "string", "format": "uuid"},
			"context": map[string]string{"type": "string"},
		},
	}
}

func (RequestApproval) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (RequestApproval) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to approval/review handler (to be added under handler/review.go)
	return mcptool.Result{Stub: true, Note: "wire to handler/review.go RequestApproval (TBD)"}, nil
}
