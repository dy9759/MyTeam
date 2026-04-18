package mcp

import (
	"context"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/mcp/mcptool"
)

// ErrPermissionDenied is re-exported from the mcptool package for callers that
// already work in terms of mcp.ErrPermissionDenied.
var ErrPermissionDenied = mcptool.ErrPermissionDenied

// EnsureWorkspaceMember and EnsureAgentInWorkspace remain on this package as
// thin wrappers so existing imports keep working. The concrete implementations
// (today: no-op stubs) live in mcptool so they can be called from the per-tool
// packages without an import cycle.
func EnsureWorkspaceMember(ctx context.Context, workspaceID, userID uuid.UUID) error {
	return mcptool.EnsureWorkspaceMember(ctx, workspaceID, userID)
}

func EnsureAgentInWorkspace(ctx context.Context, workspaceID, agentID uuid.UUID) error {
	return mcptool.EnsureAgentInWorkspace(ctx, workspaceID, agentID)
}

// RequireMember is re-exported for callers in this package.
func RequireMember(ctx context.Context, ws mcptool.Context) error {
	return mcptool.RequireMember(ctx, ws)
}
