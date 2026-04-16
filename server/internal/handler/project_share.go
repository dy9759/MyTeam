package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ShareResponse is the JSON response for a project share record.
type ShareResponse struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	OwnerID    string `json:"owner_id"`
	Role       string `json:"role"`
	CanMergePR bool   `json:"can_merge_pr"`
	GrantedBy  string `json:"granted_by"`
	GrantedAt  string `json:"granted_at"`
}

func shareToResponse(s db.ProjectShare) ShareResponse {
	return ShareResponse{
		ID:         uuidToString(s.ID),
		ProjectID:  uuidToString(s.ProjectID),
		OwnerID:    uuidToString(s.OwnerID),
		Role:       s.Role,
		CanMergePR: s.CanMergePr,
		GrantedBy:  uuidToString(s.GrantedBy),
		GrantedAt:  s.GrantedAt.Time.Format(time.RFC3339),
	}
}

// ShareProject handles POST /api/projects/{projectID}/share
func (h *Handler) ShareProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	type ShareRequest struct {
		OwnerID    string `json:"owner_id"`
		Role       string `json:"role"`
		CanMergePR bool   `json:"can_merge_pr"`
	}

	var req ShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.OwnerID == "" {
		writeError(w, http.StatusBadRequest, "owner_id is required")
		return
	}
	if req.Role == "" {
		req.Role = "viewer"
	}
	if req.Role != "viewer" && req.Role != "editor" {
		writeError(w, http.StatusBadRequest, "role must be viewer or editor")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Verify caller is project creator.
	project, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
		} else {
			slog.Error("get project for share failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get project")
		}
		return
	}

	if uuidToString(project.CreatorOwnerID) != userID {
		writeError(w, http.StatusForbidden, "only the project creator can share this project")
		return
	}

	share, err := h.Queries.CreateProjectShare(r.Context(), db.CreateProjectShareParams{
		ProjectID:  parseUUID(projectID),
		OwnerID:    parseUUID(req.OwnerID),
		Role:       req.Role,
		CanMergePr: req.CanMergePR,
		GrantedBy:  parseUUID(userID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "project is already shared with this user")
			return
		}
		slog.Error("create project share failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to share project")
		return
	}

	writeJSON(w, http.StatusCreated, shareToResponse(share))
}

// ListProjectShares handles GET /api/projects/{projectID}/shares
func (h *Handler) ListProjectShares(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	shares, err := h.Queries.ListProjectShares(r.Context(), parseUUID(projectID))
	if err != nil {
		slog.Error("list project shares failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list project shares")
		return
	}

	result := make([]ShareResponse, 0, len(shares))
	for _, s := range shares {
		result = append(result, shareToResponse(s))
	}
	writeJSON(w, http.StatusOK, result)
}

// RemoveProjectShare handles DELETE /api/projects/{projectID}/share/{ownerID}
func (h *Handler) RemoveProjectShare(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	ownerID := chi.URLParam(r, "ownerID")
	if projectID == "" || ownerID == "" {
		writeError(w, http.StatusBadRequest, "projectID and ownerID are required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Verify caller is project creator.
	project, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
		} else {
			slog.Error("get project for remove share failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get project")
		}
		return
	}

	if uuidToString(project.CreatorOwnerID) != userID {
		writeError(w, http.StatusForbidden, "only the project creator can remove shares")
		return
	}

	if err := h.Queries.DeleteProjectShare(r.Context(), db.DeleteProjectShareParams{
		ProjectID: parseUUID(projectID),
		OwnerID:   parseUUID(ownerID),
	}); err != nil {
		slog.Error("delete project share failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to remove project share")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
