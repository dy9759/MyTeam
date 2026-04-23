# ProjectLinear Phase 6: Desktop App Support

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add project management to the Electron desktop app with API client methods, a project route, and Zustand store for project state.

**Architecture:** Follows the existing desktop app pattern: `DesktopApiClient` gets project methods, a new `project-route.tsx` reuses the sidebar+detail pattern from `session-route.tsx`, and a Zustand store manages project state. The desktop version is simplified compared to web (no PR review, no sharing management -- those stay web-only for v1).

**Tech Stack:** TypeScript, React, Zustand, Tailwind CSS, Electron (via desktop app shell)

**Depends on:** Phase 1-5 (all backend endpoints must exist)

---

## File Structure

### Desktop App
- **Modify:** `packages/client-core/src/desktop-api-client.ts` -- Add all project API methods
- **Create:** `apps/desktop/src/routes/project-route.tsx` -- Project page
- **Create:** `apps/desktop/src/lib/project-store.ts` -- Desktop project Zustand store
- **Modify:** `apps/desktop/src/app.tsx` or router config -- Add project route

---

### Task 1: Desktop API Client -- Project Methods

**Files:**
- Modify: `packages/client-core/src/desktop-api-client.ts`

- [ ] **Step 1: Add project methods**

Add to `DesktopApiClient`:

```typescript
async listProjects(): Promise<unknown[]> {
  return this.request('/api/projects');
}

async getProject(id: string): Promise<unknown> {
  return this.request(`/api/projects/${id}`);
}

async createProject(data: {
  title: string;
  description?: string;
  schedule_type?: string;
}): Promise<unknown> {
  return this.request('/api/projects', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

async updateProject(id: string, data: Record<string, unknown>): Promise<unknown> {
  return this.request(`/api/projects/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

async deleteProject(id: string): Promise<void> {
  return this.request(`/api/projects/${id}`, { method: 'DELETE' });
}

async listProjectBranches(projectId: string): Promise<unknown[]> {
  return this.request(`/api/projects/${projectId}/branches`);
}

async listProjectVersions(projectId: string): Promise<unknown[]> {
  return this.request(`/api/projects/${projectId}/versions`);
}

async listProjectRuns(projectId: string): Promise<unknown[]> {
  return this.request(`/api/projects/${projectId}/runs`);
}

async getProjectResult(projectId: string, runId: string): Promise<unknown> {
  return this.request(`/api/projects/${projectId}/runs/${runId}/result`);
}

async approvePlan(projectId: string): Promise<void> {
  return this.request(`/api/projects/${projectId}/approve`, { method: 'POST' });
}

async rejectPlan(projectId: string, reason: string): Promise<void> {
  return this.request(`/api/projects/${projectId}/reject`, {
    method: 'POST',
    body: JSON.stringify({ reason }),
  });
}

async forkProject(projectId: string, data: {
  branch_name: string;
  fork_reason?: string;
}): Promise<unknown> {
  return this.request(`/api/projects/${projectId}/fork`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}
```

- [ ] **Step 2: Verify build**

Run: `pnpm typecheck`

- [ ] **Step 3: Commit**

```bash
git add packages/client-core/src/desktop-api-client.ts
git commit -m "feat(desktop): add project API methods to DesktopApiClient"
```

---

### Task 2: Desktop Project Store

**Files:**
- Create: `apps/desktop/src/lib/project-store.ts`

- [ ] **Step 1: Write the store**

```typescript
// apps/desktop/src/lib/project-store.ts
import { create } from "zustand";
import { desktopApi } from "./desktop-client";

interface ProjectItem {
  id: string;
  title: string;
  description?: string;
  status: string;
  schedule_type: string;
  channel_id?: string;
  creator_owner_id: string;
  created_at: string;
  updated_at: string;
  plan?: { id: string; title: string; approval_status: string };
  active_run?: { id: string; status: string; start_at?: string };
}

interface ProjectVersion {
  id: string;
  project_id: string;
  version_number: number;
  branch_name?: string;
  version_status: string;
  created_at: string;
}

interface ProjectRun {
  id: string;
  project_id: string;
  status: string;
  start_at?: string;
  end_at?: string;
  failure_reason?: string;
  retry_count: number;
  created_at: string;
}

interface ProjectState {
  projects: ProjectItem[];
  currentProject: ProjectItem | null;
  versions: ProjectVersion[];
  runs: ProjectRun[];
  loading: boolean;
}

interface ProjectActions {
  fetchProjects: () => Promise<void>;
  fetchProject: (id: string) => Promise<void>;
  createProject: (data: { title: string; description?: string; schedule_type?: string }) => Promise<ProjectItem>;
  updateProject: (id: string, data: Record<string, unknown>) => Promise<void>;
  deleteProject: (id: string) => Promise<void>;
  fetchVersions: (projectId: string) => Promise<void>;
  fetchRuns: (projectId: string) => Promise<void>;
  approvePlan: (projectId: string) => Promise<void>;
  rejectPlan: (projectId: string, reason: string) => Promise<void>;
}

export const useDesktopProjectStore = create<ProjectState & ProjectActions>((set, get) => ({
  projects: [],
  currentProject: null,
  versions: [],
  runs: [],
  loading: false,

  fetchProjects: async () => {
    set({ loading: true });
    try {
      const projects = (await desktopApi.listProjects()) as ProjectItem[];
      set({ projects });
    } finally {
      set({ loading: false });
    }
  },

  fetchProject: async (id: string) => {
    const project = (await desktopApi.getProject(id)) as ProjectItem;
    set({ currentProject: project });
  },

  createProject: async (data) => {
    const project = (await desktopApi.createProject(data)) as ProjectItem;
    set((s) => ({ projects: [project, ...s.projects] }));
    return project;
  },

  updateProject: async (id, data) => {
    const updated = (await desktopApi.updateProject(id, data)) as ProjectItem;
    set((s) => ({
      projects: s.projects.map((p) => (p.id === id ? updated : p)),
      currentProject: s.currentProject?.id === id ? updated : s.currentProject,
    }));
  },

  deleteProject: async (id) => {
    await desktopApi.deleteProject(id);
    set((s) => ({
      projects: s.projects.filter((p) => p.id !== id),
      currentProject: s.currentProject?.id === id ? null : s.currentProject,
    }));
  },

  fetchVersions: async (projectId) => {
    const versions = (await desktopApi.listProjectVersions(projectId)) as ProjectVersion[];
    set({ versions });
  },

  fetchRuns: async (projectId) => {
    const runs = (await desktopApi.listProjectRuns(projectId)) as ProjectRun[];
    set({ runs });
  },

  approvePlan: async (projectId) => {
    await desktopApi.approvePlan(projectId);
    get().fetchProject(projectId);
  },

  rejectPlan: async (projectId, reason) => {
    await desktopApi.rejectPlan(projectId, reason);
    get().fetchProject(projectId);
  },
}));
```

- [ ] **Step 2: Verify build**

Run: `pnpm typecheck`

- [ ] **Step 3: Commit**

```bash
git add apps/desktop/src/lib/project-store.ts
git commit -m "feat(desktop): add Zustand project store for desktop app"
```

---

### Task 3: Desktop Project Route

**Files:**
- Create: `apps/desktop/src/routes/project-route.tsx`

- [ ] **Step 1: Write the route component**

```tsx
// apps/desktop/src/routes/project-route.tsx
import { useCallback, useEffect, useState } from "react";
import { RouteShell } from "@/components/route-shell";
import { useDesktopProjectStore } from "@/lib/project-store";

const STATUS_COLORS: Record<string, string> = {
  draft: "bg-muted text-muted-foreground",
  scheduled: "bg-blue-500/10 text-blue-400",
  running: "bg-green-500/10 text-green-400",
  paused: "bg-yellow-500/10 text-yellow-400",
  completed: "bg-green-500/20 text-green-300",
  failed: "bg-red-500/10 text-red-400",
  stopped: "bg-orange-500/10 text-orange-400",
  archived: "bg-muted text-muted-foreground",
};

export function ProjectRoute() {
  const {
    projects,
    currentProject,
    versions,
    runs,
    loading,
    fetchProjects,
    fetchProject,
    fetchVersions,
    fetchRuns,
    createProject,
  } = useDesktopProjectStore();

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<"overview" | "versions" | "runs">("overview");
  const [showCreate, setShowCreate] = useState(false);
  const [newTitle, setNewTitle] = useState("");

  useEffect(() => {
    void fetchProjects();
  }, [fetchProjects]);

  useEffect(() => {
    if (!selectedId) return;
    void fetchProject(selectedId);
    void fetchVersions(selectedId);
    void fetchRuns(selectedId);
  }, [selectedId, fetchProject, fetchVersions, fetchRuns]);

  // Auto-select first project
  useEffect(() => {
    if (!selectedId && projects.length > 0 && projects[0]) {
      setSelectedId(projects[0].id);
    }
  }, [projects, selectedId]);

  const handleCreate = useCallback(async () => {
    if (!newTitle.trim()) return;
    const project = await createProject({ title: newTitle.trim() });
    setNewTitle("");
    setShowCreate(false);
    setSelectedId(project.id);
  }, [newTitle, createProject]);

  return (
    <RouteShell eyebrow="Projects" title="Project Management" description="Manage projects, plans, and execution">
      <div className="grid min-h-[70vh] gap-4 xl:grid-cols-[260px_1fr]">
        {/* Sidebar */}
        <section className="flex flex-col rounded-[28px] border border-border/70 bg-card/85 p-4">
          <div className="flex items-center justify-between px-2">
            <p className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Projects</p>
            <button
              type="button"
              onClick={() => setShowCreate(!showCreate)}
              className="rounded-md p-1 text-muted-foreground transition hover:bg-accent hover:text-foreground"
            >
              <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
                <path d="M7 1V13M1 7H13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
            </button>
          </div>

          {showCreate && (
            <div className="mt-2 flex gap-1">
              <input
                value={newTitle}
                onChange={(e) => setNewTitle(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleCreate()}
                placeholder="Project name..."
                className="flex-1 rounded-lg border border-border bg-background px-2 py-1 text-sm outline-none"
                autoFocus
              />
              <button
                type="button"
                onClick={() => void handleCreate()}
                className="rounded-lg bg-primary px-2 py-1 text-xs text-primary-foreground"
              >
                Create
              </button>
            </div>
          )}

          <div className="mt-2 flex-1 space-y-0.5 overflow-y-auto">
            {projects.map((project) => (
              <button
                key={project.id}
                type="button"
                onClick={() => setSelectedId(project.id)}
                className={`flex w-full items-center justify-between rounded-xl px-3 py-2 text-left text-sm transition ${
                  selectedId === project.id
                    ? "bg-primary text-primary-foreground"
                    : "text-foreground hover:bg-white/5"
                }`}
              >
                <span className="truncate">{project.title}</span>
                <span className={`ml-2 rounded-full px-1.5 py-0.5 text-[10px] ${STATUS_COLORS[project.status] ?? ""}`}>
                  {project.status}
                </span>
              </button>
            ))}
            {!loading && projects.length === 0 && (
              <p className="px-3 py-4 text-center text-xs text-muted-foreground">No projects yet</p>
            )}
          </div>
        </section>

        {/* Detail */}
        <section className="flex flex-col rounded-[28px] border border-border/70 bg-card/85">
          {currentProject ? (
            <>
              {/* Header */}
              <div className="flex items-center justify-between border-b border-border/70 px-5 py-3">
                <div>
                  <h3 className="text-lg font-medium text-foreground">{currentProject.title}</h3>
                  <div className="mt-1 flex items-center gap-2">
                    <span className={`rounded-full px-2 py-0.5 text-xs ${STATUS_COLORS[currentProject.status] ?? ""}`}>
                      {currentProject.status}
                    </span>
                    <span className="text-xs text-muted-foreground">{currentProject.schedule_type}</span>
                  </div>
                </div>
                <div className="flex gap-1 rounded-lg bg-background/70 p-0.5">
                  {(["overview", "versions", "runs"] as const).map((tab) => (
                    <button
                      key={tab}
                      type="button"
                      onClick={() => setActiveTab(tab)}
                      className={`rounded-md px-3 py-1 text-xs font-medium transition ${
                        activeTab === tab
                          ? "bg-primary text-primary-foreground"
                          : "text-muted-foreground hover:text-foreground"
                      }`}
                    >
                      {tab.charAt(0).toUpperCase() + tab.slice(1)}
                    </button>
                  ))}
                </div>
              </div>

              {/* Content */}
              <div className="flex-1 overflow-y-auto p-4">
                {activeTab === "overview" && (
                  <div className="space-y-3">
                    {currentProject.description && (
                      <p className="text-sm text-muted-foreground">{currentProject.description}</p>
                    )}
                    {currentProject.plan && (
                      <div className="rounded-xl border border-border/70 p-3">
                        <p className="text-xs uppercase text-muted-foreground">Plan</p>
                        <p className="mt-1 text-sm font-medium">{currentProject.plan.title}</p>
                        <span className={`mt-1 inline-block rounded-full px-2 py-0.5 text-xs ${
                          currentProject.plan.approval_status === "approved"
                            ? "bg-green-500/10 text-green-400"
                            : currentProject.plan.approval_status === "rejected"
                              ? "bg-red-500/10 text-red-400"
                              : "bg-yellow-500/10 text-yellow-400"
                        }`}>
                          {currentProject.plan.approval_status}
                        </span>
                      </div>
                    )}
                    {currentProject.active_run && (
                      <div className="rounded-xl border border-border/70 p-3">
                        <p className="text-xs uppercase text-muted-foreground">Active Run</p>
                        <p className="mt-1 text-sm">Status: {currentProject.active_run.status}</p>
                      </div>
                    )}
                  </div>
                )}

                {activeTab === "versions" && (
                  <div className="space-y-2">
                    {versions.map((v) => (
                      <div key={v.id} className="rounded-xl border border-border/70 p-3">
                        <div className="flex items-center justify-between">
                          <span className="text-sm font-medium">v{v.version_number}</span>
                          <span className="text-xs text-muted-foreground">{v.version_status}</span>
                        </div>
                        {v.branch_name && (
                          <span className="mt-1 text-xs text-muted-foreground">Branch: {v.branch_name}</span>
                        )}
                      </div>
                    ))}
                    {versions.length === 0 && (
                      <p className="text-center text-sm text-muted-foreground">No versions yet</p>
                    )}
                  </div>
                )}

                {activeTab === "runs" && (
                  <div className="space-y-2">
                    {runs.map((run) => (
                      <div key={run.id} className="rounded-xl border border-border/70 p-3">
                        <div className="flex items-center justify-between">
                          <span className={`rounded-full px-2 py-0.5 text-xs ${STATUS_COLORS[run.status] ?? ""}`}>
                            {run.status}
                          </span>
                          <span className="text-xs text-muted-foreground">
                            {new Date(run.created_at).toLocaleDateString()}
                          </span>
                        </div>
                        {run.failure_reason && (
                          <p className="mt-1 text-xs text-red-400">{run.failure_reason}</p>
                        )}
                      </div>
                    ))}
                    {runs.length === 0 && (
                      <p className="text-center text-sm text-muted-foreground">No runs yet</p>
                    )}
                  </div>
                )}
              </div>
            </>
          ) : (
            <div className="flex h-full items-center justify-center p-8">
              <p className="text-sm text-muted-foreground">Select a project or create a new one</p>
            </div>
          )}
        </section>
      </div>
    </RouteShell>
  );
}
```

- [ ] **Step 2: Register in the app router**

Find the app's route configuration and add the project route. The exact file depends on the desktop app's routing setup -- look for where `SessionRoute` is registered and add `ProjectRoute` similarly.

- [ ] **Step 3: Verify build**

Run: `pnpm typecheck`

- [ ] **Step 4: Commit**

```bash
git add apps/desktop/src/routes/project-route.tsx
git commit -m "feat(desktop): add project route with list, detail, versions, and runs views"
```

---

### Task 4: Verify Full Build

- [ ] **Step 1: Run all checks**

Run: `cd server && go build ./... && go test ./... -count=1`
Run: `pnpm typecheck && pnpm test`

- [ ] **Step 2: Commit fixes if needed**

```bash
git add -A && git commit -m "fix: resolve Phase 6 build issues"
```
