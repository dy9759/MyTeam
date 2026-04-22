package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// File Index Response
// ---------------------------------------------------------------------------

// FileIndexResponse represents a file_index record returned by the API.
// TODO: Replace with sqlc-generated struct once file_index table migration exists.
type FileIndexResponse struct {
	ID                   string  `json:"id"`
	WorkspaceID          string  `json:"workspace_id"`
	UploaderIdentityID   string  `json:"uploader_identity_id"`
	UploaderIdentityType string  `json:"uploader_identity_type"`
	OwnerID              string  `json:"owner_id"`
	SourceType           string  `json:"source_type"`
	SourceID             string  `json:"source_id"`
	FileName             string  `json:"file_name"`
	FileSize             int64   `json:"file_size"`
	ContentType          string  `json:"content_type"`
	StoragePath          string  `json:"storage_path"`
	ChannelID            *string `json:"channel_id,omitempty"`
	ProjectID            *string `json:"project_id,omitempty"`
	CreatedAt            string  `json:"created_at"`
}

func fileIndexToResponse(f db.FileIndex) FileIndexResponse {
	resp := FileIndexResponse{
		ID:                   uuidToString(f.ID),
		WorkspaceID:          uuidToString(f.WorkspaceID),
		UploaderIdentityID:   uuidToString(f.UploaderIdentityID),
		UploaderIdentityType: f.UploaderIdentityType,
		OwnerID:              uuidToString(f.OwnerID),
		SourceType:           f.SourceType,
		SourceID:             uuidToString(f.SourceID),
		FileName:             f.FileName,
		FileSize:             f.FileSize.Int64,
		ContentType:          f.ContentType.String,
		StoragePath:          f.StoragePath.String,
		CreatedAt:            f.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
	if f.ChannelID.Valid {
		s := uuidToString(f.ChannelID)
		resp.ChannelID = &s
	}
	if f.ProjectID.Valid {
		s := uuidToString(f.ProjectID)
		resp.ProjectID = &s
	}
	return resp
}

// ---------------------------------------------------------------------------
// ListFiles — GET /api/files
// Query params: source_type, project_id, channel_id, owner_id
// Returns file_index records filtered by params. Workspace scoped.
// ---------------------------------------------------------------------------

func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	_, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	// Read optional filter params.
	sourceType := r.URL.Query().Get("source_type")
	projectID := r.URL.Query().Get("project_id")
	channelID := r.URL.Query().Get("channel_id")
	ownerID := r.URL.Query().Get("owner_id")

	slog.Debug("listing files",
		"workspace_id", workspaceID,
		"source_type", sourceType,
		"project_id", projectID,
		"channel_id", channelID,
		"owner_id", ownerID,
	)

	// Build a dynamic query with optional filters.
	query := `SELECT id, workspace_id, uploader_identity_id, uploader_identity_type, owner_id,
		source_type, source_id, file_name, file_size, content_type, storage_path,
		channel_id, project_id, created_at
		FROM file_index
		WHERE workspace_id = $1`
	args := []any{parseUUID(workspaceID)}
	argIdx := 2

	if sourceType != "" {
		query += fmt.Sprintf(" AND source_type = $%d", argIdx)
		args = append(args, sourceType)
		argIdx++
	}
	if channelID != "" {
		query += fmt.Sprintf(" AND channel_id = $%d", argIdx)
		args = append(args, parseUUID(channelID))
		argIdx++
	}
	if projectID != "" {
		query += fmt.Sprintf(" AND project_id = $%d", argIdx)
		args = append(args, parseUUID(projectID))
		argIdx++
	}
	if ownerID != "" {
		query += fmt.Sprintf(" AND owner_id = $%d", argIdx)
		args = append(args, parseUUID(ownerID))
		argIdx++
	}
	_ = argIdx // suppress unused variable warning

	query += " ORDER BY created_at DESC LIMIT 200"

	rows, err := h.DB.Query(r.Context(), query, args...)
	if err != nil {
		slog.Error("failed to query file_index", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list files")
		return
	}
	defer rows.Close()

	results := []FileIndexResponse{}
	for rows.Next() {
		var f db.FileIndex
		if err := rows.Scan(
			&f.ID,
			&f.WorkspaceID,
			&f.UploaderIdentityID,
			&f.UploaderIdentityType,
			&f.OwnerID,
			&f.SourceType,
			&f.SourceID,
			&f.FileName,
			&f.FileSize,
			&f.ContentType,
			&f.StoragePath,
			&f.ChannelID,
			&f.ProjectID,
			&f.CreatedAt,
		); err != nil {
			slog.Error("failed to scan file_index row", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list files")
			return
		}
		results = append(results, fileIndexToResponse(f))
	}
	if err := rows.Err(); err != nil {
		slog.Error("file_index rows error", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list files")
		return
	}

	writeJSON(w, http.StatusOK, results)
}

// ---------------------------------------------------------------------------
// ListOwnerAndAgentFiles — GET /api/files/mine
// Returns files where owner_id = current user OR owner_id IN (user's agent IDs).
// ---------------------------------------------------------------------------

func (h *Handler) ListOwnerAndAgentFiles(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	agents, err := h.Queries.ListAgents(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("failed to list agents for file query", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list files")
		return
	}

	// Owner IDs: the user themselves + every agent owned by the user.
	// Both the attachment.uploader_id (for member uploads) and the
	// channel_member.member_id (for member-tier rows) use the user UUID,
	// so we only need that single ID set.
	ownerIDs := []pgtype.UUID{parseUUID(userID)}
	for _, agent := range agents {
		if uuidToString(agent.OwnerID) == userID {
			ownerIDs = append(ownerIDs, agent.ID)
		}
	}

	slog.Debug("listing owner and agent files",
		"workspace_id", workspaceID,
		"user_id", userID,
		"owner_ids_count", len(ownerIDs),
	)

	// Pull every attachment in this workspace that the user can see:
	//   1. They (or one of their agents) uploaded it, OR
	//   2. It is referenced by a chat message in a channel the user belongs to.
	// The LEFT JOIN to message lets us tag chat-origin files with their
	// channel_id and surface the source_type to the UI.
	// The inner query runs DISTINCT ON (a.id) first; the outer query then
	// re-orders by recency and caps the result set so the DB does the work.
	const query = `
		SELECT id, workspace_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes, created_at,
			issue_id, comment_id, channel_id
		FROM (
			SELECT DISTINCT ON (a.id)
				a.id,
				a.workspace_id,
				a.uploader_type,
				a.uploader_id,
				a.filename,
				a.url,
				a.content_type,
				a.size_bytes,
				a.created_at,
				a.issue_id,
				a.comment_id,
				m.channel_id
			FROM attachment a
			LEFT JOIN message m ON m.file_id = a.id
			WHERE a.workspace_id = $1
			  AND (
			    a.uploader_id = ANY($2::uuid[])
			    OR m.channel_id IN (
			      SELECT cm.channel_id
			      FROM channel_member cm
			      WHERE cm.member_id = ANY($2::uuid[])
			    )
			  )
			ORDER BY a.id, a.created_at DESC
		) AS distinct_files
		ORDER BY created_at DESC
		LIMIT 200
	`

	rows, err := h.DB.Query(r.Context(), query, parseUUID(workspaceID), ownerIDs)
	if err != nil {
		slog.Error("failed to query attachments for files page", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list files")
		return
	}
	defer rows.Close()

	results := []FileIndexResponse{}
	for rows.Next() {
		var (
			id, wsID, uploaderID, issueID, commentID, channelID pgtype.UUID
			uploaderType, filename, url, contentType            string
			sizeBytes                                           int64
			createdAt                                           pgtype.Timestamptz
		)
		if err := rows.Scan(
			&id, &wsID, &uploaderType, &uploaderID,
			&filename, &url, &contentType, &sizeBytes, &createdAt,
			&issueID, &commentID, &channelID,
		); err != nil {
			slog.Error("failed to scan attachment row for files page", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list files")
			return
		}

		item := FileIndexResponse{
			ID:                   uuidToString(id),
			WorkspaceID:          uuidToString(wsID),
			UploaderIdentityID:   uuidToString(uploaderID),
			UploaderIdentityType: uploaderType,
			OwnerID:              uuidToString(uploaderID),
			FileName:             filename,
			FileSize:             sizeBytes,
			ContentType:          contentType,
			StoragePath:          url,
			CreatedAt:            createdAt.Time.Format(time.RFC3339),
		}

		switch {
		case channelID.Valid:
			item.SourceType = "chat"
			item.SourceID = uuidToString(channelID)
			cid := uuidToString(channelID)
			item.ChannelID = &cid
		case commentID.Valid:
			item.SourceType = "comment"
			item.SourceID = uuidToString(commentID)
		case issueID.Valid:
			item.SourceType = "issue"
			item.SourceID = uuidToString(issueID)
		default:
			item.SourceType = "upload"
			item.SourceID = uuidToString(id)
		}

		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		slog.Error("attachment rows error for files page", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list files")
		return
	}

	writeJSON(w, http.StatusOK, results)
}

// ---------------------------------------------------------------------------
// GetFilesByProject — GET /api/projects/{projectID}/files
// Returns all files linked to a project.
// ---------------------------------------------------------------------------

func (h *Handler) GetFilesByProject(w http.ResponseWriter, r *http.Request) {
	_, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "project ID is required")
		return
	}

	slog.Debug("listing project files",
		"workspace_id", workspaceID,
		"project_id", projectID,
	)

	// TODO: Replace with sqlc query once file_index table migration exists:
	//   rows, err := h.Queries.ListFileIndexByProject(r.Context(), db.ListFileIndexByProjectParams{
	//       WorkspaceID: parseUUID(workspaceID),
	//       ProjectID:   parseUUID(projectID),
	//   })
	//   if err != nil { ... }

	// Return empty array until file_index table exists.
	writeJSON(w, http.StatusOK, []FileIndexResponse{})
}
