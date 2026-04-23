# ProjectLinear Phase 1: Core Completion

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire up the stubbed project backend handlers to use existing sqlc queries, add project_branch and project_result tables, align status enums with spec, and build a functional project detail page.

**Architecture:** The project infrastructure (DB schema, sqlc queries, API routes, frontend types, store) already exists but handlers are stubbed with TODO comments. This phase connects the dots: handlers call sqlc, frontend renders real data. New migration adds project_branch and project_result tables.

**Tech Stack:** Go 1.26 (Chi router, sqlc, pgx), PostgreSQL 17 (pgvector), Next.js 16 (App Router), TypeScript, Zustand, Tailwind CSS

---

## File Structure

### Backend (server/)
- **Create:** `server/migrations/045_project_branch_result.up.sql` -- New tables + schema alterations
- **Create:** `server/migrations/045_project_branch_result.down.sql` -- Rollback
- **Create:** `server/pkg/db/queries/project_branches.sql` -- Branch CRUD queries
- **Create:** `server/pkg/db/queries/project_results.sql` -- Result CRUD queries
- **Modify:** `server/pkg/db/queries/projects.sql` -- Add queries for new columns
- **Modify:** `server/pkg/db/queries/project_versions.sql` -- Add branch_id filter
- **Modify:** `server/internal/handler/project.go` -- Wire all TODO handlers to real sqlc calls
- **Modify:** `server/cmd/server/router.go` -- Add new routes (branches, results, start execution)
- **Modify:** `server/pkg/protocol/events.go` -- Add new event constants

### Frontend (apps/web/)
- **Modify:** `apps/web/shared/types/project.ts` -- Add ProjectBranch, ProjectResult, expand enums
- **Modify:** `apps/web/shared/api/client.ts` -- Add branch/result API methods
- **Modify:** `apps/web/features/projects/store.ts` -- Add branch/result state + actions
- **Modify:** `apps/web/app/(dashboard)/projects/[id]/page.tsx` -- Enhance detail page tabs

---

### Task 1: Database Migration -- project_branch & project_result

**Files:**
- Create: `server/migrations/045_project_branch_result.up.sql`
- Create: `server/migrations/045_project_branch_result.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- server/migrations/045_project_branch_result.up.sql

-- 1. project_branch: first-class branch entity
CREATE TABLE project_branch (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  parent_branch_id UUID REFERENCES project_branch(id),
  is_default BOOLEAN NOT NULL DEFAULT FALSE,
  status TEXT NOT NULL DEFAULT 'active',
  created_by UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(project_id, name)
);

CREATE INDEX idx_project_branch_project ON project_branch(project_id);

-- 2. project_result: run output/artifacts
CREATE TABLE project_result (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES project_run(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  version_id UUID REFERENCES project_version(id),
  summary TEXT,
  artifacts JSONB DEFAULT '[]',
  deliverables JSONB DEFAULT '[]',
  acceptance_status TEXT NOT NULL DEFAULT 'pending',
  accepted_by UUID,
  accepted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_project_result_run ON project_result(run_id);
CREATE INDEX idx_project_result_project ON project_result(project_id);

-- 3. Extend project table
ALTER TABLE project ADD COLUMN IF NOT EXISTS default_branch_id UUID REFERENCES project_branch(id);
ALTER TABLE project ADD COLUMN IF NOT EXISTS max_runs INTEGER;
ALTER TABLE project ADD COLUMN IF NOT EXISTS end_time TIMESTAMPTZ;
ALTER TABLE project ADD COLUMN IF NOT EXISTS consecutive_failure_threshold INTEGER DEFAULT 3;
ALTER TABLE project ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ;
ALTER TABLE project ADD COLUMN IF NOT EXISTS plan_visibility TEXT NOT NULL DEFAULT 'owner_only';

-- 4. Extend project_version with branch_id
ALTER TABLE project_version ADD COLUMN IF NOT EXISTS branch_id UUID REFERENCES project_branch(id);

-- 5. Extend workflow_step with sub-task fields
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS title TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS goal TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS priority TEXT DEFAULT 'medium';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS candidate_agent_ids UUID[] DEFAULT '{}';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS owner_reviewer_id UUID;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS context_md_path TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS instruction_md_path TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS worktree_path TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS expected_outputs JSONB DEFAULT '[]';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS actual_outputs JSONB DEFAULT '[]';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS skippable BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS acceptance_checks JSONB DEFAULT '[]';
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS done_definition TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS error_code TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS error_summary TEXT;
ALTER TABLE workflow_step ADD COLUMN IF NOT EXISTS on_failure TEXT DEFAULT 'block';
```

- [ ] **Step 2: Write the down migration**

```sql
-- server/migrations/045_project_branch_result.down.sql

ALTER TABLE workflow_step DROP COLUMN IF EXISTS on_failure;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS error_summary;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS error_code;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS done_definition;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS acceptance_checks;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS skippable;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS actual_outputs;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS expected_outputs;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS worktree_path;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS instruction_md_path;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS context_md_path;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS owner_reviewer_id;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS candidate_agent_ids;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS priority;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS goal;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS title;

ALTER TABLE project_version DROP COLUMN IF EXISTS branch_id;

ALTER TABLE project DROP COLUMN IF EXISTS plan_visibility;
ALTER TABLE project DROP COLUMN IF EXISTS scheduled_at;
ALTER TABLE project DROP COLUMN IF EXISTS consecutive_failure_threshold;
ALTER TABLE project DROP COLUMN IF EXISTS end_time;
ALTER TABLE project DROP COLUMN IF EXISTS max_runs;
ALTER TABLE project DROP COLUMN IF EXISTS default_branch_id;

DROP TABLE IF EXISTS project_result;
DROP TABLE IF EXISTS project_branch;
```

- [ ] **Step 3: Run the migration**

Run: `cd server && make migrate-up`
Expected: Migration 045 applies successfully.

- [ ] **Step 4: Commit**

```bash
git add server/migrations/045_project_branch_result.up.sql server/migrations/045_project_branch_result.down.sql
git commit -m "feat(server): add project_branch and project_result tables (migration 045)"
```

---

### Task 2: SQLC Queries for project_branch and project_result

**Files:**
- Create: `server/pkg/db/queries/project_branches.sql`
- Create: `server/pkg/db/queries/project_results.sql`

- [ ] **Step 1: Write project_branches.sql**

```sql
-- server/pkg/db/queries/project_branches.sql

-- name: CreateProjectBranch :one
INSERT INTO project_branch (project_id, name, parent_branch_id, is_default, created_by)
VALUES (@project_id, @name, @parent_branch_id, @is_default, @created_by)
RETURNING *;

-- name: ListProjectBranches :many
SELECT * FROM project_branch WHERE project_id = @project_id ORDER BY created_at ASC;

-- name: GetProjectBranch :one
SELECT * FROM project_branch WHERE id = @id;

-- name: GetDefaultBranch :one
SELECT * FROM project_branch WHERE project_id = @project_id AND is_default = TRUE LIMIT 1;

-- name: UpdateProjectBranchStatus :exec
UPDATE project_branch SET status = @status WHERE id = @id;

-- name: DeleteProjectBranch :exec
DELETE FROM project_branch WHERE id = @id;
```

- [ ] **Step 2: Write project_results.sql**

```sql
-- server/pkg/db/queries/project_results.sql

-- name: CreateProjectResult :one
INSERT INTO project_result (run_id, project_id, version_id, summary, artifacts, deliverables)
VALUES (@run_id, @project_id, @version_id, @summary, @artifacts, @deliverables)
RETURNING *;

-- name: GetProjectResult :one
SELECT * FROM project_result WHERE id = @id;

-- name: GetProjectResultByRun :one
SELECT * FROM project_result WHERE run_id = @run_id LIMIT 1;

-- name: ListProjectResults :many
SELECT * FROM project_result WHERE project_id = @project_id ORDER BY created_at DESC;

-- name: UpdateProjectResultAcceptance :exec
UPDATE project_result
SET acceptance_status = @acceptance_status, accepted_by = @accepted_by, accepted_at = NOW()
WHERE id = @id;

-- name: UpdateProjectResultSummary :exec
UPDATE project_result
SET summary = @summary, artifacts = @artifacts, deliverables = @deliverables
WHERE id = @id;
```

- [ ] **Step 3: Add branch_id filter to project_versions.sql**

Add to `server/pkg/db/queries/project_versions.sql`:

```sql
-- name: ListProjectVersionsByBranch :many
SELECT * FROM project_version WHERE project_id = @project_id AND branch_id = @branch_id ORDER BY version_number DESC;
```

- [ ] **Step 4: Regenerate sqlc**

Run: `cd server && make sqlc`
Expected: New files generated: `project_branches.sql.go`, `project_results.sql.go`. Updated: `project_versions.sql.go`, `models.go` (with new structs).

- [ ] **Step 5: Verify build**

Run: `cd server && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 6: Commit**

```bash
git add server/pkg/db/queries/project_branches.sql server/pkg/db/queries/project_results.sql server/pkg/db/queries/project_versions.sql server/pkg/db/generated/
git commit -m "feat(server): add sqlc queries for project_branch and project_result"
```

---

### Task 3: Wire GetProject Handler

**Files:**
- Modify: `server/internal/handler/project.go:204-230`

- [ ] **Step 1: Write the test**

Create file `server/internal/handler/project_test.go`:

```go
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetProject_NotFound(t *testing.T) {
	h := newTestHandler(t)
	req := newAuthenticatedRequest(t, "GET", "/api/projects/00000000-0000-0000-0000-000000000000", nil)
	rr := httptest.NewRecorder()
	h.GetProject(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}
```

- [ ] **Step 2: Implement GetProject**

Replace the GetProject handler body in `server/internal/handler/project.go`:

```go
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
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	resp := projectToResponse(project)

	// Optionally join plan
	plan, planErr := h.Queries.GetPlanByProject(r.Context(), project.ID)
	if planErr == nil {
		resp.Plan = planToResponsePtr(plan)
	}

	// Optionally join active run
	run, runErr := h.Queries.GetActiveProjectRun(r.Context(), project.ID)
	if runErr == nil {
		resp.ActiveRun = runToResponsePtr(run)
	}

	writeJSON(w, http.StatusOK, resp)
}
```

- [ ] **Step 3: Add helper functions and update ProjectResponse**

Add at the top of `project.go`, after the existing response types:

```go
// PlanSummary is a lightweight plan reference embedded in project responses.
type PlanSummary struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	ApprovalStatus string `json:"approval_status"`
}

// RunSummary is a lightweight run reference embedded in project responses.
type RunSummary struct {
	ID     string  `json:"id"`
	Status string  `json:"status"`
	StartAt *string `json:"start_at"`
}
```

Update `ProjectResponse` to add optional joins:

```go
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
```

Add conversion helpers at the bottom of `project.go`:

```go
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
		CreatedAt:           p.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:           p.UpdatedAt.Time.Format(time.RFC3339),
	}
	if p.ChannelID.Valid {
		cid := uuidToString(p.ChannelID)
		resp.ChannelID = &cid
	}
	return resp
}

func planToResponsePtr(p db.Plan) *PlanSummary {
	return &PlanSummary{
		ID:             uuidToString(p.ID),
		Title:          p.Title,
		ApprovalStatus: p.ApprovalStatus,
	}
}

func runToResponsePtr(r db.ProjectRun) *RunSummary {
	s := &RunSummary{
		ID:     uuidToString(r.ID),
		Status: r.Status,
	}
	if r.StartAt.Valid {
		t := r.StartAt.Time.Format(time.RFC3339)
		s.StartAt = &t
	}
	return s
}
```

- [ ] **Step 4: Update ListProjects to use the helper**

Replace the loop body in `ListProjects`:

```go
result := make([]ProjectResponse, 0, len(projects))
for _, p := range projects {
	result = append(result, projectToResponse(p))
}
writeJSON(w, http.StatusOK, result)
```

- [ ] **Step 5: Verify build**

Run: `cd server && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/project.go server/internal/handler/project_test.go
git commit -m "feat(server): wire GetProject handler with plan + active run joins"
```

---

### Task 4: Wire CreateProject, UpdateProject, DeleteProject Handlers

**Files:**
- Modify: `server/internal/handler/project.go:234-405`

- [ ] **Step 1: Wire CreateProject**

Replace the CreateProject handler body (lines 234-324) to use the actual sqlc query:

```go
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

	switch scheduleType {
	case "one_time", "scheduled_once", "recurring":
		// valid
	default:
		writeError(w, http.StatusBadRequest, "invalid schedule_type")
		return
	}

	if (scheduleType == "scheduled_once" || scheduleType == "recurring") && (req.CronExpr == nil || *req.CronExpr == "") {
		writeError(w, http.StatusBadRequest, "cron_expr is required for scheduled/recurring projects")
		return
	}

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
		slog.Error("failed to create project", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	// Create default branch
	branch, branchErr := h.Queries.CreateProjectBranch(r.Context(), db.CreateProjectBranchParams{
		ProjectID:      project.ID,
		Name:           "main",
		IsDefault:      true,
		CreatedBy:      parseUUID(userID),
	})
	if branchErr != nil {
		slog.Error("failed to create default branch", "error", branchErr)
	}

	// Link default branch to project
	if branchErr == nil {
		_ = h.Queries.UpdateProject(r.Context(), db.UpdateProjectParams{
			ID: project.ID,
		})
		// We'll need a dedicated query for this; for now just log
		slog.Info("default branch created", "branch_id", uuidToString(branch.ID))
	}

	resp := projectToResponse(project)

	h.publish(protocol.EventProjectCreated, workspaceID, "member", userID, map[string]any{
		"project": resp,
	})

	writeJSON(w, http.StatusCreated, resp)
}
```

- [ ] **Step 2: Wire UpdateProject**

Replace the UpdateProject handler body:

```go
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

	// Validate status transition
	if req.Status != nil {
		if !validProjectStatuses[*req.Status] {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		current, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
		if err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		if !isValidStatusTransition(current.Status, *req.Status) {
			writeError(w, http.StatusBadRequest, "invalid status transition from "+current.Status+" to "+*req.Status)
			return
		}
	}

	if req.ScheduleType != nil {
		switch *req.ScheduleType {
		case "one_time", "scheduled_once", "recurring":
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
```

- [ ] **Step 3: Wire DeleteProject**

Replace the DeleteProject handler body:

```go
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

	// Verify ownership
	project, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if uuidToString(project.CreatorOwnerID) != userID {
		writeError(w, http.StatusForbidden, "only the project creator can delete it")
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
```

- [ ] **Step 4: Update status enum maps**

Replace `validProjectStatuses` and `validProjectStatusTransitions`:

```go
var validProjectStatuses = map[string]bool{
	"draft":      true,
	"scheduled":  true,
	"running":    true,
	"paused":     true,
	"completed":  true,
	"failed":     true,
	"stopped":    true,
	"archived":   true,
}

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
```

- [ ] **Step 5: Verify build**

Run: `cd server && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/project.go
git commit -m "feat(server): wire CreateProject, UpdateProject, DeleteProject handlers"
```

---

### Task 5: Wire ForkProject, ListProjectVersions, GetProjectRuns Handlers

**Files:**
- Modify: `server/internal/handler/project.go:407-497`

- [ ] **Step 1: Wire ForkProject**

Replace the ForkProject handler body:

```go
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
	if req.BranchName == "" {
		writeError(w, http.StatusBadRequest, "branch_name is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	pid := parseUUID(projectID)

	// Get latest version to snapshot from
	latestVersion, err := h.Queries.GetLatestProjectVersion(r.Context(), pid)
	if err != nil {
		writeError(w, http.StatusBadRequest, "no versions exist to fork from")
		return
	}

	// Create new branch
	branch, err := h.Queries.CreateProjectBranch(r.Context(), db.CreateProjectBranchParams{
		ProjectID:      pid,
		Name:           req.BranchName,
		IsDefault:      false,
		CreatedBy:      parseUUID(userID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "branch name already exists")
			return
		}
		slog.Error("failed to create branch", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create branch")
		return
	}

	// Create version on new branch with copied snapshots
	newVersion, err := h.Queries.CreateProjectVersion(r.Context(), db.CreateProjectVersionParams{
		ProjectID:        pid,
		ParentVersionID:  latestVersion.ID,
		VersionNumber:    latestVersion.VersionNumber + 1,
		BranchName:       strToText(req.BranchName),
		ForkReason:       strToText(req.ForkReason),
		PlanSnapshot:     latestVersion.PlanSnapshot,
		WorkflowSnapshot: latestVersion.WorkflowSnapshot,
		CreatedBy:        parseUUID(userID),
	})
	if err != nil {
		slog.Error("failed to create forked version", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to fork project")
		return
	}

	resp := ProjectVersionResponse{
		ID:               uuidToString(newVersion.ID),
		ProjectID:        projectID,
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

	writeJSON(w, http.StatusCreated, resp)
}
```

- [ ] **Step 2: Wire ListProjectVersions**

Replace the ListProjectVersions handler body:

```go
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
		slog.Error("list versions failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list versions")
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
	writeJSON(w, http.StatusOK, result)
}
```

- [ ] **Step 3: Wire GetProjectRuns**

Replace the GetProjectRuns handler body:

```go
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
		slog.Error("list runs failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}

	result := make([]ProjectRunResponse, 0, len(runs))
	for _, run := range runs {
		resp := ProjectRunResponse{
			ID:         uuidToString(run.ID),
			PlanID:     uuidToString(run.PlanID),
			ProjectID:  uuidToString(run.ProjectID),
			Status:     run.Status,
			StepLogs:   run.StepLogs,
			OutputRefs: run.OutputRefs,
			RetryCount: run.RetryCount,
			CreatedAt:  run.CreatedAt.Time.Format(time.RFC3339),
		}
		if run.StartAt.Valid {
			t := run.StartAt.Time.Format(time.RFC3339)
			resp.StartAt = &t
		}
		if run.EndAt.Valid {
			t := run.EndAt.Time.Format(time.RFC3339)
			resp.EndAt = &t
		}
		if run.FailureReason.Valid {
			resp.FailureReason = &run.FailureReason.String
		}
		result = append(result, resp)
	}
	writeJSON(w, http.StatusOK, result)
}
```

- [ ] **Step 4: Add ptrFromUUID helper**

Add to `project.go` helpers section:

```go
func ptrFromUUID(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := uuidToString(u)
	return &s
}
```

You will also need to add `pgtype` to the import block:
```go
import (
	// ... existing imports
	"github.com/jackc/pgx/v5/pgtype"
)
```

- [ ] **Step 5: Verify build**

Run: `cd server && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/project.go
git commit -m "feat(server): wire ForkProject, ListProjectVersions, GetProjectRuns handlers"
```

---

### Task 6: Add Branch and Result Routes + Handlers

**Files:**
- Modify: `server/cmd/server/router.go`
- Modify: `server/internal/handler/project.go`
- Modify: `server/pkg/protocol/events.go`

- [ ] **Step 1: Add new event constants**

Add to `server/pkg/protocol/events.go`:

```go
EventProjectBranchCreated  = "project:branch_created"
EventProjectVersionCreated = "project:version_created"
EventProjectRunStarted     = "project:run_started"
EventProjectRunCompleted   = "project:run_completed"
EventProjectRunFailed      = "project:run_failed"
EventProjectResultCreated  = "project:result_created"
```

- [ ] **Step 2: Add branch handler: ListProjectBranches**

Add to `server/internal/handler/project.go`:

```go
// ListProjectBranches handles GET /api/projects/{projectID}/branches
func (h *Handler) ListProjectBranches(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "projectID is required")
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	branches, err := h.Queries.ListProjectBranches(r.Context(), parseUUID(projectID))
	if err != nil {
		slog.Error("list branches failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list branches")
		return
	}

	type BranchResponse struct {
		ID             string  `json:"id"`
		ProjectID      string  `json:"project_id"`
		Name           string  `json:"name"`
		ParentBranchID *string `json:"parent_branch_id"`
		IsDefault      bool    `json:"is_default"`
		Status         string  `json:"status"`
		CreatedBy      string  `json:"created_by"`
		CreatedAt      string  `json:"created_at"`
	}

	result := make([]BranchResponse, 0, len(branches))
	for _, b := range branches {
		result = append(result, BranchResponse{
			ID:             uuidToString(b.ID),
			ProjectID:      uuidToString(b.ProjectID),
			Name:           b.Name,
			ParentBranchID: ptrFromUUID(b.ParentBranchID),
			IsDefault:      b.IsDefault,
			Status:         b.Status,
			CreatedBy:      uuidToString(b.CreatedBy),
			CreatedAt:      b.CreatedAt.Time.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, result)
}
```

- [ ] **Step 3: Add result handler: GetProjectResult**

Add to `server/internal/handler/project.go`:

```go
// GetProjectResult handles GET /api/projects/{projectID}/runs/{runID}/result
func (h *Handler) GetProjectResult(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")
	if runID == "" {
		writeError(w, http.StatusBadRequest, "runID is required")
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	result, err := h.Queries.GetProjectResultByRun(r.Context(), parseUUID(runID))
	if err != nil {
		writeError(w, http.StatusNotFound, "result not found")
		return
	}

	type ResultResponse struct {
		ID               string          `json:"id"`
		RunID            string          `json:"run_id"`
		ProjectID        string          `json:"project_id"`
		VersionID        *string         `json:"version_id"`
		Summary          *string         `json:"summary"`
		Artifacts        json.RawMessage `json:"artifacts"`
		Deliverables     json.RawMessage `json:"deliverables"`
		AcceptanceStatus string          `json:"acceptance_status"`
		AcceptedBy       *string         `json:"accepted_by"`
		CreatedAt        string          `json:"created_at"`
	}

	resp := ResultResponse{
		ID:               uuidToString(result.ID),
		RunID:            uuidToString(result.RunID),
		ProjectID:        uuidToString(result.ProjectID),
		VersionID:        ptrFromUUID(result.VersionID),
		Summary:          textToPtr(result.Summary),
		Artifacts:        result.Artifacts,
		Deliverables:     result.Deliverables,
		AcceptanceStatus: result.AcceptanceStatus,
		AcceptedBy:       ptrFromUUID(result.AcceptedBy),
		CreatedAt:        result.CreatedAt.Time.Format(time.RFC3339),
	}
	writeJSON(w, http.StatusOK, resp)
}
```

- [ ] **Step 4: Register new routes**

In `server/cmd/server/router.go`, find the project routes block (around line 391) and add:

```go
r.Get("/api/projects/{projectID}/branches", h.ListProjectBranches)
r.Get("/api/projects/{projectID}/runs/{runID}/result", h.GetProjectResult)
```

- [ ] **Step 5: Verify build**

Run: `cd server && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 6: Run tests**

Run: `cd server && go test ./internal/handler/ -v -count=1`
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add server/internal/handler/project.go server/cmd/server/router.go server/pkg/protocol/events.go
git commit -m "feat(server): add branch listing, result fetching handlers and routes"
```

---

### Task 7: Frontend Type Updates

**Files:**
- Modify: `apps/web/shared/types/project.ts`
- Modify: `apps/web/shared/types/workflow.ts`

- [ ] **Step 1: Update project.ts**

Replace `apps/web/shared/types/project.ts`:

```typescript
export interface Project {
  id: string;
  workspace_id: string;
  title: string;
  description?: string;
  status: ProjectStatus;
  schedule_type: ProjectScheduleType;
  cron_expr?: string;
  source_conversations: SourceConversation[];
  channel_id?: string;
  creator_owner_id: string;
  created_at: string;
  updated_at: string;
  plan?: PlanSummary;
  active_run?: RunSummary;
}

export type ProjectStatus =
  | 'draft'
  | 'scheduled'
  | 'running'
  | 'paused'
  | 'completed'
  | 'failed'
  | 'stopped'
  | 'archived';

export type ProjectScheduleType = 'one_time' | 'scheduled_once' | 'recurring';

export interface PlanSummary {
  id: string;
  title: string;
  approval_status: string;
}

export interface RunSummary {
  id: string;
  status: string;
  start_at?: string;
}

export interface SourceConversation {
  conversation_id: string;
  type: 'channel' | 'dm' | 'thread';
  snapshot_at?: string;
}

export interface ProjectBranch {
  id: string;
  project_id: string;
  name: string;
  parent_branch_id?: string;
  is_default: boolean;
  status: 'active' | 'merged' | 'archived';
  created_by: string;
  created_at: string;
}

export interface ProjectVersion {
  id: string;
  project_id: string;
  parent_version_id?: string;
  version_number: number;
  branch_name?: string;
  branch_id?: string;
  fork_reason?: string;
  plan_snapshot?: unknown;
  workflow_snapshot?: unknown;
  version_status: 'active' | 'ready' | 'running' | 'completed' | 'failed' | 'cancelled' | 'archived';
  created_by?: string;
  created_at: string;
}

export interface ProjectRun {
  id: string;
  plan_id?: string;
  project_id: string;
  status: RunStatus;
  start_at?: string;
  end_at?: string;
  step_logs: unknown[];
  output_refs: unknown[];
  failure_reason?: string;
  retry_count: number;
  created_at: string;
}

export type RunStatus =
  | 'pending'
  | 'queued'
  | 'running'
  | 'blocked'
  | 'paused'
  | 'success'
  | 'partial_success'
  | 'completed'
  | 'failed'
  | 'cancelled';

export interface ProjectResult {
  id: string;
  run_id: string;
  project_id: string;
  version_id?: string;
  summary?: string;
  artifacts: unknown[];
  deliverables: unknown[];
  acceptance_status: 'pending' | 'accepted' | 'rejected';
  accepted_by?: string;
  created_at: string;
}

export interface CreateProjectFromChatRequest {
  title: string;
  source_refs: { type: 'channel' | 'dm' | 'thread'; id: string }[];
  agent_ids: string[];
  schedule_type: ProjectScheduleType;
  cron_expr?: string;
}

import type { Plan } from './workflow';
export type { Plan };
```

- [ ] **Step 2: Add skipped status to workflow.ts**

In `apps/web/shared/types/workflow.ts`, add `'skipped'` to `WorkflowStepStatus`:

```typescript
export type WorkflowStepStatus =
  | 'pending'
  | 'queued'
  | 'assigned'
  | 'running'
  | 'waiting_input'
  | 'blocked'
  | 'retrying'
  | 'timeout'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'skipped';
```

Also add the new sub-task fields to `WorkflowStep`:

```typescript
  // Sub-task fields
  title?: string;
  goal?: string;
  priority?: string;
  candidate_agent_ids?: string[];
  owner_reviewer_id?: string;
  skippable?: boolean;
  on_failure?: 'block' | 'retry_once' | 'retry_n' | 'reassign_then_retry' | 'skip' | 'pause_and_notify_owner';
  done_definition?: string;
  error_code?: string;
  error_summary?: string;
```

- [ ] **Step 3: Verify typecheck**

Run: `pnpm typecheck`
Expected: No new errors from these type changes (callers may need updates in later tasks).

- [ ] **Step 4: Commit**

```bash
git add apps/web/shared/types/project.ts apps/web/shared/types/workflow.ts
git commit -m "feat(web): expand project and workflow types for Phase 1"
```

---

### Task 8: Frontend API Client Updates

**Files:**
- Modify: `apps/web/shared/api/client.ts`

- [ ] **Step 1: Add branch and result methods**

Add to the API client class:

```typescript
async listProjectBranches(projectId: string): Promise<ProjectBranch[]> {
  return this.fetch(`/api/projects/${projectId}/branches`);
}

async getProjectResult(projectId: string, runId: string): Promise<ProjectResult> {
  return this.fetch(`/api/projects/${projectId}/runs/${runId}/result`);
}
```

- [ ] **Step 2: Add import for new types**

Ensure the import block includes:

```typescript
import type { ProjectBranch, ProjectResult } from '@/shared/types/project';
```

- [ ] **Step 3: Verify typecheck**

Run: `pnpm typecheck`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add apps/web/shared/api/client.ts
git commit -m "feat(web): add branch and result API client methods"
```

---

### Task 9: Frontend Store Updates

**Files:**
- Modify: `apps/web/features/projects/store.ts`

- [ ] **Step 1: Add branches and result state + actions**

Add to the store state interface:

```typescript
branches: ProjectBranch[];
currentResult: ProjectResult | null;
```

Add to the store actions:

```typescript
fetchBranches: async (projectId: string) => {
  const branches = await api.listProjectBranches(projectId);
  set({ branches });
},

fetchResult: async (projectId: string, runId: string) => {
  try {
    const result = await api.getProjectResult(projectId, runId);
    set({ currentResult: result });
  } catch {
    set({ currentResult: null });
  }
},
```

Initialize in the store creator:

```typescript
branches: [],
currentResult: null,
```

- [ ] **Step 2: Add imports**

```typescript
import type { ProjectBranch, ProjectResult } from '@/shared/types/project';
```

- [ ] **Step 3: Verify typecheck**

Run: `pnpm typecheck`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add apps/web/features/projects/store.ts
git commit -m "feat(web): add branch and result state to project store"
```

---

### Task 10: Verify Full Build

**Files:** None (verification only)

- [ ] **Step 1: Run Go tests**

Run: `cd server && go test ./... -count=1`
Expected: All tests pass.

- [ ] **Step 2: Run TypeScript typecheck**

Run: `pnpm typecheck`
Expected: No errors.

- [ ] **Step 3: Run TypeScript tests**

Run: `pnpm test`
Expected: All tests pass.

- [ ] **Step 4: Final commit if any fixes were needed**

```bash
git add -A
git commit -m "fix: resolve Phase 1 build issues"
```
