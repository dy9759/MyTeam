// Package mcptool defines the runtime contract shared between the MCP
// dispatcher (package mcp) and the individual tool implementations
// (package mcp/tools). Splitting these types out of the parent package
// prevents the import cycle that would otherwise arise from registry.go
// importing tools/* and tools/* importing the parent for the Tool
// interface and Context/Result types.
package mcptool

import (
	"context"

	"github.com/google/uuid"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Runtime mode constants for RuntimeModes() and Context.RuntimeMode.
const (
	RuntimeLocal = "local"
	RuntimeCloud = "cloud"
)

// Tool is the runtime contract every MCP tool implements.
type Tool interface {
	Name() string
	InputSchema() any       // JSON-schema-shaped description; opaque to dispatcher
	RuntimeModes() []string // local|cloud|both
	Exec(ctx context.Context, q *db.Queries, ws Context, args map[string]any) (Result, error)
}

// Context carries request-scoped identifiers a tool needs for permission checks.
type Context struct {
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	AgentID     uuid.UUID // nil-uuid when called outside an agent execution
	RuntimeMode string    // "local" or "cloud"
}

// Result is the JSON-serializable response from a tool execution.
type Result struct {
	Data   any      `json:"data,omitempty"`
	Stub   bool     `json:"stub,omitempty"`
	Note   string   `json:"note,omitempty"`
	Errors []string `json:"errors,omitempty"`
}
