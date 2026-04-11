"use client"
import { useEffect, useState } from "react"
import { MetricsOverview } from "@/features/workspace/components/metrics-overview"

export default function AccountPage() {
  const [user, setUser] = useState<any>(null)
  const [agents, setAgents] = useState<any[]>([])
  const [workspaces, setWorkspaces] = useState<any[]>([])

  useEffect(() => {
    fetch("/api/auth/me").then(r => r.json()).then(setUser).catch(() => {})
    fetch("/api/agents").then(r => r.json()).then(d => setAgents(d.agents ?? d ?? [])).catch(() => {})
    fetch("/api/workspaces").then(r => r.json()).then(d => setWorkspaces(d ?? [])).catch(() => {})
  }, [])

  function handleImpersonate(agentId: string) {
    localStorage.setItem("multica_impersonate_agent", agentId)
    window.location.reload()
  }

  const statusColors: Record<string, string> = {
    idle: "bg-green-500",
    working: "bg-yellow-500",
    offline: "bg-gray-400",
    error: "bg-red-500",
  }

  return (
    <div className="p-6 max-w-3xl mx-auto">
      <h1 className="text-2xl font-bold mb-6">Account</h1>

      {/* Workspace Metrics */}
      <div className="mb-6">
        <h2 className="text-lg font-semibold mb-3">Overview</h2>
        <MetricsOverview />
      </div>

      {/* Profile Card */}
      <div className="border rounded-xl overflow-hidden mb-6">
        <div className="h-20 bg-gradient-to-r from-primary/30 to-primary/10" />
        <div className="px-6 pb-6 -mt-8">
          <div className="flex items-end gap-4 mb-4">
            <div className="w-16 h-16 bg-muted rounded-xl flex items-center justify-center text-3xl border-4 border-background">
              {"\u{1F464}"}
            </div>
            <div className="pb-1">
              <h2 className="text-xl font-bold">{user?.name ?? "Loading..."}</h2>
              <div className="text-sm text-muted-foreground">{user?.email}</div>
            </div>
          </div>
          <div className="text-sm text-muted-foreground">
            Role: Owner &middot; Status: Online
          </div>
        </div>
      </div>

      {/* Workspaces */}
      {workspaces.length > 0 && (
        <div className="mb-6">
          <h2 className="text-lg font-semibold mb-3">Workspaces ({workspaces.length})</h2>
          <div className="space-y-2">
            {workspaces.map((w: any) => (
              <div key={w.id} className="p-3 border rounded-lg flex items-center justify-between">
                <div>
                  <div className="font-medium">{w.name}</div>
                  {w.description && <div className="text-xs text-muted-foreground">{w.description}</div>}
                </div>
                <div className="text-xs text-muted-foreground">{w.slug}</div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* My Agents */}
      <h2 className="text-lg font-semibold mb-3">My Agents ({agents.length})</h2>
      <div className="grid grid-cols-2 gap-3">
        {agents.map(a => (
          <div key={a.id} className="p-4 border rounded-lg">
            <div className="flex items-center gap-2 mb-2">
              <span className={`w-2.5 h-2.5 rounded-full ${statusColors[a.status] ?? "bg-gray-400"}`} />
              <span className="font-medium">{a.display_name ?? a.name}</span>
              <span className="ml-auto text-xs px-2 py-0.5 rounded-full bg-muted text-muted-foreground capitalize">
                {a.status ?? "unknown"}
              </span>
            </div>
            <div className="text-xs text-muted-foreground mb-2">{a.description?.slice(0, 80) ?? "No description"}</div>
            {a.capabilities?.length > 0 && (
              <div className="flex flex-wrap gap-1 mb-2">
                {a.capabilities.slice(0, 4).map((c: string) => (
                  <span key={c} className="text-xs bg-muted px-1.5 py-0.5 rounded">{c}</span>
                ))}
                {a.capabilities.length > 4 && (
                  <span className="text-xs text-muted-foreground">+{a.capabilities.length - 4} more</span>
                )}
              </div>
            )}
            {a.workspace_id && (
              <div className="text-xs text-muted-foreground mb-2">
                Workspace: {workspaces.find((w: any) => w.id === a.workspace_id)?.name ?? a.workspace_id.slice(0, 12)}
              </div>
            )}
            <button
              onClick={() => handleImpersonate(a.id)}
              className="w-full mt-1 px-3 py-1.5 text-xs border rounded-md hover:bg-muted/50 text-primary font-medium"
            >
              Impersonate
            </button>
          </div>
        ))}
        {agents.length === 0 && <div className="col-span-2 text-muted-foreground">No agents yet</div>}
      </div>
    </div>
  )
}
