"use client";

import { useMemo, useState, type Dispatch, type SetStateAction } from "react";
import { Bot, Circle, Cpu, Sparkles, Terminal, Wand2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import type { Agent, AgentRuntime, IdentityCard } from "@/shared/types";
import { splitList, statusMeta } from "./shared";

type AgentFilter = "all" | "personal_agent" | "local_agent" | "scoped_system_agent" | "system_agent";

interface AgentsTabProps {
  agents: Agent[];
  runtimes: AgentRuntime[];
  selectedAgent: Agent | null;
  selectedAgentId: string;
  setSelectedAgentId: (id: string) => void;
  cardDraft: IdentityCard;
  setCardDraft: Dispatch<SetStateAction<IdentityCard>>;
  suggestedCard: IdentityCard | null;
  generating: boolean;
  saving: boolean;
  activeImpersonationId: string;
  onGenerateSuggestion: () => void;
  onSaveIdentityCard: () => void;
  onImpersonate: (agentId: string) => void;
}

export function AgentsTab({
  agents,
  runtimes,
  selectedAgent,
  selectedAgentId,
  setSelectedAgentId,
  cardDraft,
  setCardDraft,
  suggestedCard,
  generating,
  saving,
  activeImpersonationId,
  onGenerateSuggestion,
  onSaveIdentityCard,
  onImpersonate,
}: AgentsTabProps) {
  const [filter, setFilter] = useState<AgentFilter>("all");

  const runtimeById = useMemo(
    () => new Map(runtimes.map((runtime) => [runtime.id, runtime])),
    [runtimes],
  );

  const filteredAgents = useMemo(() => {
    if (filter === "all") return agents;
    return agents.filter((agent) => {
      const type = agent.agent_type ?? "personal_agent";
      if (filter === "personal_agent") return type === "personal_agent";
      if (filter === "local_agent") return type === "local_agent";
      if (filter === "scoped_system_agent") {
        return type === "system_agent" && agent.scope !== null;
      }
      if (filter === "system_agent") {
        return type === "system_agent" && agent.scope === null;
      }
      return true;
    });
  }, [agents, filter]);

  const selectedRuntime = selectedAgent ? runtimeById.get(selectedAgent.runtime_id) : null;
  const selectedRuntimeMetadata = ((selectedRuntime?.metadata ?? {}) as Record<string, unknown>);

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">Agent 信息</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            按 settings 风格拆成“列表选择 + 详情编辑”，不再把 agent 管理和 owner 信息混在同一段滚动区里。
          </p>
        </div>

        <div className="grid gap-4 xl:grid-cols-[320px,1fr]">
          <Card>
            <CardHeader>
              <CardTitle>Agent 列表</CardTitle>
              <CardDescription>先选中一个 agent，再在右侧编辑身份卡和查看运行时。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex flex-wrap gap-2">
                {[
                  { value: "all", label: "全部" },
                  { value: "personal_agent", label: "Personal" },
                  { value: "local_agent", label: "Local" },
                  { value: "scoped_system_agent", label: "Scoped" },
                  { value: "system_agent", label: "System" },
                ].map((item) => (
                  <button
                    key={item.value}
                    onClick={() => setFilter(item.value as AgentFilter)}
                    className={`rounded-full border px-3 py-1 text-xs transition-colors ${
                      filter === item.value
                        ? "border-primary bg-primary/10 text-primary"
                        : "border-border bg-background text-muted-foreground hover:text-foreground"
                    }`}
                  >
                    {item.label}
                  </button>
                ))}
              </div>

              <div className="space-y-2">
                {filteredAgents.length === 0 ? (
                  <div className="rounded-lg border border-dashed border-border px-4 py-5 text-sm text-muted-foreground">
                    当前筛选下没有 agent。
                  </div>
                ) : (
                  filteredAgents.map((agent) => {
                    const status = statusMeta[agent.status] ?? { label: "离线", dot: "bg-muted-foreground/40" };
                    const runtime = runtimeById.get(agent.runtime_id);
                    const selected = selectedAgentId === agent.id;
                    return (
                      <button
                        key={agent.id}
                        onClick={() => setSelectedAgentId(agent.id)}
                        className={`w-full rounded-lg border px-4 py-3 text-left transition-colors ${
                          selected
                            ? "border-primary bg-primary/5"
                            : "border-border bg-background hover:bg-accent/40"
                        }`}
                      >
                        <div className="flex items-center justify-between gap-2">
                          <div className="truncate text-sm font-medium text-foreground">{agent.name}</div>
                          <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground">
                            <Circle className={`h-2.5 w-2.5 fill-current ${status.dot}`} />
                            {status.label}
                          </span>
                        </div>
                        <div className="mt-1 truncate text-xs text-muted-foreground">{agent.description || "暂无描述"}</div>
                        <div className="mt-2 flex flex-wrap gap-1.5">
                          <Badge variant="outline">{(agent.agent_type ?? "personal_agent").replace(/_/g, " ")}</Badge>
                          <Badge variant="outline">{runtime?.provider ?? "未绑定 runtime"}</Badge>
                          {activeImpersonationId === agent.id ? (
                            <Badge className="border-primary/30 bg-primary/10 text-primary">附身中</Badge>
                          ) : null}
                        </div>
                      </button>
                    );
                  })
                )}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>身份卡与运行时</CardTitle>
              <CardDescription>右侧聚焦单个 agent 的身份卡、接入状态、附身动作与建议版本。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-5">
              {selectedAgent ? (
                <>
                  <div className="flex flex-wrap items-center gap-2">
                    <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                      <Bot className="h-4 w-4 text-primary" />
                      {selectedAgent.name}
                    </div>
                    <Badge variant="outline">{(selectedAgent.agent_type ?? "personal_agent").replace(/_/g, " ")}</Badge>
                    <Badge variant="outline">{selectedAgent.visibility}</Badge>
                    {cardDraft.needs_attention ? (
                      <Badge className="border-amber-500/30 bg-amber-500/10 text-amber-600">needs_attention</Badge>
                    ) : null}
                    <div className="ml-auto flex flex-wrap gap-2">
                      <Button variant="outline" onClick={() => onImpersonate(selectedAgent.id)}>
                        附身
                      </Button>
                      <Button variant="outline" onClick={onGenerateSuggestion} disabled={generating}>
                        <Wand2 className="mr-1 h-4 w-4" />
                        {generating ? "生成中..." : "生成建议"}
                      </Button>
                      <Button onClick={onSaveIdentityCard} disabled={saving}>
                        <Sparkles className="mr-1 h-4 w-4" />
                        {saving ? "保存中..." : "保存身份卡"}
                      </Button>
                    </div>
                  </div>

                  <div className="grid gap-3 md:grid-cols-2">
                    <div className="rounded-lg border border-border bg-background px-4 py-3">
                      <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
                        <Terminal className="h-3 w-3" />
                        Runtime
                      </div>
                      <div className="mt-1 text-sm font-medium text-foreground">{selectedRuntime?.name ?? "未绑定"}</div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        {(selectedRuntimeMetadata.display_name as string | undefined) ?? selectedRuntime?.provider ?? "—"}
                      </div>
                      <div className="mt-2 text-[11px] text-muted-foreground">
                        {selectedRuntime?.server_host || "未上报设备"} {selectedRuntime?.working_dir ? `· ${selectedRuntime.working_dir}` : ""}
                      </div>
                    </div>
                    <div className="rounded-lg border border-border bg-background px-4 py-3">
                      <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
                        <Cpu className="h-3 w-3" />
                        Runtime Capabilities
                      </div>
                      <div className="mt-1 text-sm font-medium text-foreground">
                        {selectedRuntime?.capabilities?.length
                          ? selectedRuntime.capabilities.slice(0, 3).join(" · ")
                          : "—"}
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        {(selectedRuntime?.status ?? "offline")} · {selectedRuntime?.readiness ?? "unknown"}
                        {selectedRuntime?.last_heartbeat_at ? ` · heartbeat ${new Date(selectedRuntime.last_heartbeat_at).toLocaleString()}` : ""}
                      </div>
                    </div>
                  </div>

                  <div className="grid gap-4 lg:grid-cols-2">
                    <div className="space-y-3">
                      <div>
                        <Label className="text-xs text-muted-foreground">角色标题</Label>
                        <Input
                          value={cardDraft.title ?? ""}
                          onChange={(event) => setCardDraft((current) => ({ ...current, title: event.target.value }))}
                          className="mt-1"
                          placeholder="例如：ProjectLinear 执行代理"
                        />
                      </div>
                      <div>
                        <Label className="text-xs text-muted-foreground">手动描述</Label>
                        <Textarea
                          value={cardDraft.description_manual ?? ""}
                          onChange={(event) =>
                            setCardDraft((current) => ({ ...current, description_manual: event.target.value }))
                          }
                          className="mt-1 min-h-[120px]"
                          placeholder="Owner 对此 Agent 的定位、边界和协作方式。"
                        />
                      </div>
                      <div>
                        <Label className="text-xs text-muted-foreground">自动描述</Label>
                        <Textarea
                          value={cardDraft.description_auto ?? ""}
                          onChange={(event) =>
                            setCardDraft((current) => ({ ...current, description_auto: event.target.value }))
                          }
                          className="mt-1 min-h-[120px]"
                          placeholder="System Agent 根据历史执行生成。"
                        />
                      </div>
                    </div>

                    <div className="space-y-3">
                      <div>
                        <Label className="text-xs text-muted-foreground">Skills</Label>
                        <Textarea
                          value={(cardDraft.skills ?? []).join(", ")}
                          onChange={(event) =>
                            setCardDraft((current) => ({ ...current, skills: splitList(event.target.value) }))
                          }
                          className="mt-1 min-h-[84px]"
                          placeholder="coding, review, planning"
                        />
                      </div>
                      <div>
                        <Label className="text-xs text-muted-foreground">Tools</Label>
                        <Textarea
                          value={(cardDraft.tools ?? []).join(", ")}
                          onChange={(event) =>
                            setCardDraft((current) => ({ ...current, tools: splitList(event.target.value) }))
                          }
                          className="mt-1 min-h-[84px]"
                          placeholder="git, browser, workspace"
                        />
                      </div>
                      <div>
                        <Label className="text-xs text-muted-foreground">Capabilities</Label>
                        <Textarea
                          value={(cardDraft.capabilities ?? []).join(", ")}
                          onChange={(event) =>
                            setCardDraft((current) => ({ ...current, capabilities: splitList(event.target.value) }))
                          }
                          className="mt-1 min-h-[84px]"
                          placeholder="plan drafting, DAG execution, artifact review"
                        />
                      </div>
                      <div>
                        <Label className="text-xs text-muted-foreground">Pinned Fields</Label>
                        <Input
                          value={(cardDraft.pinned_fields ?? []).join(", ")}
                          onChange={(event) =>
                            setCardDraft((current) => ({ ...current, pinned_fields: splitList(event.target.value) }))
                          }
                          className="mt-1"
                          placeholder="title, description_manual, tools"
                        />
                      </div>
                    </div>
                  </div>

                  {suggestedCard ? (
                    <div className="rounded-xl border border-dashed border-primary/40 bg-primary/5 p-4">
                      <div className="flex items-center justify-between gap-3">
                        <div>
                          <div className="text-sm font-medium text-foreground">建议版本</div>
                          <p className="mt-1 text-xs text-muted-foreground">
                            这是 system agent 根据执行历史生成的候选身份卡，你可以直接应用后再编辑。
                          </p>
                        </div>
                        <Button variant="outline" onClick={() => setCardDraft(suggestedCard)}>
                          应用建议
                        </Button>
                      </div>
                      <div className="mt-4 grid gap-3 sm:grid-cols-3">
                        <div className="rounded-lg border border-border bg-background px-3 py-2">
                          <div className="text-[11px] text-muted-foreground">Skills</div>
                          <div className="mt-1 text-sm text-foreground">{suggestedCard.skills.join(", ") || "—"}</div>
                        </div>
                        <div className="rounded-lg border border-border bg-background px-3 py-2">
                          <div className="text-[11px] text-muted-foreground">Tools</div>
                          <div className="mt-1 text-sm text-foreground">{suggestedCard.tools.join(", ") || "—"}</div>
                        </div>
                        <div className="rounded-lg border border-border bg-background px-3 py-2">
                          <div className="text-[11px] text-muted-foreground">Capabilities</div>
                          <div className="mt-1 text-sm text-foreground">
                            {suggestedCard.capabilities.join(", ") || "—"}
                          </div>
                        </div>
                      </div>
                    </div>
                  ) : null}
                </>
              ) : (
                <div className="rounded-lg border border-dashed border-border px-4 py-8 text-sm text-muted-foreground">
                  当前工作区还没有 agent。请先到“添加 Agent”标签创建。
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </section>
    </div>
  );
}
