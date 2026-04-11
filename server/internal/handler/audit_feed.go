package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
)

type WorkspaceAuditEntryResponse struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	IssueID     *string         `json:"issue_id,omitempty"`
	ActorType   string          `json:"actor_type"`
	ActorID     *string         `json:"actor_id,omitempty"`
	Action      string          `json:"action"`
	Details     json.RawMessage `json:"details"`
	CreatedAt   string          `json:"created_at"`
}

// ListWorkspaceActivity returns the latest workspace-scoped audit log entries.
func (h *Handler) ListWorkspaceActivity(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if h.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "audit feed unavailable")
		return
	}

	limit := queryInt(r, "limit", 50)
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT id, workspace_id, issue_id, actor_type, actor_id, action, details, created_at
		FROM activity_log
		WHERE workspace_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, parseUUID(workspaceID), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load audit log")
		return
	}
	defer rows.Close()

	resp := make([]WorkspaceAuditEntryResponse, 0, limit)
	for rows.Next() {
		var (
			id        pgtype.UUID
			wsID      pgtype.UUID
			issueID   pgtype.UUID
			actorType pgtype.Text
			actorID   pgtype.UUID
			action    string
			details   []byte
			createdAt pgtype.Timestamptz
		)
		if err := rows.Scan(&id, &wsID, &issueID, &actorType, &actorID, &action, &details, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read audit log")
			return
		}
		if len(details) == 0 {
			details = []byte("{}")
		}
		resp = append(resp, WorkspaceAuditEntryResponse{
			ID:          uuidToString(id),
			WorkspaceID: uuidToString(wsID),
			IssueID:     uuidToPtr(issueID),
			ActorType:   actorType.String,
			ActorID:     uuidToPtr(actorID),
			Action:      action,
			Details:     json.RawMessage(details),
			CreatedAt:   timestampToString(createdAt),
		})
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load audit log")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"entries": resp})
}
