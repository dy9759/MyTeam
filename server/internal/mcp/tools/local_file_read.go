package tools

import (
	"context"
	"fmt"
	"os"

	"github.com/MyAIOSHub/MyTeam/server/internal/errcode"
	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// LocalFileRead reads a file from the local daemon's filesystem.
// LOCAL ONLY — refused in cloud runtime.
type LocalFileRead struct{}

func (LocalFileRead) Name() string { return "local_file_read" }

func (LocalFileRead) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"path"},
		"properties": map[string]any{
			"path":      map[string]string{"type": "string"},
			"max_bytes": map[string]string{"type": "integer"},
		},
	}
}

func (LocalFileRead) RuntimeModes() []string { return []string{mcptool.RuntimeLocal} }

func (LocalFileRead) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	if ws.RuntimeMode != mcptool.RuntimeLocal {
		return mcptool.Result{
			Note:   "local_file_read is local-only",
			Errors: []string{errcode.MCPToolNotAvailable.Code},
		}, nil
	}

	if err := ensureWorkspaceMember(ctx, q, ws); err != nil {
		if result, ok := accessErrorResult(err); ok {
			return result, nil
		}
		return mcptool.Result{}, err
	}

	path := stringArg(args, "path")
	if path == "" {
		return mcptool.Result{}, fmt.Errorf("path is required")
	}
	absPath, allowed, err := allowedPath(path)
	if err != nil {
		return mcptool.Result{}, err
	}
	if !allowed {
		return mcptool.Result{
			Note:   "path is outside daemon allowed paths",
			Errors: []string{errcode.MCPPermissionDenied.Code},
		}, nil
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return mcptool.Result{}, err
	}
	if maxBytes := maxBytesArg(args); maxBytes > 0 && len(data) > maxBytes {
		return mcptool.Result{}, fmt.Errorf("file exceeds max_bytes (%d)", maxBytes)
	}

	return mcptool.Result{Data: map[string]any{
		"path":       absPath,
		"content":    string(data),
		"size_bytes": len(data),
	}}, nil
}

func maxBytesArg(args map[string]any) int {
	switch value := args["max_bytes"].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}
