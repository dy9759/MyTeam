package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// POST /api/channels/{channelID}/merge-request
// Body: { "source_channel_id": "..." }
// Creates a merge request. Caller must be founder of source or target.
func (h *Handler) CreateMergeRequest(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	targetChannelID := chi.URLParam(r, "channelID")

	var req struct {
		SourceChannelID string `json:"source_channel_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SourceChannelID == "" {
		writeError(w, http.StatusBadRequest, "source_channel_id is required")
		return
	}

	// Look up founders of both channels.
	var sourceFounder, targetFounder pgtype.UUID
	err := h.DB.QueryRow(r.Context(),
		`SELECT COALESCE(founder_id, created_by) FROM channel WHERE id = $1 AND workspace_id = $2`,
		parseUUID(req.SourceChannelID), parseUUID(workspaceID),
	).Scan(&sourceFounder)
	if err != nil {
		writeError(w, http.StatusNotFound, "source channel not found")
		return
	}

	err = h.DB.QueryRow(r.Context(),
		`SELECT COALESCE(founder_id, created_by) FROM channel WHERE id = $1 AND workspace_id = $2`,
		parseUUID(targetChannelID), parseUUID(workspaceID),
	).Scan(&targetFounder)
	if err != nil {
		writeError(w, http.StatusNotFound, "target channel not found")
		return
	}

	// Caller must be founder of at least one.
	callerUUID := parseUUID(userID)
	if uuidToString(sourceFounder) != userID && uuidToString(targetFounder) != userID {
		writeError(w, http.StatusForbidden, "only a channel founder can initiate merge")
		return
	}

	// Build required founders list (deduplicated).
	founders := []string{uuidToString(sourceFounder)}
	tf := uuidToString(targetFounder)
	if tf != founders[0] {
		founders = append(founders, tf)
	}
	foundersJSON, _ := json.Marshal(founders)

	// Auto-approve for the initiator.
	approvals := []map[string]any{
		{"founder_id": userID, "approved_at": time.Now().UTC().Format(time.RFC3339)},
	}
	approvalsJSON, _ := json.Marshal(approvals)

	// If initiator is the only founder, execute immediately.
	if len(founders) == 1 {
		mergeID := h.executeMerge(r, workspaceID, req.SourceChannelID, targetChannelID, userID)
		if mergeID == "" {
			writeError(w, http.StatusInternalServerError, "merge execution failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "completed", "merge_id": mergeID})
		return
	}

	// Create pending merge request.
	var mergeID pgtype.UUID
	err = h.DB.QueryRow(r.Context(), `
		INSERT INTO merge_request (source_channel_id, target_channel_id, workspace_id, initiated_by, status, approvals, required_founders)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6)
		RETURNING id
	`, parseUUID(req.SourceChannelID), parseUUID(targetChannelID),
		parseUUID(workspaceID), callerUUID, approvalsJSON, foundersJSON,
	).Scan(&mergeID)
	if err != nil {
		slog.Warn("create merge request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create merge request")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"merge_id":          uuidToString(mergeID),
		"status":            "pending",
		"required_founders": founders,
		"pending_approval":  founders[1:], // those who haven't approved yet
	})
}

// POST /api/merge-requests/{mergeID}/approve
func (h *Handler) ApproveMergeRequest(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	mergeID := chi.URLParam(r, "mergeID")

	// Load merge request.
	var sourceID, targetID, wsID pgtype.UUID
	var status string
	var approvalsRaw, foundersRaw []byte
	err := h.DB.QueryRow(r.Context(), `
		SELECT source_channel_id, target_channel_id, workspace_id, status, approvals, required_founders
		FROM merge_request WHERE id = $1
	`, parseUUID(mergeID)).Scan(&sourceID, &targetID, &wsID, &status, &approvalsRaw, &foundersRaw)
	if err != nil {
		writeError(w, http.StatusNotFound, "merge request not found")
		return
	}
	if status != "pending" {
		writeError(w, http.StatusBadRequest, "merge request is not pending")
		return
	}

	// Check caller is a required founder.
	var founders []string
	if err := json.Unmarshal(foundersRaw, &founders); err != nil {
		slog.Warn("decode merge request founders failed", "merge_id", mergeID, "error", err)
		writeError(w, http.StatusInternalServerError, "invalid merge request data")
		return
	}
	isFounder := false
	for _, f := range founders {
		if f == userID {
			isFounder = true
			break
		}
	}
	if !isFounder {
		writeError(w, http.StatusForbidden, "only required founders can approve")
		return
	}

	// Add approval.
	var approvals []map[string]any
	if err := json.Unmarshal(approvalsRaw, &approvals); err != nil {
		slog.Warn("decode merge request approvals failed", "merge_id", mergeID, "error", err)
		writeError(w, http.StatusInternalServerError, "invalid merge request data")
		return
	}
	for _, a := range approvals {
		if a["founder_id"] == userID {
			writeError(w, http.StatusBadRequest, "already approved")
			return
		}
	}
	approvals = append(approvals, map[string]any{
		"founder_id":  userID,
		"approved_at": time.Now().UTC().Format(time.RFC3339),
	})
	newApprovalsJSON, _ := json.Marshal(approvals)

	// Check if all founders have approved.
	if len(approvals) >= len(founders) {
		// Execute merge.
		workspaceID := uuidToString(wsID)
		mid := h.executeMerge(r, workspaceID, uuidToString(sourceID), uuidToString(targetID), userID)
		if mid == "" {
			writeError(w, http.StatusInternalServerError, "merge execution failed")
			return
		}
		_, _ = h.DB.Exec(r.Context(), `
			UPDATE merge_request SET status = 'completed', approvals = $1, completed_at = NOW() WHERE id = $2
		`, newApprovalsJSON, parseUUID(mergeID))
		writeJSON(w, http.StatusOK, map[string]any{"status": "completed"})
		return
	}

	// Update approvals, still pending.
	_, _ = h.DB.Exec(r.Context(), `
		UPDATE merge_request SET approvals = $1 WHERE id = $2
	`, newApprovalsJSON, parseUUID(mergeID))

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "pending",
		"approved": len(approvals),
		"required": len(founders),
	})
}

// executeMerge copies messages from source to target interleaved by created_at,
// moves members, and archives the source channel.
func (h *Handler) executeMerge(r *http.Request, workspaceID, sourceChannelID, targetChannelID, initiatorID string) string {
	ctx := r.Context()
	srcUUID := parseUUID(sourceChannelID)
	tgtUUID := parseUUID(targetChannelID)
	wsUUID := parseUUID(workspaceID)

	// 1. Copy all messages from source to target (keep original timestamps).
	_, err := h.DB.Exec(ctx, `
		INSERT INTO message (workspace_id, sender_id, sender_type, channel_id, content, content_type, type, created_at, metadata)
		SELECT workspace_id, sender_id, sender_type, $1, content, content_type, type, created_at, metadata
		FROM message
		WHERE channel_id = $2
		ORDER BY created_at
	`, tgtUUID, srcUUID)
	if err != nil {
		slog.Warn("merge: copy messages failed", "error", err)
		return ""
	}

	// 2. Move members from source to target (skip duplicates).
	_, _ = h.DB.Exec(ctx, `
		INSERT INTO channel_member (channel_id, member_id, member_type, joined_at)
		SELECT $1, member_id, member_type, joined_at
		FROM channel_member
		WHERE channel_id = $2
		ON CONFLICT DO NOTHING
	`, tgtUUID, srcUUID)

	// 3. Get source channel name for the notification.
	var sourceName string
	_ = h.DB.QueryRow(ctx, `SELECT name FROM channel WHERE id = $1`, srcUUID).Scan(&sourceName)

	// 4. Post system notification in target.
	_, _ = h.DB.Exec(ctx, `
		INSERT INTO message (workspace_id, sender_id, sender_type, channel_id, content, content_type, type)
		VALUES ($1, $2, 'member', $3, $4, 'text', 'system_notification')
	`, wsUUID, parseUUID(initiatorID), tgtUUID,
		fmt.Sprintf("Channel #%s has been merged into this channel.", sourceName))

	// 5. Archive source channel (soft-delete by renaming + setting visibility).
	_, _ = h.DB.Exec(ctx, `
		UPDATE channel SET visibility = 'archived', name = name || ' [merged]' WHERE id = $1
	`, srcUUID)

	return sourceChannelID
}

// POST /api/channels/{channelID}/split
// Body: { "member_ids": [...], "name": "new-channel" }
// Creates a new channel with selected members. Original channel is unchanged.
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
