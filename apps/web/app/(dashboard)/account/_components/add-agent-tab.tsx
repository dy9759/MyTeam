"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import { Bot, Check, Copy, Cpu, Save, Shield, Sparkles } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { api } from "@/shared/api";
import type { Agent, AgentRuntime, AgentVisibility } from "@/shared/types";

interface AddAgentTabProps {
  runtimes: AgentRuntime[];
  canManageWorkspace: boolean;
  presetRuntimeId?: string;
  onCreated: (agent: Agent) => void;
}

interface CopySnippetProps {
  title: string;
  description: string;
  code: string;
  badgeLabel: string;
  badgeVariant?: "default" | "outline" | "secondary" | "destructive";
  copyLabel: string;
}

function CopySnippet({
  title,
  description,
  code,
  badgeLabel,
  badgeVariant = "outline",
  copyLabel,
}: CopySnippetProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      toast.success("内容已复制");
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      toast.error("复制失败，请手动复制");
    }
  };

  return (
    <div className="rounded-lg border border-border bg-background px-4 py-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <div className="text-sm font-medium text-foreground">{title}</div>
            <Badge variant={badgeVariant}>{badgeLabel}</Badge>
          </div>
          <p className="mt-2 text-xs leading-5 text-muted-foreground">{description}</p>
        </div>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                onClick={handleCopy}
                aria-label={copyLabel}
              >
                {copied ? <Check className="h-4 w-4 text-success" /> : <Copy className="h-4 w-4" />}
              </Button>
            }
          />
          <TooltipContent>{copied ? "已复制" : "复制"}</TooltipContent>
        </Tooltip>
      </div>
      <pre className="mt-3 overflow-x-auto rounded-md bg-muted px-3 py-3 text-xs leading-6 text-foreground">
{code}
      </pre>
    </div>
  );
}

export function AddAgentTab({
  runtimes,
  canManageWorkspace,
  presetRuntimeId = "",
  onCreated,
}: AddAgentTabProps) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [instructions, setInstructions] = useState("");
  const [runtimeId, setRuntimeId] = useState("");
  const [visibility, setVisibility] = useState<AgentVisibility>("private");
  const [maxConcurrentTasks, setMaxConcurrentTasks] = useState("6");
  const [creating, setCreating] = useState(false);
  const appliedPresetRef = useRef("");

  useEffect(() => {
    if (!runtimeId && runtimes.length > 0) {
      setRuntimeId(runtimes[0]?.id ?? "");
    }
  }, [runtimeId, runtimes]);

  useEffect(() => {
    if (!presetRuntimeId || appliedPresetRef.current === presetRuntimeId) {
      return;
    }
    if (!runtimes.some((runtime) => runtime.id === presetRuntimeId)) {
      return;
    }
    setRuntimeId(presetRuntimeId);
    appliedPresetRef.current = presetRuntimeId;
  }, [presetRuntimeId, runtimes]);

  const runtimeOptions = useMemo(
    () =>
      runtimes.map((runtime) => {
        const metadata = (runtime.metadata ?? {}) as Record<string, unknown>;
        return {
          ...runtime,
          displayName: (metadata.display_name as string | undefined) ?? runtime.provider,
          version: typeof metadata.version === "string" ? metadata.version : null,
        };
      }),
    [runtimes],
  );
  const presetRuntime = runtimeOptions.find((runtime) => runtime.id === presetRuntimeId) ?? null;
  const selectedRuntime = runtimeOptions.find((runtime) => runtime.id === runtimeId) ?? null;

  const handleCreate = async () => {
    if (!canManageWorkspace) {
      toast.error("只有 owner 或 admin 可以创建 Agent");
      return;
    }
    if (!name.trim()) {
      toast.error("请输入 Agent 名称");
      return;
    }
    if (!runtimeId) {
      toast.error("请先选择一个 Runtime");
      return;
    }

    setCreating(true);
    try {
      const agent = await api.createAgent({
        name: name.trim(),
        description: description.trim(),
        instructions: instructions.trim(),
        runtime_id: runtimeId,
        visibility,
        max_concurrent_tasks: Number(maxConcurrentTasks) || 6,
      });
      toast.success("Agent 已创建");
      setName("");
      setDescription("");
      setInstructions("");
      setVisibility("private");
      setMaxConcurrentTasks("6");
      onCreated(agent);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "创建 Agent 失败");
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">如何添加 Agent</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            先确认要新增的是一个长期角色，再选择 runtime、填写职责说明，然后创建到工作区里。
          </p>
        </div>

        <Card>
          <CardHeader className="space-y-2">
            <CardTitle>绑定到 MyTeam 平台</CardTitle>
            <CardDescription>
              当前项目不是直接上传一个 Agent，而是先让本机 CLI 通过 daemon 接入成 runtime，再把 agent 绑定到这个 runtime。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="rounded-lg border border-primary/20 bg-primary/5 px-4 py-3">
              <div className="text-xs font-medium uppercase tracking-[0.2em] text-primary">Binding Flow</div>
              <div className="mt-2 text-sm font-medium text-foreground">本机 / CLI -&gt; Runtime -&gt; Agent</div>
              <p className="mt-2 text-sm text-muted-foreground">
                Runtime 是执行宿主，Agent 是工作区里的协作身份。你在这里选择的 runtime，
                就是在决定这个 Agent 由哪台设备、哪个 provider、哪组能力来执行。
              </p>
            </div>

            <div className="grid gap-4 xl:grid-cols-[1.05fr,0.95fr]">
              <div className="space-y-4">
                <div className="rounded-lg border border-border bg-background px-4 py-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <div className="text-sm font-medium text-foreground">方式一：你在 CLI 手动运行</div>
                    <Badge variant="outline">用户手动</Badge>
                  </div>
                  <ol className="mt-3 space-y-3 text-sm text-muted-foreground">
                    <li>
                      <span className="font-medium text-foreground">步骤 0.</span> 先确认 `myteam` 命令可用；如果终端提示
                      `myteam: command not found`，先用下面的命令在仓库里构建，或者把它链接到 `~/.local/bin`。
                    </li>
                    <li>
                      <span className="font-medium text-foreground">步骤 1.</span> 默认云端登录站点走 `https://myteam.ai`；如果你在做本地联调或自部署，再显式改成自己的 app/server 地址。
                    </li>
                    <li>
                      <span className="font-medium text-foreground">步骤 2.</span> 在本机终端登录并启动 daemon。
                    </li>
                    <li>
                      <span className="font-medium text-foreground">步骤 3.</span> 检查 runtime 是否已经出现在 MyTeam。
                    </li>
                    <li>
                      <span className="font-medium text-foreground">步骤 4.</span> 如果 runtime 还没出现，再手动把目标 workspace 加入 watch list。
                    </li>
                    <li>
                      <span className="font-medium text-foreground">步骤 5.</span> 回到这个页面，选中 runtime，创建 Agent。
                    </li>
                  </ol>
                </div>

                <CopySnippet
                  title="如果提示 myteam: command not found"
                  description="先在 MyTeam 仓库根目录构建 CLI。把 `/path/to/MyTeam` 改成你的实际仓库路径。构建完成后，可以直接用仓库里的二进制继续跑，不必先装到系统里。"
                  code={`cd /path/to/MyTeam
make build
./server/bin/myteam config set app_url https://myteam.ai
./server/bin/myteam config set server_url https://api.myteam.ai
./server/bin/myteam login
./server/bin/myteam daemon start
./server/bin/myteam runtime list`}
                  badgeLabel="先跑通"
                  copyLabel="复制“如果提示 myteam: command not found”命令"
                />

                <CopySnippet
                  title="把 myteam 放进 PATH"
                  description="如果你希望后续直接在任意目录执行 myteam，可以把它链接到 ~/.local/bin。把 `/path/to/MyTeam` 改成你的实际仓库路径。"
                  code={`mkdir -p ~/.local/bin
ln -sf /path/to/MyTeam/server/bin/myteam ~/.local/bin/myteam
myteam config set app_url https://myteam.ai
myteam config set server_url https://api.myteam.ai
myteam version`}
                  badgeLabel="可选安装"
                  copyLabel="复制“把 myteam 放进 PATH”命令"
                />

                <CopySnippet
                  title="默认云端登录并启动 Runtime"
                  description="这组命令默认走 https://myteam.ai。第一次 login 可能会打开浏览器，需要你本人完成认证。"
                  code={`myteam config set app_url https://myteam.ai
myteam config set server_url https://api.myteam.ai
myteam login
myteam daemon start
myteam runtime list`}
                  badgeLabel="你手动运行"
                  copyLabel="复制“默认云端登录并启动 Runtime”命令"
                />

                <CopySnippet
                  title="本地开发 / 自部署时改地址"
                  description="只有在本地联调或连接你自己的部署时，才需要覆盖默认的 myteam.ai 地址。"
                  code={`export MYTEAM_APP_URL=http://localhost:3000
export MYTEAM_SERVER_URL=ws://localhost:8080/ws

./server/bin/myteam login
./server/bin/myteam daemon start
./server/bin/myteam runtime list`}
                  badgeLabel="本地联调"
                  badgeVariant="secondary"
                  copyLabel="复制“本地开发 / 自部署时改地址”命令"
                />

                <CopySnippet
                  title="补充 watch 指令"
                  description="当 daemon 没有自动监控目标 workspace 时，手动补这一条。把 `YOUR_WORKSPACE_ID` 改成实际工作区 ID。"
                  code={`myteam workspace watch YOUR_WORKSPACE_ID`}
                  badgeLabel="你手动运行"
                  copyLabel="复制“workspace watch”命令"
                />
              </div>

              <div className="space-y-4">
                <div className="rounded-lg border border-border bg-background px-4 py-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <div className="text-sm font-medium text-foreground">方式二：让 Agent 自动配置</div>
                    <Badge variant="secondary">Agent 可代办</Badge>
                  </div>
                  <ol className="mt-3 space-y-3 text-sm text-muted-foreground">
                    <li>
                      <span className="font-medium text-foreground">步骤 1.</span> 把下面的任务说明发给一个有终端执行权限的 agent。
                    </li>
                    <li>
                      <span className="font-medium text-foreground">步骤 2.</span> 你只需要在 agent 提示时完成浏览器登录，或提供 token。
                    </li>
                    <li>
                      <span className="font-medium text-foreground">步骤 3.</span> agent 完成后，把 runtime id 回传给你；你再回到这个页面创建 Agent。
                    </li>
                  </ol>
                </div>

                <CopySnippet
                  title="发给 Agent 的自动配置提示词"
                  description="适合 Codex、Claude Code 或任何能在你机器上执行终端命令的 agent。默认要求它走 https://myteam.ai；只有你明确说是本地联调时，才改成 localhost 或自部署地址。"
                  code={`请在当前机器上帮我完成 MyTeam runtime 接入：
1. 检查 myteam CLI 是否可用。
2. 默认把登录站点设为 https://myteam.ai，把 API 设为 https://api.myteam.ai；只有我明确说在本地联调或自部署时，才改成对应地址。
3. 如果终端提示 myteam: command not found，请先在仓库根目录执行 make build，并优先使用 ./server/bin/myteam 继续后续步骤。
4. 如果我要求把命令安装到 PATH，再执行 ln -sf /path/to/MyTeam/server/bin/myteam ~/.local/bin/myteam。
5. 执行 myteam login；如果需要我本人在浏览器确认，请停下来提醒我。
6. 执行 myteam daemon start。
7. 执行 myteam runtime list，确认 runtime 已上线。
8. 如果目标 workspace 不在 daemon watch list，执行 myteam workspace watch YOUR_WORKSPACE_ID，然后重新检查。
9. 最后把可用的 runtime 名称和 runtime id 返回给我，不要替我创建 agent。`}
                  badgeLabel="发给 Agent"
                  badgeVariant="secondary"
                  copyLabel="复制“发给 Agent 的自动配置提示词”"
                />

                <CopySnippet
                  title="只走 CLI 创建 Agent"
                  description="当 runtime 已经准备好时，你可以不走这个页面，直接在终端里创建一个绑定到指定 runtime 的 agent。把 `YOUR_RUNTIME_ID` 改成实际值。"
                  code={`myteam agent create --name "Code Review Agent" --runtime-id YOUR_RUNTIME_ID`}
                  badgeLabel="可选方式"
                  badgeVariant="outline"
                  copyLabel="复制“CLI 创建 Agent”命令"
                />

                <div className="rounded-lg border border-border bg-background px-4 py-3 text-xs text-muted-foreground">
                  <div className="mb-2 flex items-center gap-2 text-sm font-medium text-foreground">
                    <Shield className="h-4 w-4 text-primary" />
                    需要你确认的点
                  </div>
                  首次登录通常必须由你本人完成浏览器认证。agent 可以代你执行命令，但不能替你完成账号授权本身。
                </div>

                <div className="flex flex-wrap gap-2">
                  <Link
                    href="/runtimes"
                    className="inline-flex items-center rounded-md border border-border px-3 py-2 text-xs font-medium text-foreground transition-colors hover:bg-accent"
                  >
                    打开 Runtime 页面
                  </Link>
                  <Link
                    href="/runtimes"
                    className="inline-flex items-center rounded-md border border-border px-3 py-2 text-xs text-muted-foreground transition-colors hover:bg-accent"
                  >
                    先确认在线状态，再回到这里绑定 Agent
                  </Link>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>

        {presetRuntime ? (
          <div className="rounded-lg border border-primary/20 bg-primary/5 px-4 py-3 text-sm">
            <div className="flex flex-wrap items-center gap-2 font-medium text-foreground">
              <Cpu className="h-4 w-4 text-primary" />
              当前正在为 Runtime “{presetRuntime.name}” 创建 Agent
            </div>
            <div className="mt-1 text-muted-foreground">
              Runtime 决定执行宿主和可用 provider，Agent 决定在工作区里如何被看到、如何协作、以及默认执行边界。
            </div>
          </div>
        ) : null}

        <div className="grid gap-4 md:grid-cols-3">
          <Card>
            <CardContent className="space-y-2">
              <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                <Cpu className="h-4 w-4 text-primary" />
                1. 选择 Runtime
              </div>
              <div className="text-xs text-muted-foreground">
                新 agent 必须绑定一个 runtime。没有 runtime 时，不建议先创建空壳 agent。
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="space-y-2">
              <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                <Bot className="h-4 w-4 text-primary" />
                2. 写清角色
              </div>
              <div className="text-xs text-muted-foreground">
                名称和描述要回答“它负责什么、和 owner 如何协作、什么时候不该用它”。
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="space-y-2">
              <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                <Sparkles className="h-4 w-4 text-primary" />
                3. 创建后补身份卡
              </div>
              <div className="text-xs text-muted-foreground">
                创建完成后切到 “Agents” 标签编辑身份卡，再决定是否允许附身和自动建议。
              </div>
            </CardContent>
          </Card>
        </div>
      </section>

      <section className="grid gap-4 xl:grid-cols-[1.1fr,0.9fr]">
        <Card>
          <CardHeader>
            <CardTitle>创建 Agent</CardTitle>
            <CardDescription>第一版只要求最核心字段，避免把身份页做成第二套复杂配置台。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {!canManageWorkspace ? (
              <div className="rounded-lg border border-dashed border-border px-4 py-6 text-sm text-muted-foreground">
                当前账号不是 owner / admin，只能查看引导，不能直接创建 Agent。
              </div>
            ) : null}

            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">名称</Label>
              <Input
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="例如：Code Review Agent"
                disabled={!canManageWorkspace}
              />
            </div>

            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">描述</Label>
              <Textarea
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                placeholder="说明这个 Agent 的职责、边界和适用场景。"
                className="min-h-[100px]"
                disabled={!canManageWorkspace}
              />
            </div>

            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">Instructions</Label>
              <Textarea
                value={instructions}
                onChange={(event) => setInstructions(event.target.value)}
                placeholder="可选。写一些创建时就固定的协作规则或执行偏好。"
                className="min-h-[120px]"
                disabled={!canManageWorkspace}
              />
            </div>

            <div className="grid gap-4 md:grid-cols-3">
              <div className="space-y-2 md:col-span-2">
                <Label className="text-xs text-muted-foreground">Runtime</Label>
                <select
                  value={runtimeId}
                  onChange={(event) => setRuntimeId(event.target.value)}
                  disabled={!canManageWorkspace || runtimeOptions.length === 0}
                  className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground"
                >
                  {runtimeOptions.length === 0 ? <option value="">暂无可用 runtime</option> : null}
                  {runtimeOptions.map((runtime) => (
                    <option key={runtime.id} value={runtime.id}>
                      {runtime.name} · {runtime.displayName}
                    </option>
                  ))}
                </select>
              </div>
              <div className="space-y-2">
                <Label className="text-xs text-muted-foreground">Visibility</Label>
                <select
                  value={visibility}
                  onChange={(event) => setVisibility(event.target.value as AgentVisibility)}
                  disabled={!canManageWorkspace}
                  className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground"
                >
                  <option value="private">Private</option>
                  <option value="workspace">Workspace</option>
                </select>
              </div>
            </div>

            {selectedRuntime ? (
              <div className="rounded-lg border border-primary/20 bg-primary/5 px-4 py-3 text-sm">
                <div className="font-medium text-foreground">
                  当前表单会把新 Agent 绑定到 Runtime “{selectedRuntime.name}”
                </div>
                <div className="mt-1 text-muted-foreground">
                  Provider: {selectedRuntime.displayName}
                  {selectedRuntime.version ? ` · ${selectedRuntime.version}` : ""}
                  {selectedRuntime.server_host ? ` · ${selectedRuntime.server_host}` : ""}
                </div>
              </div>
            ) : null}

            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">最大并发任务数</Label>
              <Input
                type="number"
                min={1}
                value={maxConcurrentTasks}
                onChange={(event) => setMaxConcurrentTasks(event.target.value)}
                disabled={!canManageWorkspace}
              />
            </div>

            <div className="flex justify-end">
              <Button onClick={handleCreate} disabled={creating || !canManageWorkspace || runtimeOptions.length === 0}>
                <Save className="mr-1 h-4 w-4" />
                {creating ? "创建中..." : "创建 Agent"}
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>当前 Runtime</CardTitle>
            <CardDescription>这里列出现有可绑定的 runtime，创建前先确认在线状态。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {runtimeOptions.length === 0 ? (
              <div className="rounded-lg border border-dashed border-border px-4 py-6 text-sm text-muted-foreground">
                当前工作区没有在线 runtime。先确认 `myteam` 命令可用；如果命令不存在，先在仓库里执行 `make build`
                并使用 `./server/bin/myteam`。默认云端请先设成 `https://myteam.ai` / `https://api.myteam.ai`，随后运行
                `myteam login`、`myteam daemon start`，确认 `/runtimes`
                页面里出现在线 runtime，再回来创建 Agent。
              </div>
            ) : (
              runtimeOptions.map((runtime) => (
                <div key={runtime.id} className="rounded-lg border border-border bg-background px-4 py-3">
                  <div className="flex items-center justify-between gap-2">
                    <div className="text-sm font-medium text-foreground">{runtime.name}</div>
                    <Badge variant="outline">{runtime.status}</Badge>
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {runtime.displayName}
                    {runtime.version ? ` · ${runtime.version}` : ""}
                    {runtime.server_host ? ` · ${runtime.server_host}` : ""}
                  </div>
                </div>
              ))
            )}

            <div className="rounded-lg border border-border bg-background px-4 py-3 text-xs text-muted-foreground">
              <div className="mb-2 flex items-center gap-2 text-sm font-medium text-foreground">
                <Shield className="h-4 w-4 text-primary" />
                创建建议
              </div>
              优先新增长期角色型 agent。临时工作如果只是一次性执行，更适合复用现有 agent，而不是继续增加身份数量。
            </div>
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
