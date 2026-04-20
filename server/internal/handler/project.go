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
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------- Response types ----------

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
}

// ProjectVersionResponse is the JSON response for a project version.
// Migration 059 removed the legacy plan_snapshot and workflow_snapshot
// columns, so this shape only exposes the remaining version metadata.
type ProjectVersionResponse struct {
	ID              string  `json:"id"`
	ProjectID       string  `json:"project_id"`
	ParentVersionID *string `json:"parent_version_id"`
	VersionNumber   int32   `json:"version_number"`
	BranchName      *string `json:"branch_name"`
	ForkReason      *string `json:"fork_reason"`
	VersionStatus   string  `json:"version_status"`
	CreatedBy       *string `json:"created_by"`
	CreatedAt       string  `json:"created_at"`
}

// ProjectRunResponse is the JSON response for a project run.
// Migration 059 removed retry_count from project_run, so this response maps
// only the current run fields.
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
	CreatedAt     string          `json:"created_at"`
}

// projectToResponse maps a db.Project row to the JSON response shape. Kept
// in one place so every handler returning a Project sees the same field
// mapping (notably: pgtype.UUID stringification and pgtype.Text → *string).
func projectToResponse(p db.Project) ProjectResponse {
	resp := ProjectResponse{
		ID:                  uuidToString(p.ID),
		WorkspaceID:         uuidToString(p.WorkspaceID),
		Title:               p.Title,
		Description:         textToPtr(p.Description),
		Status:              p.Status,
		ScheduleType:        p.ScheduleType,
		CronExpr:            textToPtr(p.CronExpr),
		SourceConversations: p.SourceConversations,
		CreatorOwnerID:      uuidToString(p.CreatorOwnerID),
		CreatedAt:           p.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:           p.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if p.SourceConversations == nil {
		resp.SourceConversations = json.RawMessage("[]")
	}
	if p.ChannelID.Valid {
		ch := uuidToString(p.ChannelID)
		resp.ChannelID = &ch
	}
	return resp
}

// projectVersionToResponse maps the current list query shape to the JSON response.
func projectVersionToResponse(v db.ListProjectVersionsRow) ProjectVersionResponse {
	resp := ProjectVersionResponse{
		ID:            uuidToString(v.ID),
		ProjectID:     uuidToString(v.ProjectID),
		VersionNumber: v.VersionNumber,
		BranchName:    textToPtr(v.BranchName),
		ForkReason:    textToPtr(v.ForkReason),
		VersionStatus: v.VersionStatus,
		CreatedAt:     v.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
	if v.ParentVersionID.Valid {
		s := uuidToString(v.ParentVersionID)
		resp.ParentVersionID = &s
	}
	if v.CreatedBy.Valid {
		s := uuidToString(v.CreatedBy)
		resp.CreatedBy = &s
	}
	return resp
}

// projectRunToResponse maps a db.ProjectRun row to the JSON shape.
func projectRunToResponse(r db.ProjectRun) ProjectRunResponse {
	resp := ProjectRunResponse{
		ID:            uuidToString(r.ID),
		PlanID:        uuidToString(r.PlanID),
		ProjectID:     uuidToString(r.ProjectID),
		Status:        r.Status,
		StepLogs:      r.StepLogs,
		OutputRefs:    r.OutputRefs,
		FailureReason: textToPtr(r.FailureReason),
		CreatedAt:     r.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
	if r.StepLogs == nil {
		resp.StepLogs = json.RawMessage("[]")
	}
	if r.OutputRefs == nil {
		resp.OutputRefs = json.RawMessage("[]")
	}
	if r.StartAt.Valid {
		s := r.StartAt.Time.UTC().Format(time.RFC3339)
		resp.StartAt = &s
	}
	if r.EndAt.Valid {
		s := r.EndAt.Time.UTC().Format(time.RFC3339)
		resp.EndAt = &s
	}
	return resp
}

// ---------- Valid project statuses ----------

var validProjectStatuses = map[string]bool{
	"not_started": true,
	"running":     true,
	"paused":      true,
	"completed":   true,
	"failed":      true,
	"archived":    true,
}

// validProjectStatusTransitions defines which status transitions are allowed.
var validProjectStatusTransitions = map[string][]string{
	"not_started": {"running", "archived"},
	"running":     {"paused", "completed", "failed"},
	"paused":      {"running", "archived"},
	"completed":   {"archived"},
	"failed":      {"not_started", "archived"},
	"archived":    {},
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

	workspaceID := resolveWorkspaceID(r)

	project, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		slog.Error("get project failed", "error", err, "project_id", projectID)
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}

	// Workspace scoping: a project can only be read from inside its own
	// workspace. The middleware already gates membership, but the URL has no
	// workspace segment, so we cross-check here so a member of workspace A
	// cannot pull projects belonging to workspace B by guessing the UUID.
	if workspaceID != "" && uuidToString(project.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	writeJSON(w, http.StatusOK, projectToResponse(project))
}

// CreateProject handles POST /api/projects
// Basic project creation without chat context.
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	// Accept both "title" (canonical) and "name" — the latter mirrors the
	// CreateWorkspace request shape so a UI that uses the same form for
	// both surfaces doesn't have to special-case the field name. Title
	// wins when both are supplied.
	type CreateProjectRequest struct {
		Title        string  `json:"title"`
		Name         string  `json:"name"`
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
		req.Title = req.Name
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title (or name) is required")
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
	case "one_time", "scheduled", "recurring":
		// valid
	default:
		writeError(w, http.StatusBadRequest, "invalid schedule_type: must be one_time, scheduled, or recurring")
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
		Status:              "not_started",
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

	// Load the current row first so we can:
	//   - cross-check workspace ownership
	//   - validate the status transition against the current status
	current, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		slog.Error("update project: get current failed", "error", err, "project_id", projectID)
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}
	if workspaceID != "" && uuidToString(current.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

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

	if req.ScheduleType != nil {
		switch *req.ScheduleType {
		case "one_time", "scheduled", "recurring":
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
		// CronExpr is set unconditionally — passing NULL clears it, which is
		// the desired behavior when a project switches back to one_time.
		CronExpr: ptrToText(req.CronExpr),
	})
	if err != nil {
		slog.Error("update project failed", "error", err, "project_id", projectID)
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

	current, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		slog.Error("delete project: get current failed", "error", err, "project_id", projectID)
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}
	if workspaceID != "" && uuidToString(current.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	// Authorize: the creator can always delete; otherwise the caller must be
	// an admin or owner of the workspace. The membership middleware already
	// confirmed they belong; here we gate the destructive operation.
	if uuidToString(current.CreatorOwnerID) != userID {
		member, ok := h.workspaceMember(w, r, workspaceID)
		if !ok {
			return
		}
		if member.Role != "owner" && member.Role != "admin" {
			writeError(w, http.StatusForbidden, "only the creator or workspace admin/owner can delete a project")
			return
		}
	}

	if err := h.Queries.DeleteProject(r.Context(), parseUUID(projectID)); err != nil {
		slog.Error("delete project failed", "error", err, "project_id", projectID)
		writeError(w, http.StatusInternalServerError, "failed to delete project")
		return
	}

	h.publish(protocol.EventProjectDeleted, workspaceID, "member", userID, map[string]string{
		"project_id": projectID,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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

	// TODO: Implement fork logic once sqlc queries are generated.
	// 1. Get current plan for the project
	// 2. Serialize plan to plan_snapshot JSONB
	// 3. Get current workflow for the project
	// 4. Serialize workflow to workflow_snapshot JSONB
	// 5. Get latest version_number and increment
	// 6. Create new project_version row
	//
	// plan, err := h.Queries.GetPlanByProjectID(r.Context(), parseUUID(projectID))
	// workflow, err := h.Queries.GetWorkflowByPlanID(r.Context(), plan.ID)
	// latestVersion, err := h.Queries.GetLatestProjectVersion(r.Context(), parseUUID(projectID))
	// newVersion, err := h.Queries.CreateProjectVersion(r.Context(), db.CreateProjectVersionParams{...})

	resp := ProjectVersionResponse{
		ID:            "", // TODO: from created version
		ProjectID:     projectID,
		VersionNumber: 0, // TODO: from created version
		BranchName:    &req.BranchName,
		ForkReason:    &req.ForkReason,
		VersionStatus: "active",
		CreatedBy:     &userID,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}

	slog.Info("project forked", "project_id", projectID, "user_id", userID, "branch_name", req.BranchName)

	writeJSON(w, http.StatusCreated, resp)
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
		slog.Error("list project versions failed", "error", err, "project_id", projectID)
		writeError(w, http.StatusInternalServerError, "failed to list project versions")
		return
	}

	out := make([]ProjectVersionResponse, 0, len(versions))
	for _, v := range versions {
		out = append(out, projectVersionToResponse(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": out, "total": len(out)})
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
		slog.Error("list project runs failed", "error", err, "project_id", projectID)
		writeError(w, http.StatusInternalServerError, "failed to list project runs")
		return
	}

	out := make([]ProjectRunResponse, 0, len(runs))
	for _, run := range runs {
		out = append(out, projectRunToResponse(run))
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out, "total": len(out)})
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

	plan, err := h.Queries.GetPlanByProject(r.Context(), parseUUID(projectID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no plan found for this project")
			return
		}
		slog.Error("reject plan: get plan by project failed", "error", err, "project_id", projectID)
		writeError(w, http.StatusInternalServerError, "failed to load plan")
		return
	}

	if err := h.Queries.UpdatePlanApproval(r.Context(), db.UpdatePlanApprovalParams{
		ID:             plan.ID,
		ApprovalStatus: "rejected",
		ApprovedBy:     parseUUID(userID),
	}); err != nil {
		slog.Error("reject plan: update approval failed", "error", err, "plan_id", uuidToString(plan.ID))
		writeError(w, http.StatusInternalServerError, "failed to reject plan")
		return
	}

	h.publish(protocol.EventPlanRejected, workspaceID, "member", userID, map[string]any{
		"project_id": projectID,
		"plan_id":    uuidToString(plan.ID),
		"from":       plan.ApprovalStatus,
		"to":         "rejected",
		"actor_id":   userID,
		"reason":     req.Reason,
	})

	slog.Info("plan rejected", "project_id", projectID, "plan_id", uuidToString(plan.ID), "rejected_by", userID, "reason", req.Reason)

	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}
