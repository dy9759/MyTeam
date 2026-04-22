// Package handler: review.go — Review HTTP endpoints for the Plan 5
// Project API per PRD §10. Reviews are decisions (approve / request_changes
// / reject) on a single Artifact version, optionally tied to the
// human_review slot whose decision they represent.
//
// Posting a review goes through ReviewService.Submit which cascades into
// SlotService.ApplyReviewDecision and updates the parent Task status —
// see service/review.go for the full state machine.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MyAIOSHub/MyTeam/server/internal/service"
	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// GET /api/tasks/{id}/executions
// ---------------------------------------------------------------------------

// ListTaskExecutions returns every Execution row for the task, newest
// attempt first. Wraps in {executions: [...]} for forward-compat.
//
// Lives in review.go because it shares the executionToResponse helper from
// daemon_executions.go and the daemon file already has a top-level docs
// header about the daemon path.
func (h *Handler) ListTaskExecutions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	rows, err := h.Queries.ListExecutionsByTask(r.Context(), pgUUIDFrom(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed: "+err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, e := range rows {
		out = append(out, executionToResponse(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"executions": out})
}

// ---------------------------------------------------------------------------
// GET /api/artifacts/{id}/reviews
// ---------------------------------------------------------------------------

// ListArtifactReviews returns the chronological review history for an
// artifact, newest first.
func (h *Handler) ListArtifactReviews(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	rows, err := h.Queries.ListReviewsForArtifact(r.Context(), pgUUIDFrom(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed: "+err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, rv := range rows {
		out = append(out, reviewToResponse(rv))
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": out})
}

// ---------------------------------------------------------------------------
// POST /api/artifacts/{id}/reviews
// ---------------------------------------------------------------------------

// createReviewRequest is the JSON body for POST /api/artifacts/{id}/reviews.
// task_id is required because Review writes against task state via
// ReviewService.Submit; slot_id is optional and only needed when the review
// is the verdict on a specific human_review slot.
type createReviewRequest struct {
	TaskID   string `json:"task_id"`
	SlotID   string `json:"slot_id,omitempty"`
	Decision string `json:"decision"`
	Comment  string `json:"comment,omitempty"`
}

// CreateReviewHandler wires the HTTP request into ReviewService.Submit and
// returns the resulting review row plus the new task status (so the UI can
// reflect the cascade without a follow-up GET).
func (h *Handler) CreateReviewHandler(w http.ResponseWriter, r *http.Request) {
	artifactID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	// Load artifact so we can:
	//   - return 404 instead of a generic validation error from ReviewService
	//   - derive the authoritative task_id from artifact.task_id rather than
	//     trusting the client's req.TaskID (otherwise a review on artifact A
	//     could mutate task B's status via the cascade in ReviewService.Submit)
	artifact, err := h.Queries.GetArtifact(r.Context(), pgUUIDFrom(artifactID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "artifact not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get artifact failed: "+err.Error())
		return
	}

	var req createReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	// req.TaskID is optional but, when supplied, must match the artifact's
	// owning task. Reject mismatches so a malicious or buggy client can't
	// trick the cascade into mutating an unrelated task.
	if req.TaskID != "" {
		clientTaskID, err := uuid.Parse(req.TaskID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid task_id")
			return
		}
		if clientTaskID != uuid.UUID(artifact.TaskID.Bytes) {
			writeError(w, http.StatusBadRequest, "task_id does not match artifact owner")
			return
		}
	}
	taskID := uuid.UUID(artifact.TaskID.Bytes)
	if req.Decision == "" {
		writeError(w, http.StatusBadRequest, "decision required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	reviewerID, err := uuid.Parse(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var slotID uuid.UUID
	if req.SlotID != "" {
		slotID, err = uuid.Parse(req.SlotID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid slot_id")
			return
		}
	}

	if h.Reviews == nil {
		writeError(w, http.StatusInternalServerError, "review service unavailable")
		return
	}
	result, err := h.Reviews.Submit(r.Context(), service.SubmitReviewRequest{
		TaskID:       taskID,
		ArtifactID:   artifactID,
		SlotID:       slotID,
		ReviewerID:   reviewerID,
		ReviewerType: service.ReviewerTypeMember,
		Decision:     req.Decision,
		Comment:      req.Comment,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"review":          reviewToResponse(result.Review),
		"task_new_status": result.TaskNewStatus,
	})
}

// reviewToResponse maps db.Review into a JSON-friendly map. Mirrors the
// apps/web/shared/types Review interface (Batch E1).
func reviewToResponse(r db.Review) map[string]any {
	out := map[string]any{
		"id":          uuidToString(r.ID),
		"task_id":     uuidToString(r.TaskID),
		"artifact_id": uuidToString(r.ArtifactID),
		"reviewer_id": uuidToString(r.ReviewerID),
		"decision":    r.Decision,
	}
	if r.SlotID.Valid {
		out["slot_id"] = uuidToString(r.SlotID)
	}
	if r.ReviewerType.Valid {
		out["reviewer_type"] = r.ReviewerType.String
	}
	if r.Comment.Valid {
		out["comment"] = r.Comment.String
	}
	if r.CreatedAt.Valid {
		out["created_at"] = r.CreatedAt.Time
	}
	return out
}
