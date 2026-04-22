// Package handler: artifact.go — Artifact HTTP endpoints for the Plan 5
// Project API per PRD §10. Artifacts are versioned outputs of a Task —
// either headless JSONB blobs (text reports, code-patch diffs) or pointers
// to a FileIndex/Snapshot row.
//
// Artifacts are created by SchedulerService when an Execution completes,
// not by API callers. The HTTP surface is read-only: list per-task,
// get single artifact, list reviews. Creation routes deliberately live in
// the daemon execution-complete path.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// GET /api/tasks/{id}/artifacts
// ---------------------------------------------------------------------------

// ListTaskArtifacts returns every artifact bound to the task, newest version
// first. The list is wrapped in {artifacts: [...]} to leave room for
// pagination metadata in a future revision.
func (h *Handler) ListTaskArtifacts(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	rows, err := h.Queries.ListArtifactsByTask(r.Context(), pgUUIDFrom(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed: "+err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, a := range rows {
		out = append(out, artifactToResponse(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": out})
}

// ---------------------------------------------------------------------------
// GET /api/artifacts/{id}
// ---------------------------------------------------------------------------

// GetArtifactHandler returns a single artifact by id. Returns 404 when the
// artifact does not exist.
func (h *Handler) GetArtifactHandler(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	a, err := h.Queries.GetArtifact(r.Context(), pgUUIDFrom(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, artifactToResponse(a))
}

// artifactToResponse maps db.Artifact into a JSON-friendly map. Headless
// artifacts surface their content blob as raw JSON; file-backed artifacts
// expose file_index_id / file_snapshot_id pointers for clients to fetch
// the underlying file separately.
func artifactToResponse(a db.Artifact) map[string]any {
	out := map[string]any{
		"id":              uuidToString(a.ID),
		"task_id":         uuidToString(a.TaskID),
		"run_id":          uuidToString(a.RunID),
		"artifact_type":   a.ArtifactType,
		"version":         a.Version,
		"retention_class": a.RetentionClass,
	}
	if a.SlotID.Valid {
		out["slot_id"] = uuidToString(a.SlotID)
	}
	if a.ExecutionID.Valid {
		out["execution_id"] = uuidToString(a.ExecutionID)
	}
	if a.Title.Valid {
		out["title"] = a.Title.String
	}
	if a.Summary.Valid {
		out["summary"] = a.Summary.String
	}
	if len(a.Content) > 0 {
		out["content"] = json.RawMessage(a.Content)
	}
	if a.FileIndexID.Valid {
		out["file_index_id"] = uuidToString(a.FileIndexID)
	}
	if a.FileSnapshotID.Valid {
		out["file_snapshot_id"] = uuidToString(a.FileSnapshotID)
	}
	if a.CreatedByID.Valid {
		out["created_by_id"] = uuidToString(a.CreatedByID)
	}
	if a.CreatedByType.Valid {
		out["created_by_type"] = a.CreatedByType.String
	}
	if a.CreatedAt.Valid {
		out["created_at"] = a.CreatedAt.Time
	}
	return out
}
