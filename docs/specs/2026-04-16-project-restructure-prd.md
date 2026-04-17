# Project 模块重构 PRD

> Version: 1.0 | Date: 2026-04-16

---

## 1. 背景与目标

### 1.1 现状问题

当前 Project 模块的执行链路为 `Plan → Workflow → WorkflowStep → AgentTaskQueue`，存在以下核心问题：

| # | 问题 | 说明 |
|---|------|------|
| 1 | 信息三重存储 | Plan.steps (JSONB)、Workflow.dag (JSONB)、WorkflowStep (行记录) 三处存储同一份任务描述 |
| 2 | Workflow 层冗余 | Workflow 几乎只是 Plan 的执行镜像，其 type/cron_expr 与 Project 重复 |
| 3 | 人机协作原始 | WorkflowStep 仅有 `human_approval_required` 布尔值，无法表达结构化的人机协作 |
| 4 | 无产物管理 | 缺少 Artifact 和 Review 对象，任务产物和验收无结构化追踪 |
| 5 | 执行队列双重身份 | AgentTaskQueue 同时服务 Issue 和 Project 两条链路，职责不清 |

### 1.2 重构目标

建立 `Project → Version → Plan → Task → Slot` 五层模型，实现：

1. **消除冗余** — 去掉 Workflow 层，Task 同时承载规划语义和执行状态
2. **结构化人机协作** — 通过 ParticipantSlot 精确表达"谁在什么时候以什么方式参与"
3. **产物与验收闭环** — 引入 Artifact (版本化产物) 和 Review (验收记录)
4. **会话绑定** — Project ↔ Channel、Plan ↔ Thread，打通协作与执行
5. **执行解耦** — 新增 Execution 表，与 Task 业务语义分离

### 1.3 设计来源

本 PRD 基于以下两份架构文档：

- `docs/myteam-architecture-design.md` — MyTeam 架构设计文档
- AgentHub 项目设计文档 — 人机协作任务模型

---

## 2. 术语表

| 术语 | 定义 |
|------|------|
| **Project** | 项目容器。如 "SaySo"、"MyTeam Dashboard"。绑定一个项目 Channel |
| **Version** | 项目版本/里程碑。如 "1.0"、"2.0-beta"。一个 Project 下可有多个 Version |
| **Plan** | 项目计划。一个 Version 下可有多个独立 Plan，如 "增加登录方式"、"优化首页性能"。绑定一个 Thread |
| **Task** | 具体执行单元。如 "产品 PRD 书写"、"前端开发"。拥有 step 执行状态和 DAG 依赖 |
| **ParticipantSlot** | 参与槽位。定义 Task 内部的人机协作结构：谁参与、何时介入、是否阻塞 |
| **Execution** | 执行尝试。一个 Task 可能被执行多次（重试、换 Agent），每次是一条 Execution 记录 |
| **Artifact** | 版本化产物。Task 执行产出的可交付结果，如文档、代码、设计稿 |
| **Review** | 验收记录。针对 Artifact 的审批决策：approve / reject / request_changes |
| **ProjectRun** | Plan 的一次执行实例。一个 Plan 可被执行多次，每次是一个 Run |
| **Agent Owner** | Agent 的所属用户（`agent.owner_id`），可以是任何 workspace 角色。本文中大写 **Owner** 均指此含义 |
| **Project Creator** | 创建项目的用户（`project.creator_owner_id`），负责 Plan 审批和执行启动 |

> 详见 Account PRD Section 1.3 术语澄清。

---

## 3. 对象模型

### 3.1 整体层级

```
Project ("SaySo")
  │
  ├── Channel (项目频道)
  │
  └── Version ("1.0")
       │
       ├── Plan ("增加登录方式")
       │    │
       │    ├── Thread (项目 Channel 内的讨论线程)
       │    ├── ProjectRun #1 (执行实例)
       │    │
       │    ├── Task 1 ("产品 PRD 书写") ─── step_order: 1
       │    │    ├── Slot: human_input (Owner 提供需求约束)
       │    │    ├── Slot: agent_execution (Agent 撰写 PRD)
       │    │    ├── Slot: human_review (Owner 验收 PRD)
       │    │    ├── Execution #1 (Agent A 第一次尝试)
       │    │    ├── Artifact v1 (PRD 初稿)
       │    │    └── Review (Owner: request_changes)
       │    │
       │    ├── Task 2 ("前端开发") ─── step_order: 2, depends_on: [Task 1]
       │    ├── Task 3 ("后端开发") ─── step_order: 3, depends_on: [Task 1]
       │    └── Task 4 ("测试") ─── step_order: 4, depends_on: [Task 2, Task 3]
       │
       └── Plan ("优化首页性能")
            └── ...独立审批、独立执行
```

### 3.2 会话绑定

```
Project Channel ("proj-sayso-abc123")
  │
  ├── 普通消息（项目级沟通）
  │
  ├── Thread 1 ←→ Plan "增加登录方式"
  │    ├── 讨论：需求分析、方案选型
  │    ├── System Agent：生成 Plan 草稿
  │    └── Owner：审批 Plan
  │
  └── Thread 2 ←→ Plan "优化首页性能"
       └── ...
```

**绑定规则：**

| 绑定 | 方向 | 说明 |
|------|------|------|
| Project → Channel | 创建时自动 | 新建 Project 自动创建项目 Channel；或从现有 Channel 创建 Project 时绑定该 Channel |
| Channel → Project | FK 引用 | `channel.project_id` 指向所属 Project |
| Plan → Thread | 创建时绑定 | 从 Thread 生成 Plan 时绑定；或创建 Plan 时自动在项目 Channel 中创建 Thread |
| Thread → Plan | 反查 | 通过 `plan.thread_id` 反查 |

**Thread.id 解耦：**

> 现有实现 `thread.id = root message ID`。重构后 thread.id 使用独立的 `gen_random_uuid()`，不再依赖 root message。新增可选字段 `thread.root_message_id`。
>
> 这样 Plan 创建时可以先创建 Thread（独立 UUID），再在 Thread 中发布 Plan 摘要消息，不存在鸡蛋问题。

**Session 表迁移：**

> 现有 `session` 表将迁移到 Channel/Thread 模型。Session 的核心能力（多轮讨论、共享上下文、参与者追踪）由 Channel + Thread 承担。Issue 相关协作从 `session.issue_id` 迁移到 Channel 内的 Thread。迁移完成后 session 表废弃。

---

## 4. 对象定义

### 4.1 Project

项目容器，最顶层的组织单元。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID PK | |
| `workspace_id` | UUID FK → workspace | |
| `title` | TEXT | 项目名称 |
| `description` | TEXT | 项目描述 |
| `status` | TEXT | 项目状态 |
| `schedule_type` | TEXT | `one_time` \| `scheduled` \| `recurring` |
| `cron_expr` | TEXT | 仅 scheduled/recurring 时有值 |
| `channel_id` | UUID FK → channel | 项目频道 |
| `source_conversations` | JSONB | 创建时的会话上下文快照 |
| `creator_owner_id` | UUID | 创建者 |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**状态枚举：**

| 状态 | 说明 |
|------|------|
| `draft` | 项目已创建，尚未启动任何 Plan |
| `running` | 至少有一个 Plan 正在执行 |
| `paused` | 所有执行暂停 |
| `completed` | 所有 Plan 执行完毕（仅 one_time/scheduled） |
| `stopped` | 主动停止（仅 recurring） |
| `archived` | 终态，只读 |

> **不变：** Project 表结构与现有基本一致，主要变化在下游对象。

---

### 4.2 ProjectVersion

版本/里程碑，组织 Plan 的容器。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID PK | |
| `project_id` | UUID FK → project | |
| `version_number` | INTEGER | 自增序号 |
| `version_label` | TEXT | 显示标签，如 "1.0"、"2.0-beta" |
| `description` | TEXT | 版本说明 |
| `status` | TEXT | `active` \| `archived` |
| `created_by` | UUID | |
| `created_at` | TIMESTAMPTZ | |

> **变化：** 去掉了现有的 `plan_snapshot` 和 `workflow_snapshot` JSONB 字段。Plan 和 Task 直接作为行记录存在，不再需要快照。

---

### 4.3 Plan

项目计划，一个 Version 下的独立工作计划。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID PK | |
| `version_id` | UUID FK → project_version | |
| `project_id` | UUID FK → project | 冗余，便于查询 |
| `workspace_id` | UUID FK → workspace | |
| `title` | TEXT | 计划名称，如 "增加登录方式" |
| `description` | TEXT | 计划概述 |
| `task_brief` | TEXT | 任务书：目标、范围、成功标准 |
| `constraints` | TEXT | 约束条件 |
| `expected_output` | TEXT | 预期产出 |
| `context_snapshot` | JSONB | 生成 Plan 时冻结的上下文 |
| `thread_id` | UUID FK → thread | 绑定的讨论线程 |
| `assigned_agents` | JSONB | Agent 分配概要 |
| `approval_status` | TEXT | 审批状态 |
| `approved_by` | UUID | 审批人 |
| `approved_at` | TIMESTAMPTZ | 审批时间 |
| `created_by` | UUID | |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**审批状态枚举：**

| 状态 | 说明 |
|------|------|
| `draft` | 草稿，可编辑 |
| `pending_approval` | 已提交，等待 Owner 审批 |
| `approved` | Owner 已批准，可启动执行 |
| `rejected` | Owner 已拒绝，需修订 |

> **变化：** 去掉了 `steps` JSONB 字段。任务定义完全由 Task 表承载，不再重复存储。新增 `thread_id` 绑定讨论线程。

---

### 4.4 ProjectRun

Plan 的一次执行实例。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID PK | |
| `plan_id` | UUID FK → plan | |
| `project_id` | UUID FK → project | 冗余，便于查询 |
| `run_number` | INTEGER | 在 Plan 内自增 |
| `status` | TEXT | 执行状态 |
| `start_at` | TIMESTAMPTZ | 开始时间 |
| `end_at` | TIMESTAMPTZ | 结束时间 |
| `output_refs` | JSONB | 最终产出引用 |
| `failure_reason` | TEXT | 失败原因 |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**状态枚举：** `pending` | `running` | `paused` | `completed` | `failed` | `cancelled`

**约束：** 同一 Plan 同时只有一个活跃 Run（status 为 pending、running 或 paused）。

> **注：** `retry_count` 已移除。重试计数由 Task.current_retry 和 Execution.attempt 分层管理（见下方说明）。

---

### 4.5 Task

具体执行单元，同时承载规划语义和 step 执行状态。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID PK | |
| `plan_id` | UUID FK → plan | 所属 Plan |
| `run_id` | UUID FK → project_run | 当前 Run（Run 启动时设置） |
| `workspace_id` | UUID FK → workspace | |
| **规划字段** | | |
| `title` | TEXT | 任务标题，如 "前端开发" |
| `description` | TEXT | 任务详细描述 |
| `step_order` | INTEGER | 执行顺序 |
| `depends_on` | UUID[] | DAG 依赖的 Task ID 列表 |
| `primary_assignee_id` | UUID FK → agent | 主负责 Agent |
| `fallback_agent_ids` | UUID[] | 备选 Agent 列表 |
| `required_skills` | TEXT[] | 所需技能 |
| `collaboration_mode` | TEXT | 协作模式 |
| `acceptance_criteria` | TEXT | 验收标准 |
| **执行状态字段** | | |
| `status` | TEXT | step 执行状态 |
| `actual_agent_id` | UUID FK → agent | 实际执行的 Agent |
| `current_retry` | INTEGER DEFAULT 0 | 当前重试次数 |
| `started_at` | TIMESTAMPTZ | 开始时间 |
| `completed_at` | TIMESTAMPTZ | 完成时间 |
| `result` | JSONB | 执行结果 |
| `error` | TEXT | 错误信息 |
| **策略字段** | | |
| `timeout_rule` | JSONB | 超时策略 |
| `retry_rule` | JSONB | 重试策略 |
| `escalation_policy` | JSONB | 升级策略 |
| **上下文字段** | | |
| `input_context_refs` | JSONB | 输入上下文引用 |
| `output_refs` | JSONB | 输出引用 |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**数据约束：**

| 约束 | 说明 |
|------|------|
| `depends_on` 限同一 Plan | `depends_on[]` 中的 UUID 必须引用同一 `plan_id` 下的 Task。跨 Plan 依赖不允许 |
| `collaboration_mode` 与 Slot 一致 | `agent_exec_human_review` 必须有 agent_execution + human_review slot；`human_input_agent_exec` 必须有 human_input + agent_execution slot。Plan 创建/更新时校验 |
| 跨 Plan Agent 共享 | 同 Version 下两个 Plan 可并发执行。同一 Agent 可被不同 Plan 的 Task 分配，由 Runtime.concurrency_limit 控制并发。调度器不做跨 Plan 排他 |

**协作模式枚举：**

| 模式 | 说明 | 典型 Slot 组合 |
|------|------|---------------|
| `agent_exec_human_review` | Agent 执行 + 人类验收 | agent_execution → human_review |
| `human_input_agent_exec` | 人类输入 + Agent 执行 | human_input → agent_execution |
| `agent_prepare_human_action` | Agent 准备 + 人类执行高风险动作 | agent_execution → human_input |
| `mixed` | 混合多阶段协作 | 自定义 Slot 序列 |

**Task 状态枚举：**

| 状态 | 说明 | 可接收调度 |
|------|------|----------|
| `draft` | 已定义但 Slot/Agent 未就绪 | 否 |
| `ready` | 前置条件已满足，可调度 | 是 |
| `queued` | 已进入执行队列，等待 Agent 领取 | 否 |
| `assigned` | Agent 已匹配并通知 | 否 |
| `running` | Agent 正在执行 | 否 |
| `needs_human` | 正常等待人类输入（blocking human slot 激活） | 否 |
| `under_review` | 产物已生成，等待人类验收 | 否 |
| `needs_attention` | 异常：超时/Agent 离线/重试耗尽，需 Owner 介入 | 否 |
| `completed` | 成功完成并通过验收 | 否 |
| `failed` | 永久失败 | 否 |
| `cancelled` | 被取消 | 否 |

> **关键区分：**
> - `needs_human`：流程内的正常等待，不是异常
> - `under_review`：有产物等验收，不是卡住
> - `needs_attention`：系统无法自动继续，需人工处理异常

**默认策略：**

```json
// timeout_rule
{ "max_duration_seconds": 1800, "action": "retry" }

// retry_rule
{ "max_retries": 2, "retry_delay_seconds": 30 }

// escalation_policy
{ "escalate_after_seconds": 600 }
```

**Task 与 Run 的状态管理：**

Task 同时承载规划定义和 step 执行状态。一个 Plan 可有多个 Run，但同一时刻只有一个活跃 Run。状态管理规则：

| 规则 | 说明 |
|------|------|
| Task.status 表示当前 Run | Task 上的 status、actual_agent_id、current_retry、started_at、completed_at、result、error 都是"当前活跃 Run"的状态 |
| 新 Run 启动时重置 | 创建新 ProjectRun 时，Plan 下所有 Task 的执行状态字段重置：status→draft，current_retry→0，actual_agent_id→NULL，started_at/completed_at→NULL，result/error→NULL |
| 历史数据在 Execution 中 | 每次执行尝试都有独立的 Execution 记录（含 run_id），历史 Run 的执行详情通过 `SELECT * FROM execution WHERE run_id = <old_run_id>` 查询 |
| Slot 状态也重置 | 新 Run 启动时，所有 Slot 的 status 重置为 waiting。**每次 Run 都要求重新确认**，包括 human_input slot（即使内容未变）。理由：每次执行的上下文可能不同，需要 User 重新审视输入是否仍然适用 |
| 规划字段不变 | title、description、step_order、depends_on、primary_assignee_id 等规划字段在跨 Run 时不变 |

> **查询模式：**
> - "Task 3 在 Run #1 的执行结果" → `SELECT * FROM execution WHERE task_id = X AND run_id = run1`
> - "Task 3 当前状态" → 直接读 `task.status`
> - "Task 3 的所有 Artifact" → `SELECT * FROM artifact WHERE task_id = X`（Artifact 跨 Run 保留）

**重试计数器分层管理：**

| 计数器 | 层级 | 职责 | 重置时机 |
|--------|------|------|---------|
| `Execution.attempt` | 执行层 | 标识这是第几次执行尝试 | 不重置，每条 Execution 独立 |
| `Task.current_retry` | 任务层 | 同一 Agent 的重试次数，控制是否超过 retry_rule.max_retries | 换 fallback Agent 时重置为 0；新 Run 时重置为 0 |
| ~~ProjectRun.retry_count~~ | ~~已移除~~ | ~~无~~ | — |

---

### 4.6 ParticipantSlot

任务内部的人机协作槽位。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID PK | |
| `task_id` | UUID FK → task | |
| `slot_type` | TEXT | 槽位类型 |
| `slot_order` | INTEGER | 在 Task 内的顺序 |
| `participant_id` | UUID | 参与者 ID |
| `participant_type` | TEXT | `member` \| `agent` |
| `responsibility` | TEXT | 职责描述 |
| `trigger` | TEXT | 触发时机 |
| `blocking` | BOOLEAN DEFAULT true | 是否阻塞后续流程 |
| `required` | BOOLEAN DEFAULT true | 是否必需 |
| `expected_output` | TEXT | 预期产出类型 |
| `status` | TEXT | 槽位状态 |
| `timeout_seconds` | INTEGER | 超时秒数 |
| `started_at` | TIMESTAMPTZ | |
| `completed_at` | TIMESTAMPTZ | |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**Slot 类型枚举：**

| 类型 | 说明 |
|------|------|
| `human_input` | 人类提供输入/决策 |
| `agent_execution` | Agent 执行主要工作 |
| `human_review` | 人类验收产物 |

**触发时机枚举：**

| 触发 | 说明 |
|------|------|
| `before_execution` | 执行前（如人类先提供需求） |
| `during_execution` | 执行中（如 Agent 执行主体工作） |
| `before_done` | 完成前（如人类验收） |

**Slot 状态枚举：**

| 状态 | 说明 |
|------|------|
| `waiting` | 已定义，触发条件未满足 |
| `ready` | 可执行，等待参与者开始 |
| `in_progress` | 参与者正在处理 |
| `submitted` | 已提交输出 |
| `approved` | 输出被接受（Review.decision = approve） |
| `revision_requested` | 方向对但需修订（Review.decision = request_changes） |
| `rejected` | 输出被否定（Review.decision = reject） |
| `expired` | 超时未完成 |
| `skipped` | 被策略跳过 |

**Slot 状态与 Task 状态联动：**

| Slot 事件 | Task 状态变化 |
|-----------|-------------|
| blocking human_input slot → ready | Task → `needs_human` |
| human_input slot → submitted/approved | Task → `running` |
| agent_execution slot → submitted | 检查是否有 review slot |
| human_review slot → ready | Task → `under_review` |
| human_review slot → approved | Task → `completed` |
| human_review slot → revision_requested | Task → `running`（Agent 基于反馈修订） |
| human_review slot → rejected | Task → `needs_attention`（Owner 决定下一步） |
| required blocking slot → expired | Task → `needs_attention` |
| optional slot → expired | Slot → `skipped`，Task 继续 |

---

### 4.7 Execution

Task 的一次具体执行尝试。一个 Task 在生命周期内可能对应多次 Execution（重试、换 Agent、换 Runtime）。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID PK | |
| `task_id` | UUID FK → task | |
| `run_id` | UUID FK → project_run | |
| `slot_id` | UUID FK → participant_slot | 触发此次执行的 Slot |
| `agent_id` | UUID FK → agent | 执行 Agent |
| `runtime_id` | UUID FK → runtime | 执行 Runtime（provider 信息通过 runtime.provider TEXT 获取） |
| `attempt` | INTEGER | 第几次尝试 |
| `status` | TEXT | 执行状态 |
| `priority` | INTEGER | 优先级 |
| `payload` | JSONB | 执行输入（prompt、参数、上下文引用） |
| `result` | JSONB | 执行结果 |
| `error` | TEXT | 错误信息 |
| `context_ref` | JSONB NOT NULL DEFAULT '{}' | 运行环境物理事实（见下方 schema） |
| `log_retention_policy` | TEXT DEFAULT '90d' | 日志保留策略：`7d` \| `30d` \| `90d` \| `permanent` |
| `logs_expires_at` | TIMESTAMPTZ | 日志过期时间（由 log_retention_policy 计算） |
| `claimed_at` | TIMESTAMPTZ | Runtime 领取时间 |
| `started_at` | TIMESTAMPTZ | 开始执行时间 |
| `completed_at` | TIMESTAMPTZ | 完成时间 |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

**状态枚举：** `queued` | `claimed` | `running` | `completed` | `failed` | `cancelled` | `timed_out`

**context_ref 结构（按 mode 区分）：**

```jsonc
// local 模式（Daemon 执行）
{
  "mode": "local",
  "working_dir": "/Users/xxx/workspace/sayso",
  "daemon_id": "daemon-mbp-001"
}

// cloud 模式（Claude Agent SDK 执行）
{
  "mode": "cloud",
  "sdk_session_id": "sess_abc123",
  "sandbox_id": "sbx_xyz789",
  "virtual_project_path": "/workspace/sayso"
}
```

**设计约定：**

| 约定 | 说明 |
|------|------|
| 挂在 Execution 而非 Task | `context_ref` 是"这一次执行尝试的物理事实"，每次 run 独立记录；Task 已有 `input_context_refs` 表示业务输入，语义正交 |
| 重跑不复用 | 新的 Execution 重新写入 context_ref；cloud 沙箱不隐式 persist |
| local 默认来源 | claim 时若 context_ref 为空，由 Daemon 从 Runtime.working_dir 填充 |
| cloud 默认来源 | claim/start 时由 CloudExecutorService 创建 SDK session/sandbox 并写入 |

> **与 AgentTaskQueue 的关系：** Execution 表专门服务 Project 链路。现有 AgentTaskQueue 继续服务 Issue 链路（`TaskService.EnqueueTaskForIssue`）。两者共享 Daemon 执行基础设施，但业务语义独立。

---

### 4.8 Artifact

版本化的任务产物。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID PK | |
| `task_id` | UUID FK → task | |
| `slot_id` | UUID FK → participant_slot | 产出该 Artifact 的 Slot |
| `execution_id` | UUID FK → execution | 产出该 Artifact 的 Execution |
| `run_id` | UUID FK → project_run | 归属的 Run（Artifact 跨 Run 保留但必须归属特定 Run） |
| `artifact_type` | TEXT | 产物类型 |
| `version` | INTEGER | 同一 Task 内的版本号 |
| `title` | TEXT | 产物标题 |
| `summary` | TEXT | 本版本说明 |
| `content` | JSONB | 结构化内容、引用、元数据（headless Artifact 以此为权威内容） |
| `file_index_id` | UUID FK → file_index | 关联的 FileIndex 条目（headless Artifact 为 NULL） |
| `file_snapshot_id` | UUID FK → file_snapshot | 关联的具体版本快照（headless 为 NULL） |
| `retention_class` | TEXT DEFAULT 'permanent' | 保留策略：`permanent` \| `ttl` \| `temp` |
| `created_by_id` | UUID | 创建者 |
| `created_by_type` | TEXT | `member` \| `agent` |
| `created_at` | TIMESTAMPTZ | |

**类型枚举：** `document` | `design` | `code_patch` | `report` | `file` | `plan_doc`

**headless Artifact（无物理文件）：**

允许 Artifact 不关联 FileIndex。典型场景：Agent 产出的纯文本摘要、JSON 结构化结果、code_patch 的 diff 内容。此时：

- `file_index_id = NULL`，`file_snapshot_id = NULL`
- `content` 字段为**权威内容**（Review 评审对象）
- 后续如需导出为文件，由 ArtifactService 按需生成

**Artifact 与 FileIndex 的关系：**

| | Artifact | FileIndex/FileSnapshot |
|---|---------|----------------------|
| 定位 | 任务交付物（带 Review 审批） | 文件存储实体（供搜索/引用） |
| 可见性边界 | Task → Plan → Project 层级决定 | `access_scope` 字段声明 |
| 版本 | Task 内版本化（`version` 字段） | 独立快照链 |
| 用途 | Review 的评审对象 | 上传/下载/引用/检索 |

**关联约束：**

| 约束 | 说明 |
|------|------|
| 两行记录 + FK | Artifact 与 FileIndex 永远是两行，不合并为一行 |
| access_scope 对齐 | 关联的 FileIndex 必须 `access_scope = 'project'` 且 `project_id` 与 Artifact 所属 Project 一致 |
| 同事务创建 | ArtifactService 创建带物理文件的 Artifact 时，在**同一事务**内创建 FileIndex/FileSnapshot 并回填 FK |
| 不可悬挂 | Artifact 删除/归档时按 `retention_class` 联动处理 FileIndex，避免悬挂引用（见 §11 数据生命周期） |

---

### 4.9 Review

针对 Artifact 的验收决策记录。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID PK | |
| `task_id` | UUID FK → task | |
| `artifact_id` | UUID FK → artifact | 被评审的产物 |
| `slot_id` | UUID FK → participant_slot | 对应的 review slot |
| `reviewer_id` | UUID | 评审人 |
| `reviewer_type` | TEXT | `member` \| `agent` |
| `decision` | TEXT | `approve` \| `reject` \| `request_changes` |
| `comment` | TEXT | 评审意见 |
| `created_at` | TIMESTAMPTZ | |

**决策语义：**

| 决策 | 含义 | Slot 状态 | Task 后续 |
|------|------|----------|----------|
| `approve` | 产物通过 | Slot → `approved` | Task → `completed` |
| `request_changes` | 方向对，需修订 | Slot → `revision_requested` | Task → `running`，Agent 产出 Artifact v(N+1) |
| `reject` | 产物不可接受 | Slot → `rejected` | Task → `needs_attention`，Agent Owner 决定下一步 |

---

## 5. 状态机

### 5.1 Task 状态机

```
                    ┌─────────────────────────────────────────────┐
                    │              any active → cancelled          │
                    └─────────────────────────────────────────────┘

draft ──→ ready ──→ queued ──→ assigned ──→ running ──→ completed
            │                                 │  │  │
            │                                 │  │  └──→ failed
            │                                 │  │
            └──→ needs_human ←────────────────┘  └──→ under_review
                    │                                     │  │
                    │  ┌──────────────────────────────────┘  │
                    │  │                                     │
                    └──│──→ running ←────────────────────────┘
                       │        (request_changes)
                       │
                       └──→ completed
                              (approve)

running|needs_human|under_review ──→ needs_attention (异常)

needs_attention ──→ running (Owner 重试/重新分配)
needs_attention ──→ cancelled (Owner 取消)
needs_attention ──→ failed (Owner 确认失败)
```

**转换规则：**

| 转换 | 触发条件 |
|------|---------|
| `draft → ready` | Slot、Assignee、Criteria 已齐备 |
| `ready → needs_human` | 存在 blocking 的 before_execution human_input slot |
| `ready → queued` | 无前置 human slot，无未满足依赖，Run 已启动 |
| `needs_human → running` | 对应 human slot 已 submitted/approved |
| `queued → assigned` | Agent 已匹配并通知 |
| `assigned → running` | Execution 开始 |
| `running → under_review` | Artifact 已产出且存在 human_review slot |
| `running → completed` | 无 review slot，执行成功 |
| `running → needs_attention` | 超时/Agent 离线/重试耗尽 |
| `running → failed` | 执行失败且策略判定不再重试 |
| `under_review → completed` | Review decision = `approve` |
| `under_review → running` | Review decision = `request_changes` |
| `under_review → needs_attention` | Review decision = `reject` |

### 5.2 Slot 状态机

```
waiting ──→ ready ──→ in_progress ──→ submitted ──→ approved
                                          │
                                          ├──→ revision_requested (方向对，需修订)
                                          └──→ rejected (方向不对)

ready|in_progress ──→ expired
waiting|ready ──→ skipped
```

**按 Slot 类型的典型终态：**

| Slot 类型 | 典型终态 |
|-----------|---------|
| `human_input` | `submitted` 或 `approved` |
| `agent_execution` | `submitted`（再由 Artifact/Review 决定后续） |
| `human_review` | `approved`、`revision_requested` 或 `rejected` |

### 5.3 Execution 状态机

```
queued ──→ claimed ──→ running ──→ completed
                         │  │
                         │  └──→ timed_out ──→ queued (重试)
                         │
                         └──→ failed ──→ queued (重试)

queued|claimed|running ──→ cancelled
```

### 5.4 Plan 审批状态机

```
draft ──→ pending_approval ──→ approved
               │                   │
               └──→ rejected       └──→ draft (Owner 修改后)
                      │
                      └──→ draft (修订后重新提交)
```

### 5.5 跨状态机联动

```
Plan approved
  → Owner 启动执行
    → 创建 ProjectRun (pending → running)
      → 无依赖 Task: draft → ready → queued
        → Execution 创建: queued → claimed → running
          → Agent 执行中
            → 产出 Artifact
              → human_review slot: waiting → ready
                → Task: running → under_review
                  → Owner Review: approve
                    → Task: under_review → completed
                      → 下游 Task 依赖满足 → ready → queued → ...
                        → 全部 Task completed
                          → Run: running → completed
                            → Project 状态更新

Agent 离线 / Execution 失败
  → Execution: running → failed
    → retry_count < max_retries?
      → 是: 新建 Execution (attempt+1), 指数退避
      → 否: 有 fallback_agent?
        → 是: 换 Agent, 重置 retry_count
        → 否: Task → needs_attention
          → 通知 Owner (inbox_item)
            → Owner 选择: 重试 / 换 Agent / 取消

Plan 被修改（approved → draft）且有活跃 Run
  → 自动取消活跃 Run: Run → cancelled
    → 所有活跃 Task → cancelled
    → 所有活跃 Execution → cancelled
    → 通知相关 User

约束：Plan.approval_status 在有活跃 Run 时不能直接改为 rejected。
      必须先取消 Run，再修改 Plan。
```

---

## 6. 核心流程

### 6.1 从 Channel 创建 Project

```
1. Owner 在某个 Channel 中讨论，产生项目想法
2. Owner 选择消息/上下文 → 发起"创建项目"
3. 系统创建 Project:
   a. 若从 Channel 发起 → 绑定该 Channel 为项目频道
   b. 若新建 Project → 自动创建项目 Channel
4. 系统创建 Version v1 (version_label: "1.0")
5. source_conversations 快照保存原始上下文
6. 将相关 Agent 加入项目 Channel
7. 发布 project:created 事件
```

### 6.2 从 Thread 生成 Plan

```
1. 在项目 Channel 中讨论具体计划，对话形成 Thread
2. Owner 从 Thread 发起"生成计划"
3. 系统读取上下文：
   a. Thread 消息内容
   b. 项目 Channel 上下文
   c. 可用 Agent 的 Identity Card（能力、技能、工具）
4. 调用 PlanGeneratorService → LLM 生成 Plan：
   a. Task 列表（含依赖关系 DAG）
   b. 每个 Task 的 Agent 分配和 Slot 定义
   c. 协作模式推荐
   d. Task Brief（目标、范围、成功标准）
5. 创建 Plan 记录:
   a. plan.thread_id = 该 Thread ID
   b. plan.context_snapshot = 冻结的上下文
   c. plan.approval_status = 'draft'
6. 创建 Task 记录（N 条）
7. 创建 ParticipantSlot 记录（每个 Task 若干条）
8. 发布 plan:created 事件
```

### 6.3 Plan 审批

```
1. Owner 在 Plan 页面（或 Thread 内）审阅 Plan
2. 可修改：
   a. Task 描述、顺序、依赖关系
   b. Agent 分配
   c. Slot 定义（参与者、触发时机、是否阻塞）
   d. 验收标准
3. Owner 提交审批 → plan.approval_status = 'pending_approval'
4. Owner 批准 → plan.approval_status = 'approved'
   或 拒绝 → plan.approval_status = 'rejected'
5. 被拒后可修订后重新提交
```

### 6.4 启动执行

```
1. 前提：Plan.approval_status = 'approved'
2. Owner 点击"启动执行"
3. 系统创建 ProjectRun (status: 'pending')
4. 遍历 Plan 下所有 Task:
   a. 设置 task.run_id = 新 Run ID
   b. 检查 Task 前置条件:
      - Slot/Agent/Criteria 是否齐备 → draft 则停留 draft
      - 齐备 → task.status = 'ready'
5. ProjectRun.status = 'running'
6. 调度器开始调度（需对接 Account 层约束）:
   a. 找出 ready 且无未满足依赖的 Task
   b. 若 Task 有 before_execution human_input slot → Task → needs_human
   c. 否则进入 Agent 选择流程:
      i.   匹配 task.required_skills 与 agent.identity_card.skills
      ii.  检查 Agent.status（idle/busy 可调度，其他不可）
      iii. 检查 Agent 绑定的 Runtime.status（online 可用，offline/degraded 不可）
      iv.  检查 Runtime.current_load < Runtime.concurrency_limit
      v.   检查 Agent 执行权限（Agent Owner 角色 ≥ 操作风险级别，见 Account PRD 8.8）
      vi.  primary_assignee 不可用 → 遍历 fallback_agent_ids 按同样规则检查
      vii. 全部不可用 → Task → needs_attention
   d. 选中 Agent → 创建 Execution → Task → queued → assigned → running
```

### 6.5 Task 执行（Slot 驱动）

以 "产品 PRD 书写" 为例（collaboration_mode: `human_input_agent_exec` + review）：

```
Slots 定义：
  1. human_input  (Owner, before_execution, blocking)
  2. agent_execution (Agent A, during_execution, blocking)
  3. human_review (Owner, before_done, blocking)

执行流：

Task ready
  → Slot 1 (human_input) 激活 → status: ready
  → Task → needs_human
  → 系统创建 inbox_item 通知 Owner
  → Owner 提供需求约束 → Slot 1 → submitted → approved
  → Task → running
  → Slot 2 (agent_execution) 激活 → status: ready
  → 创建 Execution (agent=A, runtime=local-claude)
  → Execution: queued → claimed → running
  → Agent 执行完成 → Execution: completed
  → 创建 Artifact v1
  → Slot 2 → submitted
  → Slot 3 (human_review) 激活 → status: ready
  → Task → under_review
  → Owner Review:
    ├── approve → Slot 3 approved → Task completed ✓
    ├── request_changes → Slot 3 revision_requested → Task running → Agent 修订 → Artifact v2
    └── reject → Task needs_attention → Owner 决定下一步
```

### 6.6 DAG 调度

```
Task 1 (PRD)
  ↓
Task 2 (前端) ←── depends_on: [Task 1]
Task 3 (后端) ←── depends_on: [Task 1]
  ↓         ↓
Task 4 (测试) ←── depends_on: [Task 2, Task 3]

调度规则：
1. Task 1 completed → 检查 Task 2, Task 3 的 depends_on
2. Task 2, Task 3 全部依赖满足 → 两者并行调度
3. Task 2 completed → 检查 Task 4，Task 3 未完成 → 等待
4. Task 3 completed → Task 4 全部依赖满足 → 调度 Task 4
5. Task 4 completed → 所有 Task done → Run completed
```

### 6.7 失败与恢复

```
Execution 失败
  │
  ├── task.current_retry < retry_rule.max_retries?
  │     → 是: task.current_retry++ → 指数退避 → 新建 Execution (attempt=N+1)
  │     → 否: ↓
  │
  ├── 有可用 fallback_agent?（按 Account 层调度约束检查）
  │     → 是: 换 Agent → task.current_retry 重置为 0 → 新建 Execution
  │     → 否: ↓
  │
  └── Task → needs_attention
        → 创建 inbox_item (action_required=true, severity=critical)
        → 在项目 Channel 发消息通知 Agent Owner
        → Agent Owner 响应:
          ├── 重试 → Task → running → 新建 Execution
          ├── 换 Agent → 更新 actual_agent_id → 新建 Execution
          ├── 跳过 → Task → cancelled → 检查下游影响
          └── 取消 Run → Run → cancelled → 所有活跃 Task → cancelled
```

---

## 7. 实体关系总结

```
Project      1──N  ProjectVersion
ProjectVersion 1──N  Plan
Plan         1──N  Task
Plan         1──N  ProjectRun
Task         1──N  ParticipantSlot
Task         1──N  Execution
Task         1──N  Artifact
Artifact     1──N  Review

Project      1──1  Channel        (项目频道)
Plan         N──1  Thread         (讨论线程，Thread 属于项目 Channel)
Task         N──1  ProjectRun     (当前执行实例)
Execution    N──1  ProjectRun     (归属执行实例)
Execution    N──1  ParticipantSlot (触发该执行的 Slot)
Artifact     N──1  ParticipantSlot (产出该 Artifact 的 Slot)
Artifact     N──1  Execution      (产出该 Artifact 的执行)
Review       N──1  ParticipantSlot (对应的 review Slot)
```

---

## 8. 与现有模型的映射

### 8.1 替换关系

| 现有对象 | 新对象 | 处理方式 |
|---------|--------|---------|
| `workflow` | **删除** | 职责被 Plan + Task 承担 |
| `workflow_step` | **→ Task** | 字段迁移 + 扩展（Slot、Artifact 等） |
| `plan.steps` (JSONB) | **删除** | 由 Task 行记录替代，消除重复存储 |
| `workflow.dag` (JSONB) | **删除** | 由 Task.depends_on 替代 |
| `project_version.plan_snapshot` | **删除** | Plan 和 Task 直接作为行记录 |
| `project_version.workflow_snapshot` | **删除** | 同上 |

### 8.2 保留关系

| 现有对象 | 处理 |
|---------|------|
| `project` | 保留，字段不变 |
| `project_version` | 保留，简化（去掉 snapshot 字段） |
| `project_run` | 保留，`plan_id` FK 指向新 Plan |
| `plan` | 保留，重构（去掉 `steps` JSONB，加 `thread_id`、`version_id`） |
| `agent_task_queue` | 保留，继续服务 Issue 链路 |
| `inbox_item` | 保留，继续服务 ActionRequest 通知 |

### 8.3 新增对象

| 新对象 | 说明 |
|--------|------|
| `task` | 替代 workflow_step + plan.steps |
| `participant_slot` | 人机协作槽位（全新） |
| `execution` | Project 链路的执行尝试（独立于 agent_task_queue） |
| `artifact` | 版本化产物（全新） |
| `review` | 验收记录（全新） |

---

## 9. 服务层变化

### 9.1 服务映射

| 现有服务 | 变化 |
|---------|------|
| `SchedulerService` | 重构：从调度 WorkflowStep 改为调度 Task，增加 Slot 驱动逻辑 |
| `PlanGeneratorService` | 增强：生成 Plan 时同时生成 Task + Slot 定义 |
| `ProjectLifecycleService` | 增强：监控 Task/Execution 状态，处理 Slot 超时 |
| `TaskService` | 不变：继续服务 Issue 链路 |

### 9.2 新增服务

| 服务 | 职责 |
|------|------|
| `SlotService` | Slot 生命周期管理：激活、超时检测、状态推进 |
| `ArtifactService` | Artifact 创建、版本管理、headless ↔ file 转换 |
| `ReviewService` | Review 创建、决策处理、Task 状态回写 |

**ArtifactService 关键流程：**

```go
// 创建 Artifact（带物理文件）
func (s *ArtifactService) CreateWithFile(ctx, req CreateArtifactWithFileReq) (*Artifact, error) {
    return s.DB.InTx(ctx, func(tx) (*Artifact, error) {
        // 1. 创建 FileIndex（若不存在），access_scope='project' 且 project_id 对齐
        fi, err := s.FileService.UpsertIndex(tx, FileIndex{
            WorkspaceID: req.WorkspaceID,
            ProjectID:   req.ProjectID,
            AccessScope: "project",
            Path:        req.Path,
        })
        if err != nil { return nil, err }

        // 2. 创建 FileSnapshot（本次版本）
        fs, err := s.FileService.CreateSnapshot(tx, fi.ID, req.Content)
        if err != nil { return nil, err }

        // 3. 创建 Artifact，回填 FK
        return s.Queries.CreateArtifact(tx, CreateArtifactParams{
            TaskID:         req.TaskID,
            SlotID:         req.SlotID,
            ExecutionID:    req.ExecutionID,
            FileIndexID:    &fi.ID,
            FileSnapshotID: &fs.ID,
            RetentionClass: "permanent",
            // ...
        })
    })
}

// 创建 Headless Artifact（无物理文件）
func (s *ArtifactService) CreateHeadless(ctx, req CreateHeadlessReq) (*Artifact, error) {
    // file_index_id, file_snapshot_id = NULL
    // content 字段为权威内容
    return s.Queries.CreateArtifact(ctx, CreateArtifactParams{
        Content: req.Content,
        // ...
    })
}
```

**约束校验：**

| 校验 | 位置 |
|------|------|
| `file_index.project_id == artifact 所属 project_id` | ArtifactService 事务内 |
| `file_index.access_scope == 'project'` | 同上 |
| headless 时 `content IS NOT NULL` | CHECK 约束 |
| 有 FK 时 `content` 可空（以文件为权威） | 业务规则 |

### 9.3 调度器重构要点

**现有** `SchedulerService` 方法与新模型映射：

| 现有方法 | 新方法 | 变化 |
|---------|--------|------|
| `ScheduleWorkflow(workflowID, runID)` | `ScheduleRun(planID, runID)` | Workflow → Plan |
| `ScheduleStep(step, runID)` | `ScheduleTask(task, runID)` | Step → Task，增加 Slot 前置检查 |
| `HandleStepCompletion(stepID, result)` | `HandleTaskCompletion(taskID, result)` | 增加 Artifact 创建、Review Slot 激活 |
| `HandleStepFailure(stepID, errMsg)` | `HandleTaskFailure(taskID, errMsg)` | 逻辑不变 |
| `HandleStepTimeout(stepID)` | `HandleTaskTimeout(taskID)` | 逻辑不变 |
| `findAvailableAgent(step)` | `findAvailableAgent(task)` | 签名变化 |
| `checkWorkflowFailure(step)` | `checkRunFailure(task)` | Workflow → Run |

**新增调度逻辑：**

```
ScheduleTask(task, runID):
  1. 检查 before_execution slots:
     a. 有 blocking human_input slot → Task → needs_human → return
     b. 无 → 继续
  2. 找可用 Agent (primary → fallback)
  3. 创建 Execution (不再写 agent_task_queue)
  4. Task → queued → assigned
  5. Daemon 通过 Execution 表领取任务

HandleTaskCompletion(taskID, result):
  1. Execution → completed
  2. 创建 Artifact (若有产出)
  3. 激活 agent_execution slot → submitted
  4. 检查 before_done slots:
     a. 有 human_review slot → Task → under_review → return
     b. 无 → Task → completed
  5. 检查下游依赖 Task
  6. 检查 Run 是否全部完成
```

---

## 10. Execution 集成合约

### 10.1 双路径执行架构

```
                  SchedulerService
                       │
            创建 Execution (status: queued)
                       │
           ┌───────────┴───────────┐
           │                       │
    本地 Personal Agent        云端 Agent (Personal / System)
           │                       │
    Daemon 轮询 Execution      CloudExecutorService 轮询 Execution
           │                       │
    本地 CLI 执行               Claude Agent SDK 执行
    (claude/codex/opencode)    (带 MCP 工具)
           │                       │
    Daemon HTTP 回调            SDK session 回调
           │                       │
           └───────────┬───────────┘
                       │
              Execution → completed
              Artifact 创建 + Task 状态回写
```

### 10.2 Daemon 端点（本地执行）

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/daemon/runtimes/{runtimeId}/executions/pending` | GET | 查询 queued 状态的 Execution |
| `/api/daemon/runtimes/{runtimeId}/executions/claim` | POST | 原子领取（`FOR UPDATE SKIP LOCKED`），status: queued → claimed；**写入 context_ref**（local 模式：`{mode:"local", working_dir: Runtime.working_dir, daemon_id}`） |
| `/api/daemon/executions/{id}/start` | POST | status: claimed → running；可更新 context_ref（如实际 cwd 与 Runtime.working_dir 不同） |
| `/api/daemon/executions/{id}/progress` | POST | 流式上报进度/中间结果 |
| `/api/daemon/executions/{id}/complete` | POST | status: running → completed，含 result |
| `/api/daemon/executions/{id}/fail` | POST | status: running → failed，含 error |
| `/api/daemon/executions/{id}/messages` | POST | 流式消息上报 |

**context_ref 写入时序（local）：**

```
1. claim 时：Daemon 提交 {daemon_id, working_dir}
   → 后端写入 execution.context_ref = {mode:"local", working_dir, daemon_id}
2. start 时（可选）：若实际执行目录与 claim 时不同，Daemon 可更新 context_ref
3. 后续回调（progress/complete/fail）：context_ref 不再变更，作为本次执行的不可变物理事实
```

> Daemon 同时轮询两个来源：`agent_task_queue`（Issue 任务）和 `execution`（Project 任务）。优先级：相同 priority 时 Project Execution 优先。

**现有 Issue 端点保持不变：**

| 端点 | 说明 |
|------|------|
| `/api/daemon/runtimes/{runtimeId}/tasks/pending` | Issue 任务（agent_task_queue） |
| `/api/daemon/runtimes/{runtimeId}/tasks/claim` | Issue 任务领取 |
| `/api/daemon/tasks/{id}/start\|complete\|fail` | Issue 任务生命周期 |

### 10.3 CloudExecutorService（云端执行）

```go
// CloudExecutorService 是云端 Agent 的 "Daemon 等价物"
// 通过 Claude Agent SDK 执行任务

type CloudExecutorService struct {
    Queries    *db.Queries
    Hub        *realtime.Hub
    Bus        *events.Bus
    AgentSDK   *agentsdk.Client
}

// 轮询循环（服务端进程内运行）
func (s *CloudExecutorService) PollLoop(ctx context.Context) {
    // 1. 查询 status=queued 且 runtime.mode=cloud 的 Execution
    // 2. 原子领取 (FOR UPDATE SKIP LOCKED) → status=claimed
    // 3. 创建 SDK session + sandbox，写入 execution.context_ref:
    //    {
    //      mode: "cloud",
    //      sdk_session_id: <从 AgentSDK 创建返回>,
    //      sandbox_id: <从沙箱分配返回>,
    //      virtual_project_path: "/workspace/<project-slug>"
    //    }
    // 4. status=claimed → running，通过 Agent SDK 启动 session:
    //    - 注入 MCP 工具（get_issue, list_project_files, upload_artifact...）
    //    - 注入 Task 上下文（payload, acceptance_criteria）
    //    - MCP 工具调用回到平台后端 → 权限校验
    // 5. Session 完成 → 创建 Artifact + 回写 Execution/Task 状态
    // 6. 释放 sandbox（TTL 清理由 Gap3 定义的数据生命周期策略处理）
}
```

**context_ref 写入时序（cloud）：**

```
1. claim 时：CloudExecutorService 领取 Execution → 写入 context_ref = {mode:"cloud", ...预留字段}
2. 分配 SDK session / sandbox：
   → 调用 AgentSDK.CreateSession() → 获得 sdk_session_id
   → 调用 SandboxService.Allocate() → 获得 sandbox_id
   → 更新 execution.context_ref 填充 sdk_session_id、sandbox_id、virtual_project_path
3. start 时：status=claimed → running
4. 执行完成或失败：context_ref 保留用于审计和日志查询
5. sandbox 释放：按 retention_class / logs_expires_at 清理（见 §11 数据生命周期）
```

### 10.4 Execution 状态回调链

```
Execution completed
  → 更新 execution.status, result, completed_at
  → 创建 Artifact (若有产出)
  → 更新 Slot: agent_execution → submitted
  → 更新 Runtime: current_load -= 1
  → 推进 Task 状态（检查 review slot）
  → 检查下游依赖 Task
  → 检查 Run 是否全部完成
  → 发布 WebSocket 事件
```

### 10.5 Slot/Task 历史审计

Task/Slot 状态在新 Run 时重置，历史通过以下记录追溯：

| 记录类型 | 存储位置 | 说明 |
|---------|---------|------|
| Agent 执行历史 | `execution` 表（含 run_id） | 每次执行尝试的完整记录 |
| 产物历史 | `artifact` 表（含 run_id） | 所有版本的 Artifact |
| 验收历史 | `review` 表 | 所有 Review 决策 |
| Slot 状态变更 | `activity_log` | Slot 激活、提交、审批、超时等关键事件写入 activity_log |
| Task 状态变更 | `activity_log` | Task 每次状态转换写入 activity_log |

> **Slot/Task 不需要独立的 event 表。** 复用现有 `activity_log`，通过 `event_type` 区分（如 `slot:activated`、`slot:submitted`、`task:status_changed`）。查询模式：`SELECT * FROM activity_log WHERE related_project_id = X AND event_type LIKE 'slot:%' ORDER BY created_at`。

### 10.6 inbox_item 扩展

现有 inbox_item 仅有 `issue_id` FK。Project 链路需要扩展：

| 新增字段 | 类型 | 说明 |
|---------|------|------|
| `task_id` | UUID FK → task | 触发通知的 Task |
| `slot_id` | UUID FK → participant_slot | 需要响应的 Slot |
| `plan_id` | UUID FK → plan | 关联的 Plan |

**通知类型枚举（inbox_item.type）：**

| type 值 | 场景 | 触发时机 |
|---------|------|---------|
| `human_input_needed` | human_input slot 激活 | Task → needs_human |
| `review_needed` | human_review slot 激活 | Task → under_review |
| `task_attention_needed` | Task 需要人工介入 | Task → needs_attention |
| `plan_approval_needed` | Plan 提交审批 | Plan → pending_approval |
| `run_completed` | Run 执行完成 | Run → completed |
| `run_failed` | Run 执行失败 | Run → failed |

---

## 11. 数据生命周期

### 11.1 三类保留策略

| retention_class | 含义 | 是否可删除 |
|----------------|------|----------|
| `permanent` | 永久保留，审计依据 | 否（仅归档） |
| `ttl` | 按 `expires_at` / 策略窗口清理 | 是（到期后） |
| `temp` | 短期临时，可随时清理 | 是（按需） |

### 11.2 对象归类

| 对象 | 默认 retention_class | 说明 |
|------|---------------------|------|
| `artifact`（approved） | `permanent` | Review 批准后的产物，审计必需 |
| `artifact`（rejected / draft 超期） | `ttl` | 未批准的历史版本，可设 TTL 清理 |
| `review` | `permanent` | Review 决策记录 |
| `execution`（元数据） | `permanent` | 状态、耗时、成本等指标 |
| `execution`（原始日志） | `ttl` 默认 90d | `log_retention_policy` + `logs_expires_at` 控制 |
| `execution.context_ref`（cloud sandbox） | `ttl` | sandbox 资源随 Execution 结束释放 |
| `activity_log` | `permanent` | 完整审计链 |
| `thread_context_item`（临时条目） | `ttl` | 自动摘要、过期的 file 引用等 |
| `file_index` / `file_snapshot`（Artifact 关联） | 跟随 Artifact | 与 Artifact 的 retention_class 联动 |
| `file_index` / `file_snapshot`（未关联上传） | `ttl` | 未关联 Artifact 的临时上传 |

### 11.3 联动规则

| 规则 | 说明 |
|------|------|
| Artifact permanent → FileIndex 不可删 | Artifact 保留时，关联的 FileIndex/FileSnapshot 也必须保留 |
| Artifact ttl 到期 → 联动删 FileIndex | 通过 FK 联动删除，避免悬挂引用 |
| Execution 日志清理不影响元数据 | 清理 logs 只清理附属日志文件 / stdout-stderr 存储，execution 行保留 |
| cloud sandbox 释放独立于 context_ref | sandbox 生命周期结束后，`sandbox_id` 字段仍保留在 context_ref 中作为历史标识 |
| ThreadContextItem 引用 Artifact | ThreadContextItem 过期时只删引用关系，不删已批准 Artifact |

### 11.4 MVP vs Post-MVP

| 范围 | 内容 |
|------|------|
| **MVP** | Schema 字段预留：artifact.retention_class、execution.log_retention_policy、execution.logs_expires_at |
| **MVP** | 创建时写入默认策略（Artifact permanent、Execution 90d） |
| **MVP** | Artifact 与 FileIndex 的同事务创建和 FK 一致性 |
| **Post-MVP** | 后台清理作业（按 `logs_expires_at` 清理 Execution 原始日志） |
| **Post-MVP** | ThreadContextItem 自动过期策略（见 Session PRD §7） |
| **Post-MVP** | 未关联上传文件的 GC 作业 |
| **Post-MVP** | retention_class 策略 UI（允许手动调整） |

> **为什么 MVP 要预留字段：** 清理作业可延后，但字段上线后再加需要数据回填和默认值迁移。现在零成本预留，避免后续 breaking schema change。

---

## 12. MVP 范围

### 10.1 包含

| 项目 | 说明 |
|------|------|
| Project / Version / Plan / Task / Slot 完整模型 | 五层层级 + 所有状态机 |
| Execution 表 | 替代 AgentTaskQueue 的 Project 链路 |
| Artifact / Review | 产物版本化 + 验收闭环 |
| Project ↔ Channel 绑定 | 双向创建 |
| Plan ↔ Thread 绑定 | 从 Thread 生成 Plan |
| 3 种 Slot 类型 | human_input / agent_execution / human_review |
| 4 种协作模式 | 模板化 |
| DAG 调度 | 基于 Task.depends_on |
| 重试 / Fallback / 升级 | 延续现有 SchedulerService 逻辑 |
| Task 三种等待态 | needs_human / under_review / needs_attention |

### 10.2 不包含

| 项目 | 说明 |
|------|------|
| 通用 DAG 编辑器 UI | 第一阶段用列表 + 依赖选择 |
| 跨 Owner 协作 | Plan 内 Task 仅分配 Owner 自己的 Agent |
| Slot 自定义类型 | 仅支持 3 种内置类型 |
| Artifact 多人实时共编 | Turn-based，不做实时 |
| 复杂审批链 | 单人审批，不做多人会签 |
| 循环/定时项目的 Plan 自动生成 | 手动创建 |

---

## 13. 端到端样例

### 项目：SaySo v1.0 — 增加登录方式

**Step 1：创建 Project**

```
Project:
  title: "SaySo"
  status: draft
  channel_id: "proj-sayso-abc123" (自动创建)

Version:
  version_label: "1.0"
  status: active
```

**Step 2：Thread 讨论 → 生成 Plan**

```
Thread (in "proj-sayso-abc123"):
  "我们需要支持微信登录和手机号登录..."
  "考虑到安全性，手机号要加验证码..."
  "前端用现有组件库，后端新增 auth provider..."

Plan:
  title: "增加登录方式"
  thread_id: <上述 Thread ID>
  task_brief: "为 SaySo 增加微信登录和手机号验证码登录"
  approval_status: draft
```

**Step 3：Plan 审批**

```
Owner 审阅 → approval_status: approved
```

**Step 4：启动执行**

```
ProjectRun #1: status: running

Task 1: "编写产品 PRD"
  step_order: 1
  depends_on: []
  collaboration_mode: human_input_agent_exec
  primary_assignee: Agent-Writer
  Slots:
    1. human_input (Owner, before_execution, blocking) — 提供需求约束
    2. agent_execution (Agent-Writer, during_execution, blocking) — 撰写 PRD
    3. human_review (Owner, before_done, blocking) — 验收 PRD

Task 2: "前端开发"
  step_order: 2
  depends_on: [Task 1]
  collaboration_mode: agent_exec_human_review
  primary_assignee: Agent-Frontend
  Slots:
    1. agent_execution (Agent-Frontend, during_execution, blocking)
    2. human_review (Owner, before_done, blocking)

Task 3: "后端开发"
  step_order: 3
  depends_on: [Task 1]
  collaboration_mode: agent_exec_human_review
  primary_assignee: Agent-Backend

Task 4: "集成测试"
  step_order: 4
  depends_on: [Task 2, Task 3]
  collaboration_mode: agent_exec_human_review
  primary_assignee: Agent-QA
```

**Step 5：执行流**

```
Run starts
  → Task 1 ready → Slot 1 (human_input) activated → Task 1: needs_human
  → Owner 提供需求 → Slot 1: approved → Task 1: running
  → Slot 2 (agent_execution) activated
  → Execution #1: Agent-Writer on local-claude → running
  → Agent-Writer 完成 → Execution #1: completed
  → Artifact v1: "SaySo 登录方式 PRD"
  → Slot 2: submitted → Slot 3 (human_review) activated → Task 1: under_review
  → Owner Review: request_changes ("补充微信开放平台接入流程")
  → Slot 3: revision_requested → Task 1: running
  → Execution #2: Agent-Writer → Artifact v2: "PRD 修订版"
  → Slot 3 重新激活 → Task 1: under_review
  → Owner Review: approve
  → Slot 3: approved → Task 1: completed ✓

  → Task 2 依赖满足 → queued → assigned → running (并行)
  → Task 3 依赖满足 → queued → assigned → running (并行)
  → Task 2 completed ✓
  → Task 3 Agent-Backend 离线 → Execution failed → retry → fallback Agent
  → Task 3 completed ✓

  → Task 4 依赖满足 → queued → running → completed ✓

  → 所有 Task completed → Run completed → Project 状态更新
```

**系统保留的完整记录：**

- 4 个 Task 的全部状态变迁历史
- 每个 Slot 的激活/提交/审批时间线
- 所有 Execution 记录（含重试、Agent 切换）
- Artifact 版本链（v1 → v2）
- Review 决策链（request_changes → approve）
- inbox_item 通知记录
- 项目 Channel + Plan Thread 的完整讨论历史
