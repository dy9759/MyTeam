package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// PlanResponse is the JSON response for a plan.
type PlanResponse struct {
	ID             string          `json:"id"`
	WorkspaceID    string          `json:"workspace_id"`
	Title          string          `json:"title"`
	Description    *string         `json:"description"`
	SourceType     *string         `json:"source_type"`
	SourceRefID    *string         `json:"source_ref_id"`
	Constraints    *string         `json:"constraints"`
	ExpectedOutput *string         `json:"expected_output"`
	Steps          json.RawMessage `json:"steps"`
	CreatedBy      string          `json:"created_by"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
	ApprovalStatus string          `json:"approval_status"`
	ApprovedBy     *string         `json:"approved_by"`
	ApprovedAt     *string         `json:"approved_at"`
	ProjectID      *string         `json:"project_id"`
}

func planToResponse(p db.Plan) PlanResponse {
	return PlanResponse{
		ID:             uuidToString(p.ID),
		WorkspaceID:    uuidToString(p.WorkspaceID),
		Title:          p.Title,
		Description:    textToPtr(p.Description),
		SourceType:     textToPtr(p.SourceType),
		SourceRefID:    uuidToPtr(p.SourceRefID),
		Constraints:    textToPtr(p.Constraints),
		ExpectedOutput: textToPtr(p.ExpectedOutput),
		Steps:          p.Steps,
		CreatedBy:      uuidToString(p.CreatedBy),
		CreatedAt:      timestampToString(p.CreatedAt),
		UpdatedAt:      timestampToString(p.UpdatedAt),
		ApprovalStatus: p.ApprovalStatus,
		ApprovedBy:     uuidToPtr(p.ApprovedBy),
		ApprovedAt:     timestampToPtr(p.ApprovedAt),
		ProjectID:      uuidToPtr(p.ProjectID),
	}
}

type CreatePlanRequest struct {
	Title          string          `json:"title"`
	Description    *string         `json:"description"`
	SourceType     *string         `json:"source_type"`
	SourceRefID    *string         `json:"source_ref_id"`
	Constraints    *string         `json:"constraints"`
	ExpectedOutput *string         `json:"expected_output"`
	Steps          json.RawMessage `json:"steps"`
}

func (h *Handler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	var req CreatePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	steps := req.Steps
	if steps == nil {
		steps = []byte("[]")
	}

	plan, err := h.Queries.CreatePlan(r.Context(), db.CreatePlanParams{
		WorkspaceID:    parseUUID(workspaceID),
		Title:          req.Title,
		Description:    ptrToText(req.Description),
		SourceType:     ptrToText(req.SourceType),
		SourceRefID:    optionalUUID(req.SourceRefID),
		Constraints:    ptrToText(req.Constraints),
		ExpectedOutput: ptrToText(req.ExpectedOutput),
		Steps:          steps,
		CreatedBy:      parseUUID(userID),
	})
	if err != nil {
		slog.Error("failed to create plan", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create plan")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("plan.created", workspaceID, actorType, actorID, map[string]string{
		"plan_id": uuidToString(plan.ID),
	})

	writeJSON(w, http.StatusCreated, planToResponse(plan))
}

func (h *Handler) GetPlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "planID")
	plan, err := h.Queries.GetPlan(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "plan not found")
		return
	}
	writeJSON(w, http.StatusOK, planToResponse(plan))
}

func (h *Handler) ListPlans(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)

	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			offset = v
		}
	}

	plans, err := h.Queries.ListPlans(r.Context(), db.ListPlansParams{
		WorkspaceID: parseUUID(workspaceID),
		Limit:       int32(limit),
		Offset:      int32(offset),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list plans")
		return
	}

	resp := make([]PlanResponse, len(plans))
	for i, p := range plans {
		resp[i] = planToResponse(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": resp, "total": len(resp)})
}

func (h *Handler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "planID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	if err := h.Queries.DeletePlan(r.Context(), parseUUID(id)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete plan")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("plan.deleted", workspaceID, actorType, actorID, map[string]string{
		"plan_id": id,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GeneratePlan uses LLM to parse natural language into a structured plan.
// POST /api/plans/generate
func (h *Handler) GeneratePlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Input == "" {
		writeError(w, http.StatusBadRequest, "input is required")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	plan, err := h.PlanGenerator.GeneratePlanFromText(r.Context(), req.Input, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate plan")
		return
	}

	writeJSON(w, http.StatusOK, plan)
}

// ApprovePlan approves a plan and optionally triggers workflow creation.
// POST /api/plans/{planID}/approve
func (h *Handler) ApprovePlan(w http.ResponseWriter, r *http.Request) {
	planID := chi.URLParam(r, "planID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	// 1. Load the plan
	plan, err := h.Queries.GetPlan(r.Context(), parseUUID(planID))
	if err != nil {
		writeError(w, http.StatusNotFound, "plan not found")
		return
	}

	if plan.ApprovalStatus == "approved" {
		writeError(w, http.StatusConflict, "plan already approved")
		return
	}

	// 2. Update approval status
	updatedPlan, err := h.Queries.ApprovePlan(r.Context(), db.ApprovePlanParams{
		ID:         parseUUID(planID),
		ApprovedBy: parseUUID(userID),
	})
	if err != nil {
		slog.Error("failed to approve plan", "plan_id", planID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to approve plan")
		return
	}

	// 3. Publish event
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("plan.approved", workspaceID, actorType, actorID, map[string]string{
		"plan_id": planID,
	})

	// 4. Optionally trigger workflow creation via Scheduler
	if h.Scheduler != nil {
		slog.Info("plan approved, workflow scheduling available", "plan_id", planID)
	}

	writeJSON(w, http.StatusOK, planToResponse(updatedPlan))
}

// optionalUUID converts an optional string pointer to a pgtype.UUID.
func optionalUUID(s *string) pgtype.UUID {
	if s == nil || *s == "" {
		return pgtype.UUID{}
	}
	return parseUUID(*s)
}
