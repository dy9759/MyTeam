package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/MyAIOSHub/MyTeam/server/internal/mcp/mcptool"
	"github.com/MyAIOSHub/MyTeam/server/internal/service/memory"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// MemorySearch performs semantic search over indexed memory chunks.
type MemorySearch struct{}

func (MemorySearch) Name() string { return "memory_search" }

func (MemorySearch) InputSchema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"query"},
		"properties": map[string]any{
			"query":  map[string]string{"type": "string"},
			"top_k":  map[string]any{"type": "integer", "default": 10},
			"type":   map[string]string{"type": "string"},
			"scope":  map[string]string{"type": "string"},
			"status": map[string]any{"type": "array", "items": map[string]string{"type": "string"}, "default": []string{"confirmed"}},
		},
	}
}

func (MemorySearch) RuntimeModes() []string {
	return []string{mcptool.RuntimeLocal, mcptool.RuntimeCloud}
}

func (MemorySearch) Exec(ctx context.Context, q *db.Queries, ws mcptool.Context, args map[string]any) (mcptool.Result, error) {
	if err := ensureWorkspaceMember(ctx, q, ws); err != nil {
		if r, ok := accessErrorResult(err); ok {
			return r, nil
		}
		return mcptool.Result{}, err
	}
	if ws.Memory == nil {
		return memoryNotWiredResult(), nil
	}

	query := stringArg(args, "query")
	if query == "" {
		return mcptool.Result{}, fmt.Errorf("%w: query", errArgMissing)
	}
	topK, err := memoryIntArgDefault(args, "top_k", 10)
	if err != nil {
		return mcptool.Result{}, err
	}

	var types []memory.MemoryType
	if typ := stringArg(args, "type"); typ != "" {
		types = []memory.MemoryType{memory.MemoryType(typ)}
	}
	var scopes []memory.MemoryScope
	if scope := stringArg(args, "scope"); scope != "" {
		scopes = []memory.MemoryScope{memory.MemoryScope(scope)}
	}
	statusValues, err := memoryStringSliceArgDefault(args, "status", []string{string(memory.StatusConfirmed)})
	if err != nil {
		return mcptool.Result{}, err
	}
	statuses := make([]memory.MemoryStatus, len(statusValues))
	for i, status := range statusValues {
		statuses[i] = memory.MemoryStatus(status)
	}

	hits, err := ws.Memory.Search(ctx, memory.SearchInput{
		WorkspaceID: ws.WorkspaceID,
		Query:       query,
		TopK:        topK,
		Types:       types,
		Scopes:      scopes,
		StatusOnly:  statuses,
	})
	if err != nil {
		if errors.Is(err, memory.ErrIndexingNotWired) {
			return mcptool.Result{Errors: []string{"INDEXING_NOT_WIRED"}, Note: err.Error()}, nil
		}
		return mcptool.Result{}, err
	}

	return mcptool.Result{Data: map[string]any{"hits": hits}}, nil
}

func memoryNotWiredResult() mcptool.Result {
	return mcptool.Result{
		Errors: []string{"MEMORY_NOT_WIRED"},
		Note:   "memory service not wired in this dispatcher",
	}
}

func memoryIntArgDefault(args map[string]any, key string, fallback int) (int, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	value, err := memoryIntArg(raw)
	if err != nil {
		return 0, fmt.Errorf("argument %s must be an integer: %w", key, err)
	}
	return value, nil
}

func memoryIntArg(raw any) (int, error) {
	switch n := raw.(type) {
	case int:
		return n, nil
	case int32:
		return int(n), nil
	case int64:
		return int(n), nil
	case float64:
		i := int(n)
		if float64(i) != n {
			return 0, fmt.Errorf("got %v", n)
		}
		return i, nil
	case json.Number:
		i, err := strconv.Atoi(n.String())
		if err != nil {
			return 0, err
		}
		return i, nil
	default:
		return 0, fmt.Errorf("got %T", raw)
	}
}

func memoryStringSliceArg(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	switch values := raw.(type) {
	case []string:
		out := make([]string, len(values))
		copy(out, values)
		return out, nil
	case []any:
		out := make([]string, len(values))
		for i, value := range values {
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("argument %s[%d] must be a string", key, i)
			}
			out[i] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("argument %s must be a string array", key)
	}
}

func memoryStringSliceArgDefault(args map[string]any, key string, fallback []string) ([]string, error) {
	if _, ok := args[key]; !ok {
		out := make([]string, len(fallback))
		copy(out, fallback)
		return out, nil
	}
	return memoryStringSliceArg(args, key)
}

func memoryFloatArgDefault(args map[string]any, key string, fallback float64) (float64, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	switch n := raw.(type) {
	case int:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case float64:
		return n, nil
	case json.Number:
		f, err := strconv.ParseFloat(n.String(), 64)
		if err != nil {
			return 0, fmt.Errorf("argument %s must be a number: %w", key, err)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("argument %s must be a number", key)
	}
}
