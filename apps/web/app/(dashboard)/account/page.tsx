"use client"
import { Suspense, useState } from "react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useWorkspaceStore } from "@/features/workspace"
import { useAuthStore } from "@/features/auth"
import { api } from "@/shared/api"
import { toast } from "sonner"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import {
  Bot, Terminal, Code, Key, ChevronDown, ChevronRight,
  Copy, Check, Plus, Zap, Circle, Shield, Wrench,
  Globe, User, GitBranch, Settings, Sparkles, Layers
} from "lucide-react"
import { MetricsOverview } from "@/features/workspace/components/metrics-overview"
import { AgentAutoReplyConfig } from "@/features/workspace/components/agent-auto-reply-config"
import { AgentProfileEditor } from "@/features/workspace/components/agent-profile-editor"
import { SkillsPage } from "@/features/skills"
import { SubagentsPage } from "@/features/subagents"

/* ================================================================== */
/* Helpers                                                             */
/* ================================================================== */

function CopyBtn({ text }: { text: string }) {
  const [ok, setOk] = useState(false)
  return (
    <button onClick={() => { navigator.clipboard.writeText(text); setOk(true); setTimeout(() => setOk(false), 2000) }}
      className="p-1 rounded-[4px] hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors" title="复制">
      {ok ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3" />}
    </button>
  )
}

function CodeBlock({ code }: { code: string }) {
  return (
    <div className="relative group">
      <pre className="bg-secondary border border-border rounded-[6px] px-3.5 py-2.5 text-[13px] font-mono leading-relaxed overflow-x-auto text-secondary-foreground">{code}</pre>
      <div className="absolute top-1.5 right-1.5 opacity-0 group-hover:opacity-100 transition-opacity"><CopyBtn text={code} /></div>
    </div>
  )
}

function Collapse({ title, icon: Icon, open: defaultOpen = false, children }: {
  title: string; icon: React.ElementType; open?: boolean; children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className="border border-border rounded-[8px] bg-card overflow-hidden">
      <button type="button" onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2.5 px-4 py-3 hover:bg-secondary/50 transition-colors text-left">
        <Icon className="h-4 w-4 text-primary shrink-0" />
        <span className="text-[14px] font-medium text-foreground flex-1">{title}</span>
        {open ? <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" /> : <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />}
      </button>
      {open && <div className="px-4 pb-4 pt-1 border-t border-border">{children}</div>}
    </div>
  )
}

const STATUS: Record<string, { label: string; dot: string }> = {
  idle: { label: "空闲", dot: "bg-green-500" },
  working: { label: "工作中", dot: "bg-primary" },
  blocked: { label: "阻塞", dot: "bg-orange-400" },
  degraded: { label: "降级", dot: "bg-yellow-500" },
  suspended: { label: "已暂停", dot: "bg-muted-foreground/40" },
  offline: { label: "离线", dot: "bg-muted-foreground/30" },
  error: { label: "错误", dot: "bg-destructive" },
}

function getStatus(s: string) {
  return STATUS[s] ?? { label: "离线", dot: "bg-muted-foreground/30" }
}

/* ================================================================== */
/* Tab 1: Owner 概览                                                   */
/* ================================================================== */

function OverviewTab() {
  const user = useAuthStore(s => s.user)
  const workspace = useWorkspaceStore(s => s.workspace)
  const agents = useWorkspaceStore(s => s.agents)
  const list = Array.isArray(agents) ? agents : []

  return (
    <div className="space-y-8">
      {/* Workspace Metrics */}
      <section>
        <h2 className="text-sm font-semibold mb-4">概览</h2>
        <MetricsOverview />
      </section>

      {/* Profile */}
      <section>
        <h2 className="text-sm font-semibold mb-4">个人信息</h2>
        <div className="border border-border rounded-[12px] bg-card overflow-hidden">
          <div className="h-12 bg-gradient-to-r from-primary/20 via-primary/10 to-transparent" />
          <div className="px-5 pb-5 -mt-4 space-y-3">
            <div className="flex items-end gap-3.5">
              <div className="w-11 h-11 rounded-[10px] bg-popover border-[3px] border-background flex items-center justify-center text-lg shadow-sm">👤</div>
              <div className="flex-1 min-w-0 pb-0.5">
                <div className="text-[15px] font-semibold text-foreground truncate">{user?.name ?? "加载中..."}</div>
                <div className="text-[13px] text-muted-foreground">{user?.email}</div>
              </div>
              <div className="flex items-center gap-1.5 pb-1">
                <Circle className="h-2 w-2 fill-green-500 text-green-500" />
                <span className="text-[12px] text-muted-foreground">在线</span>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Stats */}
      <section>
        <h2 className="text-sm font-semibold mb-4">身份概览</h2>
        <div className="grid grid-cols-3 gap-3">
          <div className="bg-card border border-border rounded-[8px] px-3.5 py-3">
            <div className="text-[11px] text-muted-foreground mb-1">角色</div>
            <div className="text-[14px] font-medium text-foreground flex items-center gap-1.5">
              <Shield className="h-3.5 w-3.5 text-primary" /> Owner
            </div>
          </div>
          <div className="bg-card border border-border rounded-[8px] px-3.5 py-3">
            <div className="text-[11px] text-muted-foreground mb-1">工作区</div>
            <div className="text-[14px] font-medium text-foreground truncate">{workspace?.name ?? "—"}</div>
          </div>
          <div className="bg-card border border-border rounded-[8px] px-3.5 py-3">
            <div className="text-[11px] text-muted-foreground mb-1">Agent 数量</div>
            <div className="text-[14px] font-medium text-foreground">{list.length} 个</div>
          </div>
        </div>
      </section>

      {/* Hierarchy */}
      <section>
        <h2 className="text-sm font-semibold mb-4">组织层级</h2>
        <div className="bg-card border border-border rounded-[8px] px-4 py-3 space-y-2">
          <div className="flex items-center gap-2 text-[13px]">
            <Globe className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="text-muted-foreground">Organization:</span>
            <span className="text-foreground font-medium">{workspace?.name ?? "—"}</span>
          </div>
          <div className="flex items-center gap-2 text-[13px] pl-5">
            <User className="h-3.5 w-3.5 text-primary" />
            <span className="text-muted-foreground">Owner:</span>
            <span className="text-foreground font-medium">{user?.name ?? "—"}</span>
            <span className="text-[11px] px-1.5 py-0.5 rounded-full bg-primary/10 text-primary">你</span>
          </div>
          {list.map(a => (
            <div key={a.id} className="flex items-center gap-2 text-[13px] pl-10">
              <Bot className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-foreground">{a.name}</span>
              <span className={`h-[6px] w-[6px] rounded-full ${getStatus(a.status as string).dot}`} />
              <span className="text-[11px] text-muted-foreground">{getStatus(a.status as string).label}</span>
            </div>
          ))}
          {list.length === 0 && <div className="text-[12px] text-muted-foreground/60 pl-10">暂无 Agent</div>}
        </div>
      </section>
    </div>
  )
}

/* ================================================================== */
/* Tab 2: Agent 列表                                                   */
/* ================================================================== */

function AgentListTab() {
  const agents = useWorkspaceStore(s => s.agents)
  const list = Array.isArray(agents) ? agents : []
  const [expandedAgentId, setExpandedAgentId] = useState<string | null>(null)

  const impersonate = (id: string) => {
    localStorage.setItem("multica_impersonate_agent", id)
    window.location.reload()
  }

  return (
    <div className="space-y-6">
      <h2 className="text-sm font-semibold">Agent 列表</h2>
      {list.length > 0 ? (
        <div className="space-y-3">
          {list.map(a => {
            const s = getStatus(a.status as string)
            return (
              <div key={a.id} className="border border-border rounded-[8px] bg-card overflow-hidden hover:bg-secondary/30 transition-colors">
                <div className="p-4 space-y-3">
                  <div className="flex items-center gap-3">
                    <div className="h-9 w-9 rounded-[8px] bg-primary/10 flex items-center justify-center shrink-0">
                      <Bot className="h-4 w-4 text-primary" />
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="text-[14px] font-medium text-foreground truncate">{a.name}</div>
                      <div className="text-[12px] text-muted-foreground truncate">{a.description || "暂无描述"}</div>
                    </div>
                    <div className="flex items-center gap-1.5 shrink-0">
                      <span className={`h-[7px] w-[7px] rounded-full ${s.dot}`} />
                      <span className="text-[11px] text-muted-foreground">{s.label}</span>
                    </div>
                  </div>
                  {(a.skills?.length > 0 || (a.identity_card?.skills?.length ?? 0) > 0) && (
                    <div className="flex flex-wrap gap-1">
                      {(a.identity_card?.skills ?? a.skills?.map(s => s.name) ?? []).slice(0, 5).map((t: string) => (
                        <span key={t} className="text-[11px] px-2 py-[2px] rounded-full border border-border text-secondary-foreground bg-secondary/50">{t}</span>
                      ))}
                    </div>
                  )}
                  {(a.identity_card?.tools?.length ?? 0) > 0 && (
                    <div className="text-[12px] text-muted-foreground flex items-center gap-1.5">
                      <Wrench className="h-3 w-3" /><span>{a.identity_card?.tools?.join(" · ")}</span>
                    </div>
                  )}
                  <div className="flex gap-2">
                    <button onClick={() => setExpandedAgentId(expandedAgentId === a.id ? null : a.id)}
                      className="flex-1 text-[12px] font-medium text-muted-foreground border border-border rounded-[6px] px-3 py-1.5 hover:bg-secondary/50 transition-colors flex items-center justify-center gap-1.5">
                      <Settings className="h-3 w-3" />
                      {expandedAgentId === a.id ? "隐藏配置" : "配置"}
                    </button>
                    <button onClick={() => impersonate(a.id)}
                      className="flex-1 text-[12px] font-medium text-primary border border-border rounded-[6px] px-3 py-1.5 hover:bg-secondary/50 transition-colors">
                      附身代理
                    </button>
                  </div>
                </div>
                {expandedAgentId === a.id && (
                  <div className="border-t px-4 py-4 space-y-4 bg-muted/20">
                    <AgentProfileEditor agentId={a.id} />
                    <AgentAutoReplyConfig agentId={a.id} />
                  </div>
                )}
              </div>
            )
          })}
        </div>
      ) : (
        <div className="border border-dashed border-border rounded-[8px] bg-card/50 py-10 flex flex-col items-center">
          <Bot className="h-8 w-8 text-muted-foreground/30 mb-2" />
          <p className="text-[13px] text-muted-foreground">暂无 Agent</p>
          <p className="text-[12px] text-muted-foreground/70 mt-0.5">前往「添加 Agent」创建</p>
        </div>
      )}
    </div>
  )
}

/* ================================================================== */
/* Tab 3: 添加 Agent                                                   */
/* ================================================================== */

function CreateForm({ onDone }: { onDone: () => void }) {
  const [name, setName] = useState("")
  const [desc, setDesc] = useState("")
  const [busy, setBusy] = useState(false)
  const handle = async () => {
    if (!name.trim()) return; setBusy(true)
    try {
      await api.createAgent({ name: name.trim(), description: desc.trim() || undefined, runtime_id: "", visibility: "private" })
      toast.success(`Agent "${name}" 创建成功`); setName(""); setDesc(""); onDone()
    } catch (e) { toast.error(e instanceof Error ? e.message : "创建失败") }
    finally { setBusy(false) }
  }
  return (
    <div className="space-y-3 pt-3">
      <input value={name} onChange={e => setName(e.target.value)} placeholder="Agent 名称"
        className="w-full px-3 py-2 bg-secondary border border-border rounded-[6px] text-[13px] text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring" />
      <input value={desc} onChange={e => setDesc(e.target.value)} placeholder="描述（可选）"
        className="w-full px-3 py-2 bg-secondary border border-border rounded-[6px] text-[13px] text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring" />
      <button onClick={handle} disabled={busy || !name.trim()}
        className="px-4 py-2 bg-primary text-primary-foreground rounded-[6px] text-[13px] font-medium disabled:opacity-40 hover:opacity-90 transition-opacity">
        {busy ? "创建中..." : "创建 Agent"}
      </button>
    </div>
  )
}

function AddAgentTab() {
  const workspace = useWorkspaceStore(s => s.workspace)
  const agents = useWorkspaceStore(s => s.agents)
  const list = Array.isArray(agents) ? agents : []

  const refresh = async () => {
    if (!workspace) return
    try {
      const d = await api.listAgents({ workspace_id: workspace.id })
      useWorkspaceStore.setState({ agents: Array.isArray(d) ? d : [] })
    } catch {}
  }

  return (
    <div className="space-y-4">
      <h2 className="text-sm font-semibold">添加 Agent</h2>
      <p className="text-xs text-muted-foreground">四种方式接入 Agent，选择适合你的方式</p>

      <div className="space-y-2.5">
        <Collapse title="网页创建 — 快速创建 Personal Agent" icon={Plus} open={list.length === 0}>
          <p className="text-[13px] text-muted-foreground mt-2 mb-1">填写名称即可创建：</p>
          <CreateForm onDone={refresh} />
        </Collapse>

        <Collapse title="CLI 注册 — 通过 Daemon 注册本地运行时" icon={Terminal}>
          <div className="space-y-3 pt-3">
            <p className="text-[13px] text-muted-foreground">在终端运行以下命令：</p>
            <div className="space-y-2.5">
              <div><p className="text-[12px] text-muted-foreground mb-1">1. 安装</p><CodeBlock code="brew install multica-ai/tap/multica" /></div>
              <div><p className="text-[12px] text-muted-foreground mb-1">2. 登录</p><CodeBlock code="multica login" /></div>
              <div><p className="text-[12px] text-muted-foreground mb-1">3. 启动 Daemon</p><CodeBlock code="multica daemon start" /></div>
            </div>
            <p className="text-[12px] text-muted-foreground/80 bg-secondary rounded-[6px] px-3 py-2">
              💡 Daemon 自动检测本地 <code className="font-mono text-[11px] bg-muted px-1 rounded">claude</code>、<code className="font-mono text-[11px] bg-muted px-1 rounded">codex</code> 等 CLI 并注册为运行时。
            </p>
          </div>
        </Collapse>

        <Collapse title="Claude Code 连接 — 通过 MCP 接入 My Team" icon={Code}>
          <div className="space-y-3 pt-3">
            <p className="text-[13px] text-muted-foreground">在 Claude Code 中添加 MCP Server：</p>
            <CodeBlock code={`// .claude/settings.json\n{\n  "mcpServers": {\n    "myteam": {\n      "command": "multica",\n      "args": ["mcp", "serve"],\n      "env": { "MULTICA_TOKEN": "<your-token>" }\n    }\n  }\n}`} />
            <p className="text-[12px] text-muted-foreground/80 bg-secondary rounded-[6px] px-3 py-2">
              🔑 Token 在「<a href="/settings" className="text-primary hover:underline">设置 → API 令牌</a>」中创建。
            </p>
          </div>
        </Collapse>

        <Collapse title="REST API — 编程式注册 Agent" icon={Key}>
          <div className="space-y-3 pt-3">
            <CodeBlock code={`curl -X POST /api/agents \\\n  -H "Authorization: Bearer <token>" \\\n  -H "X-Workspace-ID: ${workspace?.id ?? '<workspace-id>'}" \\\n  -H "Content-Type: application/json" \\\n  -d '{"name":"my-agent","runtime_id":"","visibility":"private"}'`} />
          </div>
        </Collapse>
      </div>

      <div className="flex items-center gap-2 pt-1 text-[13px] text-muted-foreground">
        <Zap className="h-3.5 w-3.5 text-primary" />
        <span>管理 Token：<a href="/settings" className="text-primary hover:underline">设置 → API 令牌</a></span>
      </div>
    </div>
  )
}

/* ================================================================== */
/* Tab definitions                                                     */
/* ================================================================== */

const ownerTabs = [
  { value: "overview", label: "概览", icon: User },
  { value: "agents", label: "Agent 列表", icon: Bot },
  { value: "add-agent", label: "添加 Agent", icon: Plus },
]

// Knowledge group bundles skills + subagents so the two live next to
// each other under the identity page — agents reach skills only via
// subagents (migration 069 rule), and keeping them co-located makes
// that relationship obvious in the nav.
const knowledgeTabs = [
  { value: "skills", label: "技能", icon: Sparkles },
  { value: "subagents", label: "Subagents", icon: Layers },
]

const orgTabs = [
  { value: "hierarchy", label: "组织层级", icon: GitBranch },
]

// Tabs that host their own full-bleed layout (resizable panels, left
// rails, etc.). They skip the max-w-3xl reader-width wrapper so the
// internal layout isn't squeezed.
const FULL_WIDTH_TABS = new Set(["skills", "subagents"])

/* ================================================================== */
/* Page — Settings-style vertical tab layout                           */
/* ================================================================== */

export default function AccountPage() {
  return (
    <Suspense fallback={null}>
      <AccountPageBody />
    </Suspense>
  )
}

function AccountPageBody() {
  const search = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const currentTab = search.get("tab") || "overview"

  const setTab = (v: string) => {
    const params = new URLSearchParams(search.toString())
    params.set("tab", v)
    router.replace(`${pathname}?${params.toString()}`, { scroll: false })
  }

  const isFullWidth = FULL_WIDTH_TABS.has(currentTab)

  return (
    <Tabs
      value={currentTab}
      onValueChange={setTab}
      orientation="vertical"
      className="flex-1 min-h-0 gap-0 bg-background"
    >
      {/* Left nav — same style as Settings */}
      <div className="w-52 shrink-0 border-r border-border overflow-y-auto p-4">
        <h1 className="text-sm font-semibold mb-4 px-2 text-foreground">身份</h1>
        <TabsList variant="line" className="flex-col items-stretch">
          <span className="px-2 pb-1 pt-2 text-xs font-medium text-muted-foreground">Owner</span>
          {ownerTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}

          <span className="px-2 pb-1 pt-4 text-xs font-medium text-muted-foreground">知识库</span>
          {knowledgeTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}

          <span className="px-2 pb-1 pt-4 text-xs font-medium text-muted-foreground">组织</span>
          {orgTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>

      {/* Right content — full-width for skills/subagents which carry
          their own resizable or split layout; reader-width for the
          narrower owner/org tabs. */}
      <div className="flex-1 min-w-0 overflow-y-auto flex flex-col">
        {isFullWidth ? (
          <div className="flex-1 min-h-0 flex">
            <TabsContent value="skills" className="flex-1 min-h-0 flex"><SkillsPage /></TabsContent>
            <TabsContent value="subagents" className="flex-1 min-h-0 flex"><SubagentsPage /></TabsContent>
          </div>
        ) : (
          <div className="w-full max-w-3xl mx-auto p-6">
            <TabsContent value="overview"><OverviewTab /></TabsContent>
            <TabsContent value="agents"><AgentListTab /></TabsContent>
            <TabsContent value="add-agent"><AddAgentTab /></TabsContent>
            <TabsContent value="hierarchy"><OverviewTab /></TabsContent>
          </div>
        )}
      </div>
    </Tabs>
  )
}
