package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// ListProjectFiles enumerates indexed files for a project. Only entries whose
// access_scope.scope == "project" are returned (channel-only files stay
// hidden). Optional path_prefix filters by file_name prefix.
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

func (ListProjectFiles) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	projectID, err := uuidArg(args, "project_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	pathPrefix := stringArg(args, "path_prefix")

	if _, err := loadProjectForWorkspace(ctx, q, ws, projectID); err != nil {
		if r, ok := accessErrorResult(err); ok {
			return r, nil
		}
		return mcptool.Result{}, err
	}

	files, err := q.ListFilesByProject(ctx, pgUUID(projectID))
	if err != nil {
		return mcptool.Result{}, err
	}

	type fileOut struct {
		ID          string `json:"id"`
		ProjectID   string `json:"project_id"`
		FileName    string `json:"file_name"`
		ContentType string `json:"content_type"`
		FileSize    int64  `json:"file_size"`
	}
	out := []fileOut{}
	for _, f := range files {
		var scope struct {
			Scope string `json:"scope"`
		}
		_ = json.Unmarshal(f.AccessScope, &scope)
		if scope.Scope != "project" {
			continue
		}
		if pathPrefix != "" && !strings.HasPrefix(f.FileName, pathPrefix) {
			continue
		}
		out = append(out, fileOut{
			ID:          uuidString(f.ID),
			ProjectID:   uuidString(f.ProjectID),
			FileName:    f.FileName,
			ContentType: f.ContentType.String,
			FileSize:    f.FileSize.Int64,
		})
	}

	return mcptool.Result{Data: map[string]any{"files": out}}, nil
}
