package tools

import (
	"context"

	"github.com/multica-ai/multica/server/internal/errcode"
	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
			"path": map[string]string{"type": "string"},
		},
	}
}

func (LocalFileRead) RuntimeModes() []string { return []string{mcptool.RuntimeLocal} }

func (LocalFileRead) Exec(_ context.Context, _ *db.Queries, ws mcptool.Context, _ map[string]any) (mcptool.Result, error) {
	if ws.RuntimeMode != mcptool.RuntimeLocal {
		return mcptool.Result{
			Stub:   true,
			Note:   "local_file_read is local-only",
			Errors: []string{errcode.MCPToolNotAvailable.Code},
		}, nil
	}
	// TODO(plan4-followup): no existing handler — daemon-side filesystem read.
	return mcptool.Result{
		Stub:   true,
		Note:   "no implementation; planned: daemon/files.go ReadLocalFile",
		Errors: []string{errcode.MCPToolNotAvailable.Code},
	}, nil
}
