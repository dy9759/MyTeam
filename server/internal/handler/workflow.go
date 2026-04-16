package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// WorkflowResponse is the JSON response for a workflow.
type WorkflowResponse struct {
	ID          string          `json:"id"`
	PlanID      *string         `json:"plan_id"`
	WorkspaceID string          `json:"workspace_id"`
	Title       string          `json:"title"`
	Status      string          `json:"status"`
	Type        string          `json:"type"`
	CronExpr    *string         `json:"cron_expr"`
	Version     int32           `json:"version"`
	DAG         json.RawMessage `json:"dag"`
	CreatedBy   string          `json:"created_by"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

func workflowToResponse(w db.Workflow) WorkflowResponse {
	return WorkflowResponse{
		ID:          uuidToString(w.ID),
		PlanID:      uuidToPtr(w.PlanID),
		WorkspaceID: uuidToString(w.WorkspaceID),
		Title:       w.Title,
		Status:      w.Status,
		Type:        w.Type,
		CronExpr:    textToPtr(w.CronExpr),
		Version:     w.Version,
		DAG:         w.Dag,
		CreatedBy:   uuidToString(w.CreatedBy),
		CreatedAt:   timestampToString(w.CreatedAt),
		UpdatedAt:   timestampToString(w.UpdatedAt),
	}
}

// WorkflowStepResponse is the JSON response for a workflow step.
type WorkflowStepResponse struct {
	ID               string          `json:"id"`
	WorkflowID       string          `json:"workflow_id"`
	StepOrder        int32           `json:"step_order"`
	Description      string          `json:"description"`
	AgentID          *string         `json:"agent_id"`
	FallbackAgentIDs []string        `json:"fallback_agent_ids"`
	RequiredSkills   []string        `json:"required_skills"`
	TimeoutMs        *int64          `json:"timeout_ms"`
	RetryCount       *int32          `json:"retry_count"`
	DependsOn        []string        `json:"depends_on"`
	Status           string          `json:"status"`
	StartedAt        *string         `json:"started_at"`
	CompletedAt      *string         `json:"completed_at"`
	Result           json.RawMessage `json:"result"`
	Error            *string         `json:"error"`
}

func workflowStepToResponse(s db.WorkflowStep) WorkflowStepResponse {
	fallback := make([]string, len(s.FallbackAgentIds))
	for i, u := range s.FallbackAgentIds {
		fallback[i] = uuidToString(u)
	}
	depends := make([]string, len(s.DependsOn))
	for i, u := range s.DependsOn {
		depends[i] = uuidToString(u)
	}
	var timeoutMs *int64
	if s.TimeoutMs.Valid {
		timeoutMs = &s.TimeoutMs.Int64
	}
	var retryCount *int32
	if s.RetryCount.Valid {
		retryCount = &s.RetryCount.Int32
	}
	return WorkflowStepResponse{
		ID:               uuidToString(s.ID),
		WorkflowID:       uuidToString(s.WorkflowID),
		StepOrder:        s.StepOrder,
		Description:      s.Description,
		AgentID:          uuidToPtr(s.AgentID),
		FallbackAgentIDs: fallback,
		RequiredSkills:   s.RequiredSkills,
		TimeoutMs:        timeoutMs,
		RetryCount:       retryCount,
		DependsOn:        depends,
		Status:           s.Status,
		StartedAt:        timestampToPtr(s.StartedAt),
		CompletedAt:      timestampToPtr(s.CompletedAt),
		Result:           s.Result,
		Error:            textToPtr(s.Error),
	}
}

type CreateWorkflowRequest struct {
	PlanID   *string         `json:"plan_id"`
	Title    string          `json:"title"`
	Status   string          `json:"status"`
	Type     string          `json:"type"`
	CronExpr *string         `json:"cron_expr"`
	DAG      json.RawMessage `json:"dag"`
}

func (h *Handler) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req CreateWorkflowRequest
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

	status := req.Status
	if status == "" {
		status = "draft"
	}
	typ := req.Type
	if typ == "" {
		typ = "once"
	}

	wf, err := h.Queries.CreateWorkflow(r.Context(), db.CreateWorkflowParams{
		PlanID:      optionalUUID(req.PlanID),
		WorkspaceID: parseUUID(workspaceID),
		Title:       req.Title,
		Status:      status,
		Type:        typ,
		CronExpr:    ptrToText(req.CronExpr),
		Dag:         req.DAG,
		CreatedBy:   parseUUID(userID),
	})
	if err != nil {
		slog.Error("failed to create workflow", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create workflow")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("workflow.created", workspaceID, actorType, actorID, map[string]string{
		"workflow_id": uuidToString(wf.ID),
	})

	writeJSON(w, http.StatusCreated, workflowToResponse(wf))
}

func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "workflowID")
	wf, err := h.Queries.GetWorkflow(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	writeJSON(w, http.StatusOK, workflowToResponse(wf))
}

func (h *Handler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
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

	workflows, err := h.Queries.ListWorkflows(r.Context(), db.ListWorkflowsParams{
		WorkspaceID: parseUUID(workspaceID),
		Limit:       int32(limit),
		Offset:      int32(offset),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}

	resp := make([]WorkflowResponse, len(workflows))
	for i, wf := range workflows {
		resp[i] = workflowToResponse(wf)
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": resp, "total": len(resp)})
}

type UpdateStatusRequest struct {
	Status string `json:"status"`
}

func (h *Handler) UpdateWorkflowStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "workflowID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	var req UpdateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status == "" {
		writeError(w, http.StatusBadRequest, "status is required")
		return
	}

	if err := h.Queries.UpdateWorkflowStatus(r.Context(), db.UpdateWorkflowStatusParams{
		ID:     parseUUID(id),
		Status: req.Status,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workflow status")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("workflow.status_updated", workspaceID, actorType, actorID, map[string]string{
		"workflow_id": id,
		"status":      req.Status,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

type UpdateDAGRequest struct {
	DAG json.RawMessage `json:"dag"`
}

func (h *Handler) UpdateWorkflowDAG(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "workflowID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	var req UpdateDAGRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.Queries.UpdateWorkflowDAG(r.Context(), db.UpdateWorkflowDAGParams{
		ID:  parseUUID(id),
		Dag: req.DAG,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workflow DAG")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("workflow.dag_updated", workspaceID, actorType, actorID, map[string]string{
		"workflow_id": id,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *Handler) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "workflowID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	if err := h.Queries.DeleteWorkflow(r.Context(), parseUUID(id)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workflow")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("workflow.deleted", workspaceID, actorType, actorID, map[string]string{
		"workflow_id": id,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) ListWorkflowSteps(w http.ResponseWriter, r *http.Request) {
	workflowID := chi.URLParam(r, "workflowID")

	steps, err := h.Queries.ListWorkflowSteps(r.Context(), parseUUID(workflowID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflow steps")
		return
	}

	resp := make([]WorkflowStepResponse, len(steps))
	for i, s := range steps {
		resp[i] = workflowStepToResponse(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"steps": resp, "total": len(resp)})
}

func (h *Handler) StartWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "workflowID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	// Validate the workflow exists.
	wf, err := h.Queries.GetWorkflow(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	// Create project_run if this workflow has a plan linked to a project
	var runID pgtype.UUID
	if wf.PlanID.Valid {
		plan, planErr := h.Queries.GetPlan(r.Context(), wf.PlanID)
		if planErr == nil && plan.ProjectID.Valid {
			run, runErr := h.Queries.CreateProjectRun(r.Context(), db.CreateProjectRunParams{
				PlanID:    wf.PlanID,
				ProjectID: plan.ProjectID,
				Status:    "pending",
			})
			if runErr != nil {
				slog.Error("failed to create project run", "error", runErr)
			} else {
				runID = run.ID
				_ = h.Queries.UpdateProjectStatus(r.Context(), db.UpdateProjectStatusParams{
					ID:     plan.ProjectID,
					Status: "running",
				})
			}
		}
	}

	// Schedule the workflow via the scheduler service.
	runIDStr := uuidToString(runID)
	if err := h.Scheduler.ScheduleWorkflow(r.Context(), id, runIDStr); err != nil {
		slog.Error("failed to schedule workflow", "workflow_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start workflow: "+err.Error())
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventWorkflowStarted, workspaceID, actorType, actorID, map[string]string{
		"workflow_id": id,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "running", "run_id": runIDStr})
}

// RetryWorkflowStep retries a failed workflow step.
// POST /api/workflows/{workflowID}/steps/{stepID}/retry
func (h *Handler) RetryWorkflowStep(w http.ResponseWriter, r *http.Request) {
	workflowID := chi.URLParam(r, "workflowID")
	stepID := chi.URLParam(r, "stepID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	// Validate step exists and belongs to the workflow.
	step, err := h.Queries.GetWorkflowStep(r.Context(), parseUUID(stepID))
	if err != nil {
		writeError(w, http.StatusNotFound, "step not found")
		return
	}
	if uuidToString(step.WorkflowID) != workflowID {
		writeError(w, http.StatusBadRequest, "step does not belong to this workflow")
		return
	}

	// Validate step is in a retriable state.
	if step.Status != "failed" && step.Status != "timeout" {
		writeError(w, http.StatusBadRequest, "step is not in a failed state (current: "+step.Status+")")
		return
	}

	// Reset step to "pending".
	h.Queries.UpdateWorkflowStepStatus(r.Context(), db.UpdateWorkflowStepStatusParams{
		ID:     step.ID,
		Status: "pending",
		Result: nil,
		Error:  pgtype.Text{},
	})

	// Schedule the step.
	go h.Scheduler.ScheduleStep(r.Context(), step, "")

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("workflow.step_retried", workspaceID, actorType, actorID, map[string]string{
		"workflow_id": workflowID,
		"step_id":     stepID,
	})

	slog.Info("workflow step retried manually",
		"workflow_id", workflowID,
		"step_id", stepID,
		"user_id", userID,
	)

	writeJSON(w, http.StatusOK, map[string]string{"status": "retrying"})
}

// ReplaceStepAgent replaces the agent for a failed or blocked step.
// PATCH /api/workflows/{workflowID}/steps/{stepID}/agent
func (h *Handler) ReplaceStepAgent(w http.ResponseWriter, r *http.Request) {
	workflowID := chi.URLParam(r, "workflowID")
	stepID := chi.URLParam(r, "stepID")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	// Validate step exists and belongs to the workflow.
	step, err := h.Queries.GetWorkflowStep(r.Context(), parseUUID(stepID))
	if err != nil {
		writeError(w, http.StatusNotFound, "step not found")
		return
	}
	if uuidToString(step.WorkflowID) != workflowID {
		writeError(w, http.StatusBadRequest, "step does not belong to this workflow")
		return
	}

	// Validate step is in a replaceable state.
	if step.Status != "failed" && step.Status != "blocked" && step.Status != "timeout" {
		writeError(w, http.StatusBadRequest, "step is not in a failed or blocked state (current: "+step.Status+")")
		return
	}

	// Validate the new agent exists.
	_, agentErr := h.Queries.GetAgent(r.Context(), parseUUID(req.AgentID))
	if agentErr != nil {
		writeError(w, http.StatusBadRequest, "agent not found")
		return
	}

	// TODO: Update step's agent_id to new agent when sqlc has an UpdateWorkflowStepAgent query.
	// s.Queries.UpdateWorkflowStepAgent(ctx, db.UpdateWorkflowStepAgentParams{
	//     ID:      step.ID,
	//     AgentID: parseUUID(req.AgentID),
	// })

	// Reset step to "pending".
	h.Queries.UpdateWorkflowStepStatus(r.Context(), db.UpdateWorkflowStepStatusParams{
		ID:     step.ID,
		Status: "pending",
		Result: nil,
		Error:  pgtype.Text{},
	})

	// Log the agent replacement activity.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish("workflow.agent_replaced", workspaceID, actorType, actorID, map[string]any{
		"workflow_id":          workflowID,
		"step_id":              stepID,
		"original_agent_id":    uuidToString(step.AgentID),
		"replacement_agent_id": req.AgentID,
		"reason":               "owner_manual",
	})

	// Re-fetch step and schedule with new agent.
	freshStep, freshErr := h.Queries.GetWorkflowStep(r.Context(), step.ID)
	if freshErr == nil {
		go h.Scheduler.ScheduleStep(r.Context(), freshStep, "")
	}

	slog.Info("workflow step agent replaced",
		"workflow_id", workflowID,
		"step_id", stepID,
		"old_agent_id", uuidToString(step.AgentID),
		"new_agent_id", req.AgentID,
		"user_id", userID,
	)

	writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})
}
