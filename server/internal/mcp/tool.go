// Package mcp implements the server side of the MCP tool catalog.
// Tools are stubs in this package; concrete implementations wire to
// existing handlers in subsequent integration commits.
//
// The Tool interface and shared Context/Result types live in
// the mcp/mcptool sub-package so that registry.go can import the
// per-tool implementations under mcp/tools without an import cycle.
// They are re-exported here as aliases so callers can continue to use
// mcp.Tool, mcp.Context, mcp.Result, and the runtime constants.
package mcp

import "github.com/multica-ai/multica/server/internal/mcp/mcptool"

// Tool is the runtime contract every MCP tool implements.
type Tool = mcptool.Tool

// Context carries request-scoped identifiers a tool needs for permission checks.
type Context = mcptool.Context

// Result is the JSON-serializable response from a tool execution.
type Result = mcptool.Result

// Runtime mode constants.
const (
	RuntimeLocal = mcptool.RuntimeLocal
	RuntimeCloud = mcptool.RuntimeCloud
)
