# ProjectLinear Phase 4: Permissions & Sharing

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add project sharing with viewer/editor roles, permission middleware for project routes, and plan visibility controls.

**Architecture:** New `project_share` table tracks which owners have access to which projects and at what role. A permission middleware checks access on all project routes. Plan visibility is controlled by a project-level setting.

**Tech Stack:** Go 1.26, PostgreSQL 17, Next.js 16, TypeScript, Zustand, Tailwind CSS

**Depends on:** Phase 1, Phase 3 (PR merge requires permission checks)

---

## File Structure

### Backend
- **Create:** `server/migrations/048_project_share.up.sql`
- **Create:** `server/migrations/048_project_share.down.sql`
- **Create:** `server/pkg/db/queries/project_shares.sql`
- **Create:** `server/internal/handler/project_share.go`
- **Modify:** `server/cmd/server/router.go` -- Share routes

### Frontend
- **Create:** `apps/web/features/projects/components/share-dialog.tsx`
- **Modify:** `apps/web/shared/types/project.ts` -- ProjectShare type
- **Modify:** `apps/web/shared/api/client.ts` -- Share methods
- **Modify:** `apps/web/features/projects/store.ts` -- Share state

---

### Task 1: Migration -- project_share table

**Files:**
- Create: `server/migrations/048_project_share.up.sql`
- Create: `server/migrations/048_project_share.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- server/migrations/048_project_share.up.sql

CREATE TABLE project_share (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  owner_id UUID NOT NULL,
  role TEXT NOT NULL DEFAULT 'viewer',
  can_merge_pr BOOLEAN NOT NULL DEFAULT FALSE,
  granted_by UUID NOT NULL,
  granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(project_id, owner_id)
);

CREATE INDEX idx_project_share_project ON project_share(project_id);
CREATE INDEX idx_project_share_owner ON project_share(owner_id);
```

- [ ] **Step 2: Write the down migration**

```sql
-- server/migrations/048_project_share.down.sql
DROP TABLE IF EXISTS project_share;
```

- [ ] **Step 3: Run migration**

Run: `cd server && make migrate-up`

- [ ] **Step 4: Commit**

```bash
git add server/migrations/048_project_share.up.sql server/migrations/048_project_share.down.sql
git commit -m "feat(server): add project_share table (migration 048)"
```

---

### Task 2: SQLC Queries for project_share

**Files:**
- Create: `server/pkg/db/queries/project_shares.sql`

- [ ] **Step 1: Write queries**

```sql
-- server/pkg/db/queries/project_shares.sql

-- name: CreateProjectShare :one
INSERT INTO project_share (project_id, owner_id, role, can_merge_pr, granted_by)
VALUES (@project_id, @owner_id, @role, @can_merge_pr, @granted_by)
RETURNING *;

-- name: GetProjectShare :one
SELECT * FROM project_share WHERE project_id = @project_id AND owner_id = @owner_id;

-- name: ListProjectShares :many
SELECT * FROM project_share WHERE project_id = @project_id ORDER BY granted_at ASC;

-- name: ListSharedProjects :many
SELECT p.* FROM project p
JOIN project_share ps ON ps.project_id = p.id
WHERE ps.owner_id = @owner_id
ORDER BY p.updated_at DESC;

-- name: UpdateProjectShare :exec
UPDATE project_share SET role = @role, can_merge_pr = @can_merge_pr WHERE id = @id;

-- name: DeleteProjectShare :exec
DELETE FROM project_share WHERE project_id = @project_id AND owner_id = @owner_id;

-- name: CheckProjectAccess :one
SELECT COALESCE(
  (SELECT role FROM project_share WHERE project_id = @project_id AND owner_id = @owner_id),
  CASE WHEN EXISTS(SELECT 1 FROM project WHERE id = @project_id AND creator_owner_id = @owner_id) THEN 'owner' ELSE '' END
) AS access_role;
```

- [ ] **Step 2: Regenerate sqlc and verify build**

Run: `cd server && make sqlc && go build ./...`

- [ ] **Step 3: Commit**

```bash
git add server/pkg/db/queries/project_shares.sql server/pkg/db/generated/
git commit -m "feat(server): add sqlc queries for project_share"
```

---

### Task 3: Share Handler

**Files:**
- Create: `server/internal/handler/project_share.go`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Write the handler**

```go
// server/internal/handler/project_share.go
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ShareResponse struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	OwnerID    string `json:"owner_id"`
	Role       string `json:"role"`
	CanMergePR bool   `json:"can_merge_pr"`
	GrantedBy  string `json:"granted_by"`
	GrantedAt  string `json:"granted_at"`
}

// ShareProject handles POST /api/projects/{projectID}/share
func (h *Handler) ShareProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	type ShareRequest struct {
		OwnerID    string `json:"owner_id"`
		Role       string `json:"role"`
		CanMergePR bool   `json:"can_merge_pr"`
	}

	var req ShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.OwnerID == "" {
		writeError(w, http.StatusBadRequest, "owner_id is required")
		return
	}

	if req.Role != "viewer" && req.Role != "editor" {
		writeError(w, http.StatusBadRequest, "role must be viewer or editor")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Verify caller is the project owner
	project, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if uuidToString(project.CreatorOwnerID) != userID {
		writeError(w, http.StatusForbidden, "only the project owner can share")
		return
	}

	share, err := h.Queries.CreateProjectShare(r.Context(), db.CreateProjectShareParams{
		ProjectID:  parseUUID(projectID),
		OwnerID:    parseUUID(req.OwnerID),
		Role:       req.Role,
		CanMergePR: req.CanMergePR,
		GrantedBy:  parseUUID(userID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "project already shared with this owner")
			return
		}
		slog.Error("failed to share project", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to share project")
		return
	}

	writeJSON(w, http.StatusCreated, ShareResponse{
		ID:         uuidToString(share.ID),
		ProjectID:  uuidToString(share.ProjectID),
		OwnerID:    uuidToString(share.OwnerID),
		Role:       share.Role,
		CanMergePR: share.CanMergePR,
		GrantedBy:  uuidToString(share.GrantedBy),
		GrantedAt:  share.GrantedAt.Time.Format(time.RFC3339),
	})
}

// ListProjectShares handles GET /api/projects/{projectID}/shares
func (h *Handler) ListProjectShares(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	shares, err := h.Queries.ListProjectShares(r.Context(), parseUUID(projectID))
	if err != nil {
		slog.Error("list shares failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list shares")
		return
	}

	result := make([]ShareResponse, 0, len(shares))
	for _, s := range shares {
		result = append(result, ShareResponse{
			ID:         uuidToString(s.ID),
			ProjectID:  uuidToString(s.ProjectID),
			OwnerID:    uuidToString(s.OwnerID),
			Role:       s.Role,
			CanMergePR: s.CanMergePR,
			GrantedBy:  uuidToString(s.GrantedBy),
			GrantedAt:  s.GrantedAt.Time.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// RemoveProjectShare handles DELETE /api/projects/{projectID}/share/{ownerID}
func (h *Handler) RemoveProjectShare(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	ownerID := chi.URLParam(r, "ownerID")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Verify caller is project owner
	project, err := h.Queries.GetProject(r.Context(), parseUUID(projectID))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if uuidToString(project.CreatorOwnerID) != userID {
		writeError(w, http.StatusForbidden, "only the project owner can remove shares")
		return
	}

	_ = h.Queries.DeleteProjectShare(r.Context(), db.DeleteProjectShareParams{
		ProjectID: parseUUID(projectID),
		OwnerID:   parseUUID(ownerID),
	})

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 2: Register routes**

Add to `server/cmd/server/router.go`:

```go
r.Post("/api/projects/{projectID}/share", h.ShareProject)
r.Get("/api/projects/{projectID}/shares", h.ListProjectShares)
r.Delete("/api/projects/{projectID}/share/{ownerID}", h.RemoveProjectShare)
```

- [ ] **Step 3: Verify build**

Run: `cd server && go build ./...`

- [ ] **Step 4: Commit**

```bash
git add server/internal/handler/project_share.go server/cmd/server/router.go
git commit -m "feat(server): add project sharing handlers"
```

---

### Task 4: Frontend Share Types, API, Store

**Files:**
- Modify: `apps/web/shared/types/project.ts`
- Modify: `apps/web/shared/api/client.ts`
- Modify: `apps/web/features/projects/store.ts`

- [ ] **Step 1: Add type**

Add to `apps/web/shared/types/project.ts`:

```typescript
export interface ProjectShare {
  id: string;
  project_id: string;
  owner_id: string;
  role: 'viewer' | 'editor';
  can_merge_pr: boolean;
  granted_by: string;
  granted_at: string;
}
```

- [ ] **Step 2: Add API methods**

```typescript
async shareProject(projectId: string, data: { owner_id: string; role: string; can_merge_pr?: boolean }): Promise<ProjectShare> {
  return this.fetch(`/api/projects/${projectId}/share`, { method: 'POST', body: JSON.stringify(data) });
}

async listProjectShares(projectId: string): Promise<ProjectShare[]> {
  return this.fetch(`/api/projects/${projectId}/shares`);
}

async removeProjectShare(projectId: string, ownerId: string): Promise<void> {
  await this.fetch(`/api/projects/${projectId}/share/${ownerId}`, { method: 'DELETE' });
}
```

- [ ] **Step 3: Add store state**

```typescript
// State
shares: ProjectShare[];

// Actions
fetchShares: async (projectId: string) => {
  const shares = await api.listProjectShares(projectId);
  set({ shares });
},

shareProject: async (projectId: string, data: { owner_id: string; role: string; can_merge_pr?: boolean }) => {
  const share = await api.shareProject(projectId, data);
  set((s) => ({ shares: [...s.shares, share] }));
},

removeShare: async (projectId: string, ownerId: string) => {
  await api.removeProjectShare(projectId, ownerId);
  set((s) => ({ shares: s.shares.filter((sh) => sh.owner_id !== ownerId) }));
},
```

Initialize `shares: []`.

- [ ] **Step 4: Verify typecheck**

Run: `pnpm typecheck`

- [ ] **Step 5: Commit**

```bash
git add apps/web/shared/types/project.ts apps/web/shared/api/client.ts apps/web/features/projects/store.ts
git commit -m "feat(web): add project sharing types, API, and store"
```

---

### Task 5: Verify Full Build

- [ ] **Step 1: Run all checks**

Run: `cd server && go build ./... && go test ./... -count=1`
Run: `pnpm typecheck && pnpm test`

- [ ] **Step 2: Commit fixes if needed**

```bash
git add -A && git commit -m "fix: resolve Phase 4 build issues"
```
