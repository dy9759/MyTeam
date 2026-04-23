import { useEffect, useState } from "react";
import { RouteShell } from "@/components/route-shell";
import { useDesktopProjectStore } from "@/lib/project-store";

type TabId = "overview" | "versions" | "runs";

const STATUS_COLORS: Record<string, string> = {
  active: "bg-green-500/15 text-green-400",
  draft: "bg-yellow-500/15 text-yellow-400",
  paused: "bg-orange-500/15 text-orange-400",
  completed: "bg-blue-500/15 text-blue-400",
  archived: "bg-secondary text-muted-foreground",
  failed: "bg-destructive/15 text-destructive",
};

function statusColor(status: string) {
  return STATUS_COLORS[status] ?? "bg-primary/10 text-primary";
}

export function ProjectRoute() {
  const projects = useDesktopProjectStore((s) => s.projects);
  const currentProject = useDesktopProjectStore((s) => s.currentProject);
  const versions = useDesktopProjectStore((s) => s.versions);
  const runs = useDesktopProjectStore((s) => s.runs);
  const loading = useDesktopProjectStore((s) => s.loading);
  const fetchProjects = useDesktopProjectStore((s) => s.fetchProjects);
  const fetchProject = useDesktopProjectStore((s) => s.fetchProject);
  const fetchVersions = useDesktopProjectStore((s) => s.fetchVersions);
  const fetchRuns = useDesktopProjectStore((s) => s.fetchRuns);
  const createProject = useDesktopProjectStore((s) => s.createProject);
  const approvePlan = useDesktopProjectStore((s) => s.approvePlan);

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<TabId>("overview");
  const [showCreate, setShowCreate] = useState(false);
  const [newTitle, setNewTitle] = useState("");

  useEffect(() => {
    void fetchProjects();
  }, [fetchProjects]);

  // Auto-select first project
  useEffect(() => {
    if (!selectedId && projects.length > 0) {
      setSelectedId(projects[0].id);
    }
  }, [projects, selectedId]);

  // Fetch detail data when selection changes
  useEffect(() => {
    if (!selectedId) return;
    void fetchProject(selectedId);
    void fetchVersions(selectedId);
    void fetchRuns(selectedId);
  }, [selectedId, fetchProject, fetchVersions, fetchRuns]);

  const handleCreate = async () => {
    if (!newTitle.trim()) return;
    const project = await createProject({ title: newTitle.trim() });
    setNewTitle("");
    setShowCreate(false);
    setSelectedId(project.id);
  };

  return (
    <RouteShell
      eyebrow="Projects"
      title="Project Management"
      description="Manage projects, plans, and execution"
    >
      <div className="grid min-h-[70vh] gap-4 xl:grid-cols-[260px_1fr]">
        {/* ─── Sidebar ─── */}
        <section className="flex flex-col rounded-[28px] border border-border/70 bg-card/85 p-4">
          <div className="flex items-center justify-between px-2">
            <p className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Projects</p>
            <button
              type="button"
              onClick={() => setShowCreate((v) => !v)}
              className="rounded-md p-1 text-muted-foreground transition hover:bg-accent hover:text-foreground"
              title="New project"
            >
              <PlusIcon />
            </button>
          </div>

          {showCreate && (
            <div className="mt-3 flex gap-2 px-1">
              <input
                autoFocus
                type="text"
                value={newTitle}
                onChange={(e) => setNewTitle(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") void handleCreate();
                  if (e.key === "Escape") {
                    setShowCreate(false);
                    setNewTitle("");
                  }
                }}
                placeholder="Project title"
                className="min-w-0 flex-1 rounded-lg border border-border/70 bg-background/70 px-3 py-1.5 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-primary"
              />
              <button
                type="button"
                onClick={() => void handleCreate()}
                className="rounded-lg bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition hover:opacity-90"
              >
                Add
              </button>
            </div>
          )}

          <div className="mt-3 flex-1 space-y-0.5 overflow-y-auto">
            {loading && projects.length === 0 ? (
              <p className="px-3 py-4 text-center text-xs text-muted-foreground">Loading…</p>
            ) : projects.length === 0 ? (
              <EmptyPane message="No projects yet." />
            ) : (
              projects.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => setSelectedId(p.id)}
                  className={`flex w-full items-center justify-between rounded-xl px-3 py-2 text-left text-sm transition ${
                    selectedId === p.id
                      ? "bg-primary text-primary-foreground"
                      : "text-foreground hover:bg-white/5"
                  }`}
                >
                  <span className="truncate">{p.title}</span>
                  <span
                    className={`ml-2 shrink-0 rounded-full px-2 py-0.5 text-[10px] font-medium ${
                      selectedId === p.id ? "bg-primary-foreground/20 text-primary-foreground" : statusColor(p.status)
                    }`}
                  >
                    {p.status}
                  </span>
                </button>
              ))
            )}
          </div>
        </section>

        {/* ─── Detail ─── */}
        <section className="flex flex-col rounded-[28px] border border-border/70 bg-card/85">
          {!currentProject || currentProject.id !== selectedId ? (
            <div className="flex flex-1 items-center justify-center p-6">
              <EmptyPane message={selectedId ? "Loading…" : "Select a project."} />
            </div>
          ) : (
            <>
              {/* Header */}
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border/70 px-6 py-4">
                <div className="min-w-0">
                  <p className="text-xs uppercase tracking-[0.2em] text-muted-foreground">
                    {currentProject.schedule_type}
                  </p>
                  <h3 className="mt-1 truncate text-xl font-semibold text-foreground">
                    {currentProject.title}
                  </h3>
                </div>
                <div className="flex items-center gap-2">
                  <span className={`rounded-full px-3 py-1 text-xs font-medium ${statusColor(currentProject.status)}`}>
                    {currentProject.status}
                  </span>
                  {currentProject.plan?.approval_status === "pending" && (
                    <button
                      type="button"
                      onClick={() => void approvePlan(currentProject.id)}
                      className="rounded-full bg-green-500/15 px-3 py-1 text-xs font-medium text-green-400 transition hover:bg-green-500/25"
                    >
                      Approve plan
                    </button>
                  )}
                </div>
              </div>

              {/* Tabs */}
              <div className="flex gap-1 border-b border-border/70 px-6 pt-2">
                {(["overview", "versions", "runs"] as TabId[]).map((tab) => (
                  <TabButton key={tab} active={activeTab === tab} onClick={() => setActiveTab(tab)}>
                    {tab.charAt(0).toUpperCase() + tab.slice(1)}
                  </TabButton>
                ))}
              </div>

              {/* Tab content */}
              <div className="flex-1 overflow-y-auto p-6">
                {activeTab === "overview" && (
                  <OverviewTab project={currentProject} />
                )}
                {activeTab === "versions" && (
                  <VersionsTab versions={versions} />
                )}
                {activeTab === "runs" && (
                  <RunsTab runs={runs} />
                )}
              </div>
            </>
          )}
        </section>
      </div>
    </RouteShell>
  );
}

/* ─── Tab content components ─── */

function OverviewTab({
  project,
}: {
  project: {
    description?: string;
    plan?: { id: string; title: string; approval_status: string };
    active_run?: { id: string; status: string; start_at?: string };
  };
}) {
  return (
    <div className="space-y-5">
      <div>
        <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">Description</p>
        <p className="mt-2 text-sm leading-relaxed text-foreground">
          {project.description || "No description yet."}
        </p>
      </div>

      {project.plan && (
        <div className="rounded-2xl border border-border/70 bg-background/70 px-4 py-4">
          <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">Plan</p>
          <p className="mt-2 text-sm font-medium text-foreground">{project.plan.title}</p>
          <p className="mt-1 text-xs text-muted-foreground">
            Approval: {project.plan.approval_status}
          </p>
        </div>
      )}

      {project.active_run ? (
        <div className="rounded-2xl border border-border/70 bg-background/70 px-4 py-4">
          <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">Active run</p>
          <p className="mt-2 text-sm font-medium text-foreground">{project.active_run.status}</p>
          {project.active_run.start_at && (
            <p className="mt-1 text-xs text-muted-foreground">
              Started {new Date(project.active_run.start_at).toLocaleString()}
            </p>
          )}
        </div>
      ) : (
        <div className="rounded-2xl border border-border/70 bg-background/70 px-4 py-4">
          <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">Active run</p>
          <p className="mt-2 text-sm text-muted-foreground">No active run.</p>
        </div>
      )}
    </div>
  );
}

function VersionsTab({
  versions,
}: {
  versions: Array<{
    id: string;
    version_number: number;
    branch_name?: string;
    version_status: string;
    created_at: string;
  }>;
}) {
  if (versions.length === 0) {
    return <EmptyPane message="No versions yet." />;
  }
  return (
    <div className="space-y-3">
      {versions.map((v) => (
        <div
          key={v.id}
          className="flex items-center justify-between rounded-2xl border border-border/70 bg-background/70 px-4 py-3"
        >
          <div>
            <p className="text-sm font-medium text-foreground">
              v{v.version_number}
              {v.branch_name && (
                <span className="ml-2 text-xs text-muted-foreground">{v.branch_name}</span>
              )}
            </p>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {new Date(v.created_at).toLocaleString()}
            </p>
          </div>
          <span className="rounded-full bg-primary/10 px-3 py-1 text-xs text-primary">
            {v.version_status}
          </span>
        </div>
      ))}
    </div>
  );
}

function RunsTab({
  runs,
}: {
  runs: Array<{
    id: string;
    status: string;
    start_at?: string;
    end_at?: string;
    failure_reason?: string;
    retry_count: number;
    created_at: string;
  }>;
}) {
  if (runs.length === 0) {
    return <EmptyPane message="No runs yet." />;
  }
  return (
    <div className="space-y-3">
      {runs.map((r) => (
        <div
          key={r.id}
          className="rounded-2xl border border-border/70 bg-background/70 px-4 py-3"
        >
          <div className="flex items-center justify-between">
            <p className="text-sm font-medium text-foreground">
              {r.start_at
                ? new Date(r.start_at).toLocaleString()
                : new Date(r.created_at).toLocaleString()}
            </p>
            <span className={`rounded-full px-3 py-1 text-xs font-medium ${statusColor(r.status)}`}>
              {r.status}
            </span>
          </div>
          {r.failure_reason && (
            <p className="mt-1.5 text-xs text-destructive">{r.failure_reason}</p>
          )}
          {r.retry_count > 0 && (
            <p className="mt-1 text-xs text-muted-foreground">Retries: {r.retry_count}</p>
          )}
        </div>
      ))}
    </div>
  );
}

/* ─── Shared helpers ─── */

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-t-md px-4 py-2 text-xs font-medium transition ${
        active
          ? "border-b-2 border-primary text-foreground"
          : "text-muted-foreground hover:text-foreground"
      }`}
    >
      {children}
    </button>
  );
}

function PlusIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
      <path d="M7 1V13M1 7H13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

function EmptyPane({ message }: { message: string }) {
  return (
    <div className="rounded-3xl border border-dashed border-border/70 bg-background/50 px-4 py-10 text-center text-sm text-muted-foreground">
      {message}
    </div>
  );
}
