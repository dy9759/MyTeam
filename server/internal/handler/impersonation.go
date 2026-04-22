package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// POST /api/agents/{id}/impersonate
func (h *Handler) StartImpersonation(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	ownerID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	// Check agent exists and belongs to owner's workspace
	_, err := h.Queries.GetAgent(r.Context(), parseUUID(agentID))
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	// Check no active impersonation on this agent
	existing, _ := h.Queries.GetActiveImpersonation(r.Context(), parseUUID(agentID))
	if existing.ID.Valid {
		writeError(w, http.StatusConflict, "agent is already being impersonated")
		return
	}

	// End any existing impersonation by this owner
	h.Queries.EndImpersonation(r.Context(), parseUUID(agentID))

	session, err := h.Queries.StartImpersonation(r.Context(), db.StartImpersonationParams{
		OwnerID:     parseUUID(ownerID),
		AgentID:     parseUUID(agentID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		slog.Warn("start impersonation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start impersonation")
		return
	}

	h.publish("impersonation:started", workspaceID, "member", ownerID, map[string]any{
		"session_id": uuidToString(session.ID),
		"agent_id":   agentID,
		"owner_id":   ownerID,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         uuidToString(session.ID),
		"agent_id":   agentID,
		"owner_id":   ownerID,
		"started_at": timestampToString(session.StartedAt),
		"expires_at": timestampToString(session.ExpiresAt),
	})
}

// POST /api/agents/{id}/release
func (h *Handler) EndImpersonation(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	ownerID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	err := h.Queries.EndImpersonation(r.Context(), parseUUID(agentID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to end impersonation")
		return
	}

	h.publish("impersonation:ended", workspaceID, "member", ownerID, map[string]any{
		"agent_id": agentID,
	})

	writeJSON(w, http.StatusOK, map[string]any{"message": "impersonation ended"})
}

// GET /api/agents/{id}/impersonation
func (h *Handler) GetImpersonation(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")

	session, err := h.Queries.GetActiveImpersonation(r.Context(), parseUUID(agentID))
	if err != nil || !session.ID.Valid {
		writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"active":     true,
		"id":         uuidToString(session.ID),
		"owner_id":   uuidToString(session.OwnerID),
		"agent_id":   uuidToString(session.AgentID),
		"started_at": timestampToString(session.StartedAt),
		"expires_at": timestampToString(session.ExpiresAt),
	})
}
