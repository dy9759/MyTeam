import { useEffect, useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import {
  ChevronLeft,
  FolderGit2,
  MessageSquare,
  Settings,
  Shield,
  FileText,
  MonitorCog,
} from "lucide-react";
import mylogo from "@web/public/desktop/mylogo.png";
import { useDesktopWorkspaceStore } from "@/lib/desktop-client";
import { WindowControls } from "./window-controls";

const navItems = [
  { to: "/session", label: "Session", icon: MessageSquare },
  { to: "/projects", label: "Projects", icon: FolderGit2 },
  { to: "/files", label: "Files", icon: FileText },
  { to: "/account", label: "Account", icon: Shield },
  { to: "/settings", label: "Settings", icon: Settings },
];

export function DesktopShell() {
  const [collapsed, setCollapsed] = useState(false);
  const workspace = useDesktopWorkspaceStore((state) => state.workspace);
  const runtimePresence = useDesktopWorkspaceStore((state) => state.runtimePresence);

  useEffect(() => {
    if (!workspace?.id) return;
    void window.myteam.runtime.watchWorkspace(workspace.id);
  }, [workspace?.id]);

  return (
    <div className="flex h-screen bg-background text-foreground">
      <aside
        className={`relative flex shrink-0 flex-col border-r border-border/70 bg-card/80 transition-[width] duration-200 ${
          collapsed ? "w-20" : "w-72"
        }`}
      >
        <div className="flex items-start gap-3 border-b border-border/70 px-4 pb-4 pt-3">
          <div className="flex min-w-0 items-center gap-3 pt-6">
            <img src={mylogo} alt="MyTeam" className="h-10 w-10 rounded-xl object-contain" />
            {!collapsed && (
              <div className="min-w-0">
                <p className="truncate text-sm font-semibold tracking-wide text-foreground">
                  {workspace?.name ?? "MyTeam Desktop"}
                </p>
                <p className="text-xs text-muted-foreground">
                  Desktop workspace control plane
                </p>
              </div>
            )}
          </div>
          <button
            type="button"
            className="ml-auto mt-6 flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition hover:bg-white/5 hover:text-foreground"
            onClick={() => setCollapsed((current) => !current)}
            aria-label="Collapse sidebar"
          >
            <ChevronLeft
              className={`h-4 w-4 transition ${collapsed ? "rotate-180" : ""}`}
            />
          </button>
        </div>

        <nav className="flex-1 px-3 py-4">
          <div className="mb-5 rounded-2xl border border-border/70 bg-background/70 px-3 py-3">
            {!collapsed ? (
              <>
                <div className="flex items-center gap-2 text-xs uppercase tracking-[0.22em] text-muted-foreground">
                  <MonitorCog className="h-3.5 w-3.5" />
                  Runtime Mesh
                </div>
                <div className="mt-3 flex items-end gap-3">
                  <span className="text-3xl font-semibold text-foreground">
                    {runtimePresence.length}
                  </span>
                  <span className="pb-1 text-sm text-muted-foreground">
                    runtime{runtimePresence.length === 1 ? "" : "s"} visible
                  </span>
                </div>
              </>
            ) : (
              <div className="flex justify-center">
                <MonitorCog className="h-5 w-5 text-muted-foreground" />
              </div>
            )}
          </div>

          <div className="space-y-1">
            {navItems.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                className={({ isActive }) =>
                  `group flex items-center gap-3 rounded-2xl px-3 py-3 text-sm transition ${
                    isActive
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:bg-white/5 hover:text-foreground"
                  }`
                }
              >
                <item.icon className="h-4 w-4 shrink-0" />
                {!collapsed && <span>{item.label}</span>}
              </NavLink>
            ))}
          </div>
        </nav>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header
          className="desktop-drag-region flex h-16 items-center justify-between border-b border-border/70 bg-background/85 px-6 backdrop-blur-xl"
        >
          <div className="min-w-0">
            <p className="text-xs uppercase tracking-[0.22em] text-muted-foreground">
              MyTeam desktop
            </p>
            <h1 className="truncate text-lg font-semibold text-foreground">
              {workspace?.name ?? "Select a workspace"}
            </h1>
          </div>
          <WindowControls />
        </header>
        <main className="min-h-0 flex-1 overflow-auto bg-[radial-gradient(circle_at_top_left,_rgba(94,106,210,0.18),_transparent_28%),radial-gradient(circle_at_top_right,_rgba(255,255,255,0.06),_transparent_18%)] p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
