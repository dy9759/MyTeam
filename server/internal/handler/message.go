package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var messageDedup = util.NewBoundedUUIDSet(2000)

// POST /api/messages
func (h *Handler) CreateMessage(w http.ResponseWriter, r *http.Request) {
	type CreateRequest struct {
		ChannelID       *string         `json:"channel_id,omitempty"`
		RecipientID     *string         `json:"recipient_id,omitempty"`
		RecipientType   *string         `json:"recipient_type,omitempty"`
		Content         string          `json:"content"`
		ContentType     string          `json:"content_type,omitempty"`
		FileID          *string         `json:"file_id,omitempty"`
		FileName        *string         `json:"file_name,omitempty"`
		FileSize        *int64          `json:"file_size,omitempty"`
		Metadata        json.RawMessage `json:"metadata,omitempty"`
		ParentID        *string         `json:"parent_id,omitempty"`
		ParentMessageID *string         `json:"parent_message_id,omitempty"`
		Type            string          `json:"type,omitempty"`
	}

	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	contentType := req.ContentType
	if contentType == "" {
		contentType = "text"
	}

	msgType := req.Type
	if msgType == "" {
		msgType = "text"
	}

	// Impersonation-aware sender resolution.
	// Check for active impersonation session via X-Agent-ID header or DB lookup.
	senderID := userID
	senderType := "member"
	isImpersonated := false

	if agentIDHeader := r.Header.Get("X-Agent-ID"); agentIDHeader != "" {
		// Check if there is an active impersonation session for this agent.
		session, err := h.Queries.GetActiveImpersonation(r.Context(), parseUUID(agentIDHeader))
		if err == nil && session.ID.Valid && uuidToString(session.OwnerID) == userID {
			senderID = agentIDHeader
			senderType = "agent"
			isImpersonated = true
			slog.Info("impersonated message send",
				"owner_id", userID,
				"agent_id", agentIDHeader,
				"channel_id", req.ChannelID,
			)
		}
	}

	// Thread support: if parent_message_id is provided, find or create a thread.
	var threadID pgtype.UUID
	if req.ParentMessageID != nil && *req.ParentMessageID != "" {
		threadID = h.resolveOrCreateThread(r.Context(), *req.ParentMessageID)
	}

	channelUUID := ptrToUUID(req.ChannelID)

	// Use parent_message_id as ParentID if the legacy ParentID is not set.
	parentID := ptrToUUID(req.ParentID)
	if !parentID.Valid && req.ParentMessageID != nil {
		parentID = parseUUID(*req.ParentMessageID)
	}

	msg, err := h.Queries.CreateMessage(r.Context(), db.CreateMessageParams{
		WorkspaceID:     parseUUID(workspaceID),
		SenderID:        parseUUID(senderID),
		SenderType:      senderType,
		ChannelID:       channelUUID,
		RecipientID:     ptrToUUID(req.RecipientID),
		RecipientType:   ptrToText(req.RecipientType),
		Content:         req.Content,
		ContentType:     contentType,
		FileID:          ptrToUUID(req.FileID),
		FileName:        ptrToText(req.FileName),
		FileSize:        ptrToInt8(req.FileSize),
		FileContentType: pgtype.Text{},
		Metadata:        req.Metadata,
		ParentID:        parentID,
		Type:            msgType,
		ThreadID:        threadID,
	})
	if err != nil {
		slog.Warn("create message failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create message")
		return
	}

	// If the message lives in a thread (via parent_message_id), bump
	// counters using the PRD §4 semantics (member/agent -> reply_count +
	// last_reply_at; system -> last_activity_at only).
	h.incrementThreadCounters(r.Context(), threadID, senderType)

	// Dedup check for WS broadcast
	actorType := senderType
	if messageDedup.Add(uuidToString(msg.ID)) {
		h.publish("message:created", workspaceID, actorType, senderID, map[string]any{
			"message": messageToResponse(msg),
		})

		// Broadcast thread:created when this message is a threaded reply.
		if req.ParentID != nil && *req.ParentID != "" {
			payload := map[string]any{
				"thread_id": *req.ParentID,
			}
			if req.ChannelID != nil {
				payload["channel_id"] = *req.ChannelID
			}
			h.publish(protocol.EventThreadCreated, workspaceID, "member", senderID, payload)
		}
	}

	// Log impersonation activity if applicable.
	if isImpersonated {
		h.publish("activity:impersonation_send", workspaceID, "member", userID, map[string]any{
			"agent_id":   senderID,
			"message_id": uuidToString(msg.ID),
			"channel_id": req.ChannelID,
		})
	}

	// Channel @mention auto-reply is no longer triggered here. MediationService
	// is the single gate for channel/thread routing — it subscribes to the
	// "message:created" event published above and decides whether to dispatch
	// AutoReplyService based on the unified routing priority + anti-loop rules
	// (Plan 3 Task 6).
	//
	// Direct messages to an agent (no channel/thread context) still trigger
	// AutoReply directly because they bypass routing entirely.
	if h.AutoReplyService != nil && req.RecipientType != nil && *req.RecipientType == "agent" && req.RecipientID != nil && *req.RecipientID != "" {
		go h.AutoReplyService.ReplyToDM(context.Background(), *req.RecipientID, workspaceID, senderID, msg)
	}

	writeJSON(w, http.StatusCreated, messageToResponse(msg))
}

// resolveOrCreateThread looks up or creates a thread for a parent message.
// Thread ID equals the root message ID per the data model. Returns an invalid
// pgtype.UUID on failure (caller treats that as "no thread context").
func (h *Handler) resolveOrCreateThread(ctx context.Context, parentMessageID string) pgtype.UUID {
	parentUUID := parseUUID(parentMessageID)

	// Check if thread already exists for this parent message.
	if _, err := h.getThread(ctx, parentMessageID); err == nil {
		// Thread exists.
		return parentUUID
	}

	// Thread does not exist — look up the parent message to get its
	// channel_id + workspace_id (both required by thread NOT NULL columns).
	parentMsg, err := h.Queries.GetMessage(ctx, parentUUID)
	if err != nil {
		slog.Warn("resolve thread: parent message not found", "parent_message_id", parentMessageID, "error", err)
		return pgtype.UUID{}
	}

	// Create a new thread with id = parent_message_id.
	if err := h.upsertThread(ctx, parentUUID, parentMsg.ChannelID, parentMsg.WorkspaceID); err != nil {
		slog.Warn("resolve thread: failed to create thread", "parent_message_id", parentMessageID, "error", err)
		return pgtype.UUID{}
	}

	return parentUUID
}

// GET /api/messages?channel_id=X or recipient_id=X[&peer_type=member|agent]
//
// peer_type defaults to "member" for backward compatibility. Pass
// peer_type=agent when the DM peer is an agent — otherwise the recipient_type
// filter rejects every row and the conversation appears empty.
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	channelID := r.URL.Query().Get("channel_id")
	recipientID := r.URL.Query().Get("recipient_id")
	peerType := r.URL.Query().Get("peer_type")
	if peerType == "" {
		peerType = "member"
	}
	if peerType != "member" && peerType != "agent" {
		writeError(w, http.StatusBadRequest, "peer_type must be 'member' or 'agent'")
		return
	}
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	var messages []db.Message
	var err error

	if channelID != "" {
		messages, err = h.Queries.ListChannelMessages(r.Context(), db.ListChannelMessagesParams{
			ChannelID: parseUUID(channelID),
			Limit:     int32(limit),
			Offset:    int32(offset),
		})
	} else if recipientID != "" {
		senderID, ok := requireUserID(w, r)
		if !ok {
			return
		}
		messages, err = h.Queries.ListDMMessages(r.Context(), db.ListDMMessagesParams{
			WorkspaceID: parseUUID(workspaceID),
			SelfID:      parseUUID(senderID),
			SelfType:    "member",
			PeerID:      parseUUID(recipientID),
			PeerType:    strToText(peerType),
			LimitCount:  int32(limit),
			OffsetCount: int32(offset),
		})
	} else {
		writeError(w, http.StatusBadRequest, "specify channel_id or recipient_id")
		return
	}

	if err != nil {
		slog.Warn("list messages failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}

	resp := make([]map[string]any, len(messages))
	for i, m := range messages {
		resp[i] = messageToResponse(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": resp})
}

// GET /api/messages/conversations
func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	query := `
		WITH dm_peers AS (
			SELECT DISTINCT
				CASE WHEN sender_id = $1 THEN recipient_id ELSE sender_id END AS peer_id,
				CASE WHEN sender_id = $1 THEN recipient_type ELSE sender_type END AS peer_type
			FROM message
			WHERE workspace_id = $2
			  AND (
				(sender_id = $1 AND recipient_id IS NOT NULL AND recipient_id != '00000000-0000-0000-0000-000000000000'::uuid)
				OR
				(recipient_id = $1)
			  )
		)
		SELECT
			p.peer_id,
			COALESCE(p.peer_type, 'member') AS peer_type,
			COALESCE(a.name, u.name, '') AS peer_name,
			0 AS unread_count
		FROM dm_peers p
		LEFT JOIN agent a ON p.peer_id = a.id
		LEFT JOIN "user" u ON p.peer_id = u.id
		ORDER BY peer_name
	`

	rows, err := h.DB.Query(r.Context(), query, parseUUID(userID), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("list conversations failed", "error", err)
		writeJSON(w, http.StatusOK, map[string]any{"conversations": []any{}})
		return
	}
	defer rows.Close()

	type conversation struct {
		PeerID      string `json:"peer_id"`
		PeerType    string `json:"peer_type"`
		PeerName    string `json:"peer_name,omitempty"`
		UnreadCount int    `json:"unread_count"`
	}

	var convs []conversation
	for rows.Next() {
		var c conversation
		var peerUUID pgtype.UUID
		if err := rows.Scan(&peerUUID, &c.PeerType, &c.PeerName, &c.UnreadCount); err != nil {
			continue
		}
		c.PeerID = uuidToString(peerUUID)
		convs = append(convs, c)
	}

	if convs == nil {
		convs = []conversation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": convs})
}

// GET /api/messages/{messageID}/thread
func (h *Handler) ListThread(w http.ResponseWriter, r *http.Request) {
	messageID := chi.URLParam(r, "messageID")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	messages, err := h.Queries.ListThreadMessages(r.Context(), db.ListThreadMessagesParams{
		ParentID: parseUUID(messageID),
		Limit:    int32(limit),
		Offset:   int32(offset),
	})
	if err != nil {
		slog.Warn("list thread failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list thread")
		return
	}

	resp := make([]map[string]any, len(messages))
	for i, m := range messages {
		resp[i] = messageToResponse(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": resp})
}

// POST /api/threads/{threadID}/promote
func (h *Handler) PromoteThread(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	threadID := chi.URLParam(r, "threadID")

	var req struct {
		ChannelName string `json:"channel_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChannelName == "" {
		writeError(w, http.StatusBadRequest, "channel_name is required")
		return
	}

	// Get thread messages.
	threadMessages, err := h.Queries.ListThreadMessages(r.Context(), db.ListThreadMessagesParams{
		ParentID: parseUUID(threadID),
		Limit:    1000,
		Offset:   0,
	})
	if err != nil || len(threadMessages) == 0 {
		writeError(w, http.StatusNotFound, "thread not found or empty")
		return
	}

	// Create new channel.
	newCh, err := h.Queries.CreateChannel(r.Context(), db.CreateChannelParams{
		WorkspaceID:   parseUUID(workspaceID),
		Name:          req.ChannelName,
		Description:   textOf("Promoted from thread"),
		CreatedBy:     parseUUID(userID),
		CreatedByType: "member",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}

	// Copy messages to new channel.
	for _, msg := range threadMessages {
		_, _ = h.DB.Exec(r.Context(), `
			INSERT INTO message (workspace_id, sender_id, sender_type, channel_id, content, content_type, type, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, msg.WorkspaceID, msg.SenderID, msg.SenderType, newCh.ID,
			msg.Content, msg.ContentType, msg.Type, msg.CreatedAt)
	}

	// Post system notification in original channel.
	var origChannelID pgtype.UUID
	_ = h.DB.QueryRow(r.Context(),
		`SELECT channel_id FROM message WHERE id = $1`, parseUUID(threadID),
	).Scan(&origChannelID)

	if origChannelID.Valid {
		_, _ = h.DB.Exec(r.Context(), `
			INSERT INTO message (workspace_id, sender_id, sender_type, channel_id, content, content_type, type)
			VALUES ($1, $2, 'member', $3, $4, 'text', 'system_notification')
		`, parseUUID(workspaceID), parseUUID(userID), origChannelID,
			fmt.Sprintf("Thread promoted to channel #%s", req.ChannelName))
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"channel_id":      uuidToString(newCh.ID),
		"channel_name":    newCh.Name,
		"copied_messages": len(threadMessages),
	})
}

func messageToResponse(m db.Message) map[string]any {
	return map[string]any{
		"id":             uuidToString(m.ID),
		"workspace_id":   uuidToString(m.WorkspaceID),
		"sender_id":      uuidToString(m.SenderID),
		"sender_type":    m.SenderType,
		"channel_id":     uuidToPtr(m.ChannelID),
		"recipient_id":   uuidToPtr(m.RecipientID),
		"recipient_type": textToPtr(m.RecipientType),
		"content":        m.Content,
		"content_type":   m.ContentType,
		"file_id":        uuidToPtr(m.FileID),
		"file_name":      textToPtr(m.FileName),
		"status":         m.Status,
		"parent_id":      uuidToPtr(m.ParentID),
		"thread_id":      uuidToPtr(m.ThreadID),
		"type":           m.Type,
		"created_at":     timestampToString(m.CreatedAt),
	}
}

// ptrToUUID converts a *string to pgtype.UUID.
func ptrToUUID(s *string) pgtype.UUID {
	if s == nil {
		return pgtype.UUID{}
	}
	return parseUUID(*s)
}

// ptrToInt8 converts a *int64 to pgtype.Int8.
func ptrToInt8(n *int64) pgtype.Int8 {
	if n == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *n, Valid: true}
}

// POST /api/messages/read
//
// Marks the given message ids as read. The SQL enforces authorization
// (recipient or channel member); unauthorized ids are silently ignored
// so a noisy client doesn't accidentally 403 the whole batch.
//
// Broadcasts "message:read" for each successfully-updated row so the
// original sender's UI can flip its single-tick to a double-tick.
func (h *Handler) MarkMessagesRead(w http.ResponseWriter, r *http.Request) {
	type Req struct {
		IDs []string `json:"ids"`
	}
	var req Req
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"read_ids": []string{}})
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	ids := make([]pgtype.UUID, 0, len(req.IDs))
	for _, id := range req.IDs {
		ids = append(ids, parseUUID(id))
	}

	rows, err := h.Queries.MarkMessagesRead(r.Context(), db.MarkMessagesReadParams{
		Ids:       ids,
		ActorID:   parseUUID(userID),
		ActorType: strToText("member"),
	})
	if err != nil {
		slog.Warn("mark messages read failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to mark read")
		return
	}

	readIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		msgID := uuidToString(row.ID)
		readIDs = append(readIDs, msgID)
		payload := map[string]any{
			"message_id":  msgID,
			"reader_id":   userID,
			"sender_id":   uuidToString(row.SenderID),
			"sender_type": row.SenderType,
		}
		if row.ChannelID.Valid {
			payload["channel_id"] = uuidToString(row.ChannelID)
		}
		h.publish("message:read", workspaceID, "member", userID, payload)
	}

	writeJSON(w, http.StatusOK, map[string]any{"read_ids": readIDs})
}

// POST /api/typing
func (h *Handler) SendTypingIndicator(w http.ResponseWriter, r *http.Request) {
	type Req struct {
		ChannelID string `json:"channel_id"`
		IsTyping  bool   `json:"is_typing"`
	}
	var req Req
	json.NewDecoder(r.Body).Decode(&req)

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	h.publish("typing", workspaceID, "member", userID, map[string]any{
		"channel_id": req.ChannelID,
		"is_typing":  req.IsTyping,
		"sender_id":  userID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// queryInt reads an integer query parameter with a default.
func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
