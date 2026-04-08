package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
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
		result = append(result, ProjectResponse{
			ID:                  uuidToString(p.ID),
			WorkspaceID:         uuidToString(p.WorkspaceID),
			Title:               p.Title,
			Description:         textToPtr(p.Description),
			Status:              p.Status,
			ScheduleType:        p.ScheduleType,
			SourceConversations: p.SourceConversations,
			CreatorOwnerID:      uuidToString(p.CreatorOwnerID),
			CreatedAt:           p.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:           p.UpdatedAt.Time.Format(time.RFC3339),
		})
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

	_ = resolveWorkspaceID(r) // Used for workspace-scoped query

	// TODO: Use h.Queries.GetProject() once sqlc query is generated.
	// The query should join project + plan (by project_id) + active run (by project_id).
	// project, err := h.Queries.GetProject(r.Context(), db.GetProjectParams{
	//     ID:          parseUUID(projectID),
	//     WorkspaceID: parseUUID(workspaceID),
	// })
	// if err != nil {
	//     writeError(w, http.StatusNotFound, "project not found")
	//     return
	// }

	_ = projectID
	writeError(w, http.StatusNotFound, "project not found")
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

	// TODO: Create project record once sqlc query is generated.
	// project, err := h.Queries.CreateProject(r.Context(), db.CreateProjectParams{
	//     WorkspaceID:         parseUUID(workspaceID),
	//     Title:               req.Title,
	//     Description:         strToText(req.Description),
	//     Status:              "not_started",
	//     ScheduleType:        scheduleType,
	//     CronExpr:            ptrToText(req.CronExpr),
	//     SourceConversations: []byte("[]"),
	//     ChannelID:           ch.ID,
	//     CreatorOwnerID:      parseUUID(userID),
	// })

	channelIDStr := uuidToString(ch.ID)
	resp := ProjectResponse{
		ID:                  "", // TODO: from created project
		WorkspaceID:         workspaceID,
		Title:               req.Title,
		Description:         &req.Description,
		Status:              "not_started",
		ScheduleType:        scheduleType,
		CronExpr:            req.CronExpr,
		SourceConversations: json.RawMessage("[]"),
		ChannelID:           &channelIDStr,
		CreatorOwnerID:      userID,
		CreatedAt:           time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:           time.Now().UTC().Format(time.RFC3339),
	}

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

	// Validate status transition if status is being updated
	if req.Status != nil {
		if !validProjectStatuses[*req.Status] {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		// TODO: Load current project status and validate transition using isValidStatusTransition()
	}

	// Validate schedule_type if provided
	if req.ScheduleType != nil {
		switch *req.ScheduleType {
		case "one_time", "scheduled", "recurring":
			// valid
		default:
			writeError(w, http.StatusBadRequest, "invalid schedule_type")
			return
		}
	}

	// TODO: Use h.Queries.UpdateProject() once sqlc query is generated.
	// err := h.Queries.UpdateProject(r.Context(), db.UpdateProjectParams{...})

	h.publish(protocol.EventProjectUpdated, workspaceID, "member", userID, map[string]string{
		"project_id": projectID,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
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

	// TODO: Use h.Queries.DeleteProject() once sqlc query is generated.
	// Verify the user is the creator or has admin/owner role.

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
		VersionNumber: 0,  // TODO: from created version
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

	// TODO: Use h.Queries.ListProjectVersions() once sqlc query is generated.

	_ = projectID

	writeJSON(w, http.StatusOK, map[string]any{"versions": []ProjectVersionResponse{}, "total": 0})
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

	// TODO: Use h.Queries.ListProjectRuns() once sqlc query is generated.

	_ = projectID

	writeJSON(w, http.StatusOK, map[string]any{"runs": []ProjectRunResponse{}, "total": 0})
}

// ApprovePlan handles POST /api/projects/{projectID}/approve
// Changes plan approval_status from 'draft'/'pending_approval' to 'approved'.
func (h *Handler) ApprovePlan(w http.ResponseWriter, r *http.Request) {
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

	// Only Owner can approve
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if member.Role != "owner" {
		writeError(w, http.StatusForbidden, "only workspace owners can approve plans")
		return
	}

	// TODO: Implement once sqlc queries are generated.
	// 1. Get the plan associated with this project
	// 2. Verify approval_status is 'draft' or 'pending_approval'
	// 3. Update to 'approved', set approved_by and approved_at
	//
	// plan, err := h.Queries.GetPlanByProjectID(r.Context(), parseUUID(projectID))
	// if plan.ApprovalStatus != "draft" && plan.ApprovalStatus != "pending_approval" {
	//     writeError(w, http.StatusBadRequest, "plan is not in a state that can be approved")
	//     return
	// }
	// err = h.Queries.ApprovePlan(r.Context(), db.ApprovePlanParams{
	//     ID:         plan.ID,
	//     ApprovedBy: parseUUID(userID),
	// })

	_ = projectID

	h.publish(protocol.EventPlanApproved, workspaceID, "member", userID, map[string]string{
		"project_id": projectID,
	})

	slog.Info("plan approved", "project_id", projectID, "approved_by", userID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

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
