package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	"github.com/MyAIOSHub/MyTeam/server/internal/service/memory"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// MemoryAppend appends a candidate memory derived from an existing raw record.
type MemoryAppend struct{}

func (MemoryAppend) Name() string { return "memory_append" }

func (MemoryAppend) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"type", "scope", "source", "raw_kind", "raw_id"},
		"properties": map[string]any{
			"type":       map[string]string{"type": "string"},
			"scope":      map[string]string{"type": "string"},
			"source":     map[string]string{"type": "string"},
			"raw_kind":   map[string]string{"type": "string"},
			"raw_id":     map[string]string{"type": "string", "format": "uuid"},
			"summary":    map[string]string{"type": "string"},
			"body":       map[string]string{"type": "string"},
			"tags":       map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
			"entities":   map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
			"confidence": map[string]string{"type": "number"},
		},
	}
}

func (MemoryAppend) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (MemoryAppend) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	if err := ensureWorkspaceMember(ctx, q, ws); err != nil {
		if r, ok := accessErrorResult(err); ok {
			return r, nil
		}
		return mcptool.Result{}, err
	}
	if ws.Memory == nil {
		return memoryNotWiredResult(), nil
	}

	typ := stringArg(args, "type")
	if typ == "" {
		return mcptool.Result{}, fmt.Errorf("%w: type", errArgMissing)
	}
	scope := stringArg(args, "scope")
	if scope == "" {
		return mcptool.Result{}, fmt.Errorf("%w: scope", errArgMissing)
	}
	source := stringArg(args, "source")
	if source == "" {
		return mcptool.Result{}, fmt.Errorf("%w: source", errArgMissing)
	}
	rawKind := stringArg(args, "raw_kind")
	if rawKind == "" {
		return mcptool.Result{}, fmt.Errorf("%w: raw_kind", errArgMissing)
	}
	rawID, err := uuidArg(args, "raw_id")
	if err != nil {
		return mcptool.Result{}, err
	}
	tags, err := memoryStringSliceArg(args, "tags")
	if err != nil {
		return mcptool.Result{}, err
	}
	entities, err := memoryStringSliceArg(args, "entities")
	if err != nil {
		return mcptool.Result{}, err
	}
	confidence, err := memoryFloatArgDefault(args, "confidence", 0)
	if err != nil {
		return mcptool.Result{}, err
	}

	m, err := ws.Memory.Append(ctx, memory.AppendInput{
		WorkspaceID: ws.WorkspaceID,
		CreatedBy:   ws.UserID,
		Type:        memory.MemoryType(typ),
		Scope:       memory.MemoryScope(scope),
		Source:      source,
		Raw:         memory.RawRef{Kind: memory.RawKind(rawKind), ID: rawID},
		Summary:     stringArg(args, "summary"),
		Body:        stringArg(args, "body"),
		Tags:        tags,
		Entities:    entities,
		Confidence:  confidence,
	})
	if err != nil {
		if errors.Is(err, memory.ErrInvalidRaw) {
			return mcptool.Result{Errors: []string{"INVALID_RAW"}, Note: err.Error()}, nil
		}
		return mcptool.Result{}, err
	}

	return mcptool.Result{Data: map[string]any{"memory": m}}, nil
}
