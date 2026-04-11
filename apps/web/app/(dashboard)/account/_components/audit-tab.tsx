"use client";

import { Cpu, Shield } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import type { Agent, AgentRuntime, WorkspaceAuditEntry } from "@/shared/types";
import { formatAuditAction, formatTime } from "./shared";

interface AuditTabProps {
  auditEntries: WorkspaceAuditEntry[];
  runtimes: AgentRuntime[];
  systemAgent: Agent | null;
}

export function AuditTab({ auditEntries, runtimes, systemAgent }: AuditTabProps) {
  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">系统审计</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            将审计与运行时单独放进一个标签页，避免和身份编辑表单相互打断。
          </p>
        </div>

        <div className="grid gap-4 xl:grid-cols-[1.2fr,0.8fr]">
          <Card>
            <CardHeader>
              <CardTitle>事件日志</CardTitle>
              <CardDescription>来自 system event bus 的落库审计、自动调度记录与身份卡变更。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {auditEntries.length === 0 ? (
                <div className="rounded-lg border border-dashed border-border px-4 py-6 text-sm text-muted-foreground">
                  暂无审计记录。
                </div>
              ) : (
                auditEntries.map((entry) => (
                  <div key={entry.id} className="rounded-lg border border-border bg-background px-4 py-3">
                    <div className="flex items-center justify-between gap-3">
                      <div className="text-sm font-medium text-foreground">{formatAuditAction(entry.action)}</div>
                      <div className="text-xs text-muted-foreground">{formatTime(entry.created_at)}</div>
                    </div>
                    <div className="mt-1 text-xs text-muted-foreground">
                      actor: {entry.actor_type} {entry.actor_id ? `· ${entry.actor_id.slice(0, 8)}` : ""}
                    </div>
                    <pre className="mt-3 overflow-x-auto rounded-lg bg-secondary/50 px-3 py-2 text-xs text-secondary-foreground">
                      {JSON.stringify(entry.details, null, 2)}
                    </pre>
                  </div>
                ))
              )}
            </CardContent>
          </Card>

          <div className="space-y-4">
            <Card>
              <CardHeader>
                <CardTitle>全局 System Agent</CardTitle>
                <CardDescription>审计页只显示摘要，不在这里编辑身份卡内容。</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="rounded-lg border border-border bg-background px-4 py-3">
                  <div className="flex items-center gap-2">
                    <Shield className="h-4 w-4 text-primary" />
                    <div className="text-sm font-medium text-foreground">{systemAgent?.name ?? "未初始化"}</div>
                    {systemAgent ? <Badge variant="outline">system</Badge> : null}
                  </div>
                  <div className="mt-2 text-xs text-muted-foreground">
                    {systemAgent ? "负责默认策略、身份建议和系统调度。" : "尚未初始化全局 system agent。"}
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>运行时接入概览</CardTitle>
                <CardDescription>快速确认 runtime 是否在线，便于判断 Agent 是否具备执行条件。</CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                {runtimes.length === 0 ? (
                  <div className="rounded-lg border border-dashed border-border px-4 py-6 text-sm text-muted-foreground">
                    暂无运行时。
                  </div>
                ) : (
                  runtimes.map((runtime) => {
                    const metadata = (runtime.metadata ?? {}) as Record<string, unknown>;
                    return (
                      <div key={runtime.id} className="rounded-lg border border-border bg-background px-4 py-3">
                        <div className="flex items-center justify-between gap-2">
                          <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                            <Cpu className="h-4 w-4 text-primary" />
                            {runtime.name}
                          </div>
                          <Badge variant="outline">{runtime.status}</Badge>
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">
                          {(metadata.display_name as string | undefined) ?? runtime.provider}
                          {typeof metadata.version === "string" ? ` · ${metadata.version}` : ""}
                        </div>
                        <div className="mt-2 text-[11px] text-muted-foreground">
                          {Array.isArray(metadata.capabilities)
                            ? (metadata.capabilities as string[]).join(" · ")
                            : "No capability metadata"}
                        </div>
                      </div>
                    );
                  })
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      </section>
    </div>
  );
}
