package handler

import (
	"encoding/json"
	"net/http"

	"github.com/MyAIOSHub/MyTeam/server/internal/service"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// GetPersonalAgent — GET /api/personal-agent
// Returns the personal agent for the current user, creating one if it doesn't exist.
func (h *Handler) GetPersonalAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Look up the user's name.
	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	agent, err := service.EnsurePersonalAgent(
		r.Context(),
		h.Queries,
		parseUUID(workspaceID),
		parseUUID(userID),
		user.Name,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get personal agent: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, agentToResponse(agent))
}

// UpdatePersonalAgentConfig — PATCH /api/personal-agent/config
// Updates the cloud LLM config for the current user's personal agent. The
// config now lives on the agent's runtime metadata (not the agent row itself).
func (h *Handler) UpdatePersonalAgentConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Get existing personal agent.
	agent, err := h.Queries.GetPersonalAgent(r.Context(), db.GetPersonalAgentParams{
		WorkspaceID: parseUUID(workspaceID),
		OwnerID:     parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "personal agent not found")
		return
	}

	if !agent.RuntimeID.Valid {
		writeError(w, http.StatusConflict, "personal agent has no runtime configured")
		return
	}

	var req struct {
		CloudLLMConfig *service.CloudLLMConfig `json:"cloud_llm_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CloudLLMConfig == nil {
		writeError(w, http.StatusBadRequest, "cloud_llm_config is required")
		return
	}

	configJSON, _ := json.Marshal(req.CloudLLMConfig)

	if err := h.Queries.SetRuntimeMetadataKey(r.Context(), db.SetRuntimeMetadataKeyParams{
		ID:    agent.RuntimeID,
		Key:   "cloud_llm_config",
		Value: configJSON,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update config")
		return
	}

	writeJSON(w, http.StatusOK, agentToResponse(agent))
}
