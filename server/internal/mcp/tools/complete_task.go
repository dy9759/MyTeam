package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CompleteTask marks a task as complete with a result payload.
type CompleteTask struct{}

func (CompleteTask) Name() string { return "complete_task" }

func (CompleteTask) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"task_id", "result"},
		"properties": map[string]any{
			"task_id": map[string]string{"type": "string", "format": "uuid"},
			"result":  map[string]any{"type": "object"},
		},
	}
}

func (CompleteTask) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (CompleteTask) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to server/internal/service/task.go CompleteTask
	return mcptool.Result{Stub: true, Note: "wire to service/task.go CompleteTask"}, nil
}
