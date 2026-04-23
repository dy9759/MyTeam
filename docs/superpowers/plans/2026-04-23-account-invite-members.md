# 账户页邀请成员 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 账户页 `/account` 新增「成员」tab（替换当前 buggy `hierarchy` tab），复用 settings 的 `MembersTab` 组件以支持邀请用户加入组织、成员角色管理。

**Architecture:** 复用已有 `MembersTab` 组件（含邀请 email + 角色选择 + 成员列表 + 角色变更 + 移除）。将该组件从 `app/(dashboard)/settings/_components/` 迁至 `features/workspace/components/` 供两路由复用。其依赖的 `settings-error.ts` 因被 8 个 settings tab 共用，迁至 `shared/settings-error.ts`。账户页删掉 bug 的 `hierarchy` tab、新增 `members` tab 引用迁移后的组件。

**Tech Stack:** Next.js 16 App Router, TypeScript, React, Zustand (`useWorkspaceManagement`), shadcn UI。

**Spec:** `docs/superpowers/specs/2026-04-23-account-invite-members-design.md`

---

## File Structure

### Created
- `apps/web/shared/settings-error.ts` — 从 settings `_components/` 迁出的错误消息工具
- `apps/web/features/workspace/components/members-tab.tsx` — 从 settings `_components/` 迁出的成员管理组件

### Modified
- `apps/web/features/workspace/index.ts` — 新增 `MembersTab` export
- `apps/web/app/(dashboard)/settings/page.tsx` — 改 `MembersTab` import 路径
- `apps/web/app/(dashboard)/settings/_components/account-tab.tsx` — 改 `settings-error` import
- `apps/web/app/(dashboard)/settings/_components/members-tab.tsx` — **删除**（内容已迁走）
- `apps/web/app/(dashboard)/settings/_components/policy-tab.tsx` — 改 import
- `apps/web/app/(dashboard)/settings/_components/repositories-tab.tsx` — 改 import
- `apps/web/app/(dashboard)/settings/_components/runtime-integrations-tab.tsx` — 改 import
- `apps/web/app/(dashboard)/settings/_components/secrets-tab.tsx` — 改 import
- `apps/web/app/(dashboard)/settings/_components/settings-error.ts` — **删除**（内容已迁走）
- `apps/web/app/(dashboard)/settings/_components/tokens-tab.tsx` — 改 import
- `apps/web/app/(dashboard)/settings/_components/workspace-tab.tsx` — 改 import
- `apps/web/app/(dashboard)/account/page.tsx` — 替换 `hierarchy` tab 为 `members` tab

### Not Touched
- `apps/web/features/workspace/hooks.ts` — `inviteMember` 逻辑不变
- `apps/web/features/workspace/hooks.test.ts` — 已覆盖 `inviteMember`
- `apps/web/shared/api/client.ts` — `createMember` 不变

---

## Task 1: 迁移 `settings-error.ts` 到 `shared/`

**Files:**
- Create: `apps/web/shared/settings-error.ts`
- Delete: `apps/web/app/(dashboard)/settings/_components/settings-error.ts`
- Modify: 8 个 settings tab 文件的 import 路径

此文件被 `members-tab.tsx` + 7 个其他 settings tab 共用。先迁到 shared 供两路由下游共享。

- [ ] **Step 1: 创建新文件 `apps/web/shared/settings-error.ts`**

内容与原文件一致：

```ts
function hasCjkCharacters(text: string): boolean {
  return /[\u3400-\u9fff]/.test(text);
}

export function getSettingsErrorMessage(error: unknown, fallback: string): string {
  const message =
    typeof error === "string"
      ? error.trim()
      : error instanceof Error
        ? error.message.trim()
        : "";

  if (message && hasCjkCharacters(message)) {
    return message;
  }

  return fallback;
}
```

- [ ] **Step 2: 更新 8 个 settings tab 的 import**

在下列 8 个文件中将 `import { getSettingsErrorMessage } from "./settings-error"` 改为 `import { getSettingsErrorMessage } from "@/shared/settings-error"`:

- `apps/web/app/(dashboard)/settings/_components/account-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/members-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/policy-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/repositories-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/runtime-integrations-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/secrets-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/tokens-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/workspace-tab.tsx`

- [ ] **Step 3: 删除旧文件**

```bash
rm apps/web/app/\(dashboard\)/settings/_components/settings-error.ts
```

- [ ] **Step 4: typecheck 验证**

运行：`pnpm typecheck`

期望：PASS，无 TS2307（cannot find module）错误。

若有失败，检查是否遗漏某个 settings tab 的 import。

- [ ] **Step 5: Commit**

```bash
git add apps/web/shared/settings-error.ts apps/web/app/\(dashboard\)/settings/_components/
git commit -m "$(cat <<'EOF'
refactor(web): move settings-error util to shared/

Used by 8 settings tab files plus the soon-to-be-shared MembersTab;
shared/ is the correct home for cross-route utilities.
EOF
)"
```

---

## Task 2: 迁移 `MembersTab` 到 `features/workspace/components/`

**Files:**
- Create: `apps/web/features/workspace/components/members-tab.tsx`
- Delete: `apps/web/app/(dashboard)/settings/_components/members-tab.tsx`
- Modify: `apps/web/features/workspace/index.ts`
- Modify: `apps/web/app/(dashboard)/settings/page.tsx`

把组件挪到符合项目架构的位置，然后两路由都从这里引用。

- [ ] **Step 1: 创建新文件 `apps/web/features/workspace/components/members-tab.tsx`**

完整内容（与原文件一致，仅改 settings-error 的 import 路径已在 Task 1 完成的位置上 — 这里直接写新路径）：

```tsx
"use client";

import { useState } from "react";
import { Crown, Shield, User, Plus, MoreHorizontal, UserMinus, Users } from "lucide-react";
import { ActorAvatar } from "@/components/common/actor-avatar";
import type { MemberWithUser, MemberRole } from "@/shared/types";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@/components/ui/alert-dialog";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from "@/components/ui/dropdown-menu";
import { toast } from "sonner";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceManagement } from "../hooks";
import { getSettingsErrorMessage } from "@/shared/settings-error";

const roleConfig: Record<MemberRole, { label: string; icon: typeof Crown; description: string }> = {
  owner: { label: "所有者", icon: Crown, description: "完全访问权限，管理所有设置" },
  admin: { label: "管理员", icon: Shield, description: "管理成员和设置" },
  member: { label: "成员", icon: User, description: "创建和处理任务" },
};

function MemberRow({
  member,
  canManage,
  canManageOwners,
  isSelf,
  busy,
  onRoleChange,
  onRemove,
}: {
  member: MemberWithUser;
  canManage: boolean;
  canManageOwners: boolean;
  isSelf: boolean;
  busy: boolean;
  onRoleChange: (role: MemberRole) => void;
  onRemove: () => void;
}) {
  const rc = roleConfig[member.role];
  const RoleIcon = rc.icon;
  const canEditRole = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const canRemove = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const showMenu = canEditRole || canRemove;

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <ActorAvatar actorType="member" actorId={member.user_id} size={32} />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium truncate">{member.name}</div>
        <div className="text-xs text-muted-foreground truncate">{member.email}</div>
      </div>
      {showMenu && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="icon-sm" disabled={busy}>
                <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-auto">
            {canEditRole && (
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <Shield className="h-3.5 w-3.5" />
                  更改角色
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-auto">
                  {(Object.entries(roleConfig) as [MemberRole, (typeof roleConfig)[MemberRole]][]).map(
                    ([role, config]) => {
                      if (role === "owner" && !canManageOwners) return null;
                      const Icon = config.icon;
                      return (
                        <DropdownMenuItem
                          key={role}
                          onClick={() => onRoleChange(role)}
                        >
                          <Icon className="h-3.5 w-3.5" />
                          <div className="flex flex-col">
                            <span>{config.label}</span>
                            <span className="text-xs text-muted-foreground font-normal">
                              {config.description}
                            </span>
                          </div>
                          {member.role === role && (
                            <span className="ml-auto text-xs text-muted-foreground">&#10003;</span>
                          )}
                        </DropdownMenuItem>
                      );
                    }
                  )}
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            )}
            {canEditRole && canRemove && <DropdownMenuSeparator />}
            {canRemove && (
              <DropdownMenuItem variant="destructive" onClick={onRemove}>
                <UserMinus className="h-3.5 w-3.5" />
                从工作区移除
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      <Badge variant="secondary">
        <RoleIcon className="h-3 w-3" />
        {rc.label}
      </Badge>
    </div>
  );
}

export function MembersTab() {
  const user = useAuthStore((s) => s.user);
  const {
    workspace,
    members,
    canManageWorkspace,
    isOwner,
    inviteMember,
    changeMemberRole,
    removeMember,
  } = useWorkspaceManagement();

  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<MemberRole>("member");
  const [inviteLoading, setInviteLoading] = useState(false);
  const [memberActionId, setMemberActionId] = useState<string | null>(null);
  const [confirmAction, setConfirmAction] = useState<{
    title: string;
    description: string;
    variant?: "destructive";
    onConfirm: () => Promise<void>;
  } | null>(null);

  const handleAddMember = async () => {
    if (!workspace) return;
    setInviteLoading(true);
    try {
      await inviteMember({
        email: inviteEmail,
        role: inviteRole,
      });
      setInviteEmail("");
      setInviteRole("member");
      toast.success("已添加成员");
    } catch (e) {
      toast.error(getSettingsErrorMessage(e, "添加成员失败"));
    } finally {
      setInviteLoading(false);
    }
  };

  const handleRoleChange = async (memberId: string, role: MemberRole) => {
    if (!workspace) return;
    setMemberActionId(memberId);
    try {
      await changeMemberRole(memberId, role);
      toast.success("角色已更新");
    } catch (e) {
      toast.error(getSettingsErrorMessage(e, "更新成员失败"));
    } finally {
      setMemberActionId(null);
    }
  };

  const handleRemoveMember = (member: MemberWithUser) => {
    if (!workspace) return;
    setConfirmAction({
      title: `移除 ${member.name}`,
      description: `从 ${workspace.name} 中移除 ${member.name}？该成员将失去对此工作区的访问权限。`,
      variant: "destructive",
      onConfirm: async () => {
        setMemberActionId(member.id);
        try {
          await removeMember(member.id);
          toast.success("成员已移除");
        } catch (e) {
          toast.error(getSettingsErrorMessage(e, "移除成员失败"));
        } finally {
          setMemberActionId(null);
        }
      },
    });
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Users className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">成员 ({members.length})</h2>
        </div>

        {canManageWorkspace && (
          <Card>
            <CardContent className="space-y-3">
              <div className="flex items-center gap-2">
                <Plus className="h-4 w-4 text-muted-foreground" />
                <h3 className="text-sm font-medium">添加成员</h3>
              </div>
              <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
                <Input
                  type="email"
                  value={inviteEmail}
                  onChange={(e) => setInviteEmail(e.target.value)}
                  placeholder="user@company.com"
                />
                <Select value={inviteRole} onValueChange={(value) => setInviteRole(value as MemberRole)}>
                  <SelectTrigger size="sm"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="member">成员</SelectItem>
                    <SelectItem value="admin">管理员</SelectItem>
                    {isOwner && <SelectItem value="owner">所有者</SelectItem>}
                  </SelectContent>
                </Select>
                <Button
                  onClick={handleAddMember}
                  disabled={inviteLoading || !inviteEmail.trim()}
                >
                  {inviteLoading ? "添加中..." : "添加"}
                </Button>
              </div>
            </CardContent>
          </Card>
        )}

        {members.length > 0 ? (
          <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
            {members.map((m, i) => (
              <div key={m.id} className={i > 0 ? "border-t border-border/50" : ""}>
                <MemberRow
                  member={m}
                  canManage={canManageWorkspace}
                  canManageOwners={isOwner}
                  isSelf={m.user_id === user?.id}
                  busy={memberActionId === m.id}
                  onRoleChange={(role) => handleRoleChange(m.id, role)}
                  onRemove={() => handleRemoveMember(m)}
                />
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">暂无成员。</p>
        )}
      </section>

      <AlertDialog open={!!confirmAction} onOpenChange={(v) => { if (!v) setConfirmAction(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmAction?.title}</AlertDialogTitle>
            <AlertDialogDescription>{confirmAction?.description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              variant={confirmAction?.variant === "destructive" ? "destructive" : "default"}
              onClick={async () => {
                await confirmAction?.onConfirm();
                setConfirmAction(null);
              }}
            >
              确认
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
```

- [ ] **Step 2: 更新 `apps/web/features/workspace/index.ts` 新增 export**

原内容：

```ts
export { useWorkspaceStore } from "./store";
export { useActorName, useWorkspaceManagement } from "./hooks";
export { WorkspaceAvatar } from "./components/workspace-avatar";
```

改为：

```ts
export { useWorkspaceStore } from "./store";
export { useActorName, useWorkspaceManagement } from "./hooks";
export { WorkspaceAvatar } from "./components/workspace-avatar";
export { MembersTab } from "./components/members-tab";
```

- [ ] **Step 3: 更新 `apps/web/app/(dashboard)/settings/page.tsx` 的 import**

第 10 行原内容：

```ts
import { MembersTab } from "./_components/members-tab";
```

改为：

```ts
import { MembersTab } from "@/features/workspace";
```

- [ ] **Step 4: 删除旧文件**

```bash
rm apps/web/app/\(dashboard\)/settings/_components/members-tab.tsx
```

- [ ] **Step 5: typecheck 验证**

运行：`pnpm typecheck`

期望：PASS。

若失败常见原因：
- `features/workspace/index.ts` export 漏 `MembersTab`
- settings page 仍用旧路径

- [ ] **Step 6: unit tests 验证（既有测试不应受影响）**

运行：`pnpm --filter /web exec vitest run features/workspace/hooks.test.ts`

期望：PASS，未改动的 `inviteMember` 测试仍通过。

- [ ] **Step 7: Commit**

```bash
git add apps/web/features/workspace/components/members-tab.tsx \
        apps/web/features/workspace/index.ts \
        apps/web/app/\(dashboard\)/settings/page.tsx \
        apps/web/app/\(dashboard\)/settings/_components/
git commit -m "$(cat <<'EOF'
refactor(web): move MembersTab to features/workspace

Position the component in its feature-based home so both the settings
and account routes can import it without crossing a _components
route-private boundary.
EOF
)"
```

---

## Task 3: 账户页替换 `hierarchy` tab 为 `members` tab

**Files:**
- Modify: `apps/web/app/(dashboard)/account/page.tsx`

删掉 bug 的 hierarchy 路径（当前 render OverviewTab 副本），改放 MembersTab。

- [ ] **Step 1: 修改 icon import**

第 10-14 行原内容：

```tsx
import {
  Bot, Terminal, Code, Key, ChevronDown, ChevronRight,
  Copy, Check, Plus, Zap, Circle, Shield, Wrench,
  Globe, User, GitBranch, Settings, Sparkles, Layers
} from "lucide-react"
```

改为（删 `GitBranch`，新增 `Users`）：

```tsx
import {
  Bot, Terminal, Code, Key, ChevronDown, ChevronRight,
  Copy, Check, Plus, Zap, Circle, Shield, Wrench,
  Globe, User, Users, Settings, Sparkles, Layers
} from "lucide-react"
```

注：`OverviewTab` 内也使用了 `GitBranch`,需保留？Check：

```bash
grep -n "GitBranch" apps/web/app/\(dashboard\)/account/page.tsx
```

如只剩在 `orgTabs` 中出现（即将被替换掉），则可安全删除 import。如 `OverviewTab` 层级 section 也用到，则保留 `GitBranch`,仅新增 `Users`。

**安全写法：保留 `GitBranch`,仅新增 `Users`:**

```tsx
import {
  Bot, Terminal, Code, Key, ChevronDown, ChevronRight,
  Copy, Check, Plus, Zap, Circle, Shield, Wrench,
  Globe, User, Users, GitBranch, Settings, Sparkles, Layers
} from "lucide-react"
```

typecheck 会提示 `GitBranch` 未使用时再删。

- [ ] **Step 2: 新增 `MembersTab` import**

在第 18 行 `import { SubagentsPage } from "@/features/subagents"` 之后加一行：

```tsx
import { MembersTab } from "@/features/workspace"
```

最终 imports 段（第 15-19 行）：

```tsx
import { MetricsOverview } from "@/features/workspace/components/metrics-overview"
import { AgentAutoReplyConfig } from "@/features/workspace/components/agent-auto-reply-config"
import { AgentProfileEditor } from "@/features/workspace/components/agent-profile-editor"
import { SubagentsPage } from "@/features/subagents"
import { MembersTab } from "@/features/workspace"
```

注：已有 `useWorkspaceStore` 从 `@/features/workspace` 的 import 在第 4 行；合并与否风格自由，本步不做合并。

- [ ] **Step 3: 修改 `orgTabs` 定义**

第 529-531 行原内容：

```tsx
const orgTabs = [
  { value: "hierarchy", label: "组织层级", icon: GitBranch },
]
```

改为：

```tsx
const orgTabs = [
  { value: "members", label: "成员", icon: Users },
]
```

- [ ] **Step 4: 修改 `TabsContent` 区域**

第 611-614 行原内容：

```tsx
<TabsContent value="overview"><OverviewTab /></TabsContent>
<TabsContent value="agents"><AgentListTab /></TabsContent>
<TabsContent value="add-agent"><AddAgentTab /></TabsContent>
<TabsContent value="hierarchy"><OverviewTab /></TabsContent>
```

改为：

```tsx
<TabsContent value="overview"><OverviewTab /></TabsContent>
<TabsContent value="agents"><AgentListTab /></TabsContent>
<TabsContent value="add-agent"><AddAgentTab /></TabsContent>
<TabsContent value="members"><MembersTab /></TabsContent>
```

- [ ] **Step 5: 清理未用 `GitBranch` import（若存在）**

运行：

```bash
grep -n "GitBranch" apps/web/app/\(dashboard\)/account/page.tsx
```

若仅剩 import 行本身（说明 `OverviewTab` 不再使用，通常 `OverviewTab` 内仍用），保留 import。若完全未用则从 import 列表中删除 `GitBranch`。由 typecheck 报 TS6133 (declared but never read) 驱动决定。

- [ ] **Step 6: typecheck 验证**

运行：`pnpm typecheck`

期望：PASS。若报 `GitBranch` 未使用，回到 Step 5 删除。

- [ ] **Step 7: lint 验证**

运行：`pnpm lint`

期望：PASS，无 unused import warning。

- [ ] **Step 8: 手测（启动 dev server）**

启动：`make start`（或 `pnpm dev:web` + `make dev`）。

用 owner 账号登录，访问：

1. `http://localhost:3000/account` — 侧栏「组织」组显示「成员」tab（Users 图标）
2. 点击「成员」,右侧应显示：
   - 邀请卡片（email input + role select + 添加按钮）
   - 现有成员列表 + 角色 badge + 三点菜单
3. `http://localhost:3000/settings` tab=members 仍显示同一 UI（回归检查）
4. 输入 `foo@bar.com` + role=member + 点「添加」,若该 email 未注册应看到错误 toast,若已注册应看到「已添加成员」toast 且列表刷新
5. 访问 `http://localhost:3000/account?tab=hierarchy`(旧 URL)— 应 fallback 回默认 overview tab(URL 参数无匹配时 `currentTab || "overview"` 生效)

注：账户页 `AccountPageBody` 第 554 行 `const currentTab = search.get("tab") || "overview"` — 不匹配的 tab 值传给 `Tabs value`,shadcn Tabs 对未匹配 value 会无激活 tab。实际体验可能是显示空白。若需更友好,可在 commit 后跟进加 tab 白名单校验,但**本 plan 不做**(非回归；原 `hierarchy` 被删后,用户要么重新点 tab,要么看到默认/空白)。

- [ ] **Step 9: Commit**

```bash
git add apps/web/app/\(dashboard\)/account/page.tsx
git commit -m "$(cat <<'EOF'
feat(account): replace buggy hierarchy tab with members tab

Hierarchy previously rendered an OverviewTab duplicate; replace with
the shared MembersTab so org owners can invite users and manage
membership directly from /account.
EOF
)"
```

---

## Final Verification

- [ ] **Step 1: 全量 typecheck**

```bash
pnpm typecheck
```

期望：PASS。

- [ ] **Step 2: 全量 unit test**

```bash
pnpm test
```

期望：PASS,`features/workspace/hooks.test.ts` 中 `inviteMember` 测试通过。

- [ ] **Step 3: git log 检查**

```bash
git log --oneline main..HEAD
```

期望看到 4 个原子 commit（Task 2 下方的 refactor fix 用于避免 MembersTab 内部对自身 barrel 的循环 import）：
1. `refactor(web): move settings-error util to shared/`
2. `refactor(web): move MembersTab to features/workspace`
3. `refactor(workspace): use relative import for useWorkspaceManagement in MembersTab`
4. `feat(account): replace buggy hierarchy tab with members tab`

---

## Notes

- **不新增测试**：本 plan 仅迁移文件 + 改 import + 做一处 tab 替换;`hooks.test.ts` 已覆盖底层 `inviteMember`,`MembersTab` UI 无单元测试历史,保持现状。
- **不改 inviteMember 语义**：当前实现是「直接添加已注册用户」,非 email 邀请链接流程。若后续需实现后者(pending invitation / email 链接),属独立 feature,另起 spec。
- **不改 OverviewTab**：组织层级 section 已在 overview 中展示,不重复。
- **不加 `/account?tab=hierarchy` 的 redirect**：hierarchy 是 bug 状态,无产品依赖,直接删除。
