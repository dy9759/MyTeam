package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/MyAIOSHub/MyTeam/server/pkg/protocol"
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

// POST /api/channels/from-dm
//
// Creates a new channel and seeds it with the selected DM messages as
// history. Adds the current user, the peer (when peer_type == "member"),
// and the peer (when peer_type == "agent") as channel members. Message
// authorship is preserved so the resulting channel reads as a continuation
// of the original chat.
func (h *Handler) CreateChannelFromDM(w http.ResponseWriter, r *http.Request) {
	type CreateRequest struct {
		Name       string   `json:"name"`
		PeerID     string   `json:"peer_id"`
		PeerType   string   `json:"peer_type"`
		MessageIDs []string `json:"message_ids,omitempty"`
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
	if req.PeerID == "" {
		writeError(w, http.StatusBadRequest, "peer_id required")
		return
	}
	if req.PeerType != "member" && req.PeerType != "agent" {
		writeError(w, http.StatusBadRequest, "peer_type must be 'member' or 'agent'")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	ctx := r.Context()

	ch, err := h.Queries.CreateChannel(ctx, db.CreateChannelParams{
		WorkspaceID:   parseUUID(workspaceID),
		Name:          req.Name,
		CreatedBy:     parseUUID(userID),
		CreatedByType: "member",
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "channel name already exists")
			return
		}
		slog.Warn("create channel from dm failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}

	// Auto-join creator.
	_ = h.Queries.AddChannelMember(ctx, db.AddChannelMemberParams{
		ChannelID:  ch.ID,
		MemberID:   parseUUID(userID),
		MemberType: "member",
	})

	// Add the DM peer. Agents count as members for channel-membership purposes.
	_ = h.Queries.AddChannelMember(ctx, db.AddChannelMemberParams{
		ChannelID:  ch.ID,
		MemberID:   parseUUID(req.PeerID),
		MemberType: req.PeerType,
	})

	// Copy selected DM messages into the new channel so users keep context.
	// When MessageIDs is empty we skip copying — the channel stays empty and
	// the user can start fresh.
	copied := 0
	if len(req.MessageIDs) > 0 {
		messages, err := h.Queries.ListDMMessages(ctx, db.ListDMMessagesParams{
			WorkspaceID: parseUUID(workspaceID),
			SelfID:      parseUUID(userID),
			SelfType:    "member",
			PeerID:      parseUUID(req.PeerID),
			PeerType:    strToText(req.PeerType),
			LimitCount:  500,
			OffsetCount: 0,
		})
		if err != nil {
			slog.Warn("list dm messages for channel seed failed", "error", err)
		} else {
			messages = filterMessagesByID(messages, req.MessageIDs)
			for _, m := range messages {
				_, err := h.Queries.CreateMessage(ctx, db.CreateMessageParams{
					WorkspaceID: parseUUID(workspaceID),
					SenderID:    m.SenderID,
					SenderType:  m.SenderType,
					ChannelID:   ch.ID,
					Content:     m.Content,
					ContentType: m.ContentType,
					Type:        m.Type,
				})
				if err != nil {
					slog.Warn("copy dm message to channel failed", "error", err, "message_id", uuidToString(m.ID))
					continue
				}
				copied++
			}
		}
	}

	h.publish("channel:created", workspaceID, "member", userID, map[string]any{
		"channel": channelToResponse(ch),
	})

	resp := channelToResponse(ch)
	resp["copied_messages"] = copied
	writeJSON(w, http.StatusCreated, resp)
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
	h.publish("channel:member_added", workspaceID, "member", userID, map[string]any{
		"channel_id": channelID,
		"member_id":  userID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// POST /api/channels/{channelID}/members
//
// Invites a workspace member or agent into the channel. Idempotent — a
// second invite for the same (member_id, member_type) pair is a no-op.
func (h *Handler) AddChannelMemberByID(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")

	type InviteRequest struct {
		MemberID   string `json:"member_id"`
		MemberType string `json:"member_type"`
	}
	var req InviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.MemberID == "" {
		writeError(w, http.StatusBadRequest, "member_id required")
		return
	}
	if req.MemberType != "member" && req.MemberType != "agent" {
		writeError(w, http.StatusBadRequest, "member_type must be 'member' or 'agent'")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	err := h.Queries.AddChannelMember(r.Context(), db.AddChannelMemberParams{
		ChannelID:  parseUUID(channelID),
		MemberID:   parseUUID(req.MemberID),
		MemberType: req.MemberType,
	})
	if err != nil {
		slog.Warn("invite channel member failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to invite member")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	h.publish("channel:member_added", workspaceID, "member", userID, map[string]any{
		"channel_id":  channelID,
		"member_id":   req.MemberID,
		"member_type": req.MemberType,
	})

	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/channels/{channelID}/members/{memberID}
//
// Removes a member or agent from the channel. `member_type` is required
// as a query param since members and agents share the UUID space.
func (h *Handler) RemoveChannelMemberByID(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	memberID := chi.URLParam(r, "memberID")
	memberType := r.URL.Query().Get("member_type")
	if memberType != "member" && memberType != "agent" {
		writeError(w, http.StatusBadRequest, "member_type query param must be 'member' or 'agent'")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	err := h.Queries.RemoveChannelMember(r.Context(), db.RemoveChannelMemberParams{
		ChannelID:  parseUUID(channelID),
		MemberID:   parseUUID(memberID),
		MemberType: memberType,
	})
	if err != nil {
		slog.Warn("remove channel member failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	h.publish("channel:member_removed", workspaceID, "member", userID, map[string]any{
		"channel_id":  channelID,
		"member_id":   memberID,
		"member_type": memberType,
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
	h.publish("channel:member_removed", workspaceID, "member", userID, map[string]any{
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
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

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

	workspaceID := resolveWorkspaceID(r)
	h.publish(protocol.EventChannelUpdated, workspaceID, "member", userID, map[string]any{
		"channel_id": channelID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/channels/{channelID}/category
func (h *Handler) UpdateChannelCategory(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

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

	workspaceID := resolveWorkspaceID(r)
	h.publish(protocol.EventChannelUpdated, workspaceID, "member", userID, map[string]any{
		"channel_id": channelID,
	})

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

// POST /api/channels/{channelID}/transfer-founder
func (h *Handler) TransferFounder(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	channelID := chi.URLParam(r, "channelID")

	var req struct {
		NewFounderID string `json:"new_founder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NewFounderID == "" {
		writeError(w, http.StatusBadRequest, "new_founder_id is required")
		return
	}

	var currentFounder pgtype.UUID
	err := h.DB.QueryRow(r.Context(),
		`SELECT COALESCE(founder_id, created_by) FROM channel WHERE id = $1`,
		parseUUID(channelID),
	).Scan(&currentFounder)
	if err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if uuidToString(currentFounder) != userID {
		writeError(w, http.StatusForbidden, "only the founder can transfer ownership")
		return
	}

	_, err = h.DB.Exec(r.Context(),
		`UPDATE channel SET founder_id = $1 WHERE id = $2`,
		parseUUID(req.NewFounderID), parseUUID(channelID),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to transfer founder")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "transferred"})
}

func channelToResponse(ch db.Channel) map[string]any {
	var archivedAt *string
	if ch.ArchivedAt.Valid {
		s := timestampToString(ch.ArchivedAt)
		archivedAt = &s
	}
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
		"archived_at":     archivedAt,
	}
}

// PATCH /api/channels/{channelID}/archive
// Marks the channel as archived (archived_at = NOW()). Idempotent — a
// second archive is a no-op. Archive is workspace-wide; use Unarchive to
// restore.
func (h *Handler) ArchiveChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	if err := h.Queries.ArchiveChannel(r.Context(), parseUUID(channelID)); err != nil {
		slog.Warn("archive channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to archive channel")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	h.publish("channel:archived", workspaceID, "member", userID, map[string]any{
		"channel_id": channelID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/channels/{channelID}/unarchive
// Clears archived_at (sets it to NULL), returning the channel to the
// active list.
func (h *Handler) UnarchiveChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	if err := h.Queries.UnarchiveChannel(r.Context(), parseUUID(channelID)); err != nil {
		slog.Warn("unarchive channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to unarchive channel")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	h.publish("channel:unarchived", workspaceID, "member", userID, map[string]any{
		"channel_id": channelID,
	})

	w.WriteHeader(http.StatusNoContent)
}
