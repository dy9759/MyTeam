// Package handler: task.go — Task HTTP endpoints for the Plan 5 Project
// API per PRD §10. Tasks are the unit of agent execution: they belong to
// a Plan, optionally to a ProjectRun, and own their ParticipantSlot /
// Execution / Artifact / Review children.
//
// The lifecycle (draft → ready → queued → running → completed/needs_*)
// is managed by SchedulerService — these handlers only expose the CRUD
// surface plus a thin "cancel" verb. Other status transitions must flow
// through the scheduler so slot + run state stays consistent.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// GET /api/plans/{planID}/tasks
// ---------------------------------------------------------------------------

// ListTasksByPlan returns every Task on a plan, ordered by step_order.
// Wraps the rows in {tasks: [...]} for forward-compatibility (Plan 5 may
// add pagination metadata alongside the list).
func (h *Handler) ListTasksByPlan(w http.ResponseWriter, r *http.Request) {
	planID, err := uuid.Parse(chi.URLParam(r, "planID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan id")
		return
	}
	rows, err := h.Queries.ListTasksByPlan(r.Context(), pgUUIDFrom(planID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed: "+err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		out = append(out, projectTaskToResponse(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

// ---------------------------------------------------------------------------
// POST /api/tasks
// ---------------------------------------------------------------------------

// createTaskRequest is the JSON body for POST /api/tasks. Mirrors the
// frontend client.ts CreateTaskRequest from Batch E1 — keep field names in
// sync. workspace_id is resolved from the plan to avoid the caller faking
// cross-workspace inserts.
type createTaskRequest struct {
	PlanID             string   `json:"plan_id"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	StepOrder          int      `json:"step_order,omitempty"`
	DependsOn          []string `json:"depends_on,omitempty"`
	PrimaryAssigneeID  string   `json:"primary_assignee_id,omitempty"`
	FallbackAgentIDs   []string `json:"fallback_agent_ids,omitempty"`
	RequiredSkills     []string `json:"required_skills,omitempty"`
	CollaborationMode  string   `json:"collaboration_mode,omitempty"`
	AcceptanceCriteria string   `json:"acceptance_criteria,omitempty"`
}

// CreateTaskHandler creates a Task tied to an existing Plan. The new task
// starts in 'draft' (the SQL default) — SchedulerService promotes it to
// ready/queued when ScheduleRun runs. workspace_id is copied from the plan.
func (h *Handler) CreateTaskHandler(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.PlanID == "" || req.Title == "" {
		writeError(w, http.StatusBadRequest, "plan_id and title required")
		return
	}

	planID, err := uuid.Parse(req.PlanID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan_id")
		return
	}

	plan, err := h.Queries.GetPlan(r.Context(), pgUUIDFrom(planID))
	if err != nil {
		writeError(w, http.StatusNotFound, "plan not found")
		return
	}

	dependsOn, err := parseUUIDList(req.DependsOn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid depends_on uuid")
		return
	}
	fallbackIDs, err := parseUUIDList(req.FallbackAgentIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid fallback_agent_id")
		return
	}

	requiredSkills := req.RequiredSkills
	if requiredSkills == nil {
		requiredSkills = []string{}
	}

	params := db.CreateTaskParams{
		PlanID:             pgUUIDFrom(planID),
		WorkspaceID:        plan.WorkspaceID,
		Title:              req.Title,
		Description:        strToText(req.Description),
		StepOrder:          int4Of(int32(req.StepOrder)),
		DependsOn:          dependsOn,
		FallbackAgentIds:   fallbackIDs,
		RequiredSkills:     requiredSkills,
		AcceptanceCriteria: strToText(req.AcceptanceCriteria),
	}
	if req.PrimaryAssigneeID != "" {
		pID, err := uuid.Parse(req.PrimaryAssigneeID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid primary_assignee_id")
			return
		}
		params.PrimaryAssigneeID = pgUUIDFrom(pID)
	}
	if req.CollaborationMode != "" {
		params.CollaborationMode = textOf(req.CollaborationMode)
	}

	task, err := h.Queries.CreateTask(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, projectTaskToResponse(task))
}

// ---------------------------------------------------------------------------
// GET /api/tasks/{id}
// ---------------------------------------------------------------------------

// GetTaskHandler returns a single Task by id.
func (h *Handler) GetTaskHandler(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	t, err := h.Queries.GetTask(r.Context(), pgUUIDFrom(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get failed")
		return
	}
	writeJSON(w, http.StatusOK, projectTaskToResponse(t))
}

// ---------------------------------------------------------------------------
// PATCH /api/tasks/{id}
// ---------------------------------------------------------------------------

// updateTaskRequest carries edits from the plan stepper UI and the
// cancel path. Status transitions beyond "cancelled" still flow
// through SchedulerService so run / slot / artifact state stays
// consistent; title / description / primary_assignee / required_skills
// / acceptance_criteria are user-level metadata and are safe to edit
// in-place via UpdateTaskFields.
type updateTaskRequest struct {
	Status             *string   `json:"status,omitempty"`
	Title              *string   `json:"title,omitempty"`
	Description        *string   `json:"description,omitempty"`
	PrimaryAssigneeID  *string   `json:"primary_assignee_id,omitempty"`
	RequiredSkills     *[]string `json:"required_skills,omitempty"`
	AcceptanceCriteria *string   `json:"acceptance_criteria,omitempty"`
}

// UpdateTaskHandler handles PATCH /api/tasks/{id}. It accepts the
// small surface of fields the plan-stepper UI exposes plus the
// status=cancelled cancellation path; every other status transition
// must go through scheduler endpoints (POST /api/runs/{id}/start etc).
func (h *Handler) UpdateTaskHandler(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req updateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	// Cancel branch — unchanged.
	if req.Status != nil {
		if *req.Status != "cancelled" {
			writeError(w, http.StatusBadRequest, "PATCH only supports status=cancelled; other transitions go through scheduler")
			return
		}
		t, err := h.Queries.UpdateTaskStatus(r.Context(), db.UpdateTaskStatusParams{
			ID:     pgUUIDFrom(id),
			Status: "cancelled",
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "update failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, projectTaskToResponse(t))
		return
	}

	// Field-edit branch — reject empty bodies so the client gets a
	// clear error rather than a silent no-op.
	if req.Title == nil &&
		req.Description == nil &&
		req.PrimaryAssigneeID == nil &&
		req.RequiredSkills == nil &&
		req.AcceptanceCriteria == nil {
		writeError(w, http.StatusBadRequest, "nothing to update")
		return
	}

	params := db.UpdateTaskFieldsParams{ID: pgUUIDFrom(id)}
	if req.Title != nil {
		params.Title = textOf(*req.Title)
	}
	if req.Description != nil {
		params.Description = textOf(*req.Description)
	}
	if req.PrimaryAssigneeID != nil {
		if *req.PrimaryAssigneeID == "" {
			params.PrimaryAssigneeID = pgtype.UUID{}
		} else {
			assigneeID, err := uuid.Parse(*req.PrimaryAssigneeID)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid primary_assignee_id")
				return
			}
			params.PrimaryAssigneeID = pgUUIDFrom(assigneeID)
		}
	}
	if req.RequiredSkills != nil {
		params.RequiredSkills = *req.RequiredSkills
	}
	if req.AcceptanceCriteria != nil {
		params.AcceptanceCriteria = textOf(*req.AcceptanceCriteria)
	}

	t, err := h.Queries.UpdateTaskFields(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "update failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, projectTaskToResponse(t))
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// parseUUIDList converts a slice of UUID strings to pgtype.UUID values.
// Returns an error if any element fails to parse so callers can surface a
// 400 instead of silently dropping ids.
func parseUUIDList(in []string) ([]pgtype.UUID, error) {
	out := make([]pgtype.UUID, 0, len(in))
	for _, s := range in {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, err
		}
		out = append(out, pgUUIDFrom(id))
	}
	return out, nil
}

// projectTaskToResponse maps a db.Task into the JSON shape the frontend
// consumes. Mirrors the apps/web/shared/types Task interface (Batch E1).
//
// Named projectTaskToResponse (not taskToResponse) because the legacy
// AgentTaskQueue handler in agent.go already owns that name. The two
// "tasks" are distinct concepts: AgentTaskQueue is an issue→agent
// dispatch row, while db.Task is the Plan 5 project-level work item.
func projectTaskToResponse(t db.Task) map[string]any {
	out := map[string]any{
		"id":            uuidToString(t.ID),
		"plan_id":       uuidToString(t.PlanID),
		"workspace_id":  uuidToString(t.WorkspaceID),
		"title":         t.Title,
		"step_order":    t.StepOrder,
		"depends_on":    pgUUIDsToStrings(t.DependsOn),
		"required_skills":    nonNilStrings(t.RequiredSkills),
		"fallback_agent_ids": pgUUIDsToStrings(t.FallbackAgentIds),
		"collaboration_mode": t.CollaborationMode,
		"status":             t.Status,
		"current_retry":      t.CurrentRetry,
		"timeout_rule":       rawJSONOrEmpty(t.TimeoutRule),
		"retry_rule":         rawJSONOrEmpty(t.RetryRule),
		"escalation_policy":  rawJSONOrEmpty(t.EscalationPolicy),
		"input_context_refs": rawJSONOrEmpty(t.InputContextRefs),
		"output_refs":        rawJSONOrEmpty(t.OutputRefs),
	}
	if t.RunID.Valid {
		out["run_id"] = uuidToString(t.RunID)
	}
	if t.Description.Valid {
		out["description"] = t.Description.String
	}
	if t.PrimaryAssigneeID.Valid {
		out["primary_assignee_id"] = uuidToString(t.PrimaryAssigneeID)
	}
	if t.AcceptanceCriteria.Valid {
		out["acceptance_criteria"] = t.AcceptanceCriteria.String
	}
	if t.ActualAgentID.Valid {
		out["actual_agent_id"] = uuidToString(t.ActualAgentID)
	}
	if t.StartedAt.Valid {
		out["started_at"] = t.StartedAt.Time
	}
	if t.CompletedAt.Valid {
		out["completed_at"] = t.CompletedAt.Time
	}
	if len(t.Result) > 0 {
		out["result"] = json.RawMessage(t.Result)
	}
	if t.Error.Valid {
		out["error"] = t.Error.String
	}
	if t.CreatedAt.Valid {
		out["created_at"] = t.CreatedAt.Time
	}
	if t.UpdatedAt.Valid {
		out["updated_at"] = t.UpdatedAt.Time
	}
	return out
}

// pgUUIDsToStrings flattens a []pgtype.UUID for JSON output, dropping
// invalid entries.
func pgUUIDsToStrings(ps []pgtype.UUID) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		if p.Valid {
			out = append(out, uuidToString(p))
		}
	}
	return out
}

// nonNilStrings returns an empty slice instead of nil so JSON consumers
// always see an array (text[] columns may come back as nil).
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
