# Account 层重构 PRD

> Version: 1.0 | Date: 2026-04-16

---

## 1. 背景与目标

### 1.1 现状问题

| # | 问题 | 说明 |
|---|------|------|
| 1 | Agent 表膨胀 | 45+ 列，身份、执行配置、状态、LLM 配置、UI 元数据全部塞在一张表 |
| 2 | agent_type 三分类冗余 | `personal_agent` / `system_agent` / `page_system_agent` 三种，但 system 和 page_system 本质都是系统级 Agent，仅 scope 不同 |
| 3 | 无独立 Provider 表 | Provider 只是 `agent_runtime.provider` 的一个字符串字段，无法管理 provider 元信息 |
| 4 | Runtime 字段不足 | 缺少 concurrency_limit、load、lease、owner_scope 等调度必需字段 |
| 5 | Impersonation 审计不够细 | 消息只有 `is_impersonated` 布尔值，无法区分 effective_actor 和 real_operator |

### 1.2 重构目标

1. **Organization → Owner → Agent** 层级清晰化
2. **合并 system_agent / page_system_agent** 为统一的 `system_agent`（通过 scope 区分）
3. **Provider / Runtime / Agent 三层分离**，各司其职
4. **Identity Card** 正式建模
5. **Impersonation** 保留并增强审计

### 1.3 术语澄清：三种 "Owner"

本文档和 Project PRD 中 "Owner" 出现在三种语境，含义不同：

| 术语 | 含义 | 标识方式 |
|------|------|---------|
| **Workspace Role: owner** | workspace 的最高权限角色 | `member.role = 'owner'` |
| **Agent Owner** | Agent 的所属用户，可以是任何 workspace 角色 | `agent.owner_id` FK → `user.id` |
| **Project Creator** | 创建项目的用户 | `project.creator_owner_id` FK → `user.id` |

**关键区分：**
- 一个 `member.role = 'member'` 的用户，可以是自己 Agent 的 Agent Owner
- Plan 审批人 = Project Creator（不要求是 workspace owner 角色）
- RBAC 中的 "owner" 列指 workspace role = owner；"自己的 Agent" 指 Agent Owner 关系

> 在行文中，大写 **Owner** 表示 Agent Owner（agent.owner_id 的所有者），小写 **owner** 表示 workspace 角色。需要特指 workspace 角色时使用 "workspace owner"。

---

## 2. 身份层级与隶属关系

### 2.1 层级结构

```
Organization (workspace)
  │
  ├── admin (member.role = 'owner' | 'admin')
  │     │
  │     └── 管理 System Agent (隶属于 Organization，admin 管理)
  │           │
  │           ├── scope = NULL (全局编排者，每 workspace 1 个)
  │           │     - 始终运行在云端
  │           │     - 生成计划、分配任务、调解消息
  │           │     - 不直接执行任务
  │           │
  │           └── scope = 'account' | 'conversation' | 'project' | 'file'
  │                 - 始终运行在云端
  │                 - scope 限定功能域，不绑定具体实体
  │                 - 每 workspace 每 scope 一个
  │
  └── user (member.role = 'owner' | 'admin' | 'member')
        │
        └── Personal Agent (0..N, 隶属于 user)
              │
              ├── 云端 Personal Agent (runtime.mode = 'cloud')
              │     - 通过云端 LLM API 执行
              │     - 拥有 project 级别数据访问权限
              │     - 通过 MCP/API 工具访问平台数据
              │
              └── 本地 Personal Agent (runtime.mode = 'local')
                    - 通过本地 Daemon + CLI 执行
                    - 直接访问本地 repo、文件、工具
                    - 通过 MCP/CLI 访问平台数据
                    - 执行结果（Artifact）上传回云端
```

### 2.2 隶属关系

| Agent 类型 | 隶属于 | owner_id | owner_type | 运行位置 |
|-----------|--------|----------|------------|---------|
| System Agent | Organization | NULL | `organization` | 始终云端 |
| 云端 Personal Agent | User | user.id (必填) | `user` | 云端 |
| 本地 Personal Agent | User | user.id (必填) | `user` | 本地 |

> **修正：** System Agent 的 `owner_type = 'organization'`（属于组织），不再是 `owner_id = NULL`（含义模糊）。admin 是管理者，不是所有者。admin 离开时 System Agent 不受影响。

**Agent 表新增字段：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `owner_type` | TEXT | `user` \| `organization` |

```sql
CHECK (
  (agent_type = 'personal_agent' AND owner_type = 'user' AND owner_id IS NOT NULL)
  OR
  (agent_type = 'system_agent' AND owner_type = 'organization' AND owner_id IS NULL)
)
```

### 2.3 关键约束

| 约束 | 说明 |
|------|------|
| Personal Agent 隶属 user | `owner_type = 'user'`，`owner_id` 必填 |
| System Agent 隶属 organization | `owner_type = 'organization'`，`owner_id = NULL`，由 admin 管理 |
| System Agent 始终云端 | System Agent 只绑定 `mode = 'cloud'` 的 Runtime |
| Personal Agent 区分云端/本地 | 通过绑定的 Runtime.mode 决定，可重绑 |
| System Agent 不可 impersonate | Impersonation 仅限 user 对自己的 Personal Agent |
| Agent 不能跨 user 分发 | User A 不能调度 User B 的 Agent（MVP） |
| System Agent (scope=project) 自动加入项目 Channel | 创建 Project Channel 时自动添加 project scope 的 System Agent 为成员 |
| System Agent 数据访问不依赖 Channel 成员 | System Agent 有 admin 级数据访问权限，Channel 成员身份仅用于接收 WebSocket 实时消息 |

---

## 3. 两层分离：Runtime / Agent

### 3.1 总览

```
Runtime (动态执行端点)                    Agent (协作身份)
┌──────────────────────────────┐     ┌─────────────────────────┐
│ workspace_id                 │     │ name: "前端助手"          │
│ provider (TEXT): "claude"    │     │ identity_card (JSONB)    │
│ daemon_id                    │     │ runtime_id → Runtime    │
│ mode: local / cloud          │     │ owner_id → User         │
│ status: online/offline       │     │ agent_type              │
│ concurrency_limit            │     │ instructions            │
│ current_load                 │     │ status (7 态)            │
│ lease_expires_at             │     └─────────────────────────┘
│ device_info                  │
└──────────────────────────────┘
             1 ────── N
        一个 Runtime 可被
        多个 Agent 绑定
```

**分离原则：**

| 层 | 性质 | 回答的问题 |
|----|------|----------|
| Runtime | 动态，workspace 级 | 现在哪些执行端点在线？能力如何？用的什么 provider？ |
| Agent | 身份，workspace 级 | 这个协作者叫什么？会什么？谁拥有？ |

**分离带来的好处：**
- Runtime 可以下线而 Agent 身份不受影响
- 多个 Agent 可以共享同一个 Runtime
- 一个 Agent 可以重绑到不同的 Runtime

### 3.2 Provider（代码注册表，非数据库表）

Provider 信息保留在代码中，不建数据库表。原因：添加新 provider 必须伴随代码变更（实现新 Backend + Daemon 探测逻辑），数据库行无法独立启用一个新 provider。

```go
// pkg/provider/registry.go

type ProviderSpec struct {
    Key             string       // "claude", "codex", "opencode", "cloud_llm"
    DisplayName     string       // "Claude Code", "Codex", ...
    Kind            ProviderKind // LocalCLI, CloudAPI
    Executable      string       // CLI 可执行文件名（云端为空）
    SupportedModels []string
    DefaultModel    string
    Capabilities    []string
}

var Registry = map[string]ProviderSpec{
    "claude":    {Key: "claude", DisplayName: "Claude Code", Kind: LocalCLI, Executable: "claude", ...},
    "codex":     {Key: "codex", DisplayName: "Codex", Kind: LocalCLI, Executable: "codex", ...},
    "opencode":  {Key: "opencode", DisplayName: "OpenCode", Kind: LocalCLI, Executable: "opencode", ...},
    "cloud_llm": {Key: "cloud_llm", DisplayName: "Cloud LLM", Kind: CloudAPI, ...},
}
```

**用途：**

| 场景 | 使用方式 |
|------|---------|
| Daemon 启动探测 | 遍历 Registry，检查 `which <Executable>` |
| Runtime 注册校验 | 验证 `runtime.provider` 是否在 Registry 中 |
| API 查询 | `GET /api/providers` 直接返回 Registry 数据 |
| UI 展示 | 前端通过 API 获取 provider 列表和显示名 |
| 模型验证 | 校验请求的模型是否在 SupportedModels 中 |

**何时升级为数据库表：** 当 provider 可以不改代码就添加/安装/启用时（插件化、通用协议、marketplace）。

### 3.3 Runtime 表（重构 agent_runtime）

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID PK | |
| `workspace_id` | UUID FK → workspace | |
| `provider` | TEXT NOT NULL | provider 标识：`claude` \| `codex` \| `opencode` \| `cloud_llm` |
| `daemon_id` | TEXT | 本地 daemon 标识 |
| `name` | TEXT | 运行时名称，如 "hunter-macbook-claude" |
| `mode` | TEXT | `local` \| `cloud` |
| `status` | TEXT | `online` \| `offline` \| `degraded` |
| `device_info` | TEXT | 设备信息 |
| `server_host` | TEXT | 服务器地址 |
| `working_dir` | TEXT | 工作目录；**local 模式下作为 `execution.context_ref.working_dir` 的默认来源**（见 Project PRD §4.7） |
| `capabilities` | JSONB | 运行时能力声明 |
| `concurrency_limit` | INTEGER DEFAULT 1 | 并发执行上限 |
| `current_load` | INTEGER DEFAULT 0 | 当前负载（活跃任务数） |
| `lease_expires_at` | TIMESTAMPTZ | Lease 过期时间 |
| `last_heartbeat_at` | TIMESTAMPTZ | 最近心跳 |
| `metadata` | JSONB | 扩展元数据（含云端 LLM 配置） |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

> `provider` 保持 TEXT 字段（与现有 agent_runtime.provider 同类型），由应用层校验是否在代码 Registry 中。

**与现有 agent_runtime 的映射：**

| 现有字段 | 新字段 | 变化 |
|---------|--------|------|
| `provider` (TEXT) | `provider` (TEXT) | **不变** |
| `runtime_mode` | `mode` | 改名 |
| `status` ('online'/'offline') | `status` + 3 值 | 增加 `degraded` |
| 无 | `concurrency_limit` | 新增 |
| 无 | `current_load` | 新增 |
| 无 | `lease_expires_at` | 新增 |
| `last_seen_at` | `last_heartbeat_at` | 改名，语义更精确 |
| `readiness` | 删除 | 被 `status` 覆盖 |

### 3.4 Agent 表（瘦身重构）

当前 Agent 表 45+ 列。重构目标是将执行配置剥离到 Provider/Runtime，Agent 只保留身份和协作相关字段。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID PK | |
| `workspace_id` | UUID FK → workspace | |
| `owner_id` | UUID FK → user | Personal Agent 必填，System Agent 为 NULL |
| `owner_type` | TEXT | `user` \| `organization` |
| `runtime_id` | UUID FK → runtime | 当前绑定的 Runtime |
| **身份字段** | | |
| `name` | TEXT | Agent 名称 |
| `display_name` | TEXT | 显示名 |
| `avatar_url` | TEXT | 头像 |
| `description` | TEXT | 简介 |
| `bio` | TEXT | 个人说明 |
| `instructions` | TEXT | Agent 指令 |
| `agent_type` | TEXT | `personal_agent` \| `system_agent` |
| `scope` | TEXT | System Agent 的作用域。NULL=全局，`account`/`session`/`project`/`file` |
| **Identity Card** | | |
| `identity_card` | JSONB | 结构化身份档案（见 4.1） |
| **状态字段** | | |
| `status` | TEXT | Agent 状态（合并原 online_status + workload_status） |
| `last_active_at` | TIMESTAMPTZ | 最近活跃时间 |
| `needs_attention` | BOOLEAN DEFAULT FALSE | 是否需要关注 |
| `needs_attention_reason` | TEXT | 关注原因 |
| **行为配置** | | |
| `auto_reply_enabled` | BOOLEAN DEFAULT FALSE | 是否开启自动回复 |
| `auto_reply_config` | JSONB | 自动回复配置 |
| `max_concurrent_tasks` | INTEGER DEFAULT 1 | 最大并发任务 |
| `visibility` | TEXT | `private` \| `workspace` |
| **元数据** | | |
| `tags` | TEXT[] | 标签 |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |
| `archived_at` | TIMESTAMPTZ | 软删除 |
| `archived_by` | UUID | |

**从 Agent 表移除的字段：**

| 移除字段 | 原因 | 迁移去向 |
|---------|------|---------|
| `runtime_mode` | 冗余，Runtime 表已有 `mode` | 通过 `runtime_id` 查 Runtime |
| `runtime_config` | 冗余，Runtime 表已有配置 | 同上 |
| `cloud_llm_config` | 应属于 Runtime/Provider | 移入 Runtime.metadata 或 Provider |
| `capabilities` (TEXT[]) | 与 identity_card.capabilities 重复 | 统一到 identity_card |
| `tools` (JSONB) | 与 identity_card.tools 重复 | 统一到 identity_card |
| `triggers` (JSONB) | 行为配置，可精简 | 保留在 auto_reply_config 中 |
| `is_system` (BOOLEAN) | 被 agent_type = 'system_agent' 替代 | 删除 |
| `system_config` (JSONB) | System Agent 配置 | 移入 instructions 或独立配置 |
| `page_scope` | 改名为 `scope` | 字段保留，改名 |
| `agent_metadata` (JSONB) | 与 identity_card 重复 | 统一到 identity_card |
| `accessible_files_scope` | 权限配置 | 未来移入独立权限表 |
| `allowed_channels_scope` | 权限配置 | 同上 |

**Agent 状态字段（合并为单一 `status`）：**

> 现有实现拆分为 `online_status`（7 值）和 `workload_status`（5 值），但两者共享 5 个值（idle/busy/blocked/degraded/suspended），语义不清。重构为单一 `status` 字段。

| 状态 | 说明 | 可接收任务 |
|------|------|----------|
| `offline` | 进程未运行 / 无心跳 | 否 |
| `online` | 已注册但尚未就绪 | 否 |
| `idle` | 准备好接受任务 | 是 |
| `busy` | 正在执行任务（并发上限内可能仍可接收） | 有条件 |
| `blocked` | 卡住：超时/依赖未满足/工具故障 | 否 |
| `degraded` | 能力降级 | 有条件 |
| `suspended` | 被 Owner 或 admin 手动暂停 | 否 |

**调度可用性判断（Agent + Runtime 组合）：**

Agent 状态与 Runtime 状态解耦。调度器需同时检查两者：

| Agent.status | Runtime.status | 可调度 | UI 展示 |
|-------------|---------------|--------|---------|
| idle | online | 是 | 空闲 |
| idle | offline | 否 | 空闲 (runtime 离线) |
| idle | degraded | 有条件（仅 low/medium 风险任务） | 空闲 (runtime 降级) |
| busy | online | 看并发 | 忙碌 |
| suspended | any | 否 | 已暂停 |
| offline | any | 否 | 离线 |

**scope 约束：**

```sql
CHECK (
  (agent_type = 'personal_agent' AND scope IS NULL)
  OR
  (agent_type = 'system_agent')
)
```

> Personal Agent 的 scope 必须为 NULL。scope 仅对 System Agent 有意义。

---

## 4. Identity Card

### 4.1 结构定义

```jsonc
{
  // Agent 能力声明
  "capabilities": ["code_generation", "code_review", "testing"],
  "tools": ["claude_code", "shell", "browser"],
  "skills": ["golang", "typescript", "sql", "react"],

  // 协作信息
  "subagents": [],
  "completed_projects": [
    { "project_id": "uuid", "title": "SaySo v1.0", "completed_at": "2026-04-10T..." }
  ],

  // 描述
  "description_auto": "基于任务历史自动生成的能力描述",
  "description_manual": "Owner 手动编辑的描述（覆盖 auto）",

  // 控制
  "pinned_fields": ["skills", "description_manual"]
}
```

### 4.2 生命周期

```
创建 Agent
  → identity_card = {} (空)
  → Owner 可手动编辑任意字段
  → 系统定时自动生成（最小间隔 6h）：
      分析任务历史、技能使用、项目完成记录
      → 更新 description_auto、capabilities、skills 等
      → pinned_fields 内的字段不会被自动覆盖
  → Agent 可提议修改（需 Owner 确认）
```

### 4.3 用途

| 场景 | 使用方式 |
|------|---------|
| Plan 生成 | PlanGeneratorService 读取 Agent 的 skills/capabilities 做任务分配 |
| 任务调度 | SchedulerService 匹配 task.required_skills 与 identity_card.skills |
| Account 页面 | 展示 Agent 档案、能力雷达图 |
| Fallback 选择 | 选择 fallback agent 时匹配能力 |

---

## 5. Impersonation

### 5.1 保留现有模型

```
impersonation_session 表（不变）
├── id, owner_id, agent_id, workspace_id
├── started_at, expires_at (30 分钟), ended_at
└── UNIQUE: 同一 Agent 同时只有一个活跃 session
```

### 5.2 增强：消息审计双字段

当前 `message.is_impersonated` 只是布尔值，无法回溯"谁代谁操作"。

**新增消息字段：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `effective_actor_id` | UUID | 消息看起来是谁发的（Agent ID） |
| `effective_actor_type` | TEXT | `member` \| `agent` |
| `real_operator_id` | UUID | 实际操作者（Owner 的 User ID） |
| `real_operator_type` | TEXT | `member`（Owner 始终是 member） |

**逻辑：**
- 非 impersonation 消息：`effective_actor = real_operator`（或 `real_operator` 为 NULL）
- Impersonation 消息：`effective_actor = Agent`，`real_operator = Owner`

**兼容：** `is_impersonated` 布尔值保留，作为快速查询字段。可通过 `real_operator_id IS NOT NULL AND real_operator_id != effective_actor_id` 推导。

### 5.3 约束

| 约束 | 说明 |
|------|------|
| 仅 Personal Agent | 不能 impersonate System Agent |
| 仅自己的 Agent | Owner 只能 impersonate 自己 owner_id 下的 Agent |
| 同时一个 | 一个 Owner 同时只能 impersonate 一个 Agent |
| 不可替代 agent_execution slot | Impersonation 不能代替 Agent 执行 Task 的 agent_execution slot |
| 可在项目 Channel/Thread 发消息 | Owner 可以 impersonate Agent 在项目频道和 Plan Thread 中发消息（聊天场景） |
| 审计记录 | 每次 impersonation 操作记录到 activity_log，消息携带 effective_actor + real_operator |

---

## 6. System Agent 合并

### 6.1 当前状态

```
现有 3 种 agent_type：
├── personal_agent    — Owner 的私有 Agent
├── system_agent      — 全局编排者（is_system=TRUE，每 workspace 1 个）
└── page_system_agent — 页面助手（page_scope: account/session/project/file）
```

### 6.2 合并后

```
合并为 2 种 agent_type：
├── personal_agent    — 不变
└── system_agent      — 统一，通过 scope 字段区分
      ├── scope = NULL     → 全局编排者（原 system_agent）
      ├── scope = 'account' → Account 页面助手
      ├── scope = 'session' → Session 页面助手
      ├── scope = 'project' → Project 页面助手
      └── scope = 'file'    → File 页面助手
```

**约束：**

| 约束 | SQL |
|------|-----|
| 全局 System Agent 唯一 | `UNIQUE (workspace_id) WHERE agent_type = 'system_agent' AND scope IS NULL` |
| 带 scope 的 System Agent 唯一 | `UNIQUE (workspace_id, scope) WHERE agent_type = 'system_agent' AND scope IS NOT NULL` |

### 6.3 迁移

```sql
-- 1. 将 page_system_agent 改为 system_agent
UPDATE agent
SET agent_type = 'system_agent',
    scope = page_scope
WHERE agent_type = 'page_system_agent';

-- 2. 将原 system_agent 的 scope 设为 NULL（全局）
UPDATE agent
SET scope = NULL
WHERE agent_type = 'system_agent' AND is_system = TRUE;

-- 3. 删除 is_system 列（被 agent_type 替代）
-- 4. 将 page_scope 改名为 scope
-- 5. 更新唯一约束
```

---

## 7. 实体关系

```
Organization (workspace) 1──N Owner (user via member)
Owner                    1──N Personal Agent
Organization             1──N System Agent (scope 区分)

Runtime     1──N Agent (通过 agent.runtime_id)
Runtime     N──1 Daemon (宿主进程)
Runtime.provider → 代码 ProviderRegistry（非 FK，TEXT 字段）

Owner       1──N ImpersonationSession
Agent       1──N ImpersonationSession

Agent.identity_card → JSONB (内嵌，非独立表)
```

---

## 8. RBAC 权限矩阵

### 8.1 角色层级

```
owner  ──  workspace 所有者，最高权限
admin  ──  管理员，可管理 Agent 和 Runtime
member ──  普通成员，受限操作
```

角色由 `member.role` 字段定义，作用于 workspace 级别。

### 8.2 Agent 操作权限

| 操作 | owner | admin | member | 说明 |
|------|:-----:|:-----:|:------:|------|
| 创建 Personal Agent | 自己的 | 自己的 | 自己的 | 每个角色只能为自己创建 |
| 查看 Personal Agent | 自己的 | 全部 | 自己的 | admin 可查看 workspace 内所有 Agent |
| 编辑 Personal Agent | 自己的 | 自己的 | 自己的 | 仅 Owner 可编辑自己的 Agent |
| 删除（归档）Personal Agent | 自己的 | 自己的 + 其他人的 | 自己的 | admin 可归档任意 Agent |
| Impersonate Agent | 自己的 | 自己的 | 自己的 | 仅限自己的 Personal Agent |
| 暂停 Agent (suspended) | 自己的 | 全部 | 自己的 | admin 可暂停任意 Agent |
| 恢复 Agent | 自己的 | 全部 | 自己的 | 与暂停对称 |

### 8.3 System Agent 操作权限

| 操作 | owner | admin | member | 说明 |
|------|:-----:|:-----:|:------:|------|
| 查看 System Agent | 是 | 是 | 是 | 所有成员可查看 |
| 创建 System Agent | 是 | 是 | 否 | 全局 System Agent 自动创建，scope Agent 由 admin+ 管理 |
| 编辑 instructions | 是 | 是 | 否 | |
| 编辑 auto_reply_config | 是 | 是 | 否 | |
| 归档 System Agent | 是 | 否 | 否 | 仅 owner，防止误删编排者 |

### 8.4 Identity Card 操作权限

| 操作 | owner | admin | member | 说明 |
|------|:-----:|:-----:|:------:|------|
| 查看自己 Agent 的 Card | 是 | 是 | 是 | |
| 查看他人 Agent 的 Card | 是 | 是 | 否 | member 只能看自己的 |
| 编辑 Card（手动） | 自己的 Agent | 自己的 + 其他人的 | 自己的 Agent | admin 可编辑任意 Agent 的 Card（与归档权限对齐） |
| 触发自动生成 | 自己的 Agent | 自己的 + 其他人的 | 自己的 Agent | 同上 |
| Pin/Unpin 字段 | 自己的 Agent | 自己的 Agent | 自己的 Agent | Pin 仅 Agent owner 操作 |

### 8.5 Runtime 操作权限

| 操作 | owner | admin | member | 说明 |
|------|:-----:|:-----:|:------:|------|
| 查看 Runtime 列表 | 是 | 是 | 是 | 所有成员可查看 workspace 内 Runtime |
| 注册 Runtime（Daemon） | 是 | 是 | 否 | Daemon 启动时自动注册，需 admin+ 的 PAT |
| 手动移除 Runtime | 是 | 是 | 否 | |
| 将 Agent 绑定到 Runtime | 自己的 Agent | 自己的 Agent | 自己的 Agent | 仅 Agent 的 owner |
| 查看 Runtime 负载/心跳 | 是 | 是 | 是 | |

### 8.6 Impersonation 审计权限

| 操作 | owner | admin | member | 说明 |
|------|:-----:|:-----:|:------:|------|
| 查看自己的 impersonation 记录 | 是 | 是 | 是 | |
| 查看他人的 impersonation 记录 | 是 | 是 | 否 | admin 可查看全部，用于审计 |
| 查看 activity_log | 是 | 是 | 自己的 | admin 可查看全部审计日志 |

### 8.7 功能权限层级（Agent 执行时能做什么）

> Section 8.2-8.6 定义的是"谁能管理 Agent"。本节定义"Agent 执行任务时拥有什么权限"。

**权限层级：**

```
Organization (admin) ──→ 拥有所有功能权限
       │
       ├── System Agent ──→ 固定 admin 级功能权限
       │
       └── User ──→ 拥有部分功能权限（由 member.role 决定）
              │
              └── Personal Agent ──→ 拥有 User 的部分功能权限（受风险分级约束）
```

**功能权限继承规则：**

| Actor | 权限来源 | 权限上限 |
|-------|---------|---------|
| admin (workspace role) | 角色赋予 | 全部功能权限 |
| System Agent | 隶属 Organization | 固定 admin 级，不随管理者变化 |
| user (workspace role) | 角色赋予 | 部分功能权限（由 role 决定） |
| Personal Agent | 继承 User | `min(User 权限, 风险分级策略)` |

**操作风险分级：**

| 风险级别 | 典型操作 | Personal Agent 策略 | System Agent 策略 |
|---------|---------|-------------------|-----------------|
| `low` | 只读检索、分析、总结 | 允许 | 允许 + 审计 |
| `medium` | 生成草稿、创建非关键文件 | 允许 + 审计 | 允许 + 审计 |
| `high` | 修改代码、发送消息、执行命令 | 需 Task Slot 中有对应 agent_execution slot | 需策略允许 |
| `critical` | 删除数据、发布上线、权限变更 | 需 **human_input slot（执行前审批）** | 需人类审批 |

> **注意：** critical 操作的审批必须发生在 Agent 执行**之前**（human_input slot, trigger=before_execution），而非执行之后的 human_review。

**与 Project Task Slot 的联动：**

```
Task 分配给 Agent 执行
  → 检查 Agent 功能权限上限（User 角色 or admin 固定）
    → 权限不足 → Task → needs_attention，通知 User
    → 权限足够 → 检查操作风险级别
      → low/medium → 直接执行
      → high → 必须有 agent_execution slot 授权
      → critical → 必须有 human_input slot (trigger=before_execution, blocking=true)
                    → User 审批后才创建 Execution
```

### 8.8 数据权限层级（Agent 能访问什么数据）

**数据权限层级：**

```
Organization (admin) ──→ 可访问全部 workspace 数据
       │
       ├── System Agent ──→ 可访问全部 workspace 数据（编排需要）
       │
       └── User ──→ 可访问 User 相关数据
              │
              └── Personal Agent ──→ 数据访问受 云端/本地 模式约束
```

**User 可访问的数据范围：**

| 数据类型 | 访问规则 |
|---------|---------|
| Project | User 创建的 + User 被邀请加入的 |
| Task | User 的 Agent 被分配的 Task |
| Channel | User 加入的 Channel |
| File | User 上传的 + User 可见 Project/Channel 中的文件 |
| Issue | User 创建的 + User 被分配的 |
| Inbox | 仅自己的通知 |
| Agent | 仅自己拥有的 Personal Agent |
| Activity Log | 仅自己相关的记录 |

**Personal Agent 数据访问（按运行位置区分）：**

| 维度 | 云端 Personal Agent | 本地 Personal Agent |
|------|-------------------|-------------------|
| **平台数据** | 通过 MCP/API 工具访问 User 可见范围内的 project、task、channel、file | 通过 MCP/CLI 工具访问 User 可见范围内的数据（同云端） |
| **代码仓库** | 通过 API 或 cloud checkout 访问（受限） | 直接访问本地 repo（完整读写） |
| **本地文件** | 无法访问 | 直接访问 Daemon 工作目录和允许的路径 |
| **执行结果上传** | 结果直接写入平台 | Artifact、日志、状态通过 Daemon 上传到平台 |
| **上下文获取** | 通过工具按需拉取（不做批量 dump） | 通过 CLI/MCP 按需拉取平台数据 + 直接读本地文件 |

**数据访问的执行机制：**

```
Agent 需要数据
  │
  ├── 云端 Agent
  │     → 通过 MCP 工具调用平台 API
  │     → 后端校验：workspace_id + user_id + agent_id + 对象级权限
  │     → 返回 User 可见范围内的数据
  │
  └── 本地 Agent
        │
        ├── 平台数据（project、issue、channel...）
        │     → 通过 myteam CLI 或 MCP 工具
        │     → 后端校验同云端
        │
        └── 本地数据（repo、文件、工具...）
              → Daemon 直接提供，不经过平台
              → 执行结果通过 Daemon 上传
                → 上传 = Artifact 创建 + 日志同步 + 状态回写
                → 不自动上传本地 repo/文件/secrets
```

**MCP 工具清单（Agent 可调用）：**

| 工具 | 用途 | 云端 | 本地 |
|------|------|:----:|:----:|
| `get_issue` | 获取 Issue 详情 | 是 | 是 |
| `list_issue_comments` | 获取 Issue 评论 | 是 | 是 |
| `create_comment` | 创建评论 | 是 | 是 |
| `update_issue_status` | 更新 Issue 状态 | 是 | 是 |
| `list_assigned_projects` | 获取分配的项目 | 是 | 是 |
| `get_project` | 获取项目详情 | 是 | 是 |
| `search_project_context` | 搜索项目上下文 | 是 | 是 |
| `list_project_files` | 获取项目文件 | 是 | 是 |
| `download_attachment` | 下载附件 | 是 | 是 |
| `upload_artifact` | 上传执行产物 | 是 | 是 |
| `complete_task` | 完成任务 | 是 | 是 |
| `request_approval` | 请求人类审批 | 是 | 是 |
| `read_file` | 读取项目文件内容 | 是 | 是 |
| `apply_patch` | 提交代码修改 | 是 | 是 |
| `create_pr` | 创建 Pull Request | 是 | 是 |
| `checkout_repo` | 检出代码仓库到本地 | 否 | 是 |
| `local_file_read` | 读取本地文件系统 | 否 | 是 |

> **权限边界在后端，不在工具：** MCP/CLI 是执行机制，不是权限边界。每次工具调用都在后端校验 `workspace_id + user_id + agent_id + 对象级权限`。Agent 不能通过工具绕过 User 的数据可见范围。

**示例：**

| 场景 | User 角色 | Agent 类型 | 运行位置 | 数据访问 |
|------|-----------|-----------|---------|---------|
| Agent 读取自己项目的 PRD | member | personal | 云端 | 允许（User 可见的 project） |
| Agent 读取他人私有项目 | member | personal | 云端 | 拒绝（User 不可见） |
| Agent 读取本地 repo 代码 | member | personal | 本地 | 允许（Daemon 直接提供） |
| Agent 上传代码修改结果 | member | personal | 本地 | 允许（通过 upload_artifact） |
| System Agent 读取全部项目 | — | system | 云端 | 允许（admin 级别） |
| System Agent 跨项目调度 | — | system | 云端 | 允许（编排需要） |

> **MVP 简化：** 第一阶段不实现独立的 Policy Engine。功能权限通过 Task Slot 隐式实现，数据权限通过现有的 workspace_id + 对象级查询实现。MCP 工具复用现有 CLI API 端点。

### 8.9 实现方式

当前系统的权限检查在 handler 层通过 middleware 注入的 `X-User-ID` + 查库判断 member.role 实现。RBAC 重构不引入独立的权限引擎，沿用现有模式：

```go
// 权限检查伪代码
func (h *Handler) requireAgentOwner(ctx, agentID) error {
    agent := h.Queries.GetAgent(ctx, agentID)
    userID := auth.UserIDFromContext(ctx)
    if agent.OwnerID != userID {
        return ErrForbidden
    }
    return nil
}

func (h *Handler) requireAdminOrAbove(ctx) error {
    member := h.Queries.GetMember(ctx, workspaceID, userID)
    if member.Role != "owner" && member.Role != "admin" {
        return ErrForbidden
    }
    return nil
}
```

**新增的权限检查点：**

| 端点 | 检查 |
|------|------|
| `PATCH /api/agents/{id}` | requireAgentOwner（编辑 Agent） |
| `PATCH /api/agents/{id}/identity-card` | requireAgentOwnerOrAdmin（Agent Owner 或 admin+ 可编辑） |
| `DELETE /api/agents/{id}` | requireAgentOwner 或 requireAdminOrAbove |
| `PATCH /api/agents/{id}/status` (suspended) | requireAgentOwner 或 requireAdminOrAbove |
| `POST /api/system-agents` | requireAdminOrAbove |
| `PATCH /api/system-agents/{id}` | requireAdminOrAbove |
| `DELETE /api/system-agents/{id}` | requireOwner（仅 workspace owner） |
| `POST /api/runtimes` (手动注册) | requireAdminOrAbove |
| `DELETE /api/runtimes/{id}` | requireAdminOrAbove |

---

## 9. 与 Project 模块的衔接

Account 层为 Project 模块提供以下能力：

| Account 能力 | Project 使用方式 |
|-------------|----------------|
| Agent.identity_card.skills | PlanGeneratorService 匹配 task.required_skills |
| Agent.status + Runtime.status | SchedulerService **同时检查两者**判断是否可调度（见 3.4 组合表） |
| Runtime.concurrency_limit / current_load | 调度时限流 |
| Runtime.provider + 代码 ProviderRegistry | 任务路由时匹配 provider 能力 |
| Agent 执行权限（见 8.7） | Task 执行时的权限边界 |
| ImpersonationSession | Task 执行中 Owner 附身代操作时的审计 |

**Session 表迁移声明：**

> 现有 `session` 表（含 session_participant）将迁移到 Channel/Thread 模型。Session 的核心能力由 Channel + Thread 承担：
> - 多轮讨论 → Thread
> - 共享上下文 → Channel / Thread 消息
> - 参与者追踪 → channel_member
> - Issue 链接 → Channel 内的 Thread
>
> Account PRD 中 `scope = 'session'` 的 System Agent 将服务于 Channel/Thread 模型的会话管理（自动回复路由、消息分配等），而非旧 session 表。
>
> 迁移时序：Session → Channel/Thread 迁移在 Project 重构之后进行，作为独立阶段。

---

## 10. 发现的问题与决策

### 10.1 必须解决

| # | 问题 | 说明 | 建议 |
|---|------|------|------|
| 1 | **Agent 表迁移复杂** | 45+ 列瘦身到 ~25 列，涉及删列、改名、数据迁移 | 分步迁移：先加新表/新列 → 数据迁移 → 再删旧列 |
| 2 | **provider 遗留值清理** | 现有 `agent_runtime.provider` 含 'multica_agent'、'legacy_local' 等非标准值 | 数据迁移统一为 'claude'/'codex'/'opencode'/'cloud_llm'，应用层通过代码 ProviderRegistry 校验 |
| 3 | **cloud_llm_config 归属** | 当前存在 Agent 表上（每个 Agent 一份 LLM 配置快照） | 移到 Runtime.metadata。同一 Runtime 下的 Agent 共享 LLM 配置 |
| 4 | **is_system 唯一约束** | 现有唯一索引 `UNIQUE (workspace_id) WHERE is_system = TRUE`，合并后需替换 | 先创建新唯一约束，再删旧约束 |
| 5 | **前端 AgentType 引用** | 前端大量 `agent_type === 'page_system_agent'` 判断 | 全部改为 `agent_type === 'system_agent' && scope === 'xxx'` |
| 6 | **online_status + workload_status 合并** | 两个字段共享 5/7 个值，语义重叠 | 合并为单一 `status` 字段（7 值） |
| 7 | **Agent 执行权限模型** | 现有系统无 Agent 执行权限定义 | 新增 Section 8.8，Personal Agent 继承 Owner 权限 + 风险分级，System Agent 固定 admin |

### 10.2 已决策

| # | 问题 | 决策 | 理由 |
|---|------|------|------|
| 1 | Provider 表 vs 代码注册表 | **不建表，用代码 ProviderRegistry** | 添加 provider 必须改代码（新 Backend），数据库行无法独立启用 provider |
| 2 | capabilities 统一 | **只保留 identity_card** | 删 Agent 表 capabilities/tools 列，统一到 identity_card JSONB |
| 3 | System Agent owner_id | **可选（NULL）** | 全局 System Agent 属于 workspace 而非个人，owner_id 为 NULL |
| 4 | Runtime 下线时 Agent 状态 | **解耦** | Agent 保持自身状态，UI 组合展示 "idle (runtime offline)" |
| 5 | triggers 去留 | **合并到 auto_reply_config** | triggers 是自动回复触发条件，归入 auto_reply_config 更内聚 |
| 6 | scope 枚举 | **固定枚举 + CHECK** | `CHECK (scope IS NULL OR scope IN ('account','conversation','project','file'))` |

### 10.3 风险提示

| # | 风险 | 影响 | 缓解 |
|---|------|------|------|
| 1 | **Daemon 兼容性** | Runtime 表新增字段（concurrency_limit、lease 等），Daemon 注册 API 需要适配 | 新字段均有默认值，Daemon 不传则用默认值，零破坏 |
| 2 | **CLI 发版** | Agent 表瘦身后 CLI 取 agent 信息的 API 返回结构变化 | CLI 需同步发版。加 API 版本兼容或新端点 |
| 3 | **并行开发冲突** | Account 重构和 Project 重构都涉及 Agent 表，可能产生迁移冲突 | 建议 Account 迁移先行（Agent/Runtime/Provider 表），Project 迁移在其之上 |

---

## 11. 迁移顺序

```
Phase 1: 加列（无破坏性）
  1. agent_runtime 加列：concurrency_limit, current_load, lease_expires_at
  2. agent 加列：scope
  3. message 加列：effective_actor_id, effective_actor_type, real_operator_id, real_operator_type
  4. 创建代码级 ProviderRegistry（pkg/provider/registry.go）

Phase 2: 数据迁移
  5. agent_runtime.provider 遗留值清理：
       - 'multica_agent' → 'cloud_llm'
       - 'legacy_local' → 'claude'
       - 其他非标准值 → 人工确认后映射
  6. page_system_agent → system_agent + scope
  7. agent.cloud_llm_config → runtime.metadata
  8. agent.capabilities/tools → identity_card (合并)

Phase 3: RBAC 权限检查
  9. 新增 handler 权限检查函数：requireAgentOwner, requireAdminOrAbove, requireOwner
  10. 为 Agent/SystemAgent/Runtime/IdentityCard 端点加权限守卫
  11. 审计日志补充：归档、暂停、impersonation 操作写入 activity_log

Phase 4: 清理（破坏性，需确认）
  12. 合并 agent.online_status + agent.workload_status → agent.status（单一字段）
  13. 删除 agent 冗余列：runtime_mode, runtime_config, cloud_llm_config, capabilities, tools, triggers, is_system, system_config, page_scope, agent_metadata, accessible_files_scope, allowed_channels_scope, online_status, workload_status
  14. agent_runtime 改名列：last_seen_at → last_heartbeat_at, runtime_mode → mode
  14. 删除旧约束，创建新约束
  15. 前端同步更新
```

---

## 12. MVP 范围

### 包含

| 项目 | 说明 |
|------|------|
| 代码 ProviderRegistry | `pkg/provider/registry.go`，4 个 provider + `GET /api/providers` 端点 |
| Runtime 表增强 | 加 concurrency_limit、current_load、lease_expires_at；provider 保持 TEXT |
| Agent 表瘦身 | 移除冗余列，合并 online_status + workload_status → status，统一到 identity_card |
| agent_type 合并 | system_agent + page_system_agent → system_agent + scope |
| Identity Card | 结构正式化，自动生成保留 |
| Impersonation 审计增强 | message 加 effective_actor / real_operator |
| 唯一约束更新 | 适配合并后的 system_agent + scope CHECK |
| RBAC 管理权限 | Agent/SystemAgent/Runtime/IdentityCard 全部端点加权限守卫 |
| RBAC 执行权限 | Personal Agent 继承 Owner 权限 + 风险分级，System Agent 固定 admin |
| 权限检查函数 | requireAgentOwner / requireAdminOrAbove / requireOwner |

### 不包含

| 项目 | 说明 |
|------|------|
| 独立 Policy Engine | 风险分级通过 Task Slot 隐式实现，不做独立引擎 |
| take_over / delegate_to_agent | 后续阶段（当前仅保留 impersonation） |
| Runtime owner_scope | 后续阶段（当前 Runtime 绑定 workspace 级别） |
| Identity Card AI 自动生成增强 | 保持现有逻辑，不做新的 LLM 管线 |
