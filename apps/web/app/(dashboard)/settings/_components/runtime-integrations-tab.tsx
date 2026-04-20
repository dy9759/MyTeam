"use client";

import { useEffect, useMemo, useState } from "react";
import { Cpu, Save } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useWorkspaceManagement } from "@/features/workspace";
import { api } from "@/shared/api";
import type { AgentRuntime } from "@/shared/types";

interface RuntimeSettings {
  default_provider: string;
  default_model: string;
  browser_persistence: boolean;
  repo_checkout_strategy: string;
  mcp_enabled: boolean;
}

const defaultSettings: RuntimeSettings = {
  default_provider: "",
  default_model: "",
  browser_persistence: true,
  repo_checkout_strategy: "worktree_per_run",
  mcp_enabled: false,
};

export function RuntimeIntegrationsTab() {
  const { workspace, canManageWorkspace, saveWorkspaceSettings } = useWorkspaceManagement();
  const [runtimes, setRuntimes] = useState<AgentRuntime[]>([]);
  const [values, setValues] = useState<RuntimeSettings>(defaultSettings);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      const nextRuntimes = await api.listRuntimes().catch(() => []);
      if (cancelled) return;
      setRuntimes(nextRuntimes);
    }
    load();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const stored = ((workspace?.settings ?? {}) as Record<string, unknown>).runtime_integrations as
      | Partial<RuntimeSettings>
      | undefined;
    setValues({
      ...defaultSettings,
      ...stored,
    });
  }, [workspace]);

  const providerOptions = useMemo(() => {
    const byProvider = new Map<string, string>();
    runtimes.forEach((runtime) => {
      const metadata = (runtime.metadata ?? {}) as Record<string, unknown>;
      byProvider.set(runtime.provider, (metadata.display_name as string | undefined) ?? runtime.provider);
    });
    return Array.from(byProvider.entries());
  }, [runtimes]);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    try {
      await saveWorkspaceSettings((currentSettings) => ({
        ...currentSettings,
        runtime_integrations: values,
      }));
      toast.success("Runtime & Integrations 已保存");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "保存运行策略失败");
    } finally {
      setSaving(false);
    }
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">Runtime & Integrations</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            管理 provider 默认值、browser 持久化、MCP 开关和 repo checkout 策略。
          </p>
        </div>

        <Card>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">默认 Provider</Label>
              <select
                value={values.default_provider}
                onChange={(event) => setValues((current) => ({ ...current, default_provider: event.target.value }))}
                disabled={!canManageWorkspace}
                className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground"
              >
                <option value="">跟随具体 Agent 绑定</option>
                {providerOptions.map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">默认 Model</Label>
              <Input
                value={values.default_model}
                onChange={(event) => setValues((current) => ({ ...current, default_model: event.target.value }))}
                disabled={!canManageWorkspace}
                placeholder="例如 gpt-5.4 / claude-sonnet"
              />
            </div>

            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">Repo Checkout 策略</Label>
              <select
                value={values.repo_checkout_strategy}
                onChange={(event) =>
                  setValues((current) => ({ ...current, repo_checkout_strategy: event.target.value }))
                }
                disabled={!canManageWorkspace}
                className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground"
              >
                <option value="worktree_per_run">每次 run 独立 worktree</option>
                <option value="tracked_branch">跟踪当前分支</option>
                <option value="clean_main">始终从 main 开始</option>
              </select>
            </div>

            <div className="flex items-center justify-between rounded-lg border border-border px-3 py-3">
              <div>
                <div className="text-sm font-medium text-foreground">Browser persistence</div>
                <div className="mt-1 text-xs text-muted-foreground">保留会话浏览器状态，便于多 agent 连续协作。</div>
              </div>
              <Switch
                checked={values.browser_persistence}
                onCheckedChange={(checked) => setValues((current) => ({ ...current, browser_persistence: checked }))}
                disabled={!canManageWorkspace}
              />
            </div>

            <div className="flex items-center justify-between rounded-lg border border-border px-3 py-3">
              <div>
                <div className="text-sm font-medium text-foreground">MCP enabled</div>
                <div className="mt-1 text-xs text-muted-foreground">控制新的 MCP / external tool connector 是否默认开放。</div>
              </div>
              <Switch
                checked={values.mcp_enabled}
                onCheckedChange={(checked) => setValues((current) => ({ ...current, mcp_enabled: checked }))}
                disabled={!canManageWorkspace}
              />
            </div>

            <div className="flex items-center justify-end">
              <Button size="sm" onClick={handleSave} disabled={saving || !canManageWorkspace}>
                <Save className="h-3 w-3" />
                {saving ? "保存中..." : "保存配置"}
              </Button>
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="space-y-4">
        <h2 className="text-sm font-semibold">已注册 Runtime</h2>
        <div className="space-y-3">
          {runtimes.length === 0 ? (
            <Card>
              <CardContent className="text-sm text-muted-foreground">当前工作区还没有在线 runtime。</CardContent>
            </Card>
          ) : (
            runtimes.map((runtime) => {
              const metadata = (runtime.metadata ?? {}) as Record<string, unknown>;
              const capabilities = Array.isArray(metadata.capabilities)
                ? (metadata.capabilities as string[])
                : [];
              return (
                <Card key={runtime.id}>
                  <CardContent className="space-y-3">
                    <div className="flex items-start justify-between gap-3">
                      <div>
                        <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                          <Cpu className="h-4 w-4 text-primary" />
                          {runtime.name}
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">
                          {(metadata.display_name as string | undefined) ?? runtime.provider}
                          {typeof metadata.version === "string" ? ` · ${metadata.version}` : ""}
                        </div>
                      </div>
                      <span className="rounded-full border border-border px-2 py-0.5 text-[11px] text-muted-foreground">
                        {runtime.status}
                      </span>
                    </div>
                    <div className="flex flex-wrap gap-1.5">
                      {capabilities.length > 0 ? (
                        capabilities.map((capability) => (
                          <span
                            key={capability}
                            className="rounded-full border border-border bg-secondary/60 px-2 py-0.5 text-[11px] text-secondary-foreground"
                          >
                            {capability}
                          </span>
                        ))
                      ) : (
                        <span className="text-xs text-muted-foreground">无 capability metadata</span>
                      )}
                    </div>
                  </CardContent>
                </Card>
              );
            })
          )}
        </div>
      </section>
    </div>
  );
}
