"use client"

import { useEffect, useState, useCallback } from "react"
import { useSearchParams, useRouter } from "next/navigation"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import RuntimesPage from "@/features/runtimes/components/runtimes-page"
import SkillsPage from "@/features/skills/components/skills-page"
import { useWorkspaceStore } from "@/features/workspace"
import { Badge } from "@/components/ui/badge"

const TAB_VALUES = ["overview", "agents", "runtimes", "skills"] as const
type TabValue = (typeof TAB_VALUES)[number]

function isValidTab(v: string | null): v is TabValue {
  return TAB_VALUES.includes(v as TabValue)
}

// ---------------------------------------------------------------------------
// Overview tab — original account page content
// ---------------------------------------------------------------------------

function OverviewTab() {
  const [user, setUser] = useState<any>(null)
  const [agents, setAgents] = useState<any[]>([])
  const [workspaces, setWorkspaces] = useState<any[]>([])

  useEffect(() => {
    fetch("/api/auth/me").then(r => r.json()).then(setUser).catch(() => {})
    fetch("/api/agents").then(r => r.json()).then(d => setAgents(d.agents ?? d ?? [])).catch(() => {})
    fetch("/api/workspaces").then(r => r.json()).then(d => setWorkspaces(d ?? [])).catch(() => {})
  }, [])

  const statusColors: Record<string, string> = {
    idle: "bg-green-500",
    working: "bg-yellow-500",
    offline: "bg-gray-400",
    error: "bg-red-500",
  }

  return (
    <div className="p-6 max-w-3xl mx-auto">
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

      {/* My Agents Summary */}
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
          </div>
        ))}
        {agents.length === 0 && <div className="col-span-2 text-muted-foreground">No agents yet</div>}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Agents Tab — inline agent list
// ---------------------------------------------------------------------------

function AgentsTab() {
  const agents = useWorkspaceStore((s) => s.agents)

  if (agents.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-muted-foreground">
        <p>暂无 Agent</p>
        <p className="text-sm">前往 Agent 管理页面创建</p>
      </div>
    )
  }

  return (
    <div className="space-y-3 p-4">
      {agents.map((agent) => (
        <div key={agent.id} className="rounded-lg border p-4 space-y-2">
          <div className="flex items-center gap-3">
            <div className="h-10 w-10 rounded-full bg-primary/10 flex items-center justify-center text-sm font-medium">
              {agent.name?.[0]?.toUpperCase() ?? "A"}
            </div>
            <div className="flex-1 min-w-0">
              <div className="font-medium truncate">{agent.name}</div>
              <div className="text-xs text-muted-foreground truncate">{agent.description || "No description"}</div>
            </div>
            <Badge variant={agent.status === "idle" ? "secondary" : agent.status === "working" ? "default" : "outline"}>
              {agent.status}
            </Badge>
          </div>
        </div>
      ))}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Account Page with Tabs
// ---------------------------------------------------------------------------

export default function AccountPage() {
  const searchParams = useSearchParams()
  const router = useRouter()
  const tabParam = searchParams.get("tab")
  const initialTab: TabValue = isValidTab(tabParam) ? tabParam : "overview"
  const [activeTab, setActiveTab] = useState<TabValue>(initialTab)

  const handleTabChange = useCallback(
    (value: string | number | null) => {
      if (typeof value !== "string") return
      const tab = value as TabValue
      setActiveTab(tab)
      const params = new URLSearchParams(searchParams.toString())
      if (tab === "overview") {
        params.delete("tab")
      } else {
        params.set("tab", tab)
      }
      const qs = params.toString()
      router.replace(qs ? `?${qs}` : "/account", { scroll: false })
    },
    [router, searchParams],
  )

  // Sync from URL on popstate / external navigation
  useEffect(() => {
    const fromUrl = searchParams.get("tab")
    const next = isValidTab(fromUrl) ? fromUrl : "overview"
    setActiveTab(next)
  }, [searchParams])

  return (
    <Tabs
      value={activeTab}
      onValueChange={handleTabChange}
      className="flex flex-col flex-1 min-h-0"
    >
      <div className="border-b px-4">
        <TabsList variant="line">
          <TabsTrigger value="overview">概览</TabsTrigger>
          <TabsTrigger value="agents">我的Agent</TabsTrigger>
          <TabsTrigger value="runtimes">运行时</TabsTrigger>
          <TabsTrigger value="skills">技能</TabsTrigger>
        </TabsList>
      </div>

      <TabsContent value="overview" className="flex-1 overflow-y-auto">
        <OverviewTab />
      </TabsContent>

      <TabsContent value="agents" className="flex-1 overflow-y-auto">
        <AgentsTab />
      </TabsContent>

      <TabsContent value="runtimes" className="flex flex-1 min-h-0">
        <RuntimesPage />
      </TabsContent>

      <TabsContent value="skills" className="flex flex-1 min-h-0">
        <SkillsPage />
      </TabsContent>
    </Tabs>
  )
}
