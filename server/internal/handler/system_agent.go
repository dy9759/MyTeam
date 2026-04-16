package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GetOrCreateSystemAgent — GET /api/system-agent
// Returns the workspace system agent, creating one if it doesn't exist.
// Also ensures the page system agents are present for the workspace
// and a personal agent exists for the current user.
func (h *Handler) GetOrCreateSystemAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	wsUUID := parseUUID(workspaceID)
	ownerUUID := parseUUID(userID)

	// Ensure personal agent exists for the user (fire-and-forget).
	go func() {
		user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
		if err != nil {
			return
		}
		if _, err := service.EnsurePersonalAgent(r.Context(), h.Queries, wsUUID, ownerUUID, user.Name); err != nil {
			slog.Debug("ensure personal agent failed", "error", err)
		}
	}()

	// Try to get existing
	agent, err := h.Queries.GetSystemAgent(r.Context(), wsUUID)
	if err == nil {
		service.EnsurePageAgents(r.Context(), h.Queries, wsUUID, ownerUUID)
		writeJSON(w, http.StatusOK, agentToResponse(agent))
		return
	}

	// Ensure cloud runtime exists for this workspace (needed as FK on agent row).
	cloudRuntime, rterr := h.Queries.EnsureCloudRuntime(r.Context(), wsUUID)
	if rterr != nil {
		slog.Warn("ensure cloud runtime failed", "error", rterr)
		writeError(w, http.StatusInternalServerError, "failed to ensure cloud runtime")
		return
	}

	// Create system agent
	agent, err = h.Queries.CreateSystemAgent(r.Context(), db.CreateSystemAgentParams{
		WorkspaceID: wsUUID,
		OwnerID:     ownerUUID,
		RuntimeID:   cloudRuntime.ID,
	})
	if err != nil {
		slog.Warn("create system agent failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create system agent")
		return
	}

	service.EnsurePageAgents(r.Context(), h.Queries, wsUUID, ownerUUID)

	h.publish("agent:created", workspaceID, "system", userID, map[string]any{
		"agent":     agentToResponse(agent),
		"is_system": true,
	})

	writeJSON(w, http.StatusCreated, agentToResponse(agent))
}

// ListPageAgents — GET /api/page-agents
// Returns the page system agents for the current workspace.
func (h *Handler) ListPageAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	agents, err := h.Queries.ListPageSystemAgents(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("list page agents failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list page agents")
		return
	}

	resp := make([]AgentResponse, len(agents))
	for i, a := range agents {
		resp[i] = agentToResponse(a)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetPageAgent — GET /api/page-agents/{scope}
// Returns a single page system agent for the given scope.
func (h *Handler) GetPageAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	scope := chi.URLParam(r, "scope")
	if !isValidPageScope(scope) {
		writeError(w, http.StatusBadRequest, "invalid page scope")
		return
	}

	agent, err := h.Queries.GetPageSystemAgent(r.Context(), db.GetPageSystemAgentParams{
		WorkspaceID: parseUUID(workspaceID),
		PageScope:   pgtype.Text{String: scope, Valid: true},
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "page agent not found")
			return
		}
		slog.Warn("get page agent failed", "error", err, "scope", scope)
		writeError(w, http.StatusInternalServerError, "failed to load page agent")
		return
	}

	writeJSON(w, http.StatusOK, agentToResponse(agent))
}

func isValidPageScope(s string) bool {
	switch s {
	case "account", "session", "project", "file":
		return true
	}
	return false
}
