package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ListAssignedProjects returns projects assigned to the calling agent or member.
type ListAssignedProjects struct{}

func (ListAssignedProjects) Name() string { return "list_assigned_projects" }

func (ListAssignedProjects) InputSchema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]string{"type": "string"},
		},
	}
}

func (ListAssignedProjects) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (ListAssignedProjects) Exec(_ context.Context, _ *db.Queries, _ mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	// TODO(plan4-followup): wire to server/internal/handler/project.go ListProjects (filter by assignee)
	return mcptool.Result{Stub: true, Note: "wire to handler/project.go ListProjects (assignee filter)"}, nil
}
