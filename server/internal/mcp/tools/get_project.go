package tools

import (
	"context"

	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
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

func (GetProject) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	projectID, err := uuidArg(args, "project_id")
	if err != nil {
		return mcptool.Result{}, err
	}

	if err := ensureWorkspaceMember(ctx, q, ws); err != nil {
		if r, ok := accessErrorResult(err); ok {
			return r, nil
		}
		return mcptool.Result{}, err
	}
	project, err := q.GetProject(ctx, pgUUID(projectID))
	if err != nil {
		if r, ok := accessErrorResult(err); ok {
			return r, nil
		}
		return mcptool.Result{}, err
	}
	if !sameUUID(project.WorkspaceID, ws.WorkspaceID) {
		return mcptool.Result{
			Errors: []string{errcode.ProjectNotFound.Code},
			Note:   errcode.ProjectNotFound.Message,
		}, nil
	}

	return mcptool.Result{Data: map[string]any{
		"project": map[string]any{
			"id":           uuidString(project.ID),
			"workspace_id": uuidString(project.WorkspaceID),
			"title":        project.Title,
			"description":  project.Description.String,
			"status":       project.Status,
			"channel_id":   uuidString(project.ChannelID),
		},
	}}, nil
}
