"use client";

import { Bot, Building2, Shield, Sparkles, UserRound } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import type { Agent, User, Workspace } from "@/shared/types";

interface OwnerTabProps {
  user: User | null;
  workspace: Workspace | null;
  systemAgent: Agent | null;
  personalAgentsCount: number;
  pageAgentsCount: number;
  workspaceSystemAgentsCount: number;
}

export function OwnerTab({
  user,
  workspace,
  systemAgent,
  personalAgentsCount,
  pageAgentsCount,
  workspaceSystemAgentsCount,
}: OwnerTabProps) {
  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">身份概览</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            用设置页的浏览方式集中查看 owner、组织和 system agent 的角色关系。
          </p>
        </div>

        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <Card>
            <CardContent className="space-y-2">
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <Building2 className="h-3.5 w-3.5" />
                Workspace
              </div>
              <div className="text-base font-medium text-foreground">{workspace?.name ?? "—"}</div>
              <div className="text-xs text-muted-foreground">{workspace?.slug ?? "workspace"}</div>
            </CardContent>
          </Card>

          <Card>
            <CardContent className="space-y-2">
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <UserRound className="h-3.5 w-3.5" />
                Owner
              </div>
              <div className="text-base font-medium text-foreground">{user?.name ?? "—"}</div>
              <div className="text-xs text-muted-foreground">{user?.email ?? "未登录"}</div>
            </CardContent>
          </Card>

          <Card>
            <CardContent className="space-y-2">
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <Bot className="h-3.5 w-3.5" />
                Personal Agents
              </div>
              <div className="text-2xl font-semibold text-foreground">{personalAgentsCount}</div>
              <div className="text-xs text-muted-foreground">由 owner 直接管理与附身的执行代理。</div>
            </CardContent>
          </Card>

          <Card>
            <CardContent className="space-y-2">
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <Shield className="h-3.5 w-3.5" />
                System Agents
              </div>
              <div className="text-2xl font-semibold text-foreground">{pageAgentsCount + workspaceSystemAgentsCount}</div>
              <div className="text-xs text-muted-foreground">页面系统 agent 与全局 system agent 的总和。</div>
            </CardContent>
          </Card>
        </div>
      </section>

      <section className="grid gap-4 xl:grid-cols-[1.1fr,0.9fr]">
        <Card>
          <CardHeader>
            <CardTitle>Owner 身份</CardTitle>
            <CardDescription>这部分只讲谁在决策，不混入具体 agent 编辑表单。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="rounded-lg border border-border bg-background px-4 py-3">
              <div className="text-xs text-muted-foreground">主责任</div>
              <div className="mt-1 text-sm text-foreground">
                决定 agent 的定位、批准身份卡内容、选择何时新增 agent，以及何时允许附身与系统调度。
              </div>
            </div>
            <div className="rounded-lg border border-border bg-background px-4 py-3">
              <div className="text-xs text-muted-foreground">协作方式</div>
              <div className="mt-1 text-sm text-foreground">
                Owner 负责“谁是谁”，Settings 负责“默认怎么跑”；Agents 标签负责查看和编辑已有 agent。
              </div>
            </div>
            <div className="rounded-lg border border-border bg-background px-4 py-3">
              <div className="text-xs text-muted-foreground">新增 Agent 的时机</div>
              <div className="mt-1 text-sm text-foreground">
                当某类工作长期重复、需要独立身份卡或需要单独 runtime 时，再到“添加 Agent”标签创建，而不是在这里堆更多说明。
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>系统角色</CardTitle>
            <CardDescription>全局 system agent 与页面 agent 的分工摘要。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="rounded-lg border border-border bg-background px-4 py-3">
              <div className="flex items-center gap-2">
                <Shield className="h-4 w-4 text-primary" />
                <div className="text-sm font-medium text-foreground">{systemAgent?.name ?? "未初始化"}</div>
                {systemAgent ? <Badge variant="outline">global</Badge> : null}
              </div>
              <div className="mt-2 text-xs text-muted-foreground">
                {systemAgent ? "负责默认策略、身份卡建议和自动调度。" : "首次访问后端 system agent 接口后自动创建。"}
              </div>
            </div>

            <div className="rounded-lg border border-border bg-background px-4 py-3">
              <div className="flex items-center gap-2">
                <Sparkles className="h-4 w-4 text-primary" />
                <div className="text-sm font-medium text-foreground">页面系统 Agent</div>
              </div>
              <div className="mt-2 text-xs text-muted-foreground">
                当前共有 {pageAgentsCount} 个页面系统 agent，主要负责具体页面上下文里的默认协作行为。
              </div>
            </div>

            <div className="rounded-lg border border-border bg-background px-4 py-3">
              <div className="text-xs text-muted-foreground">工作区系统 Agent</div>
              <div className="mt-1 text-sm text-foreground">{workspaceSystemAgentsCount} 个</div>
              <div className="mt-1 text-xs text-muted-foreground">这类 agent 处理更偏全局的自动调度和状态判定。</div>
            </div>
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
