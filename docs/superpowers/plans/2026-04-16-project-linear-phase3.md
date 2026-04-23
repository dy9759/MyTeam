# ProjectLinear Phase 3: Branching & PR

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add pull request infrastructure for merging branches, with conflict detection, merge logic, and frontend PR review UI.

**Architecture:** New `project_pr` table tracks PRs between branches. Merge creates a new version on the target branch by combining plan snapshots. V1 has manual conflict resolution only. Frontend gets PR creation dialog and review page within the project detail.

**Tech Stack:** Go 1.26, PostgreSQL 17, Next.js 16, TypeScript, Zustand, Tailwind CSS

**Depends on:** Phase 1 (project_branch table), Phase 2 (context import)

---

## File Structure

### Backend
- **Create:** `server/migrations/047_project_pr.up.sql`
- **Create:** `server/migrations/047_project_pr.down.sql`
- **Create:** `server/pkg/db/queries/project_prs.sql`
- **Create:** `server/internal/handler/project_pr.go`
- **Modify:** `server/cmd/server/router.go` -- PR routes
- **Modify:** `server/pkg/protocol/events.go` -- PR events

### Frontend
- **Create:** `apps/web/features/projects/components/pr-dialog.tsx`
- **Create:** `apps/web/features/projects/components/pr-review.tsx`
- **Modify:** `apps/web/shared/types/project.ts` -- ProjectPR type
- **Modify:** `apps/web/shared/api/client.ts` -- PR methods
- **Modify:** `apps/web/features/projects/store.ts` -- PR state

---

### Task 1: Migration -- project_pr table

**Files:**
- Create: `server/migrations/047_project_pr.up.sql`
- Create: `server/migrations/047_project_pr.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- server/migrations/047_project_pr.up.sql

CREATE TABLE project_pr (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  source_branch_id UUID NOT NULL REFERENCES project_branch(id),
  target_branch_id UUID NOT NULL REFERENCES project_branch(id),
  source_version_id UUID NOT NULL REFERENCES project_version(id),
  title TEXT NOT NULL,
  description TEXT,
  status TEXT NOT NULL DEFAULT 'open',
  has_conflicts BOOLEAN NOT NULL DEFAULT FALSE,
  merged_version_id UUID REFERENCES project_version(id),
  created_by UUID NOT NULL,
  merged_by UUID,
  merged_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_project_pr_project ON project_pr(project_id);
CREATE INDEX idx_project_pr_status ON project_pr(project_id, status);
```

- [ ] **Step 2: Write the down migration**

```sql
-- server/migrations/047_project_pr.down.sql
DROP TABLE IF EXISTS project_pr;
```

- [ ] **Step 3: Run migration**

Run: `cd server && make migrate-up`

- [ ] **Step 4: Commit**

```bash
git add server/migrations/047_project_pr.up.sql server/migrations/047_project_pr.down.sql
git commit -m "feat(server): add project_pr table (migration 047)"
```

---

### Task 2: SQLC Queries for project_pr

**Files:**
- Create: `server/pkg/db/queries/project_prs.sql`

- [ ] **Step 1: Write queries**

```sql
-- server/pkg/db/queries/project_prs.sql

-- name: CreateProjectPR :one
INSERT INTO project_pr (project_id, source_branch_id, target_branch_id, source_version_id, title, description, created_by)
VALUES (@project_id, @source_branch_id, @target_branch_id, @source_version_id, @title, @description, @created_by)
RETURNING *;

-- name: GetProjectPR :one
SELECT * FROM project_pr WHERE id = @id;

-- name: ListProjectPRs :many
SELECT * FROM project_pr WHERE project_id = @project_id ORDER BY created_at DESC;

-- name: ListProjectPRsByStatus :many
SELECT * FROM project_pr WHERE project_id = @project_id AND status = @status ORDER BY created_at DESC;

-- name: UpdateProjectPRStatus :exec
UPDATE project_pr SET status = @status, updated_at = NOW() WHERE id = @id;

-- name: MergeProjectPR :exec
UPDATE project_pr
SET status = 'merged', merged_version_id = @merged_version_id, merged_by = @merged_by, merged_at = NOW(), updated_at = NOW()
WHERE id = @id;

-- name: CloseProjectPR :exec
UPDATE project_pr SET status = 'closed', updated_at = NOW() WHERE id = @id;

-- name: UpdateProjectPRConflicts :exec
UPDATE project_pr SET has_conflicts = @has_conflicts, updated_at = NOW() WHERE id = @id;
```

- [ ] **Step 2: Regenerate sqlc and verify build**

Run: `cd server && make sqlc && go build ./...`

- [ ] **Step 3: Commit**

```bash
git add server/pkg/db/queries/project_prs.sql server/pkg/db/generated/
git commit -m "feat(server): add sqlc queries for project_pr"
```

---

### Task 3: PR Handler -- Create, List, Get, Merge, Close

**Files:**
- Create: `server/internal/handler/project_pr.go`
- Modify: `server/cmd/server/router.go`
- Modify: `server/pkg/protocol/events.go`

- [ ] **Step 1: Add PR event constants**

Add to `server/pkg/protocol/events.go`:

```go
EventProjectPRCreated = "project:pr_created"
EventProjectPRMerged  = "project:pr_merged"
EventProjectPRClosed  = "project:pr_closed"
```

- [ ] **Step 2: Write the PR handler**

```go
// server/internal/handler/project_pr.go
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type PRResponse struct {
	ID               string  `json:"id"`
	ProjectID        string  `json:"project_id"`
	SourceBranchID   string  `json:"source_branch_id"`
	TargetBranchID   string  `json:"target_branch_id"`
	SourceVersionID  string  `json:"source_version_id"`
	Title            string  `json:"title"`
	Description      *string `json:"description"`
	Status           string  `json:"status"`
	HasConflicts     bool    `json:"has_conflicts"`
	MergedVersionID  *string `json:"merged_version_id"`
	CreatedBy        string  `json:"created_by"`
	MergedBy         *string `json:"merged_by"`
	MergedAt         *string `json:"merged_at"`
	CreatedAt        string  `json:"created_at"`
}

func prToResponse(pr db.ProjectPr) PRResponse {
	resp := PRResponse{
		ID:              uuidToString(pr.ID),
		ProjectID:       uuidToString(pr.ProjectID),
		SourceBranchID:  uuidToString(pr.SourceBranchID),
		TargetBranchID:  uuidToString(pr.TargetBranchID),
		SourceVersionID: uuidToString(pr.SourceVersionID),
		Title:           pr.Title,
		Description:     textToPtr(pr.Description),
		Status:          pr.Status,
		HasConflicts:    pr.HasConflicts,
		MergedVersionID: ptrFromUUID(pr.MergedVersionID),
		CreatedBy:       uuidToString(pr.CreatedBy),
		MergedBy:        ptrFromUUID(pr.MergedBy),
		CreatedAt:       pr.CreatedAt.Time.Format(time.RFC3339),
	}
	if pr.MergedAt.Valid {
		t := pr.MergedAt.Time.Format(time.RFC3339)
		resp.MergedAt = &t
	}
	return resp
}

// CreateProjectPR handles POST /api/projects/{projectID}/prs
func (h *Handler) CreateProjectPR(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	type CreatePRRequest struct {
		SourceBranchID  string `json:"source_branch_id"`
		TargetBranchID  string `json:"target_branch_id"`
		SourceVersionID string `json:"source_version_id"`
		Title           string `json:"title"`
		Description     string `json:"description"`
	}

	var req CreatePRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" || req.SourceBranchID == "" || req.TargetBranchID == "" || req.SourceVersionID == "" {
		writeError(w, http.StatusBadRequest, "title, source_branch_id, target_branch_id, and source_version_id are required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	pr, err := h.Queries.CreateProjectPR(r.Context(), db.CreateProjectPRParams{
		ProjectID:       parseUUID(projectID),
		SourceBranchID:  parseUUID(req.SourceBranchID),
		TargetBranchID:  parseUUID(req.TargetBranchID),
		SourceVersionID: parseUUID(req.SourceVersionID),
		Title:           req.Title,
		Description:     strToText(req.Description),
		CreatedBy:       parseUUID(userID),
	})
	if err != nil {
		slog.Error("failed to create PR", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create PR")
		return
	}

	h.publish(protocol.EventProjectPRCreated, workspaceID, "member", userID, map[string]any{
		"pr_id":      uuidToString(pr.ID),
		"project_id": projectID,
	})

	writeJSON(w, http.StatusCreated, prToResponse(pr))
}

// ListProjectPRs handles GET /api/projects/{projectID}/prs
func (h *Handler) ListProjectPRs(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	prs, err := h.Queries.ListProjectPRs(r.Context(), parseUUID(projectID))
	if err != nil {
		slog.Error("list PRs failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list PRs")
		return
	}

	result := make([]PRResponse, 0, len(prs))
	for _, pr := range prs {
		result = append(result, prToResponse(pr))
	}
	writeJSON(w, http.StatusOK, result)
}

// GetProjectPR handles GET /api/projects/{projectID}/prs/{prID}
func (h *Handler) GetProjectPR(w http.ResponseWriter, r *http.Request) {
	prID := chi.URLParam(r, "prID")

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	pr, err := h.Queries.GetProjectPR(r.Context(), parseUUID(prID))
	if err != nil {
		writeError(w, http.StatusNotFound, "PR not found")
		return
	}

	writeJSON(w, http.StatusOK, prToResponse(pr))
}

// MergeProjectPR handles POST /api/projects/{projectID}/prs/{prID}/merge
func (h *Handler) MergeProjectPR(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	prID := chi.URLParam(r, "prID")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	pr, err := h.Queries.GetProjectPR(r.Context(), parseUUID(prID))
	if err != nil {
		writeError(w, http.StatusNotFound, "PR not found")
		return
	}

	if pr.Status != "open" {
		writeError(w, http.StatusBadRequest, "PR is not open")
		return
	}

	if pr.HasConflicts {
		writeError(w, http.StatusConflict, "PR has conflicts that must be resolved first")
		return
	}

	// Get source version to copy snapshots from
	sourceVersion, err := h.Queries.GetProjectVersion(r.Context(), pr.SourceVersionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load source version")
		return
	}

	// Get latest version number on target branch for incrementing
	latestVersion, _ := h.Queries.GetLatestProjectVersion(r.Context(), parseUUID(projectID))
	nextVersionNumber := int32(1)
	if latestVersion.ID.Valid {
		nextVersionNumber = latestVersion.VersionNumber + 1
	}

	// Create merged version on target branch
	mergedVersion, err := h.Queries.CreateProjectVersion(r.Context(), db.CreateProjectVersionParams{
		ProjectID:        parseUUID(projectID),
		ParentVersionID:  pr.SourceVersionID,
		VersionNumber:    nextVersionNumber,
		BranchName:       pgtype.Text{}, // Will be set from target branch
		PlanSnapshot:     sourceVersion.PlanSnapshot,
		WorkflowSnapshot: sourceVersion.WorkflowSnapshot,
		CreatedBy:        parseUUID(userID),
	})
	if err != nil {
		slog.Error("failed to create merged version", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to merge")
		return
	}

	// Update PR as merged
	_ = h.Queries.MergeProjectPR(r.Context(), db.MergeProjectPRParams{
		ID:              parseUUID(prID),
		MergedVersionID: mergedVersion.ID,
		MergedBy:        parseUUID(userID),
	})

	// Update source branch status to merged
	_ = h.Queries.UpdateProjectBranchStatus(r.Context(), db.UpdateProjectBranchStatusParams{
		ID:     pr.SourceBranchID,
		Status: "merged",
	})

	h.publish(protocol.EventProjectPRMerged, workspaceID, "member", userID, map[string]any{
		"pr_id":      prID,
		"project_id": projectID,
		"version_id": uuidToString(mergedVersion.ID),
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "merged",
		"version_id": uuidToString(mergedVersion.ID),
	})
}

// CloseProjectPR handles POST /api/projects/{projectID}/prs/{prID}/close
func (h *Handler) CloseProjectPR(w http.ResponseWriter, r *http.Request) {
	prID := chi.URLParam(r, "prID")
	projectID := chi.URLParam(r, "projectID")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	_ = h.Queries.CloseProjectPR(r.Context(), parseUUID(prID))

	h.publish(protocol.EventProjectPRClosed, workspaceID, "member", userID, map[string]any{
		"pr_id":      prID,
		"project_id": projectID,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}
```

- [ ] **Step 3: Register PR routes**

Add to `server/cmd/server/router.go`:

```go
r.Post("/api/projects/{projectID}/prs", h.CreateProjectPR)
r.Get("/api/projects/{projectID}/prs", h.ListProjectPRs)
r.Get("/api/projects/{projectID}/prs/{prID}", h.GetProjectPR)
r.Post("/api/projects/{projectID}/prs/{prID}/merge", h.MergeProjectPR)
r.Post("/api/projects/{projectID}/prs/{prID}/close", h.CloseProjectPR)
```

- [ ] **Step 4: Verify build**

Run: `cd server && go build ./...`

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/project_pr.go server/cmd/server/router.go server/pkg/protocol/events.go
git commit -m "feat(server): add PR handlers for create, list, merge, close"
```

---

### Task 4: Frontend PR Types, API, Store

**Files:**
- Modify: `apps/web/shared/types/project.ts`
- Modify: `apps/web/shared/api/client.ts`
- Modify: `apps/web/features/projects/store.ts`

- [ ] **Step 1: Add ProjectPR type**

Add to `apps/web/shared/types/project.ts`:

```typescript
export interface ProjectPR {
  id: string;
  project_id: string;
  source_branch_id: string;
  target_branch_id: string;
  source_version_id: string;
  title: string;
  description?: string;
  status: 'open' | 'merged' | 'closed' | 'needs_review';
  has_conflicts: boolean;
  merged_version_id?: string;
  created_by: string;
  merged_by?: string;
  merged_at?: string;
  created_at: string;
}
```

- [ ] **Step 2: Add API methods**

Add to `apps/web/shared/api/client.ts`:

```typescript
async listProjectPRs(projectId: string): Promise<ProjectPR[]> {
  return this.fetch(`/api/projects/${projectId}/prs`);
}

async createProjectPR(projectId: string, data: {
  source_branch_id: string;
  target_branch_id: string;
  source_version_id: string;
  title: string;
  description?: string;
}): Promise<ProjectPR> {
  return this.fetch(`/api/projects/${projectId}/prs`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

async mergeProjectPR(projectId: string, prId: string): Promise<void> {
  await this.fetch(`/api/projects/${projectId}/prs/${prId}/merge`, { method: 'POST' });
}

async closeProjectPR(projectId: string, prId: string): Promise<void> {
  await this.fetch(`/api/projects/${projectId}/prs/${prId}/close`, { method: 'POST' });
}
```

- [ ] **Step 3: Add store state**

Add to store:

```typescript
// State
prs: ProjectPR[];

// Actions
fetchPRs: async (projectId: string) => {
  const prs = await api.listProjectPRs(projectId);
  set({ prs });
},

createPR: async (projectId: string, data: {
  source_branch_id: string;
  target_branch_id: string;
  source_version_id: string;
  title: string;
  description?: string;
}) => {
  const pr = await api.createProjectPR(projectId, data);
  set((s) => ({ prs: [pr, ...s.prs] }));
  return pr;
},

mergePR: async (projectId: string, prId: string) => {
  await api.mergeProjectPR(projectId, prId);
  get().fetchPRs(projectId);
  get().fetchVersions(projectId);
  get().fetchBranches(projectId);
},

closePR: async (projectId: string, prId: string) => {
  await api.closeProjectPR(projectId, prId);
  set((s) => ({ prs: s.prs.map((pr) => pr.id === prId ? { ...pr, status: 'closed' as const } : pr) }));
},
```

Initialize `prs: []`.

- [ ] **Step 4: Verify typecheck**

Run: `pnpm typecheck`

- [ ] **Step 5: Commit**

```bash
git add apps/web/shared/types/project.ts apps/web/shared/api/client.ts apps/web/features/projects/store.ts
git commit -m "feat(web): add PR types, API methods, and store actions"
```

---

### Task 5: Verify Full Build

- [ ] **Step 1: Run all checks**

Run: `cd server && go build ./... && go test ./... -count=1`
Run: `pnpm typecheck && pnpm test`

- [ ] **Step 2: Commit fixes if needed**

```bash
git add -A && git commit -m "fix: resolve Phase 3 build issues"
```
