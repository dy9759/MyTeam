package mcp

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrPermissionDenied is returned by permission helpers when the caller is not
// authorized to perform the requested action.
var ErrPermissionDenied = errors.New("mcp: permission denied")

// EnsureWorkspaceMember is the baseline permission check every tool calls before
// reading data. Returns ErrPermissionDenied if user is not a member of the workspace.
// Concrete implementation deferred — for now this stub allows all (skeleton phase).
func EnsureWorkspaceMember(ctx context.Context, workspaceID, userID uuid.UUID) error {
	// TODO(plan4-followup): wire to db.GetMemberByUserAndWorkspace and return
	// ErrPermissionDenied when not a member.
	_ = ctx
	_ = workspaceID
	_ = userID
	return nil
}

// EnsureAgentInWorkspace verifies the agent_id (if non-nil) belongs to the workspace.
// Stub passes everything.
func EnsureAgentInWorkspace(ctx context.Context, workspaceID, agentID uuid.UUID) error {
	if agentID == uuid.Nil {
		return nil
	}
	// TODO(plan4-followup): query agent table.
	_ = ctx
	_ = workspaceID
	return nil
}
