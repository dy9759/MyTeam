package handler

import (
	"context"
	"encoding/json"
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
		SessionID       *string         `json:"session_id,omitempty"`
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

	// Use parent_message_id as ParentID if the legacy ParentID is not set.
	parentID := ptrToUUID(req.ParentID)
	if !parentID.Valid && req.ParentMessageID != nil {
		parentID = parseUUID(*req.ParentMessageID)
	}

	msg, err := h.Queries.CreateMessage(r.Context(), db.CreateMessageParams{
		WorkspaceID:     parseUUID(workspaceID),
		SenderID:        parseUUID(senderID),
		SenderType:      senderType,
		ChannelID:       ptrToUUID(req.ChannelID),
		RecipientID:     ptrToUUID(req.RecipientID),
		RecipientType:   ptrToText(req.RecipientType),
		SessionID:       ptrToUUID(req.SessionID),
		Content:         req.Content,
		ContentType:     contentType,
		FileID:          ptrToUUID(req.FileID),
		FileName:        ptrToText(req.FileName),
		FileSize:        ptrToInt8(req.FileSize),
		FileContentType: pgtype.Text{},
		Metadata:        req.Metadata,
		ParentID:        parentID,
		Type:            msgType,
		// TODO: wire after sqlc generation — add these fields to CreateMessageParams:
		// IsImpersonated: isImpersonated,
		// ThreadID:       threadID,
	})
	if err != nil {
		slog.Warn("create message failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create message")
		return
	}

	// If we created/resolved a thread, increment its reply count.
	if threadID.Valid {
		h.incrementThreadReplyCount(r.Context(), threadID)
	}

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

	// Check for @mentions and trigger auto-reply
	if h.AutoReplyService != nil {
		mentions := ParseMentions(req.Content)
		if len(mentions) > 0 {
			h.AutoReplyService.CheckAndReply(r.Context(), mentions, workspaceID,
				uuidToString(msg.ChannelID), msg)
		}
	}

	// DM to agent: trigger auto-reply for direct messages sent to an agent.
	if h.AutoReplyService != nil && req.RecipientType != nil && *req.RecipientType == "agent" && req.RecipientID != nil && *req.RecipientID != "" {
		go h.AutoReplyService.ReplyToDM(context.Background(), *req.RecipientID, workspaceID, senderID, msg)
	}

	writeJSON(w, http.StatusCreated, messageToResponse(msg))
}

// resolveOrCreateThread looks up or creates a thread for a parent message.
// Thread ID equals the root message ID per the data model.
func (h *Handler) resolveOrCreateThread(ctx context.Context, parentMessageID string) pgtype.UUID {
	parentUUID := parseUUID(parentMessageID)

	// Check if thread already exists for this parent message.
	_, err := h.getThread(ctx, parentMessageID)
	if err == nil {
		// Thread exists.
		return parentUUID
	}

	// Thread does not exist — look up the parent message to get its channel_id.
	parentMsg, err := h.Queries.GetMessage(ctx, parentUUID)
	if err != nil {
		slog.Warn("resolve thread: parent message not found", "parent_message_id", parentMessageID, "error", err)
		return pgtype.UUID{}
	}

	// Create a new thread with id = parent_message_id.
	if err := h.upsertThread(ctx, parentUUID, parentMsg.ChannelID); err != nil {
		slog.Warn("resolve thread: failed to create thread", "parent_message_id", parentMessageID, "error", err)
		return pgtype.UUID{}
	}

	return parentUUID
}

// GET /api/messages?channel_id=X or recipient_id=X or session_id=X
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	channelID := r.URL.Query().Get("channel_id")
	recipientID := r.URL.Query().Get("recipient_id")
	sessionID := r.URL.Query().Get("session_id")
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
	} else if sessionID != "" {
		messages, err = h.Queries.ListSessionMessages(r.Context(), db.ListSessionMessagesParams{
			SessionID: parseUUID(sessionID),
			Limit:     int32(limit),
			Offset:    int32(offset),
		})
	} else if recipientID != "" {
		senderID, ok := requireUserID(w, r)
		if !ok {
			return
		}
		messages, err = h.Queries.ListDMMessages(r.Context(), db.ListDMMessagesParams{
			WorkspaceID:   parseUUID(workspaceID),
			SenderID:      parseUUID(senderID),
			SenderType:    "member",
			RecipientID:   parseUUID(recipientID),
			RecipientType: strToText("member"),
			Limit:         int32(limit),
			Offset:        int32(offset),
		})
	} else {
		writeError(w, http.StatusBadRequest, "specify channel_id, recipient_id, or session_id")
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
	writeJSON(w, http.StatusOK, map[string]any{"conversations": []any{}})
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

func messageToResponse(m db.Message) map[string]any {
	return map[string]any{
		"id":             uuidToString(m.ID),
		"workspace_id":   uuidToString(m.WorkspaceID),
		"sender_id":      uuidToString(m.SenderID),
		"sender_type":    m.SenderType,
		"channel_id":     uuidToPtr(m.ChannelID),
		"recipient_id":   uuidToPtr(m.RecipientID),
		"recipient_type": textToPtr(m.RecipientType),
		"session_id":     uuidToPtr(m.SessionID),
		"content":        m.Content,
		"content_type":   m.ContentType,
		"file_id":        uuidToPtr(m.FileID),
		"file_name":      textToPtr(m.FileName),
		"status":         m.Status,
		"parent_id":      uuidToPtr(m.ParentID),
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

// POST /api/typing
func (h *Handler) SendTypingIndicator(w http.ResponseWriter, r *http.Request) {
	type Req struct {
		ChannelID string `json:"channel_id"`
		SessionID string `json:"session_id"`
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
		"session_id": req.SessionID,
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
