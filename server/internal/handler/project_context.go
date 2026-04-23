package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// ProjectContextResponse is the JSON response for a project context record.
type ProjectContextResponse struct {
	ID                string  `json:"id"`
	ProjectID         string  `json:"project_id"`
	VersionID         *string `json:"version_id,omitempty"`
	SourceType        string  `json:"source_type"`
	SourceID          string  `json:"source_id"`
	SourceName        *string `json:"source_name,omitempty"`
	MessageRangeStart *string `json:"message_range_start,omitempty"`
	MessageRangeEnd   *string `json:"message_range_end,omitempty"`
	SnapshotMd        string  `json:"snapshot_md"`
	MessageCount      int32   `json:"message_count"`
	ImportedBy        string  `json:"imported_by"`
	ImportedAt        string  `json:"imported_at"`
}

func projectContextToResponse(c db.ProjectContext) ProjectContextResponse {
	return ProjectContextResponse{
		ID:                uuidToString(c.ID),
		ProjectID:         uuidToString(c.ProjectID),
		VersionID:         uuidToPtr(c.VersionID),
		SourceType:        c.SourceType,
		SourceID:          uuidToString(c.SourceID),
		SourceName:        textToPtr(c.SourceName),
		MessageRangeStart: timestampToPtr(c.MessageRangeStart),
		MessageRangeEnd:   timestampToPtr(c.MessageRangeEnd),
		SnapshotMd:        c.SnapshotMd,
		MessageCount:      c.MessageCount,
		ImportedBy:        uuidToString(c.ImportedBy),
		ImportedAt:        c.ImportedAt.Time.Format(time.RFC3339),
	}
}

// ImportProjectContext handles POST /api/projects/{projectID}/import-context
// Accepts a source (channel, dm, or thread) and imports its messages as a
// markdown snapshot attached to the project.
func (h *Handler) ImportProjectContext(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	type ImportContextRequest struct {
		SourceType string  `json:"source_type"` // "channel", "dm", "thread"
		SourceID   string  `json:"source_id"`
		DateFrom   *string `json:"date_from,omitempty"`
		DateTo     *string `json:"date_to,omitempty"`
	}

	var req ImportContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.SourceType == "" {
		writeError(w, http.StatusBadRequest, "source_type is required")
		return
	}
	if req.SourceID == "" {
		writeError(w, http.StatusBadRequest, "source_id is required")
		return
	}

	switch req.SourceType {
	case "channel", "dm", "thread":
		// valid
	default:
		writeError(w, http.StatusBadRequest, "source_type must be channel, dm, or thread")
		return
	}

	// Verify the project exists.
	project, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	_ = project

	workspaceID := resolveWorkspaceID(r)

	// Fetch messages from the source.
	var messages []db.Message
	var sourceName string

	switch req.SourceType {
	case "channel":
		// Fetch channel info for the snapshot header.
		ch, chErr := h.Queries.GetChannel(r.Context(), parseUUID(req.SourceID))
		if chErr == nil {
			sourceName = ch.Name
		}
		msgs, msgErr := h.Queries.ListChannelMessages(r.Context(), db.ListChannelMessagesParams{
			ChannelID: parseUUID(req.SourceID),
			Limit:     500,
			Offset:    0,
		})
		if msgErr != nil {
			slog.Error("failed to list channel messages for context import", "error", msgErr)
			writeError(w, http.StatusInternalServerError, "failed to fetch channel messages")
			return
		}
		messages = msgs

	case "dm":
		// DM: fetch messages between the requesting user and the source entity.
		// We interpret source_id as the recipient (the other party) for the DM conversation.
		msgs, msgErr := h.Queries.ListDMMessages(r.Context(), db.ListDMMessagesParams{
			WorkspaceID: parseUUID(workspaceID),
			SelfID:      parseUUID(userID),
			SelfType:    "member",
			PeerID:      parseUUID(req.SourceID),
			PeerType:    pgtype.Text{String: "member", Valid: true},
			LimitCount:  500,
			OffsetCount: 0,
		})
		if msgErr != nil {
			slog.Error("failed to list DM messages for context import", "error", msgErr)
			writeError(w, http.StatusInternalServerError, "failed to fetch DM messages")
			return
		}
		messages = msgs
		sourceName = "DM conversation"

	case "thread":
		// Thread: list messages by parent message ID.
		msgs, msgErr := h.Queries.ListThreadMessages(r.Context(), db.ListThreadMessagesParams{
			ParentID: parseUUID(req.SourceID),
			Limit:    500,
			Offset:   0,
		})
		if msgErr != nil {
			slog.Error("failed to list thread messages for context import", "error", msgErr)
			writeError(w, http.StatusInternalServerError, "failed to fetch thread messages")
			return
		}
		messages = msgs
		sourceName = "thread"
	}

	// Filter by date range if provided.
	var rangeStart, rangeEnd *time.Time
	if req.DateFrom != nil && *req.DateFrom != "" {
		if t, parseErr := time.Parse(time.RFC3339, *req.DateFrom); parseErr == nil {
			rangeStart = &t
		}
	}
	if req.DateTo != nil && *req.DateTo != "" {
		if t, parseErr := time.Parse(time.RFC3339, *req.DateTo); parseErr == nil {
			rangeEnd = &t
		}
	}

	if rangeStart != nil || rangeEnd != nil {
		filtered := messages[:0]
		for _, m := range messages {
			if !m.CreatedAt.Valid {
				continue
			}
			t := m.CreatedAt.Time
			if rangeStart != nil && t.Before(*rangeStart) {
				continue
			}
			if rangeEnd != nil && t.After(*rangeEnd) {
				continue
			}
			filtered = append(filtered, m)
		}
		messages = filtered
	}

	// Build markdown snapshot.
	snapshotMd := buildContextSnapshot(req.SourceType, sourceName, messages)

	// Compute actual range from the messages.
	var msgRangeStart, msgRangeEnd pgtype.Timestamptz
	if len(messages) > 0 {
		msgRangeStart = messages[0].CreatedAt
		msgRangeEnd = messages[len(messages)-1].CreatedAt
	}

	ctx, err2 := h.Queries.CreateProjectContext(r.Context(), db.CreateProjectContextParams{
		ProjectID:         parseUUID(projectID),
		SourceType:        req.SourceType,
		SourceID:          parseUUID(req.SourceID),
		SourceName:        strToText(sourceName),
		MessageRangeStart: msgRangeStart,
		MessageRangeEnd:   msgRangeEnd,
		SnapshotMd:        snapshotMd,
		MessageCount:      int32(len(messages)),
		ImportedBy:        parseUUID(userID),
	})
	if err2 != nil {
		slog.Error("failed to save project context", "error", err2)
		writeError(w, http.StatusInternalServerError, "failed to save project context")
		return
	}

	writeJSON(w, http.StatusCreated, projectContextToResponse(ctx))
}

// ListProjectContexts handles GET /api/projects/{projectID}/contexts
// Lists all context records imported for a project.
func (h *Handler) ListProjectContexts(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	contexts, err := h.Queries.ListProjectContexts(r.Context(), parseUUID(projectID))
	if err != nil {
		slog.Error("list project contexts failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list project contexts")
		return
	}

	result := make([]ProjectContextResponse, 0, len(contexts))
	for _, c := range contexts {
		result = append(result, projectContextToResponse(c))
	}
	writeJSON(w, http.StatusOK, result)
}

// buildContextSnapshot formats a slice of messages into a readable markdown snapshot.
func buildContextSnapshot(sourceType, sourceName string, messages []db.Message) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# Context Import: %s", sourceType))
	if sourceName != "" {
		b.WriteString(fmt.Sprintf(" (%s)", sourceName))
	}
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("**Total messages:** %d\n\n", len(messages)))

	if len(messages) == 0 {
		b.WriteString("_No messages found in the specified range._\n")
		return b.String()
	}

	b.WriteString("---\n\n")
	for _, m := range messages {
		timestamp := ""
		if m.CreatedAt.Valid {
			timestamp = m.CreatedAt.Time.Format("2006-01-02 15:04:05")
		}
		senderID := uuidToString(m.SenderID)
		b.WriteString(fmt.Sprintf("**[%s] %s (%s):**\n", timestamp, senderID, m.SenderType))
		if m.Content != "" {
			b.WriteString(m.Content)
		}
		b.WriteString("\n\n")
	}

	return b.String()
}
