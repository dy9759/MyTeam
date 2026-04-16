package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// POST /api/channels/{channelID}/split
// Body: { "member_ids": [...], "name": "new-channel" }
func (h *Handler) SplitChannel(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	sourceChannelID := chi.URLParam(r, "channelID")

	var req struct {
		MemberIDs []string `json:"member_ids"`
		Name      string   `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || len(req.MemberIDs) == 0 {
		writeError(w, http.StatusBadRequest, "name and member_ids are required")
		return
	}

	// Verify source channel exists.
	var sourceName string
	err := h.DB.QueryRow(r.Context(),
		`SELECT name FROM channel WHERE id = $1 AND workspace_id = $2`,
		parseUUID(sourceChannelID), parseUUID(workspaceID),
	).Scan(&sourceName)
	if err != nil {
		writeError(w, http.StatusNotFound, "source channel not found")
		return
	}

	// Create new channel.
	newCh, err := h.Queries.CreateChannel(r.Context(), db.CreateChannelParams{
		WorkspaceID:   parseUUID(workspaceID),
		Name:          req.Name,
		Description:   strToText(fmt.Sprintf("Split from #%s", sourceName)),
		CreatedBy:     parseUUID(userID),
		CreatedByType: "member",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}

	// Add selected members.
	for _, memberID := range req.MemberIDs {
		_, _ = h.DB.Exec(r.Context(), `
			INSERT INTO channel_member (channel_id, member_id, member_type, joined_at)
			VALUES ($1, $2, 'member', NOW()) ON CONFLICT DO NOTHING
		`, newCh.ID, parseUUID(memberID))
	}

	// System notification on source channel.
	_, _ = h.DB.Exec(r.Context(), `
		INSERT INTO message (workspace_id, sender_id, sender_type, channel_id, content, content_type, type)
		VALUES ($1, $2, 'member', $3, $4, 'text', 'system_notification')
	`, parseUUID(workspaceID), parseUUID(userID), parseUUID(sourceChannelID),
		fmt.Sprintf("Members split into new channel #%s", req.Name))

	// System notification on new channel.
	_, _ = h.DB.Exec(r.Context(), `
		INSERT INTO message (workspace_id, sender_id, sender_type, channel_id, content, content_type, type)
		VALUES ($1, $2, 'member', $3, $4, 'text', 'system_notification')
	`, parseUUID(workspaceID), parseUUID(userID), newCh.ID,
		fmt.Sprintf("Split from #%s", sourceName))

	writeJSON(w, http.StatusCreated, map[string]any{
		"channel_id":   uuidToString(newCh.ID),
		"channel_name": newCh.Name,
		"members":      len(req.MemberIDs),
	})
}
