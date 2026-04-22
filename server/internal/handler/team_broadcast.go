package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// POST /api/skills/{id}/broadcast — broadcast message to all agents in workspace
func (h *Handler) SkillBroadcast(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "id")
	type Req struct {
		Text string `json:"text"`
	}
	var req Req
	json.NewDecoder(r.Body).Decode(&req)

	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	agents, _ := h.Queries.ListAgents(r.Context(), parseUUID(workspaceID))

	sent := 0
	for _, agent := range agents {
		_, err := h.Queries.CreateMessage(r.Context(), db.CreateMessageParams{
			WorkspaceID:   parseUUID(workspaceID),
			SenderID:      parseUUID(userID),
			SenderType:    "member",
			RecipientID:   agent.ID,
			RecipientType: strToText("agent"),
			Content:       req.Text,
			ContentType:   "text",
			Type:          "text",
		})
		if err == nil {
			sent++
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"delivered": sent,
		"total":     len(agents),
		"skill_id":  skillID,
	})
}
