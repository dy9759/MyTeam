package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	"github.com/MyAIOSHub/MyTeam/server/internal/service"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
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

func (ReadFile) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	projectID, err := uuidArg(args, "project_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	if _, err := loadProjectForWorkspace(ctx, q, ws, projectID); err != nil {
		if result, ok := accessErrorResult(err); ok {
			return result, nil
		}
		return mcptool.Result{}, err
	}

	allFiles, err := q.ListFilesByProject(ctx, pgUUID(projectID))
	if err != nil {
		return mcptool.Result{}, err
	}

	// Narrow to project-scoped files only — channel-only rows that happen to
	// carry a project_id MUST NOT leak through here. Defensive workspace
	// match too, in case a stale row still references this project but lives
	// in a different workspace.
	files := make([]db.FileIndex, 0, len(allFiles))
	for _, f := range allFiles {
		if !sameUUID(f.WorkspaceID, ws.WorkspaceID) {
			continue
		}
		var scope struct {
			Scope string `json:"scope"`
		}
		_ = json.Unmarshal(f.AccessScope, &scope)
		if scope.Scope != "project" {
			continue
		}
		files = append(files, f)
	}

	file, err := selectIndexedFile(files, args)
	if err != nil {
		return mcptool.Result{}, err
	}

	storagePath := file.StoragePath.String
	var snapshotID string
	if snapshots, err := q.ListFileSnapshotsByFile(ctx, file.ID); err != nil {
		return mcptool.Result{}, err
	} else if len(snapshots) > 0 {
		storagePath = snapshots[0].StoragePath
		snapshotID = uuidString(snapshots[0].ID)
	}

	content, err := service.ReadIndexedFileContent(ctx, storagePath)
	if err != nil {
		return mcptool.Result{}, err
	}

	data := map[string]any{
		"project_id":    projectID.String(),
		"file_index_id": uuidString(file.ID),
		"path":          file.FileName,
		"storage_path":  storagePath,
		"content":       string(content),
		"size_bytes":    len(content),
	}
	if file.ContentType.Valid {
		data["content_type"] = file.ContentType.String
	}
	if snapshotID != "" {
		data["file_snapshot_id"] = snapshotID
	}
	return mcptool.Result{Data: data}, nil
}

func selectIndexedFile(files []db.FileIndex, args map[string]any) (db.FileIndex, error) {
	if rawID := stringArg(args, "file_index_id"); rawID != "" {
		fileIndexID, err := uuidArg(args, "file_index_id")
		if err != nil {
			return db.FileIndex{}, err
		}
		for _, file := range files {
			if sameUUID(file.ID, fileIndexID) {
				return file, nil
			}
		}
		return db.FileIndex{}, fmt.Errorf("file_index_id %s not found in project", fileIndexID)
	}

	path := stringArg(args, "path")
	if path == "" {
		return db.FileIndex{}, fmt.Errorf("file_index_id or path is required")
	}
	for _, file := range files {
		if file.FileName == path || file.StoragePath.String == path {
			return file, nil
		}
	}
	return db.FileIndex{}, fmt.Errorf("path %q not found in project file index", path)
}
