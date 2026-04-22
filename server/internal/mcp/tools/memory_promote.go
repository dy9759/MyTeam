package tools

import (
	"context"
	"errors"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	"github.com/MyAIOSHub/MyTeam/server/internal/service/memory"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// MemoryPromote promotes a memory record from candidate to confirmed.
type MemoryPromote struct{}

func (MemoryPromote) Name() string { return "memory_promote" }

func (MemoryPromote) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"memory_id"},
		"properties": map[string]any{
			"memory_id": map[string]string{"type": "string", "format": "uuid"},
		},
	}
}

func (MemoryPromote) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (MemoryPromote) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	if err := ensureWorkspaceMember(ctx, q, ws); err != nil {
		if r, ok := accessErrorResult(err); ok {
			return r, nil
		}
		return mcptool.Result{}, err
	}
	if ws.Memory == nil {
		return memoryNotWiredResult(), nil
	}

	memoryID, err := uuidArg(args, "memory_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	m, err := ws.Memory.Promote(ctx, memoryID)
	if err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			return mcptool.Result{Errors: []string{"MEMORY_NOT_FOUND"}}, nil
		}
		return mcptool.Result{}, err
	}

	return mcptool.Result{Data: map[string]any{"memory": m}}, nil
}
