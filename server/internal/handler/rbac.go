package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/errcode"
)

// requireWorkspaceOwner centralizes owner-only RBAC checks for handlers whose
// workspace comes from X-Workspace-ID rather than a workspace URL segment.
func (h *Handler) requireWorkspaceOwner(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	userID := requestUserID(r)
	if userID == "" {
		errcode.Write(w, errcode.AuthUnauthorized, "", nil)
		return false
	}
	wsUUID, err := uuid.Parse(workspaceID)
	if err != nil {
		errcode.Write(w, errcode.AuthForbidden, "invalid workspace id", nil)
		return false
	}
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		errcode.Write(w, errcode.AuthForbidden, "invalid user id", nil)
		return false
	}
	if err := h.Guards.RequireOwner(r.Context(), wsUUID, userUUID); err != nil {
		if errors.Is(err, auth.ErrForbidden) {
			errcode.Write(w, errcode.AuthForbidden, "", nil)
			return false
		}
		errcode.Write(w, errcode.AuthForbidden, "", nil)
		return false
	}
	return true
}
