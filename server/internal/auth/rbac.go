// Package auth provides authentication and RBAC helpers.
package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrForbidden is returned when a guard rejects a request.
var ErrForbidden = errors.New("forbidden")

// MemberLookup resolves a workspace member's role.
type MemberLookup interface {
	GetMemberRole(ctx context.Context, workspaceID, userID uuid.UUID) (string, error)
}

// AgentLookup resolves an agent's owner.
type AgentLookup interface {
	GetAgentOwnerID(ctx context.Context, agentID uuid.UUID) (uuid.UUID, error)
}

// Guards bundles RBAC checks. Construct via NewGuards (rbac_adapter.go).
type Guards struct {
	Member MemberLookup
	Agent  AgentLookup
}

// RequireAdminOrAbove allows owner and admin roles.
func (g Guards) RequireAdminOrAbove(ctx context.Context, workspaceID, userID uuid.UUID) error {
	role, err := g.Member.GetMemberRole(ctx, workspaceID, userID)
	if err != nil {
		return err
	}
	if role == "owner" || role == "admin" {
		return nil
	}
	return ErrForbidden
}

// RequireOwner allows only the workspace owner role.
func (g Guards) RequireOwner(ctx context.Context, workspaceID, userID uuid.UUID) error {
	role, err := g.Member.GetMemberRole(ctx, workspaceID, userID)
	if err != nil {
		return err
	}
	if role == "owner" {
		return nil
	}
	return ErrForbidden
}

// RequireAgentOwner allows only the agent's Agent Owner.
func (g Guards) RequireAgentOwner(ctx context.Context, agentID, userID uuid.UUID) error {
	ownerID, err := g.Agent.GetAgentOwnerID(ctx, agentID)
	if err != nil {
		return err
	}
	if ownerID == userID {
		return nil
	}
	return ErrForbidden
}
