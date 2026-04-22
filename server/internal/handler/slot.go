// Package handler: slot.go — ParticipantSlot HTTP endpoints for the Plan 5
// Project API per PRD §10. Slots represent the human/agent participation
// hand-offs inside a Task (human input forms, agent execution stages,
// human review gates).
//
// Slot lifecycle (waiting → ready → in_progress → submitted → approved/...)
// is owned by SlotService — these handlers only expose list + create.
// State transitions happen via SchedulerService (activation) or
// ReviewService (decision cascade).
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// GET /api/tasks/{id}/slots
// ---------------------------------------------------------------------------

// ListTaskSlots returns every slot bound to the given task, ordered by
// slot_order then created_at.
func (h *Handler) ListTaskSlots(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	rows, err := h.Queries.ListSlotsByTask(r.Context(), pgUUIDFrom(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed: "+err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, s := range rows {
		out = append(out, slotToResponse(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"slots": out})
}

// ---------------------------------------------------------------------------
// POST /api/tasks/{id}/slots
// ---------------------------------------------------------------------------

// createSlotRequest mirrors the frontend client.ts CreateParticipantSlot
// shape from Batch E1. Pointer fields let the caller omit Blocking /
// Required without forcing them to false (the SQL defaults are TRUE).
type createSlotRequest struct {
	SlotType        string `json:"slot_type"`
	SlotOrder       int    `json:"slot_order,omitempty"`
	ParticipantID   string `json:"participant_id,omitempty"`
	ParticipantType string `json:"participant_type,omitempty"`
	Responsibility  string `json:"responsibility,omitempty"`
	Trigger         string `json:"trigger,omitempty"`
	Blocking        *bool  `json:"blocking,omitempty"`
	Required        *bool  `json:"required,omitempty"`
	ExpectedOutput  string `json:"expected_output,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty"`
}

// CreateTaskSlot inserts a new ParticipantSlot on the task. The slot starts
// in 'waiting' (SQL default) and is later promoted by SchedulerService.
func (h *Handler) CreateTaskSlot(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req createSlotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.SlotType == "" {
		writeError(w, http.StatusBadRequest, "slot_type required")
		return
	}

	params := db.CreateParticipantSlotParams{
		TaskID:    pgUUIDFrom(taskID),
		SlotType:  req.SlotType,
		SlotOrder: int4Of(int32(req.SlotOrder)),
	}
	if req.ParticipantID != "" {
		pID, err := uuid.Parse(req.ParticipantID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid participant_id")
			return
		}
		params.ParticipantID = pgUUIDFrom(pID)
	}
	if req.ParticipantType != "" {
		params.ParticipantType = textOf(req.ParticipantType)
	}
	if req.Responsibility != "" {
		params.Responsibility = textOf(req.Responsibility)
	}
	if req.Trigger != "" {
		params.Trigger = textOf(req.Trigger)
	}
	if req.Blocking != nil {
		params.Blocking = pgtype.Bool{Bool: *req.Blocking, Valid: true}
	}
	if req.Required != nil {
		params.Required = pgtype.Bool{Bool: *req.Required, Valid: true}
	}
	if req.ExpectedOutput != "" {
		params.ExpectedOutput = textOf(req.ExpectedOutput)
	}
	if req.TimeoutSeconds > 0 {
		params.TimeoutSeconds = int4Of(int32(req.TimeoutSeconds))
	}

	s, err := h.Queries.CreateParticipantSlot(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create slot failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, slotToResponse(s))
}

// ---------------------------------------------------------------------------
// POST /api/slots/{id}/submit
// ---------------------------------------------------------------------------

type submitSlotInputRequest struct {
	Content json.RawMessage `json:"content"`
	Comment string          `json:"comment,omitempty"`
}

// SubmitSlotInput records a human_input slot submission and resumes the parent
// task when it was waiting on that human handoff.
func (h *Handler) SubmitSlotInput(w http.ResponseWriter, r *http.Request) {
	slotID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req submitSlotInputRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(req.Content) == 0 {
		writeError(w, http.StatusBadRequest, "content required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	slot, err := h.Queries.GetSlot(r.Context(), pgUUIDFrom(slotID))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "slot not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get slot failed: "+err.Error())
		return
	}
	if slot.SlotType != service.SlotTypeHumanInput {
		writeError(w, http.StatusBadRequest, "slot is not human input")
		return
	}
	if !slot.ParticipantType.Valid || slot.ParticipantType.String != "member" || !slot.ParticipantID.Valid {
		writeError(w, http.StatusForbidden, "slot is not assigned to a member")
		return
	}
	if uuid.UUID(slot.ParticipantID.Bytes) != userUUID {
		writeError(w, http.StatusForbidden, "slot is assigned to another participant")
		return
	}

	task, err := h.Queries.GetTask(r.Context(), slot.TaskID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get task failed: "+err.Error())
		return
	}
	if workspaceID := resolveWorkspaceID(r); workspaceID != "" && uuidToString(task.WorkspaceID) != workspaceID {
		writeError(w, http.StatusForbidden, "slot is outside the current workspace")
		return
	}

	if h.Slots == nil {
		writeError(w, http.StatusInternalServerError, "slot service unavailable")
		return
	}
	if h.TxStarter == nil {
		writeError(w, http.StatusInternalServerError, "transaction support unavailable")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "begin tx failed: "+err.Error())
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	txQueries := h.Queries.WithTx(tx)
	txSlots := service.NewSlotService(txQueries)
	updated, err := txSlots.SubmitHumanInput(r.Context(), slotID, userUUID, []byte(req.Content), req.Comment)
	if err != nil {
		if errors.Is(err, service.ErrSlotInvalidTransition) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	task, err = txQueries.GetTask(r.Context(), updated.TaskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get task failed: "+err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "commit tx failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slot":            slotToResponse(*updated),
		"task_new_status": task.Status,
	})
}

// ListSlotSubmissions returns append-only human input submission history
// for the given slot, newest first.
func (h *Handler) ListSlotSubmissions(w http.ResponseWriter, r *http.Request) {
	slotID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	slot, err := h.Queries.GetSlot(r.Context(), pgUUIDFrom(slotID))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "slot not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get slot failed: "+err.Error())
		return
	}

	task, err := h.Queries.GetTask(r.Context(), slot.TaskID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get task failed: "+err.Error())
		return
	}
	if workspaceID := resolveWorkspaceID(r); workspaceID != "" && uuidToString(task.WorkspaceID) != workspaceID {
		writeError(w, http.StatusForbidden, "slot is outside the current workspace")
		return
	}

	rows, err := h.Queries.ListParticipantSlotSubmissions(r.Context(), pgUUIDFrom(slotID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list submissions failed: "+err.Error())
		return
	}

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, slotSubmissionToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"submissions": out})
}

// slotToResponse maps a db.ParticipantSlot into a JSON-friendly map. Mirrors
// the apps/web/shared/types ParticipantSlot interface (Batch E1).
func slotToResponse(s db.ParticipantSlot) map[string]any {
	out := map[string]any{
		"id":         uuidToString(s.ID),
		"task_id":    uuidToString(s.TaskID),
		"slot_type":  s.SlotType,
		"slot_order": s.SlotOrder,
		"trigger":    s.Trigger,
		"blocking":   s.Blocking,
		"required":   s.Required,
		"status":     s.Status,
	}
	if s.ParticipantID.Valid {
		out["participant_id"] = uuidToString(s.ParticipantID)
	}
	if s.ParticipantType.Valid {
		out["participant_type"] = s.ParticipantType.String
	}
	if s.Responsibility.Valid {
		out["responsibility"] = s.Responsibility.String
	}
	if s.ExpectedOutput.Valid {
		out["expected_output"] = s.ExpectedOutput.String
	}
	if len(s.Content) > 0 {
		out["content"] = json.RawMessage(s.Content)
	}
	if s.TimeoutSeconds.Valid {
		out["timeout_seconds"] = s.TimeoutSeconds.Int32
	}
	if s.StartedAt.Valid {
		out["started_at"] = s.StartedAt.Time
	}
	if s.CompletedAt.Valid {
		out["completed_at"] = s.CompletedAt.Time
	}
	if s.CreatedAt.Valid {
		out["created_at"] = s.CreatedAt.Time
	}
	if s.UpdatedAt.Valid {
		out["updated_at"] = s.UpdatedAt.Time
	}
	return out
}

func slotSubmissionToResponse(s db.ParticipantSlotSubmission) map[string]any {
	out := map[string]any{
		"id":         uuidToString(s.ID),
		"slot_id":    uuidToString(s.SlotID),
		"task_id":    uuidToString(s.TaskID),
		"content":    json.RawMessage(s.Content),
		"created_at": timestampToString(s.CreatedAt),
	}
	if s.RunID.Valid {
		out["run_id"] = uuidToString(s.RunID)
	}
	if s.SubmittedBy.Valid {
		out["submitted_by"] = uuidToString(s.SubmittedBy)
	}
	if s.Comment.Valid {
		out["comment"] = s.Comment.String
	}
	return out
}
