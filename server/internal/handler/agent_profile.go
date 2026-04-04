package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type AgentProfileHandler struct{}

func NewAgentProfileHandler() *AgentProfileHandler {
	return &AgentProfileHandler{}
}

// GET /api/agents/{id}/profile
func (h *AgentProfileHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// TODO: query agent profile from DB
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"agent_id": id,
		"note":     "Needs sqlc wiring",
	})
}

// PATCH /api/agents/{id}/profile
func (h *AgentProfileHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	type UpdateRequest struct {
		DisplayName *string  `json:"display_name,omitempty"`
		Avatar      *string  `json:"avatar,omitempty"`
		Bio         *string  `json:"bio,omitempty"`
		Tags        []string `json:"tags,omitempty"`
	}

	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	// TODO: update agent profile via sqlc
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"agent_id": id,
		"message":  "Profile updated",
	})
}

// PATCH /api/agents/{id}/auto-reply
func (h *AgentProfileHandler) UpdateAutoReply(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	type AutoReplyRequest struct {
		Enabled bool            `json:"enabled"`
		Config  json.RawMessage `json:"config,omitempty"`
	}

	var req AutoReplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	// TODO: update via sqlc
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"agent_id":           id,
		"auto_reply_enabled": req.Enabled,
	})
}

// GET /api/agents/{id}/auto-reply
func (h *AgentProfileHandler) GetAutoReply(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"agent_id":           id,
		"auto_reply_enabled": false,
		"note":               "Needs sqlc wiring",
	})
}
