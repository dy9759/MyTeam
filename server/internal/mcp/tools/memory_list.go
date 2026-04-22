package tools

import (
	"context"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	"github.com/MyAIOSHub/MyTeam/server/internal/service/memory"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// MemoryList lists memory records for the caller's workspace.
type MemoryList struct{}

func (MemoryList) Name() string { return "memory_list" }

func (MemoryList) InputSchema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type":   map[string]string{"type": "string"},
			"scope":  map[string]string{"type": "string"},
			"status": map[string]string{"type": "string"},
			"limit":  map[string]any{"type": "integer", "default": 50},
			"offset": map[string]string{"type": "integer"},
		},
	}
}

func (MemoryList) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (MemoryList) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	if err := ensureWorkspaceMember(ctx, q, ws); err != nil {
		if r, ok := accessErrorResult(err); ok {
			return r, nil
		}
		return mcptool.Result{}, err
	}
	if ws.Memory == nil {
		return memoryNotWiredResult(), nil
	}

	limit, err := memoryIntArgDefault(args, "limit", 50)
	if err != nil {
		return mcptool.Result{}, err
	}
	offset, err := memoryIntArgDefault(args, "offset", 0)
	if err != nil {
		return mcptool.Result{}, err
	}
	if offset < 0 {
		offset = 0
	}

	memories, err := ws.Memory.ListByWorkspace(ctx, ws.WorkspaceID, memory.ListFilter{
		Type:   memory.MemoryType(stringArg(args, "type")),
		Scope:  memory.MemoryScope(stringArg(args, "scope")),
		Status: memory.MemoryStatus(stringArg(args, "status")),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return mcptool.Result{}, err
	}

	return mcptool.Result{Data: map[string]any{"memories": memories}}, nil
}
