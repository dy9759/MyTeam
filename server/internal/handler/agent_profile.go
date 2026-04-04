package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------- response types ----------

type AgentProfileResponse struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	DisplayName   *string         `json:"display_name,omitempty"`
	Avatar        *string         `json:"avatar,omitempty"`
	Bio           *string         `json:"bio,omitempty"`
	Tags          []string        `json:"tags"`
	Capabilities  []string        `json:"capabilities"`
	AgentMetadata json.RawMessage `json:"agent_metadata,omitempty"`
	Status        string          `json:"status"`
	Description   string          `json:"description"`
}

type AutoReplyResponse struct {
	AgentID          string          `json:"agent_id"`
	AutoReplyEnabled bool            `json:"auto_reply_enabled"`
	AutoReplyConfig  json.RawMessage `json:"auto_reply_config,omitempty"`
}

// ---------- handlers ----------

// GET /api/agents/{id}/profile
func (h *Handler) GetAgentProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentID := chi.URLParam(r, "id")

	profile, err := h.Queries.GetAgentProfile(ctx, parseUUID(agentID))
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	writeJSON(w, http.StatusOK, AgentProfileResponse{
		ID:            uuidToString(profile.ID),
		Name:          profile.Name,
		DisplayName:   textToPtr(profile.DisplayName),
		Avatar:        textToPtr(profile.Avatar),
		Bio:           textToPtr(profile.Bio),
		Tags:          profile.Tags,
		Capabilities:  profile.Capabilities,
		AgentMetadata: profile.AgentMetadata,
		Status:        profile.Status,
		Description:   profile.Description,
	})
}

// PATCH /api/agents/{id}/profile
func (h *Handler) UpdateAgentProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentID := chi.URLParam(r, "id")

	type updateReq struct {
		DisplayName *string         `json:"display_name,omitempty"`
		Avatar      *string         `json:"avatar,omitempty"`
		Bio         *string         `json:"bio,omitempty"`
		Tags        []string        `json:"tags,omitempty"`
		Metadata    json.RawMessage `json:"agent_metadata,omitempty"`
	}

	var req updateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := h.Queries.UpdateAgentProfile(ctx, db.UpdateAgentProfileParams{
		ID:            parseUUID(agentID),
		DisplayName:   ptrToText(req.DisplayName),
		Avatar:        ptrToText(req.Avatar),
		Bio:           ptrToText(req.Bio),
		Tags:          req.Tags,
		AgentMetadata: req.Metadata,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "profile updated"})
}

// GET /api/agents/{id}/auto-reply
func (h *Handler) GetAgentAutoReply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentID := chi.URLParam(r, "id")

	agent, err := h.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	writeJSON(w, http.StatusOK, AutoReplyResponse{
		AgentID:          uuidToString(agent.ID),
		AutoReplyEnabled: agent.AutoReplyEnabled.Bool,
		AutoReplyConfig:  agent.AutoReplyConfig,
	})
}

// PATCH /api/agents/{id}/auto-reply
func (h *Handler) UpdateAgentAutoReply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentID := chi.URLParam(r, "id")

	type autoReplyReq struct {
		Enabled bool            `json:"enabled"`
		Config  json.RawMessage `json:"config,omitempty"`
	}

	var req autoReplyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := h.Queries.UpdateAgentAutoReply(ctx, db.UpdateAgentAutoReplyParams{
		ID:               parseUUID(agentID),
		AutoReplyEnabled: pgtype.Bool{Bool: req.Enabled, Valid: true},
		AutoReplyConfig:  req.Config,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update auto-reply")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":           agentID,
		"auto_reply_enabled": req.Enabled,
	})
}
