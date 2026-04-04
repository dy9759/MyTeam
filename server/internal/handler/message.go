package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// POST /api/messages
func (h *Handler) CreateMessage(w http.ResponseWriter, r *http.Request) {
	type CreateRequest struct {
		ChannelID     *string         `json:"channel_id,omitempty"`
		RecipientID   *string         `json:"recipient_id,omitempty"`
		RecipientType *string         `json:"recipient_type,omitempty"`
		SessionID     *string         `json:"session_id,omitempty"`
		Content       string          `json:"content"`
		ContentType   string          `json:"content_type,omitempty"`
		FileID        *string         `json:"file_id,omitempty"`
		FileName      *string         `json:"file_name,omitempty"`
		FileSize      *int64          `json:"file_size,omitempty"`
		Metadata      json.RawMessage `json:"metadata,omitempty"`
		ParentID      *string         `json:"parent_id,omitempty"`
		Type          string          `json:"type,omitempty"`
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

	senderID, ok := requireUserID(w, r)
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

	msg, err := h.Queries.CreateMessage(r.Context(), db.CreateMessageParams{
		WorkspaceID:     parseUUID(workspaceID),
		SenderID:        parseUUID(senderID),
		SenderType:      "member",
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
		ParentID:        ptrToUUID(req.ParentID),
		Type:            msgType,
	})
	if err != nil {
		slog.Warn("create message failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create message")
		return
	}

	h.publish("message:created", workspaceID, "member", senderID, map[string]any{
		"message": messageToResponse(msg),
	})

	writeJSON(w, http.StatusCreated, messageToResponse(msg))
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
