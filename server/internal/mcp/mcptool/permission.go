package mcptool

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrPermissionDenied is returned by permission helpers when the caller is not
// authorized to perform the requested action.
var ErrPermissionDenied = errors.New("mcp: permission denied")

// EnsureWorkspaceMember is the baseline permission check every tool calls before
// reading data. Returns ErrPermissionDenied if user is not a member of the
// workspace. Concrete implementation deferred — for now this stub allows all
// (skeleton phase). Lives in mcptool (not the parent mcp package) so individual
// tool packages can call it without an import cycle.
func EnsureWorkspaceMember(ctx context.Context, workspaceID, userID uuid.UUID) error {
	// TODO(plan4-followup): wire to db.GetMemberByUserAndWorkspace and return
	// ErrPermissionDenied when not a member.
	_ = ctx
	_ = workspaceID
	_ = userID
	return nil
}

// EnsureAgentInWorkspace verifies the agent_id (if non-nil) belongs to the
// workspace. Stub passes everything.
func EnsureAgentInWorkspace(ctx context.Context, workspaceID, agentID uuid.UUID) error {
	if agentID == uuid.Nil {
		return nil
	}
	// TODO(plan4-followup): query agent table.
	_ = ctx
	_ = workspaceID
	return nil
}

// RequireMember is the single preflight every issue/comment tool calls before
// touching the database. Today the underlying stubs are no-ops, so this is a
// forward-compat hook: when EnsureWorkspaceMember/EnsureAgentInWorkspace gain
// real implementations, every tool that already calls RequireMember picks up
// the new boundary automatically — no per-tool re-threading required.
//
// The check is performed in this order:
//  1. workspace_id is present (defensive — most tools also reject nil ws.WorkspaceID).
//  2. caller is a member of the workspace.
//  3. when ctx.AgentID is set (i.e. the call is acting on behalf of an agent),
//     that agent belongs to the workspace.
func RequireMember(ctx context.Context, ws Context) error {
	if ws.WorkspaceID == uuid.Nil {
		return errors.New("workspace_id required")
	}
	if err := EnsureWorkspaceMember(ctx, ws.WorkspaceID, ws.UserID); err != nil {
		return err
	}
	if err := EnsureAgentInWorkspace(ctx, ws.WorkspaceID, ws.AgentID); err != nil {
		return err
	}
	return nil
}
