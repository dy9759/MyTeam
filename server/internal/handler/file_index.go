package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
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

	// TODO: Replace with sqlc query once file_index table migration exists:
	//   rows, err := h.Queries.ListFileIndex(r.Context(), db.ListFileIndexParams{
	//       WorkspaceID: parseUUID(workspaceID),
	//       SourceType:  sourceType,
	//       ProjectID:   parseUUID(projectID),
	//       ChannelID:   parseUUID(channelID),
	//       OwnerID:     parseUUID(ownerID),
	//   })
	//   if err != nil { ... }

	// Return empty array until file_index table exists.
	writeJSON(w, http.StatusOK, []FileIndexResponse{})
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

	// Step 1: Get user's member ID in this workspace.
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	// Step 2: Get all agent IDs owned by this user in this workspace.
	agents, err := h.Queries.ListAgents(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("failed to list agents for file query", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list files")
		return
	}

	// Collect agent IDs owned by this user.
	ownerIDs := []string{uuidToString(member.UserID)}
	for _, agent := range agents {
		if uuidToString(agent.OwnerID) == userID {
			ownerIDs = append(ownerIDs, uuidToString(agent.ID))
		}
	}

	slog.Debug("listing owner and agent files",
		"workspace_id", workspaceID,
		"user_id", userID,
		"owner_ids_count", len(ownerIDs),
	)

	// TODO: Replace with sqlc query once file_index table migration exists:
	//   rows, err := h.Queries.ListFileIndexByOwnerIDs(r.Context(), db.ListFileIndexByOwnerIDsParams{
	//       WorkspaceID: parseUUID(workspaceID),
	//       OwnerIDs:    ownerUUIDs,
	//   })
	//   if err != nil { ... }

	// Return empty array until file_index table exists.
	writeJSON(w, http.StatusOK, []FileIndexResponse{})
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
