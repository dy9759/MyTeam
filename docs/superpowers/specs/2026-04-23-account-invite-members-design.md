# 账户页邀请成员功能 — 设计

## 目标

在账户页（`/account`）增加邀请用户加入组织的能力。复用 `/settings` 已有的成员管理 UI，避免重复实现。

## 决策摘要

| 决策 | 选择 | 理由 |
|---|---|---|
| UI 形态 | 复用 settings 的 `MembersTab`（完整列表 + 邀请表单 + 角色管理） | 已有组件，零新增逻辑 |
| settings 旧入口 | 两处并存 | 组件共享，避免破坏已有习惯 |
| 账户页接入方式 | 替换现有 `hierarchy` tab | `hierarchy` 当前渲染 `OverviewTab` 副本，是 bug；overview 本身已含层级信息 |
| 组件位置 | 移至 `features/workspace/components/` | 符合项目 feature-based 架构；跨 feature 共享代码归属 feature |

## 架构变更

### 文件移动

- `apps/web/app/(dashboard)/settings/_components/members-tab.tsx` → `apps/web/features/workspace/components/members-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/settings-error.ts` → `apps/web/shared/settings-error.ts`（被 9 个文件共用：8 个 settings tab + MembersTab，挪到 shared 是正确位置）

### settings-error.ts import 更新

以下 9 个文件需改 import 路径为 `@/shared/settings-error`：

- `apps/web/app/(dashboard)/settings/_components/workspace-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/tokens-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/secrets-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/runtime-integrations-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/repositories-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/policy-tab.tsx`
- `apps/web/app/(dashboard)/settings/_components/account-tab.tsx`
- `apps/web/features/workspace/components/members-tab.tsx`（迁移后的新位置）
- （原 settings 下的 members-tab.tsx 文件删除）

### 导出

`apps/web/features/workspace/index.ts` 新增：

```ts
export { MembersTab } from "./components/members-tab";
```

### MembersTab import 更新

- `apps/web/app/(dashboard)/settings/page.tsx`（或其 tabs 容器）— 改为 `import { MembersTab } from "@/features/workspace"`
- `apps/web/app/(dashboard)/account/page.tsx` — 新增 `import { MembersTab } from "@/features/workspace"`

### 账户页 tab 定义改动

`apps/web/app/(dashboard)/account/page.tsx`：

```tsx
// 新增 icon
import { Users } from "lucide-react";
import { MembersTab } from "@/features/workspace";

// 替换
const orgTabs = [
  { value: "members", label: "成员", icon: Users },
];

// TabsContent 区域：删 value="hierarchy"，加：
<TabsContent value="members"><MembersTab /></TabsContent>
```

> 同时删除 `GitBranch` icon 的 import（若除 OverviewTab 外无其他引用）。

## 权限

`MembersTab` 内部通过 `useWorkspaceManagement()` 读 `canManageWorkspace` / `isOwner`：

- owner / admin：见邀请表单 + 成员列表 + 角色管理 + 移除
- member：只见成员列表

账户页无需额外 guard。

## 邀请行为（沿用现有）

- 输入 email + 选择 role（member / admin / owner 仅 owner 可选）
- 点「添加」→ 调 `api.createMember(workspaceId, { email, role })`
- 用户需已注册系统（当前非 email 邀请流程，是直接加入）
- 成功 toast「已添加成员」，失败通过 `getSettingsErrorMessage` 展示

## 测试

- **不新增测试**：`apps/web/features/workspace/hooks.test.ts` 已覆盖 `inviteMember` 逻辑；MembersTab 无独立测试，迁移后行为不变
- **typecheck** 验证 import 路径正确
- **手测**：以 owner 账户访问 `/account?tab=members`，应见邀请卡片 + 成员列表；非 owner 不见卡片

## 风险与取舍

- **旧 URL `/account?tab=hierarchy` 失效** — hierarchy 本就是 bug，无产品依赖，直接删除不加 redirect
- **跨路由 import 漏改** — typecheck 捕获
- **`_components` 下仍有其他组件** — 只动 members-tab + settings-error，不影响同目录其他文件

## 非目标

- 不实现 email 邀请链接 / pending invitation 流程（超出当前 API 能力）
- 不改 `inviteMember` hook 逻辑
- 不改 settings 页布局，仅切换 import 路径
- 不动 `OverviewTab` 内的层级 section
