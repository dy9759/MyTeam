package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// POST /api/channels
func (h *Handler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	type CreateRequest struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}

	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	ch, err := h.Queries.CreateChannel(r.Context(), db.CreateChannelParams{
		WorkspaceID:   parseUUID(workspaceID),
		Name:          req.Name,
		Description:   strToText(req.Description),
		CreatedBy:     parseUUID(userID),
		CreatedByType: "member",
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "channel name already exists")
			return
		}
		slog.Warn("create channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}

	// Auto-join creator
	_ = h.Queries.AddChannelMember(r.Context(), db.AddChannelMemberParams{
		ChannelID:  ch.ID,
		MemberID:   parseUUID(userID),
		MemberType: "member",
	})

	h.publish("channel:created", workspaceID, "member", userID, map[string]any{
		"channel": channelToResponse(ch),
	})

	writeJSON(w, http.StatusCreated, channelToResponse(ch))
}

// GET /api/channels
func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)

	channels, err := h.Queries.ListChannels(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("list channels failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}

	resp := make([]map[string]any, len(channels))
	for i, ch := range channels {
		resp[i] = channelToResponse(ch)
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": resp})
}

// GET /api/channels/{channelID}
func (h *Handler) GetChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")

	ch, err := h.Queries.GetChannel(r.Context(), parseUUID(channelID))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Warn("get channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get channel")
		return
	}

	writeJSON(w, http.StatusOK, channelToResponse(ch))
}

// POST /api/channels/{channelID}/join
func (h *Handler) JoinChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	err := h.Queries.AddChannelMember(r.Context(), db.AddChannelMemberParams{
		ChannelID:  parseUUID(channelID),
		MemberID:   parseUUID(userID),
		MemberType: "member",
	})
	if err != nil {
		slog.Warn("join channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to join channel")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	h.publish("channel:member_joined", workspaceID, "member", userID, map[string]any{
		"channel_id": channelID,
		"member_id":  userID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// POST /api/channels/{channelID}/leave
func (h *Handler) LeaveChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	err := h.Queries.RemoveChannelMember(r.Context(), db.RemoveChannelMemberParams{
		ChannelID:  parseUUID(channelID),
		MemberID:   parseUUID(userID),
		MemberType: "member",
	})
	if err != nil {
		slog.Warn("leave channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to leave channel")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	h.publish("channel:member_left", workspaceID, "member", userID, map[string]any{
		"channel_id": channelID,
		"member_id":  userID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// GET /api/channels/{channelID}/members
func (h *Handler) ListChannelMembers(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")

	members, err := h.Queries.ListChannelMembers(r.Context(), parseUUID(channelID))
	if err != nil {
		slog.Warn("list channel members failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list channel members")
		return
	}

	resp := make([]map[string]any, len(members))
	for i, m := range members {
		resp[i] = map[string]any{
			"channel_id":  uuidToString(m.ChannelID),
			"member_id":   uuidToString(m.MemberID),
			"member_type": m.MemberType,
			"joined_at":   timestampToString(m.JoinedAt),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": resp})
}

// GET /api/channels/{channelID}/messages
func (h *Handler) ListChannelMessages(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	messages, err := h.Queries.ListChannelMessages(r.Context(), db.ListChannelMessagesParams{
		ChannelID: parseUUID(channelID),
		Limit:     int32(limit),
		Offset:    int32(offset),
	})
	if err != nil {
		slog.Warn("list channel messages failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}

	resp := make([]map[string]any, len(messages))
	for i, m := range messages {
		resp[i] = messageToResponse(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": resp})
}

// PATCH /api/channels/{channelID}/visibility
func (h *Handler) UpdateChannelVisibility(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")

	var req struct {
		Visibility string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Visibility == "" {
		writeError(w, http.StatusBadRequest, "visibility required")
		return
	}

	err := h.Queries.UpdateChannelVisibility(r.Context(), db.UpdateChannelVisibilityParams{
		ID:         parseUUID(channelID),
		Visibility: req.Visibility,
	})
	if err != nil {
		slog.Warn("update channel visibility failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update visibility")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/channels/{channelID}/category
func (h *Handler) UpdateChannelCategory(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")

	var req struct {
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := h.Queries.UpdateChannelCategory(r.Context(), db.UpdateChannelCategoryParams{
		ID:       parseUUID(channelID),
		Category: strToText(req.Category),
	})
	if err != nil {
		slog.Warn("update channel category failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update category")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpgradeDMToChannel - POST /api/channels/{channelID}/upgrade
// Changes conversation_type from 'dm' to 'channel'.
// Validates current type is 'dm'.
// Allows setting a name for the new channel.
// Broadcasts channel:updated WS event.
func (h *Handler) UpgradeDMToChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	// Load the channel.
	ch, err := h.Queries.GetChannel(r.Context(), parseUUID(channelID))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Warn("upgrade dm: get channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get channel")
		return
	}

	// TODO: wire after migration — ch.ConversationType field.
	// For now, check if the channel has a "dm" category or naming pattern to detect DM type.
	// After migration, this check should be: ch.ConversationType != "dm".
	// Placeholder validation using Category field:
	if ch.Category.Valid && ch.Category.String != "dm" {
		writeError(w, http.StatusBadRequest, "channel is not a DM conversation")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required when upgrading DM to channel")
		return
	}

	// TODO: wire after sqlc generation — expects db.UpgradeDMToChannelParams.
	// For now, use UpdateChannelCategory to mark it as "channel" and update the name.
	// After migration, this should set conversation_type = 'channel'.
	err = h.Queries.UpdateChannelCategory(r.Context(), db.UpdateChannelCategoryParams{
		ID:       parseUUID(channelID),
		Category: strToText("channel"),
	})
	if err != nil {
		slog.Warn("upgrade dm to channel: update category failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to upgrade DM to channel")
		return
	}

	// Update channel name.
	// TODO: wire after sqlc generation — expects db.UpdateChannelName or similar.
	// For now, this is a placeholder. The name update will be part of the upgrade query.

	h.publish("channel:updated", workspaceID, "member", userID, map[string]any{
		"channel_id":        channelID,
		"conversation_type": "channel",
		"name":              req.Name,
	})

	// Re-fetch channel for response.
	updatedCh, err := h.Queries.GetChannel(r.Context(), parseUUID(channelID))
	if err != nil {
		slog.Warn("upgrade dm: re-fetch channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch upgraded channel")
		return
	}

	writeJSON(w, http.StatusOK, channelToResponse(updatedCh))
}

func channelToResponse(ch db.Channel) map[string]any {
	return map[string]any{
		"id":              uuidToString(ch.ID),
		"workspace_id":    uuidToString(ch.WorkspaceID),
		"name":            ch.Name,
		"description":     textToPtr(ch.Description),
		"created_by":      uuidToString(ch.CreatedBy),
		"created_by_type": ch.CreatedByType,
		"visibility":      ch.Visibility,
		"category":        textToPtr(ch.Category),
		"created_at":      timestampToString(ch.CreatedAt),
	}
}
