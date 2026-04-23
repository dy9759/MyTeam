package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
	"github.com/MyAIOSHub/MyTeam/server/pkg/protocol"
)

// ---------- Response types ----------

// ProjectPRResponse is the JSON response for a project PR.
type ProjectPRResponse struct {
	ID              string  `json:"id"`
	ProjectID       string  `json:"project_id"`
	SourceBranchID  string  `json:"source_branch_id"`
	TargetBranchID  string  `json:"target_branch_id"`
	SourceVersionID string  `json:"source_version_id"`
	Title           string  `json:"title"`
	Description     *string `json:"description"`
	Status          string  `json:"status"`
	HasConflicts    bool    `json:"has_conflicts"`
	MergedVersionID *string `json:"merged_version_id"`
	CreatedBy       string  `json:"created_by"`
	MergedBy        *string `json:"merged_by"`
	MergedAt        *string `json:"merged_at"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

func prToResponse(pr db.ProjectPr) ProjectPRResponse {
	return ProjectPRResponse{
		ID:              uuidToString(pr.ID),
		ProjectID:       uuidToString(pr.ProjectID),
		SourceBranchID:  uuidToString(pr.SourceBranchID),
		TargetBranchID:  uuidToString(pr.TargetBranchID),
		SourceVersionID: uuidToString(pr.SourceVersionID),
		Title:           pr.Title,
		Description:     textToPtr(pr.Description),
		Status:          pr.Status,
		HasConflicts:    pr.HasConflicts,
		MergedVersionID: uuidToPtr(pr.MergedVersionID),
		CreatedBy:       uuidToString(pr.CreatedBy),
		MergedBy:        uuidToPtr(pr.MergedBy),
		MergedAt:        timestampToPtr(pr.MergedAt),
		CreatedAt:       pr.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:       pr.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// ---------- Handlers ----------

// CreateProjectPR handles POST /api/projects/{projectID}/prs
func (h *Handler) CreateProjectPR(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	type CreatePRRequest struct {
		SourceBranchID  string  `json:"source_branch_id"`
		TargetBranchID  string  `json:"target_branch_id"`
		SourceVersionID string  `json:"source_version_id"`
		Title           string  `json:"title"`
		Description     *string `json:"description"`
	}

	var req CreatePRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.SourceBranchID == "" || req.TargetBranchID == "" || req.SourceVersionID == "" {
		writeError(w, http.StatusBadRequest, "source_branch_id, target_branch_id, and source_version_id are required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	pr, err := h.Queries.CreateProjectPR(r.Context(), db.CreateProjectPRParams{
		ProjectID:       parseUUID(projectID),
		SourceBranchID:  parseUUID(req.SourceBranchID),
		TargetBranchID:  parseUUID(req.TargetBranchID),
		SourceVersionID: parseUUID(req.SourceVersionID),
		Title:           req.Title,
		Description:     ptrToText(req.Description),
		CreatedBy:       parseUUID(userID),
	})
	if err != nil {
		slog.Error("create project PR failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create PR")
		return
	}

	resp := prToResponse(pr)

	h.publish(protocol.EventProjectPRCreated, workspaceID, "member", userID, map[string]any{
		"pr": resp,
	})

	writeJSON(w, http.StatusCreated, resp)
}

// ListProjectPRs handles GET /api/projects/{projectID}/prs
func (h *Handler) ListProjectPRs(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	prs, err := h.Queries.ListProjectPRs(r.Context(), parseUUID(projectID))
	if err != nil {
		slog.Error("list project PRs failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list PRs")
		return
	}

	result := make([]ProjectPRResponse, 0, len(prs))
	for _, pr := range prs {
		result = append(result, prToResponse(pr))
	}
	writeJSON(w, http.StatusOK, result)
}

// GetProjectPR handles GET /api/projects/{projectID}/prs/{prID}
func (h *Handler) GetProjectPR(w http.ResponseWriter, r *http.Request) {
	prID := chi.URLParam(r, "prID")
	if prID == "" {
		writeError(w, http.StatusBadRequest, "prID is required")
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	pr, err := h.Queries.GetProjectPR(r.Context(), parseUUID(prID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "PR not found")
		} else {
			slog.Error("get project PR failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get PR")
		}
		return
	}

	writeJSON(w, http.StatusOK, prToResponse(pr))
}

// MergeProjectPR handles POST /api/projects/{projectID}/prs/{prID}/merge
// Creates a new version on the target branch copying snapshots from the source version,
// then marks the PR as merged and the source branch as merged.
func (h *Handler) MergeProjectPR(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	prID := chi.URLParam(r, "prID")
	if projectID == "" || prID == "" {
		writeError(w, http.StatusBadRequest, "projectID and prID are required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	// Load the PR.
	pr, err := h.Queries.GetProjectPR(r.Context(), parseUUID(prID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "PR not found")
		} else {
			slog.Error("get project PR for merge failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get PR")
		}
		return
	}

	// Verify PR is open and has no conflicts.
	if pr.Status != "open" {
		writeError(w, http.StatusBadRequest, "only open PRs can be merged")
		return
	}
	if pr.HasConflicts {
		writeError(w, http.StatusBadRequest, "PR has conflicts and cannot be merged")
		return
	}

	// Get source version so the merged version can point back to it.
	sourceVersion, err := h.Queries.GetProjectVersion(r.Context(), pr.SourceVersionID)
	if err != nil {
		slog.Error("get source version for merge failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get source version")
		return
	}

	// Get latest version number to determine next number.
	latestVersion, err := h.Queries.GetLatestProjectVersion(r.Context(), parseUUID(projectID))
	var nextVersionNumber int32 = 1
	if err == nil {
		nextVersionNumber = latestVersion.VersionNumber + 1
	} else if !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("get latest version for merge failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get latest version")
		return
	}

	// Get target branch to use its name.
	targetBranch, err := h.Queries.GetProjectBranch(r.Context(), pr.TargetBranchID)
	if err != nil {
		slog.Error("get target branch for merge failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get target branch")
		return
	}

	// Legacy snapshots were removed in migration 059. The merged version
	// now carries forward version lineage plus target branch naming, which
	// is enough for the current API surface.
	newVersion, err := h.Queries.CreateProjectVersion(r.Context(), db.CreateProjectVersionParams{
		ProjectID:       parseUUID(projectID),
		ParentVersionID: sourceVersion.ID,
		VersionNumber:   nextVersionNumber,
		BranchName:      strToText(targetBranch.Name),
		ForkReason:      strToText("merge from PR: " + pr.Title),
		CreatedBy:       parseUUID(userID),
	})
	if err != nil {
		slog.Error("create merged version failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create merged version")
		return
	}

	// Mark PR as merged.
	if err := h.Queries.MergeProjectPR(r.Context(), db.MergeProjectPRParams{
		MergedVersionID: newVersion.ID,
		MergedBy:        parseUUID(userID),
		ID:              pr.ID,
	}); err != nil {
		slog.Error("update PR to merged failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update PR status")
		return
	}

	// Mark source branch as merged.
	if err := h.Queries.UpdateProjectBranchStatus(r.Context(), db.UpdateProjectBranchStatusParams{
		Status: "merged",
		ID:     pr.SourceBranchID,
	}); err != nil {
		slog.Warn("failed to update source branch status to merged", "branch_id", uuidToString(pr.SourceBranchID), "error", err)
	}

	// Reload PR to return updated state.
	updatedPR, err := h.Queries.GetProjectPR(r.Context(), pr.ID)
	if err != nil {
		slog.Error("reload merged PR failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to reload PR")
		return
	}

	resp := prToResponse(updatedPR)

	h.publish(protocol.EventProjectPRMerged, workspaceID, "member", userID, map[string]any{
		"pr": resp,
	})

	writeJSON(w, http.StatusOK, resp)
}

// CloseProjectPR handles POST /api/projects/{projectID}/prs/{prID}/close
func (h *Handler) CloseProjectPR(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	prID := chi.URLParam(r, "prID")
	if projectID == "" || prID == "" {
		writeError(w, http.StatusBadRequest, "projectID and prID are required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	// Load the PR to verify it exists and is open.
	pr, err := h.Queries.GetProjectPR(r.Context(), parseUUID(prID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "PR not found")
		} else {
			slog.Error("get project PR for close failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get PR")
		}
		return
	}

	if pr.Status != "open" {
		writeError(w, http.StatusBadRequest, "only open PRs can be closed")
		return
	}

	if err := h.Queries.CloseProjectPR(r.Context(), pr.ID); err != nil {
		slog.Error("close project PR failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to close PR")
		return
	}

	// Reload to return updated state.
	updatedPR, err := h.Queries.GetProjectPR(r.Context(), pr.ID)
	if err != nil {
		slog.Error("reload closed PR failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to reload PR")
		return
	}

	resp := prToResponse(updatedPR)

	h.publish(protocol.EventProjectPRClosed, workspaceID, "member", userID, map[string]any{
		"pr": resp,
	})

	// Suppress unused variable warning.
	_ = projectID

	writeJSON(w, http.StatusOK, resp)
}
