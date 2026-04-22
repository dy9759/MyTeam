package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/MyAIOSHub/MyTeam/server/internal/logger"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/MyAIOSHub/MyTeam/server/pkg/protocol"
)

// allowedResolveValues is the user-facing whitelist for inbox resolutions.
// 'auto_resolved' is intentionally excluded — only system writers may set it.
var allowedResolveValues = map[string]struct{}{
	"approved":  {},
	"rejected":  {},
	"dismissed": {},
}

type InboxItemResponse struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	RecipientType string          `json:"recipient_type"`
	RecipientID   string          `json:"recipient_id"`
	Type          string          `json:"type"`
	Severity      string          `json:"severity"`
	IssueID       *string         `json:"issue_id"`
	Title         string          `json:"title"`
	Body          *string         `json:"body"`
	Read          bool            `json:"read"`
	Archived      bool            `json:"archived"`
	CreatedAt     string          `json:"created_at"`
	IssueStatus   *string         `json:"issue_status"`
	ActorType     *string         `json:"actor_type"`
	ActorID       *string         `json:"actor_id"`
	Details       json.RawMessage `json:"details"`
}

func inboxToResponse(i db.InboxItem) InboxItemResponse {
	return InboxItemResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		RecipientType: i.RecipientType,
		RecipientID:   uuidToString(i.RecipientID),
		Type:          i.Type,
		Severity:      i.Severity,
		IssueID:       uuidToPtr(i.IssueID),
		Title:         i.Title,
		Body:          textToPtr(i.Body),
		Read:          i.Read,
		Archived:      i.Archived,
		CreatedAt:     timestampToString(i.CreatedAt),
		ActorType:     textToPtr(i.ActorType),
		ActorID:       uuidToPtr(i.ActorID),
		Details:       json.RawMessage(i.Details),
	}
}

func inboxRowToResponse(r db.ListInboxItemsRow) InboxItemResponse {
	return InboxItemResponse{
		ID:            uuidToString(r.ID),
		WorkspaceID:   uuidToString(r.WorkspaceID),
		RecipientType: r.RecipientType,
		RecipientID:   uuidToString(r.RecipientID),
		Type:          r.Type,
		Severity:      r.Severity,
		IssueID:       uuidToPtr(r.IssueID),
		Title:         r.Title,
		Body:          textToPtr(r.Body),
		Read:          r.Read,
		Archived:      r.Archived,
		CreatedAt:     timestampToString(r.CreatedAt),
		IssueStatus:   textToPtr(r.IssueStatus),
		ActorType:     textToPtr(r.ActorType),
		ActorID:       uuidToPtr(r.ActorID),
		Details:       json.RawMessage(r.Details),
	}
}

// inboxItemsToResponse converts a slice of unresolved inbox items (db.InboxItem)
// to the API response shape. issue_status is not joined here because the
// unresolved feed prioritises latency and is not always issue-scoped.
func inboxItemsToResponse(items []db.InboxItem) []InboxItemResponse {
	out := make([]InboxItemResponse, len(items))
	for i, item := range items {
		out[i] = inboxToResponse(item)
	}
	return out
}

// paginationFromRequest extracts limit/offset query params with defaults.
func paginationFromRequest(r *http.Request, defLimit, maxLimit int) (int, int) {
	limit := defLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	return limit, offset
}

func (h *Handler) enrichInboxResponse(ctx context.Context, resp InboxItemResponse, issueID pgtype.UUID) InboxItemResponse {
	if !issueID.Valid {
		return resp
	}
	issue, err := h.Queries.GetIssue(ctx, issueID)
	if err == nil {
		s := issue.Status
		resp.IssueStatus = &s
	}
	return resp
}

func (h *Handler) ListInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Plan 4 §8: ?unresolved=true returns the cross-workspace, paginated
	// list of items that still need attention, scoped strictly to the
	// requesting user as recipient. This is workspace-agnostic on purpose
	// (the partial index is keyed by recipient_id only).
	if r.URL.Query().Get("unresolved") == "true" {
		limit, offset := paginationFromRequest(r, 50, 200)
		items, err := h.Queries.ListInboxUnresolved(r.Context(), db.ListInboxUnresolvedParams{
			RecipientID: parseUUID(userID),
			LimitCount:  int32(limit),
			OffsetCount: int32(offset),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list unresolved inbox")
			return
		}
		writeJSON(w, http.StatusOK, inboxItemsToResponse(items))
		return
	}

	workspaceID := r.Header.Get("X-Workspace-ID")
	items, err := h.Queries.ListInboxItems(r.Context(), db.ListInboxItemsParams{
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list inbox")
		return
	}

	resp := make([]InboxItemResponse, len(items))
	for i, item := range items {
		resp[i] = inboxRowToResponse(item)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) MarkInboxRead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.loadInboxItemForUser(w, r, id); !ok {
		return
	}
	item, err := h.Queries.MarkInboxRead(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark read")
		return
	}

	userID := requestUserID(r)
	workspaceID := uuidToString(item.WorkspaceID)
	h.publish(protocol.EventInboxRead, workspaceID, "member", userID, map[string]any{
		"item_id":      uuidToString(item.ID),
		"recipient_id": uuidToString(item.RecipientID),
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(item), item.IssueID)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ArchiveInboxItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.loadInboxItemForUser(w, r, id); !ok {
		return
	}
	item, err := h.Queries.ArchiveInboxItem(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive")
		return
	}

	userID := requestUserID(r)
	workspaceID := uuidToString(item.WorkspaceID)
	h.publish(protocol.EventInboxArchived, workspaceID, "member", userID, map[string]any{
		"item_id":      uuidToString(item.ID),
		"recipient_id": uuidToString(item.RecipientID),
		"resolution":   "archived",
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(item), item.IssueID)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CountUnreadInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.CountUnreadInbox(r.Context(), db.CountUnreadInboxParams{
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count unread inbox")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
}

func (h *Handler) MarkAllInboxRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.MarkAllInboxRead(r.Context(), db.MarkAllInboxReadParams{
		WorkspaceID: parseUUID(workspaceID),
		RecipientID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark all inbox read")
		return
	}

	slog.Info("inbox: mark all read", append(logger.RequestAttrs(r), "user_id", userID, "count", count)...)
	h.publish(protocol.EventInboxBatchRead, workspaceID, "member", userID, map[string]any{
		"recipient_id": userID,
		"count":        count,
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *Handler) ArchiveAllInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.ArchiveAllInbox(r.Context(), db.ArchiveAllInboxParams{
		WorkspaceID: parseUUID(workspaceID),
		RecipientID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive all inbox")
		return
	}

	slog.Info("inbox: archive all", append(logger.RequestAttrs(r), "user_id", userID, "count", count)...)
	h.publish(protocol.EventInboxBatchArchived, workspaceID, "member", userID, map[string]any{
		"recipient_id": userID,
		"count":        count,
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *Handler) ArchiveAllReadInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.ArchiveAllReadInbox(r.Context(), db.ArchiveAllReadInboxParams{
		WorkspaceID: parseUUID(workspaceID),
		RecipientID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive all read inbox")
		return
	}

	slog.Info("inbox: archive all read", append(logger.RequestAttrs(r), "user_id", userID, "count", count)...)
	h.publish(protocol.EventInboxBatchArchived, workspaceID, "member", userID, map[string]any{
		"recipient_id": userID,
		"count":        count,
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *Handler) ArchiveCompletedInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.ArchiveCompletedInbox(r.Context(), db.ArchiveCompletedInboxParams{
		WorkspaceID: parseUUID(workspaceID),
		RecipientID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive completed inbox")
		return
	}

	slog.Info("inbox: archive completed", append(logger.RequestAttrs(r), "user_id", userID, "count", count)...)
	h.publish(protocol.EventInboxBatchArchived, workspaceID, "member", userID, map[string]any{
		"recipient_id": userID,
		"count":        count,
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

// resolveInboxRequest is the body for POST /api/inbox/{id}/resolve.
type resolveInboxRequest struct {
	Resolution string `json:"resolution"`
}

// ResolveInboxItem marks an actionable inbox item as resolved.
// Per PRD §8: only the recipient may resolve their own items, and the
// resolution value is restricted to user-facing values
// (approved | rejected | dismissed). 'auto_resolved' is reserved for
// system writers and is intentionally not accepted here.
func (h *Handler) ResolveInboxItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	itemID := chi.URLParam(r, "id")
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	var req resolveInboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if _, allowed := allowedResolveValues[req.Resolution]; !allowed {
		writeError(w, http.StatusBadRequest, "resolution must be approved|rejected|dismissed")
		return
	}

	itemUUID := parseUUID(itemID)
	userUUID := parseUUID(userID)
	if !itemUUID.Valid || !userUUID.Valid {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.Queries.ResolveInboxItem(r.Context(), db.ResolveInboxItemParams{
		ID:           itemUUID,
		RecipientID:  userUUID,
		Resolution:   textOf(req.Resolution),
		ResolutionBy: userUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve inbox item")
		return
	}

	slog.Info("inbox: resolve",
		append(logger.RequestAttrs(r), "user_id", userID, "item_id", itemID, "resolution", req.Resolution)...,
	)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "resolved",
		"resolution": req.Resolution,
	})
}

// MarkInboxItemRead marks one inbox item as read for the requesting user
// (recipient ownership enforced in SQL). Idempotent: a second call after
// the row is already read is a no-op.
func (h *Handler) MarkInboxItemRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	itemID := chi.URLParam(r, "id")
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	itemUUID := parseUUID(itemID)
	userUUID := parseUUID(userID)
	if !itemUUID.Valid || !userUUID.Valid {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.Queries.MarkInboxItemRead(r.Context(), db.MarkInboxItemReadParams{
		ID:          itemUUID,
		RecipientID: userUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark inbox item read")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "read"})
}

// MarkAllInboxItemsRead marks every unread inbox item for the requesting
// user as read, across all workspaces (per PRD §8 the recipient-keyed
// version is workspace-agnostic; the legacy per-workspace endpoint
// `MarkAllInboxRead` remains for the existing UI surface).
func (h *Handler) MarkAllInboxItemsRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	count, err := h.Queries.MarkAllInboxItemsRead(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark all inbox items read")
		return
	}

	slog.Info("inbox: mark all read (recipient-scoped)",
		append(logger.RequestAttrs(r), "user_id", userID, "count", count)...,
	)
	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}
