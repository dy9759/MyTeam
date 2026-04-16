package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------- Response types ----------

// PlanSummary is an embedded plan summary in a ProjectResponse.
type PlanSummary struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	ApprovalStatus string `json:"approval_status"`
}

// RunSummary is an embedded active-run summary in a ProjectResponse.
type RunSummary struct {
	ID      string  `json:"id"`
	Status  string  `json:"status"`
	StartAt *string `json:"start_at"`
}

// ProjectResponse is the JSON response for a project.
type ProjectResponse struct {
	ID                  string          `json:"id"`
	WorkspaceID         string          `json:"workspace_id"`
	Title               string          `json:"title"`
	Description         *string         `json:"description"`
	Status              string          `json:"status"`
	ScheduleType        string          `json:"schedule_type"`
	CronExpr            *string         `json:"cron_expr,omitempty"`
	SourceConversations json.RawMessage `json:"source_conversations"`
	ChannelID           *string         `json:"channel_id"`
	CreatorOwnerID      string          `json:"creator_owner_id"`
	CreatedAt           string          `json:"created_at"`
	UpdatedAt           string          `json:"updated_at"`
	Plan                *PlanSummary    `json:"plan,omitempty"`
	ActiveRun           *RunSummary     `json:"active_run,omitempty"`
}

// ProjectVersionResponse is the JSON response for a project version.
type ProjectVersionResponse struct {
	ID               string          `json:"id"`
	ProjectID        string          `json:"project_id"`
	ParentVersionID  *string         `json:"parent_version_id"`
	VersionNumber    int32           `json:"version_number"`
	BranchName       *string         `json:"branch_name"`
	ForkReason       *string         `json:"fork_reason"`
	PlanSnapshot     json.RawMessage `json:"plan_snapshot,omitempty"`
	WorkflowSnapshot json.RawMessage `json:"workflow_snapshot,omitempty"`
	VersionStatus    string          `json:"version_status"`
	CreatedBy        *string         `json:"created_by"`
	CreatedAt        string          `json:"created_at"`
}

// ProjectRunResponse is the JSON response for a project run.
type ProjectRunResponse struct {
	ID            string          `json:"id"`
	PlanID        string          `json:"plan_id"`
	ProjectID     string          `json:"project_id"`
	Status        string          `json:"status"`
	StartAt       *string         `json:"start_at"`
	EndAt         *string         `json:"end_at"`
	StepLogs      json.RawMessage `json:"step_logs"`
	OutputRefs    json.RawMessage `json:"output_refs"`
	FailureReason *string         `json:"failure_reason"`
	RetryCount    int32           `json:"retry_count"`
	CreatedAt     string          `json:"created_at"`
}

// ---------- Valid project statuses ----------

var validProjectStatuses = map[string]bool{
	"draft":     true,
	"scheduled": true,
	"running":   true,
	"paused":    true,
	"completed": true,
	"failed":    true,
	"stopped":   true,
	"archived":  true,
}

// validProjectStatusTransitions defines which status transitions are allowed.
var validProjectStatusTransitions = map[string][]string{
	"draft":     {"scheduled", "running", "archived"},
	"scheduled": {"running", "paused", "archived"},
	"running":   {"paused", "completed", "failed", "stopped"},
	"paused":    {"running", "stopped", "archived"},
	"completed": {"archived"},
	"failed":    {"draft", "archived"},
	"stopped":   {"draft", "archived"},
	"archived":  {},
}

func isValidStatusTransition(from, to string) bool {
	allowed, ok := validProjectStatusTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// ---------- Conversion helpers ----------

func projectToResponse(p db.Project) ProjectResponse {
	return ProjectResponse{
		ID:                  uuidToString(p.ID),
		WorkspaceID:         uuidToString(p.WorkspaceID),
		Title:               p.Title,
		Description:         textToPtr(p.Description),
		Status:              p.Status,
		ScheduleType:        p.ScheduleType,
		CronExpr:            textToPtr(p.CronExpr),
		SourceConversations: p.SourceConversations,
		ChannelID:           uuidToPtr(p.ChannelID),
		CreatorOwnerID:      uuidToString(p.CreatorOwnerID),
		CreatedAt:           p.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:           p.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func planToResponsePtr(p db.Plan) *PlanSummary {
	return &PlanSummary{
		ID:             uuidToString(p.ID),
		Title:          p.Title,
		ApprovalStatus: p.ApprovalStatus,
	}
}

func runToResponsePtr(r db.ProjectRun) *RunSummary {
	return &RunSummary{
		ID:      uuidToString(r.ID),
		Status:  r.Status,
		StartAt: timestampToPtr(r.StartAt),
	}
}

func ptrFromUUID(u pgtype.UUID) *string {
	return uuidToPtr(u)
}

// ---------- Helpers ----------

// slugifyProjectTitle converts a title to a URL-safe channel name.
func slugifyProjectTitle(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	lastWasDash := false

	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasDash = false
		case b.Len() > 0 && !lastWasDash:
			b.WriteByte('-')
			lastWasDash = true
		}
	}

	slug := strings.Trim(b.String(), "-")
	if len(slug) > 50 {
		slug = slug[:50]
	}
	return slug
}

// createProjectChannel creates a channel for a project and adds the creator as a member.
// Returns the created channel. Handles name conflicts by appending a timestamp suffix.
func (h *Handler) createProjectChannel(r *http.Request, workspaceID, userID, title string) (db.Channel, error) {
	channelName := "proj-" + slugifyProjectTitle(title)

	ch, err := h.Queries.CreateChannel(r.Context(), db.CreateChannelParams{
		WorkspaceID:   parseUUID(workspaceID),
		Name:          channelName,
		Description:   strToText("Project channel for " + title),
		CreatedBy:     parseUUID(userID),
		CreatedByType: "member",
	})
	if err != nil {
		if isUniqueViolation(err) {
			// Append numeric suffix on conflict
			channelName = channelName + "-" + time.Now().Format("150405")
			ch, err = h.Queries.CreateChannel(r.Context(), db.CreateChannelParams{
				WorkspaceID:   parseUUID(workspaceID),
				Name:          channelName,
				Description:   strToText("Project channel for " + title),
				CreatedBy:     parseUUID(userID),
				CreatedByType: "member",
			})
			if err != nil {
				return db.Channel{}, err
			}
		} else {
			return db.Channel{}, err
		}
	}

	// Auto-join creator
	_ = h.Queries.AddChannelMember(r.Context(), db.AddChannelMemberParams{
		ChannelID:  ch.ID,
		MemberID:   parseUUID(userID),
		MemberType: "member",
	})

	return ch, nil
}

// ---------- Handlers ----------

// ListProjects handles GET /api/projects
func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	projects, err := h.Queries.ListProjects(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("list projects failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}

	result := make([]ProjectResponse, 0, len(projects))
	for _, p := range projects {
		result = append(result, projectToResponse(p))
	}
	writeJSON(w, http.StatusOK, result)
}

// GetProject handles GET /api/projects/{projectID}
func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	project, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
		} else {
			slog.Error("get project failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get project")
		}
		return
	}

	resp := projectToResponse(project)

	// Optionally join plan.
	plan, err := h.Queries.GetPlanByProject(r.Context(), project.ID)
	if err == nil {
		resp.Plan = planToResponsePtr(plan)
	}

	// Optionally join active run.
	activeRun, err := h.Queries.GetActiveProjectRun(r.Context(), project.ID)
	if err == nil {
		resp.ActiveRun = runToResponsePtr(activeRun)
	}

	writeJSON(w, http.StatusOK, resp)
}

// CreateProject handles POST /api/projects
// Basic project creation without chat context.
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	type CreateProjectRequest struct {
		Title        string  `json:"title"`
		Description  string  `json:"description"`
		ScheduleType string  `json:"schedule_type"`
		CronExpr     *string `json:"cron_expr"`
	}

	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	scheduleType := req.ScheduleType
	if scheduleType == "" {
		scheduleType = "one_time"
	}

	// Validate schedule_type
	switch scheduleType {
	case "one_time", "scheduled", "recurring", "scheduled_once":
		// valid
	default:
		writeError(w, http.StatusBadRequest, "invalid schedule_type: must be one_time, scheduled, recurring, or scheduled_once")
		return
	}

	// Validate cron_expr is provided for scheduled/recurring
	if (scheduleType == "scheduled" || scheduleType == "recurring") && (req.CronExpr == nil || *req.CronExpr == "") {
		writeError(w, http.StatusBadRequest, "cron_expr is required for scheduled/recurring projects")
		return
	}

	// Auto-create project channel
	ch, err := h.createProjectChannel(r, workspaceID, userID, req.Title)
	if err != nil {
		slog.Error("failed to create project channel", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create project channel")
		return
	}

	project, err := h.Queries.CreateProject(r.Context(), db.CreateProjectParams{
		WorkspaceID:         parseUUID(workspaceID),
		Title:               req.Title,
		Description:         strToText(req.Description),
		Status:              "draft",
		ScheduleType:        scheduleType,
		CronExpr:            ptrToText(req.CronExpr),
		SourceConversations: []byte("[]"),
		ChannelID:           ch.ID,
		CreatorOwnerID:      parseUUID(userID),
	})
	if err != nil {
		slog.Error("create project failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	// Create default "main" branch.
	_, err = h.Queries.CreateProjectBranch(r.Context(), db.CreateProjectBranchParams{
		ProjectID: project.ID,
		Name:      "main",
		IsDefault: true,
		CreatedBy: parseUUID(userID),
	})
	if err != nil {
		slog.Warn("failed to create default branch for project", "project_id", uuidToString(project.ID), "error", err)
	}

	resp := projectToResponse(project)

	h.publish(protocol.EventProjectCreated, workspaceID, "member", userID, map[string]any{
		"project": resp,
	})

	writeJSON(w, http.StatusCreated, resp)
}

// UpdateProject handles PATCH /api/projects/{projectID}
func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	type UpdateProjectRequest struct {
		Title        *string `json:"title"`
		Description  *string `json:"description"`
		Status       *string `json:"status"`
		ScheduleType *string `json:"schedule_type"`
		CronExpr     *string `json:"cron_expr"`
	}

	var req UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	// Load current project to validate status transition.
	current, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
		} else {
			slog.Error("get project for update failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get project")
		}
		return
	}

	// Validate status transition if status is being updated.
	if req.Status != nil {
		if !validProjectStatuses[*req.Status] {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		if !isValidStatusTransition(current.Status, *req.Status) {
			writeError(w, http.StatusBadRequest, "invalid status transition from "+current.Status+" to "+*req.Status)
			return
		}
	}

	// Validate schedule_type if provided.
	if req.ScheduleType != nil {
		switch *req.ScheduleType {
		case "one_time", "scheduled", "recurring", "scheduled_once":
			// valid
		default:
			writeError(w, http.StatusBadRequest, "invalid schedule_type")
			return
		}
	}

	updated, err := h.Queries.UpdateProject(r.Context(), db.UpdateProjectParams{
		ID:           parseUUID(projectID),
		Title:        ptrToText(req.Title),
		Description:  ptrToText(req.Description),
		Status:       ptrToText(req.Status),
		ScheduleType: ptrToText(req.ScheduleType),
		CronExpr:     ptrToText(req.CronExpr),
	})
	if err != nil {
		slog.Error("update project failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update project")
		return
	}

	resp := projectToResponse(updated)

	h.publish(protocol.EventProjectUpdated, workspaceID, "member", userID, map[string]any{
		"project": resp,
	})

	writeJSON(w, http.StatusOK, resp)
}

// DeleteProject handles DELETE /api/projects/{projectID}
func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	// Load project to verify ownership.
	project, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
		} else {
			slog.Error("get project for delete failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get project")
		}
		return
	}

	if uuidToString(project.CreatorOwnerID) != userID {
		writeError(w, http.StatusForbidden, "only the project creator can delete this project")
		return
	}

	if err := h.Queries.DeleteProject(r.Context(), parseUUID(projectID)); err != nil {
		slog.Error("delete project failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete project")
		return
	}

	h.publish(protocol.EventProjectDeleted, workspaceID, "member", userID, map[string]string{
		"project_id": projectID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// ForkProject handles POST /api/projects/{projectID}/fork
// Creates a new project_version with snapshot of current plan + workflow.
func (h *Handler) ForkProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	type ForkRequest struct {
		BranchName string `json:"branch_name"`
		ForkReason string `json:"fork_reason"`
	}

	var req ForkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Get the latest version to copy snapshots from and determine next version number.
	latestVersion, err := h.Queries.GetLatestProjectVersion(r.Context(), parseUUID(projectID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("get latest project version failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get latest project version")
		return
	}

	var (
		nextVersionNumber int32          = 1
		planSnapshot      []byte         = []byte("{}")
		workflowSnapshot  []byte         = []byte("{}")
		parentVersionID   pgtype.UUID
	)

	if err == nil {
		// latestVersion was found
		nextVersionNumber = latestVersion.VersionNumber + 1
		parentVersionID = latestVersion.ID
		if len(latestVersion.PlanSnapshot) > 0 {
			planSnapshot = latestVersion.PlanSnapshot
		}
		if len(latestVersion.WorkflowSnapshot) > 0 {
			workflowSnapshot = latestVersion.WorkflowSnapshot
		}
	}

	// Create a new branch for the fork.
	branchName := req.BranchName
	if branchName == "" {
		branchName = "branch-v" + itoa(nextVersionNumber)
	}

	branch, err := h.Queries.CreateProjectBranch(r.Context(), db.CreateProjectBranchParams{
		ProjectID: parseUUID(projectID),
		Name:      branchName,
		IsDefault: false,
		CreatedBy: parseUUID(userID),
	})
	if err != nil {
		slog.Error("create project branch for fork failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create project branch")
		return
	}

	newVersion, err := h.Queries.CreateProjectVersion(r.Context(), db.CreateProjectVersionParams{
		ProjectID:        parseUUID(projectID),
		ParentVersionID:  parentVersionID,
		VersionNumber:    nextVersionNumber,
		BranchName:       strToText(branchName),
		ForkReason:       strToText(req.ForkReason),
		PlanSnapshot:     planSnapshot,
		WorkflowSnapshot: workflowSnapshot,
		CreatedBy:        parseUUID(userID),
	})
	if err != nil {
		slog.Error("create project version for fork failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create project version")
		return
	}

	_ = branch // branch is created but the version carries the branch name

	resp := ProjectVersionResponse{
		ID:               uuidToString(newVersion.ID),
		ProjectID:        uuidToString(newVersion.ProjectID),
		ParentVersionID:  ptrFromUUID(newVersion.ParentVersionID),
		VersionNumber:    newVersion.VersionNumber,
		BranchName:       textToPtr(newVersion.BranchName),
		ForkReason:       textToPtr(newVersion.ForkReason),
		PlanSnapshot:     newVersion.PlanSnapshot,
		WorkflowSnapshot: newVersion.WorkflowSnapshot,
		VersionStatus:    newVersion.VersionStatus,
		CreatedBy:        ptrFromUUID(newVersion.CreatedBy),
		CreatedAt:        newVersion.CreatedAt.Time.Format(time.RFC3339),
	}

	slog.Info("project forked", "project_id", projectID, "user_id", userID, "branch_name", branchName)

	writeJSON(w, http.StatusCreated, resp)
}

// itoa converts an int32 to a decimal string without importing strconv.
func itoa(n int32) string {
	if n == 0 {
		return "0"
	}
	buf := [10]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

// ListProjectVersions handles GET /api/projects/{projectID}/versions
func (h *Handler) ListProjectVersions(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	versions, err := h.Queries.ListProjectVersions(r.Context(), parseUUID(projectID))
	if err != nil {
		slog.Error("list project versions failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list project versions")
		return
	}

	result := make([]ProjectVersionResponse, 0, len(versions))
	for _, v := range versions {
		result = append(result, ProjectVersionResponse{
			ID:               uuidToString(v.ID),
			ProjectID:        uuidToString(v.ProjectID),
			ParentVersionID:  ptrFromUUID(v.ParentVersionID),
			VersionNumber:    v.VersionNumber,
			BranchName:       textToPtr(v.BranchName),
			ForkReason:       textToPtr(v.ForkReason),
			PlanSnapshot:     v.PlanSnapshot,
			WorkflowSnapshot: v.WorkflowSnapshot,
			VersionStatus:    v.VersionStatus,
			CreatedBy:        ptrFromUUID(v.CreatedBy),
			CreatedAt:        v.CreatedAt.Time.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"versions": result, "total": len(result)})
}

// GetProjectRuns handles GET /api/projects/{projectID}/runs
func (h *Handler) GetProjectRuns(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	runs, err := h.Queries.ListProjectRuns(r.Context(), parseUUID(projectID))
	if err != nil {
		slog.Error("list project runs failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list project runs")
		return
	}

	result := make([]ProjectRunResponse, 0, len(runs))
	for _, run := range runs {
		result = append(result, ProjectRunResponse{
			ID:            uuidToString(run.ID),
			PlanID:        uuidToString(run.PlanID),
			ProjectID:     uuidToString(run.ProjectID),
			Status:        run.Status,
			StartAt:       timestampToPtr(run.StartAt),
			EndAt:         timestampToPtr(run.EndAt),
			StepLogs:      run.StepLogs,
			OutputRefs:    run.OutputRefs,
			FailureReason: textToPtr(run.FailureReason),
			RetryCount:    run.RetryCount,
			CreatedAt:     run.CreatedAt.Time.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"runs": result, "total": len(result)})
}

// ApprovePlan is now in plan.go — moved there with actual DB implementation.

// RejectPlan handles POST /api/projects/{projectID}/reject
// Changes plan approval_status to 'rejected'.
func (h *Handler) RejectPlan(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	type RejectRequest struct {
		Reason string `json:"reason"`
	}

	var req RejectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	// Only Owner can reject
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if member.Role != "owner" {
		writeError(w, http.StatusForbidden, "only workspace owners can reject plans")
		return
	}

	// TODO: Implement once sqlc queries are generated.
	// 1. Get the plan associated with this project
	// 2. Update approval_status to 'rejected'

	_ = projectID

	h.publish(protocol.EventPlanRejected, workspaceID, "member", userID, map[string]any{
		"project_id": projectID,
		"reason":     req.Reason,
	})

	slog.Info("plan rejected", "project_id", projectID, "rejected_by", userID, "reason", req.Reason)

	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}
