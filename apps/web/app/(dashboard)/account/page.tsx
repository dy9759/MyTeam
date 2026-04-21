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
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold">Agent 列表</h2>
        <span className="text-[11px] text-muted-foreground font-mono">
          {list.length}
        </span>
      </div>
      {list.length > 0 ? (
        // Compact row layout — skills/tools chips and the wide dual
        // buttons were reshuffled behind the expand arrow so the list
        // stays scannable when a workspace has dozens of agents.
        <div className="border border-border rounded-[8px] bg-card divide-y divide-border">
          {list.map(a => {
            const s = getStatus(a.status as string)
            const expanded = expandedAgentId === a.id
            return (
              <div key={a.id}>
                <div className="flex items-center gap-3 px-3 py-2 hover:bg-secondary/30 transition-colors">
                  <div className="h-7 w-7 rounded bg-primary/10 flex items-center justify-center shrink-0">
                    <Bot className="h-3.5 w-3.5 text-primary" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-[13px] font-medium text-foreground truncate">
                        {a.name}
                      </span>
                      <span className={`h-[6px] w-[6px] rounded-full shrink-0 ${s.dot}`} />
                      <span className="text-[10px] text-muted-foreground shrink-0">
                        {s.label}
                      </span>
                    </div>
                    {a.description && (
                      <div className="text-[11px] text-muted-foreground truncate">
                        {a.description}
                      </div>
                    )}
                  </div>
                  <button
                    onClick={() => impersonate(a.id)}
                    className="text-[11px] text-primary hover:underline shrink-0"
                  >
                    附身
                  </button>
                  <button
                    onClick={() =>
                      setExpandedAgentId(expanded ? null : a.id)
                    }
                    className="text-muted-foreground hover:text-foreground shrink-0 p-1"
                    aria-label={expanded ? "收起" : "展开"}
                  >
                    {expanded ? (
                      <ChevronDown className="h-3.5 w-3.5" />
                    ) : (
                      <ChevronRight className="h-3.5 w-3.5" />
                    )}
                  </button>
                </div>
                {expanded && (
                  <div className="px-4 py-4 space-y-4 bg-muted/20">
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
  const serverUrl = typeof window !== "undefined"
    ? `${window.location.protocol}//${window.location.host}`
    : ""

  const refresh = async () => {
    if (!workspace) return
    try {
      const d = await api.listAgents({ workspace_id: workspace.id })
      useWorkspaceStore.setState({ agents: Array.isArray(d) ? d : [] })
    } catch {}
  }

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-sm font-semibold">添加 Agent</h2>
        <p className="text-xs text-muted-foreground mt-1">
          Agent 是工作区里会被分配任务的执行者。每个 Agent 绑定一个 Runtime（本地 CLI 或云端 Provider），
          并通过 Subagent 包装的 Skill 扩展能力。下面几种方式按接入难度从易到难排列。
        </p>
      </div>

      {/* Framework cheat-sheet — what an Agent actually is in MyTeam */}
      <div className="rounded-[6px] border border-border bg-secondary/40 px-3 py-2.5 space-y-1.5">
        <div className="flex items-center gap-2 text-[12px] font-medium">
          <Sparkles className="h-3.5 w-3.5 text-primary" />
          Agent · Subagent · Skill 框架
        </div>
        <div className="text-[11px] text-muted-foreground leading-relaxed">
          <span className="text-foreground">Agent</span> 是被调度的执行者；
          <span className="text-foreground"> Subagent</span> 是封装一组 Skill 的模板（Agent 调用 Skill 必须经过它）；
          <span className="text-foreground"> Skill</span> 是可复用的能力包（bundle / upload / manual）。
          完整规则见{" "}
          <a href="/account?tab=subagents" className="text-primary hover:underline">Subagents</a>
          （Skill 在对应 Subagent 内展示）。
        </div>
      </div>

      <div className="space-y-2.5">
        <Collapse title="网页创建 — 最快方式，立即得到一个云端 Personal Agent" icon={Plus} open={list.length === 0}>
          <div className="space-y-2 pt-2">
            <p className="text-[12px] text-muted-foreground">
              系统自动绑定当前工作区里已注册的云端 Runtime。创建后可在「Agent 列表」里补身份卡、附身，
              也可以作为项目编排的候选执行者。
            </p>
            <CreateForm onDone={refresh} />
          </div>
        </Collapse>

        <Collapse title="一键接入 — 复制粘贴即可完成 CLI 安装 + 登录 + Daemon 启动" icon={Terminal} open>
          <div className="space-y-3 pt-3">
            <p className="text-[13px] text-muted-foreground">
              整个链路：安装 <code className="font-mono text-[11px] bg-muted px-1 rounded">multica</code> CLI → 注入 Token 登录 →
              启动 Daemon，Daemon 会自动探测本机 <code className="font-mono text-[11px] bg-muted px-1 rounded">claude</code>
              、<code className="font-mono text-[11px] bg-muted px-1 rounded">codex</code> 并注册成可被 Agent 绑定的 Runtime。
              粘贴下面这一段到终端，替换 <code className="font-mono text-[11px] bg-muted px-1 rounded">&lt;PASTE_TOKEN&gt;</code> 即可。
            </p>
            <div>
              <p className="text-[12px] text-muted-foreground mb-1">macOS / Linux（Homebrew，推荐）</p>
              <CodeBlock code={`# 1) 生成 Token：${serverUrl || "<server-url>"}/settings → API 令牌 → 复制
export MULTICA_TOKEN="<PASTE_TOKEN>"
export MULTICA_SERVER_URL="${serverUrl || "<server-url>"}"

# 2) 一行完成：安装 + 登录 + 启动 Daemon + 查看 Runtime
brew install multica-ai/tap/multica \\
  && echo "$MULTICA_TOKEN" | multica login --token \\
  && multica daemon start \\
  && multica runtime list`} />
            </div>
            <div>
              <p className="text-[12px] text-muted-foreground mb-1">从源码（本地开发或未发布 Homebrew 时）</p>
              <CodeBlock code={`# 仓库根目录执行
export MULTICA_TOKEN="<PASTE_TOKEN>"
export MULTICA_SERVER_URL="${serverUrl || "<server-url>"}"

make build \\
  && ln -sf "$(pwd)/server/bin/multica" ~/.local/bin/multica \\
  && echo "$MULTICA_TOKEN" | multica login --token \\
  && multica daemon start \\
  && multica runtime list`} />
            </div>
            <p className="text-[12px] text-muted-foreground/80 bg-secondary rounded-[6px] px-3 py-2">
              ✅ 命令结束后你会看到本机出现在线 Runtime；回到
              <a href="/account?tab=agents" className="text-primary hover:underline"> Agent 列表 </a>
              即可创建或绑定 Agent。Daemon 需要保持运行（可用 <code className="font-mono text-[11px] bg-muted px-1 rounded">multica daemon status</code> 检查、
              <code className="font-mono text-[11px] bg-muted px-1 rounded">multica daemon logs -f</code> 查看日志）。
            </p>
          </div>
        </Collapse>

        <Collapse title="第三方客户端接入 — Claude Code / Cursor 通过 Daemon Runtime 共享任务调度" icon={Code}>
          <div className="space-y-3 pt-3">
            <p className="text-[13px] text-muted-foreground">
              目前 MyTeam 的客户端接入走的是 <span className="text-foreground font-medium">Daemon → Runtime</span> 路径，
              而不是 MCP stdio 直连：Daemon 启动时会把本机的 Claude Code 或 Codex 识别成 Runtime，Agent 绑定该 Runtime 后，
              平台派发的任务就会由对应的客户端执行。所以最小接入只要完成上一步的「一键接入」即可。
            </p>
            <div>
              <p className="text-[12px] text-muted-foreground mb-1">验证：客户端是否已被识别</p>
              <CodeBlock code={`multica runtime list
# 期望看到 claude / codex 作为 provider 在列`} />
            </div>
            <p className="text-[12px] text-muted-foreground/80 bg-secondary rounded-[6px] px-3 py-2">
              ℹ️ MCP stdio 直连（在客户端 settings 里通过 <code className="font-mono text-[11px] bg-muted px-1 rounded">multica mcp serve</code>
              反向把 MyTeam 工具暴露给客户端）目前在 <span className="text-foreground">规划中</span>，不要再参考老文档里的
              <code className="font-mono text-[11px] bg-muted px-1 rounded"> mcpServers.myteam </code>配置 — 命令未发布。
            </p>
          </div>
        </Collapse>

        <Collapse title="REST API — 编程式接入、CI / 第三方系统首选" icon={Key}>
          <div className="space-y-3 pt-3">
            <p className="text-[13px] text-muted-foreground">
              直接调用 <code className="font-mono text-[11px] bg-muted px-1 rounded">POST /api/agents</code> 创建 Agent；
              同域页面直接拷贝 curl 即可。
            </p>
            <CodeBlock code={`curl -X POST ${serverUrl || "<server-url>"}/api/agents \\
  -H "Authorization: Bearer <token>" \\
  -H "X-Workspace-ID: ${workspace?.id ?? "<workspace-id>"}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "name": "ci-agent",
    "description": "CI 流水线专用",
    "runtime_id": "<runtime-id>",
    "visibility": "workspace",
    "max_concurrent_tasks": 4
  }'`} />
            <p className="text-[12px] text-muted-foreground/80 bg-secondary rounded-[6px] px-3 py-2">
              ⚠️ 只有 <span className="text-foreground font-medium">owner</span> / <span className="text-foreground font-medium">admin</span> 可以调用；
              <code className="font-mono text-[11px] bg-muted px-1 rounded">runtime_id</code> 留空会落到默认云端 Runtime（若存在）。
            </p>
          </div>
        </Collapse>
      </div>

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 pt-1 text-[12px] text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <Zap className="h-3.5 w-3.5 text-primary" />
          管理 Token：<a href="/settings" className="text-primary hover:underline">设置 → API 令牌</a>
        </span>
        <span className="flex items-center gap-1.5">
          <Shield className="h-3.5 w-3.5 text-primary" />
          Runtime 在线状态：<a href="/runtimes" className="text-primary hover:underline">/runtimes</a>
        </span>
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
// Skills live under each Subagent now — agents can only call a skill
// via a subagent, so the standalone 技能 tab was redundant with the
// Subagents view. Keep Subagents as the single entry to the knowledge
// layer; Subagents page shows linked skills inline.
const knowledgeTabs = [
  { value: "subagents", label: "Subagents", icon: Layers },
]

const orgTabs = [
  { value: "hierarchy", label: "组织层级", icon: GitBranch },
]

// Tabs that host their own full-bleed layout (resizable panels, left
// rails, etc.). They skip the max-w-3xl reader-width wrapper so the
// internal layout isn't squeezed.
const FULL_WIDTH_TABS = new Set(["subagents"])

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
