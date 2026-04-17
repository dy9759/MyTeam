package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/protocol"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ListThreads - GET /api/channels/{channelID}/threads
// Returns threads for a channel, ordered by last_activity_at DESC.
func (h *Handler) ListThreads(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	threads, err := h.Queries.ListThreadsByChannel(r.Context(), db.ListThreadsByChannelParams{
		ChannelID: parseUUID(channelID),
		Status:    pgtype.Text{},
	})
	if err != nil {
		slog.Warn("list threads failed", "error", err, "channel_id", channelID)
		writeError(w, http.StatusInternalServerError, "failed to list threads")
		return
	}

	resp := make([]map[string]any, len(threads))
	for i, t := range threads {
		resp[i] = threadToResponse(t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": resp})
}

// GetThread - GET /api/threads/{threadID}
// Returns thread with metadata.
func (h *Handler) GetThread(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "threadID")
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	thread, err := h.Queries.GetThread(r.Context(), parseUUID(threadID))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "thread not found")
			return
		}
		slog.Warn("get thread failed", "error", err, "thread_id", threadID)
		writeError(w, http.StatusInternalServerError, "failed to get thread")
		return
	}

	writeJSON(w, http.StatusOK, threadToResponse(thread))
}

// CreateThread - POST /api/channels/{channelID}/threads
// Creates a new thread (with an independent UUID). Optional root_message_id, issue_id, title.
func (h *Handler) CreateThread(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	type CreateRequest struct {
		RootMessageID *string         `json:"root_message_id,omitempty"`
		IssueID       *string         `json:"issue_id,omitempty"`
		Title         *string         `json:"title,omitempty"`
		Status        *string         `json:"status,omitempty"`
		Metadata      json.RawMessage `json:"metadata,omitempty"`
	}
	var req CreateRequest
	// Body is optional — empty body is allowed.
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	thread, err := h.Queries.CreateThread(r.Context(), db.CreateThreadParams{
		ID:            pgtype.UUID{},
		ChannelID:     parseUUID(channelID),
		WorkspaceID:   parseUUID(workspaceID),
		RootMessageID: ptrToUUID(req.RootMessageID),
		IssueID:       ptrToUUID(req.IssueID),
		Title:         ptrToText(req.Title),
		Status:        ptrToText(req.Status),
		CreatedBy:     parseUUID(actorID),
		CreatedByType: strToText(actorType),
		Metadata:      []byte(req.Metadata),
	})
	if err != nil {
		slog.Warn("create thread failed", "error", err, "channel_id", channelID)
		writeError(w, http.StatusInternalServerError, "failed to create thread")
		return
	}

	payload := map[string]any{
		"thread":      threadToResponse(thread),
		"channel_ids": []string{channelID},
	}
	h.publish(protocol.EventThreadCreated, workspaceID, actorType, actorID, payload)

	writeJSON(w, http.StatusCreated, threadToResponse(thread))
}

// ListThreadMessages - GET /api/threads/{threadID}/messages
// Returns messages in the thread, paginated.
func (h *Handler) ListThreadMessages(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "threadID")
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	messages, err := h.Queries.ListMessagesByThread(r.Context(), db.ListMessagesByThreadParams{
		ThreadID: parseUUID(threadID),
		Limit:    int32(limit),
		Offset:   int32(offset),
	})
	if err != nil {
		slog.Warn("list thread messages failed", "error", err, "thread_id", threadID)
		writeError(w, http.StatusInternalServerError, "failed to list thread messages")
		return
	}

	resp := make([]map[string]any, len(messages))
	for i, m := range messages {
		resp[i] = messageToResponse(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": resp})
}

// PostThreadMessage - POST /api/threads/{threadID}/messages
// Posts a new message in the thread. Sets thread_id + channel_id from the
// thread's stored values; session_id is left NULL (no longer used).
func (h *Handler) PostThreadMessage(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "threadID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	type CreateRequest struct {
		Content     string          `json:"content"`
		ContentType string          `json:"content_type,omitempty"`
		Type        string          `json:"type,omitempty"`
		Metadata    json.RawMessage `json:"metadata,omitempty"`
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

	// Look up the thread to derive channel_id.
	thread, err := h.Queries.GetThread(r.Context(), parseUUID(threadID))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "thread not found")
			return
		}
		slog.Warn("post thread message: get thread failed", "error", err, "thread_id", threadID)
		writeError(w, http.StatusInternalServerError, "failed to load thread")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	contentType := req.ContentType
	if contentType == "" {
		contentType = "text"
	}
	msgType := req.Type
	if msgType == "" {
		msgType = "text"
	}

	metadata := []byte(req.Metadata)
	if len(metadata) == 0 {
		metadata = []byte("{}")
	}

	msg, err := h.Queries.InsertMessageWithAudit(r.Context(), db.InsertMessageWithAuditParams{
		WorkspaceID:        parseUUID(workspaceID),
		ChannelID:          thread.ChannelID,
		ThreadID:           thread.ID,
		SessionID:          pgtype.UUID{},
		SenderID:           parseUUID(actorID),
		SenderType:         actorType,
		Content:            req.Content,
		ContentType:        contentType,
		Type:               msgType,
		Metadata:           metadata,
		IsImpersonated:     false,
		EffectiveActorID:   parseUUID(actorID),
		EffectiveActorType: strToText(actorType),
		RealOperatorID:     parseUUID(userID),
		RealOperatorType:   strToText("member"),
	})
	if err != nil {
		slog.Warn("post thread message: insert failed", "error", err, "thread_id", threadID)
		writeError(w, http.StatusInternalServerError, "failed to create message")
		return
	}

	// Thread counters: increment reply_count + last_reply_at for member/agent
	// senders (per PRD §4 reply_count semantics fix); just touch last_activity_at
	// for system senders.
	if actorType == "member" || actorType == "agent" {
		if err := h.Queries.IncrementThreadReply(r.Context(), thread.ID); err != nil {
			slog.Warn("increment thread reply failed", "error", err, "thread_id", threadID)
		}
	} else {
		if err := h.Queries.TouchThreadActivity(r.Context(), thread.ID); err != nil {
			slog.Warn("touch thread activity failed", "error", err, "thread_id", threadID)
		}
	}

	if messageDedup.Add(uuidToString(msg.ID)) {
		h.publish(protocol.EventMessageCreated, workspaceID, actorType, actorID, map[string]any{
			"message": messageToResponse(msg),
		})
	}

	writeJSON(w, http.StatusCreated, messageToResponse(msg))
}

// ListThreadContextItems - GET /api/threads/{threadID}/context-items
func (h *Handler) ListThreadContextItems(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "threadID")
	if _, ok := requireUserID(w, r); !ok {
		return
	}

	items, err := h.Queries.ListThreadContextItems(r.Context(), parseUUID(threadID))
	if err != nil {
		slog.Warn("list thread context items failed", "error", err, "thread_id", threadID)
		writeError(w, http.StatusInternalServerError, "failed to list context items")
		return
	}

	resp := make([]map[string]any, 0, len(items))
	for _, it := range items {
		resp = append(resp, threadContextItemToResponse(it))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": resp})
}

// defaultRetentionForItemType returns the default retention_class for an item
// type per PRD §3.2: decision/summary -> permanent, others -> ttl.
// Note: 'summary' here defaults to 'permanent' (manual case); system-created
// summaries can override to 'ttl' explicitly.
func defaultRetentionForItemType(itemType string) string {
	switch itemType {
	case "decision", "summary":
		return "permanent"
	default:
		return "ttl"
	}
}

// CreateThreadContextItem - POST /api/threads/{threadID}/context-items
func (h *Handler) CreateThreadContextItem(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "threadID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	type CreateRequest struct {
		ItemType        string          `json:"item_type"`
		Title           *string         `json:"title,omitempty"`
		Body            *string         `json:"body,omitempty"`
		Metadata        json.RawMessage `json:"metadata,omitempty"`
		SourceMessageID *string         `json:"source_message_id,omitempty"`
		RetentionClass  *string         `json:"retention_class,omitempty"`
		ExpiresAt       *string         `json:"expires_at,omitempty"`
	}
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ItemType == "" {
		writeError(w, http.StatusBadRequest, "item_type is required")
		return
	}
	switch req.ItemType {
	case "decision", "file", "code_snippet", "summary", "reference":
	default:
		writeError(w, http.StatusBadRequest, "invalid item_type")
		return
	}

	// Resolve thread to verify it exists and to scope workspace.
	thread, err := h.Queries.GetThread(r.Context(), parseUUID(threadID))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "thread not found")
			return
		}
		slog.Warn("create context item: get thread failed", "error", err, "thread_id", threadID)
		writeError(w, http.StatusInternalServerError, "failed to load thread")
		return
	}

	// Apply per-item-type default for retention_class when caller did not specify.
	retention := req.RetentionClass
	if retention == nil || *retention == "" {
		def := defaultRetentionForItemType(req.ItemType)
		retention = &def
	}

	// Parse expires_at if provided.
	var expiresAt pgtype.Timestamptz
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, perr := time.Parse(time.RFC3339, *req.ExpiresAt)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid expires_at (RFC3339 required)")
			return
		}
		expiresAt = pgtype.Timestamptz{Time: t, Valid: true}
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	item, err := h.Queries.CreateThreadContextItem(r.Context(), db.CreateThreadContextItemParams{
		WorkspaceID:     thread.WorkspaceID,
		ThreadID:        thread.ID,
		ItemType:        req.ItemType,
		Title:           ptrToText(req.Title),
		Body:            ptrToText(req.Body),
		Metadata:        []byte(req.Metadata),
		SourceMessageID: ptrToUUID(req.SourceMessageID),
		RetentionClass:  ptrToText(retention),
		ExpiresAt:       expiresAt,
		CreatedBy:       parseUUID(actorID),
		CreatedByType:   strToText(actorType),
	})
	if err != nil {
		slog.Warn("create thread context item failed", "error", err, "thread_id", threadID)
		writeError(w, http.StatusInternalServerError, "failed to create context item")
		return
	}

	h.publish(protocol.EventThreadContextItemCreated, workspaceID, actorType, actorID, map[string]any{
		"item": threadContextItemToResponse(item),
	})

	writeJSON(w, http.StatusCreated, threadContextItemToResponse(item))
}

// DeleteThreadContextItem - DELETE /api/threads/{threadID}/context-items/{itemID}
func (h *Handler) DeleteThreadContextItem(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "threadID")
	itemID := chi.URLParam(r, "itemID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	// Verify the item belongs to this thread before deleting.
	item, err := h.Queries.GetThreadContextItem(r.Context(), parseUUID(itemID))
	if err != nil || uuidToString(item.ThreadID) != threadID {
		writeError(w, http.StatusNotFound, "context item not found")
		return
	}

	if err := h.Queries.DeleteThreadContextItem(r.Context(), item.ID); err != nil {
		slog.Warn("delete thread context item failed", "error", err, "item_id", itemID)
		writeError(w, http.StatusInternalServerError, "failed to delete context item")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventThreadContextItemDeleted, workspaceID, actorType, actorID, map[string]any{
		"item_id":   itemID,
		"thread_id": threadID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// ===== Response shaping =====

func threadToResponse(t db.Thread) map[string]any {
	resp := map[string]any{
		"id":               uuidToString(t.ID),
		"channel_id":       uuidToString(t.ChannelID),
		"workspace_id":     uuidToString(t.WorkspaceID),
		"root_message_id":  uuidToPtr(t.RootMessageID),
		"issue_id":         uuidToPtr(t.IssueID),
		"created_by":       uuidToPtr(t.CreatedBy),
		"created_by_type":  textToPtr(t.CreatedByType),
		"title":            textToPtr(t.Title),
		"status":           t.Status,
		"reply_count":      t.ReplyCount,
		"last_reply_at":    timestampToPtr(t.LastReplyAt),
		"last_activity_at": timestampToPtr(t.LastActivityAt),
		"created_at":       timestampToString(t.CreatedAt),
	}
	if len(t.Metadata) > 0 {
		var md any
		if err := json.Unmarshal(t.Metadata, &md); err == nil {
			resp["metadata"] = md
		} else {
			resp["metadata"] = map[string]any{}
		}
	} else {
		resp["metadata"] = map[string]any{}
	}
	return resp
}

func threadContextItemToResponse(it db.ThreadContextItem) map[string]any {
	resp := map[string]any{
		"id":                uuidToString(it.ID),
		"workspace_id":      uuidToString(it.WorkspaceID),
		"thread_id":         uuidToString(it.ThreadID),
		"item_type":         it.ItemType,
		"title":             textToPtr(it.Title),
		"body":              textToPtr(it.Body),
		"source_message_id": uuidToPtr(it.SourceMessageID),
		"retention_class":   it.RetentionClass,
		"expires_at":        timestampToPtr(it.ExpiresAt),
		"created_by":        uuidToPtr(it.CreatedBy),
		"created_by_type":   textToPtr(it.CreatedByType),
		"created_at":        timestampToString(it.CreatedAt),
	}
	if len(it.Metadata) > 0 {
		var md any
		if err := json.Unmarshal(it.Metadata, &md); err == nil {
			resp["metadata"] = md
		} else {
			resp["metadata"] = map[string]any{}
		}
	} else {
		resp["metadata"] = map[string]any{}
	}
	return resp
}

// ===== Legacy raw-SQL helpers (still consumed by message.go's resolveOrCreateThread) =====

// getThread is the legacy helper retained for the message.CreateMessage path.
// New code should call h.Queries.GetThread directly.
func (h *Handler) getThread(ctx context.Context, threadID string) (db.Thread, error) {
	if h.DB == nil {
		return db.Thread{}, pgx.ErrNoRows
	}
	return h.Queries.GetThread(ctx, parseUUID(threadID))
}

// upsertThread is retained for message.CreateMessage's resolveOrCreateThread.
// Uses the legacy UpsertThread query (id == root message id).
func (h *Handler) upsertThread(ctx context.Context, threadID, channelID pgtype.UUID) error {
	if h.DB == nil {
		return nil
	}
	_, err := h.Queries.UpsertThread(ctx, db.UpsertThreadParams{
		ID:        threadID,
		ChannelID: channelID,
		Title:     pgtype.Text{},
	})
	return err
}

// incrementThreadReplyCount keeps existing message.CreateMessage callers
// working. New thread handlers use IncrementThreadReply / TouchThreadActivity.
func (h *Handler) incrementThreadReplyCount(ctx context.Context, threadID pgtype.UUID) {
	if h.DB == nil {
		return
	}
	if err := h.Queries.IncrementThreadReplyCount(ctx, threadID); err != nil {
		slog.Warn("increment thread reply count failed", "thread_id", uuidToString(threadID), "error", err)
	}
}
