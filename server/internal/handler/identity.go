package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

	// TODO: wire after sqlc generation — agent.IdentityCard field will be added by migration.
	// For now, read from the agent_metadata JSONB as a fallback.
	var card map[string]any
	if agent.AgentMetadata != nil {
		if err := json.Unmarshal(agent.AgentMetadata, &card); err != nil {
			slog.Warn("failed to parse agent metadata as identity card", "error", err, "agent_id", agentID)
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

	// Store identity card in agent_metadata column.
	// After migration adds dedicated identity_card column, switch to that.
	err = h.Queries.UpdateAgentProfile(r.Context(), db.UpdateAgentProfileParams{
		ID:            parseUUID(agentID),
		AgentMetadata: cardJSON,
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

// validOnlineStatusTransitions defines allowed online_status transitions per state machine.
var validOnlineStatusTransitions = map[string][]string{
	"offline":   {"online"},
	"online":    {"idle", "offline"},
	"idle":      {"busy", "degraded", "suspended", "offline"},
	"busy":      {"idle", "blocked", "degraded", "suspended", "offline"},
	"blocked":   {"idle", "offline"},
	"degraded":  {"idle", "offline"},
	"suspended": {"idle", "offline"},
}

// validWorkloadStatusTransitions defines allowed workload_status transitions per state machine.
var validWorkloadStatusTransitions = map[string][]string{
	"idle":      {"busy", "blocked", "degraded", "suspended"},
	"busy":      {"idle", "blocked", "degraded", "suspended"},
	"blocked":   {"idle", "busy", "suspended"},
	"degraded":  {"idle", "busy", "suspended"},
	"suspended": {"idle"},
}

func isValidTransition(transitions map[string][]string, from, to string) bool {
	allowed, ok := transitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// UpdateAgentStatus - PATCH /api/agents/{id}/status
// Update online_status and/or workload_status.
// Validates state transitions per state machine rules.
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
		OnlineStatus   *string `json:"online_status,omitempty"`
		WorkloadStatus *string `json:"workload_status,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.OnlineStatus == nil && req.WorkloadStatus == nil {
		writeError(w, http.StatusBadRequest, "at least one of online_status or workload_status is required")
		return
	}

	// TODO: wire after migration — agent.OnlineStatus / agent.WorkloadStatus fields.
	// For now, use agent.Status as the current state for online_status validation,
	// and default "idle" for workload_status since the field doesn't exist yet.

	// Validate online_status transition
	if req.OnlineStatus != nil {
		currentOnline := agent.Status // Closest existing field; will be agent.OnlineStatus after migration
		if !isValidTransition(validOnlineStatusTransitions, currentOnline, *req.OnlineStatus) {
			writeError(w, http.StatusBadRequest, "invalid online_status transition from '"+currentOnline+"' to '"+*req.OnlineStatus+"'")
			return
		}
	}

	// Validate workload_status transition
	if req.WorkloadStatus != nil {
		currentWorkload := "idle" // TODO: read from agent.WorkloadStatus after migration
		if !isValidTransition(validWorkloadStatusTransitions, currentWorkload, *req.WorkloadStatus) {
			writeError(w, http.StatusBadRequest, "invalid workload_status transition from '"+currentWorkload+"' to '"+*req.WorkloadStatus+"'")
			return
		}
	}

	// Build update params. For now, map online_status to the existing Status field.
	params := db.UpdateAgentParams{
		ID: parseUUID(agentID),
	}
	if req.OnlineStatus != nil {
		params.Status = pgtype.Text{String: *req.OnlineStatus, Valid: true}
	}
	// TODO: wire workload_status after migration adds the column.

	updatedAgent, err := h.Queries.UpdateAgent(r.Context(), params)
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
