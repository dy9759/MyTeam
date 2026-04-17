package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GetIdentityCard - GET /api/agents/{id}/identity-card
// Returns the agent's identity_card JSONB field.
// Only the agent's owner or workspace admin can access.
func (h *Handler) GetIdentityCard(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return
	}

	// Only agent owner or workspace admin can view identity card.
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var card map[string]any
	if agent.IdentityCard != nil {
		if err := json.Unmarshal(agent.IdentityCard, &card); err != nil {
			slog.Warn("failed to parse agent identity card", "error", err, "agent_id", agentID)
			card = map[string]any{}
		}
	}
	if card == nil {
		card = map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":      agentID,
		"identity_card": card,
	})
}

// UpdateIdentityCard - PATCH /api/agents/{id}/identity-card
// Allows owner to manually edit the identity card.
// Logs activity: "identity_card_updated".
func (h *Handler) UpdateIdentityCard(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cardJSON, err := json.Marshal(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid identity card data")
		return
	}

	err = h.Queries.UpdateAgentIdentityCard(r.Context(), db.UpdateAgentIdentityCardParams{
		ID:           parseUUID(agentID),
		IdentityCard: cardJSON,
	})
	if err != nil {
		slog.Warn("update identity card failed", "error", err, "agent_id", agentID)
		writeError(w, http.StatusInternalServerError, "failed to update identity card")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	userID := requestUserID(r)

	// Log activity
	h.publish("activity:identity_card_updated", workspaceID, "member", userID, map[string]any{
		"agent_id":      agentID,
		"identity_card": body,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":      agentID,
		"identity_card": body,
	})
}

// GenerateIdentityCard - POST /api/agents/{id}/identity-card/generate
// Calls IdentityGeneratorService to auto-generate identity card from agent's task history.
// Returns the generated card (doesn't auto-save - owner reviews first).
func (h *Handler) GenerateIdentityCard(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	workspaceID := resolveWorkspaceID(r)

	if h.IdentityGenerator == nil {
		writeError(w, http.StatusServiceUnavailable, "identity generator not available")
		return
	}

	card, err := h.IdentityGenerator.GenerateCard(r.Context(), agentID, workspaceID)
	if err != nil {
		slog.Warn("generate identity card failed", "error", err, "agent_id", agentID)
		writeError(w, http.StatusInternalServerError, "failed to generate identity card")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":      agentID,
		"identity_card": card,
		"generated":     true,
	})
}

// validAgentStatuses defines the canonical 7-value status enum for the unified
// agent.status field (post Account Phase 2).
var validAgentStatuses = map[string]struct{}{
	"offline":   {},
	"online":    {},
	"idle":      {},
	"busy":      {},
	"blocked":   {},
	"degraded":  {},
	"suspended": {},
}

// UpdateAgentStatus - PATCH /api/agents/{id}/status
// Updates the unified agent.status field with one of the 7 canonical values.
// Broadcasts agent:status_changed WS event.
func (h *Handler) UpdateAgentStatus(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Status == "" {
		writeError(w, http.StatusBadRequest, "status is required")
		return
	}
	if _, ok := validAgentStatuses[req.Status]; !ok {
		writeError(w, http.StatusBadRequest, "invalid status: "+req.Status)
		return
	}

	updatedAgent, err := h.Queries.UpdateAgentStatus(r.Context(), db.UpdateAgentStatusParams{
		ID:     parseUUID(agentID),
		Status: req.Status,
	})
	if err != nil {
		slog.Warn("update agent status failed", "error", err, "agent_id", agentID)
		writeError(w, http.StatusInternalServerError, "failed to update agent status")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	userID := requestUserID(r)

	resp := agentToResponse(updatedAgent)
	h.publish("agent:status_changed", workspaceID, "member", userID, map[string]any{
		"agent": resp,
	})

	writeJSON(w, http.StatusOK, resp)
}
