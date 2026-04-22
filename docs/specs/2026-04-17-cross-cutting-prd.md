# 跨模块补充 PRD — 事件、审计、服务契约、运维

> Version: 1.0 | Date: 2026-04-17

---

## 1. 背景与范围

三份主 PRD（Project / Account / Session-Channel）引用了大量服务、事件、审计、工具，但未给出完整定义。本 PRD 收集这些跨模块关注点，作为三份主 PRD 的 **配套规格**，不重复已定义内容。

**本 PRD 覆盖：**

| # | 主题 | 对应章节 |
|---|------|---------|
| 1 | 事件目录（WebSocket & 事件总线） | §2 |
| 2 | Activity Log 审计日志 | §3 |
| 3 | PlanGeneratorService | §4 |
| 4 | MediationService | §5 |
| 5 | CloudExecutorService 详细规格 | §6 |
| 6 | MCP 工具契约 | §7 |
| 7 | Inbox / 通知模型 | §8 |
| 8 | Agent 匹配与评分 | §9 |
| 9 | DAG 校验 | §10 |
| 10 | 密钥与凭证管理 | §11 |
| 11 | 成本与配额 | §12 |
| 12 | 错误码分类 | §13 |
| 13 | 可观测性 | §14 |

**与主 PRD 的关系：**

| 主 PRD | 本 PRD 补充 |
|--------|------------|
| Project PRD | §4 PlanGenerator, §6 CloudExecutor, §8 Inbox, §9 Agent 匹配, §10 DAG 校验 |
| Account PRD | §7 MCP 工具契约, §11 密钥 |
| Session PRD | §5 MediationService |
| 三者共用 | §2 事件目录, §3 Activity Log, §12 成本, §13 错误码, §14 可观测性 |

---

## 2. 事件目录（WebSocket & 事件总线）

三份主 PRD 多处 "publish event" / "发布 WebSocket 事件"，本节给出完整目录。

### 2.1 分发机制

```
业务状态变更
  │
  ├── 内部事件总线 (events.Bus)      → 同进程服务订阅（如 SchedulerService 订阅 slot:submitted）
  │
  └── WebSocket Hub (realtime.Hub)   → 前端订阅（通过 channel_id / user_id 路由）
```

**约定：**

| 命名 | 格式 | 示例 |
|------|------|------|
| event_type | `<domain>:<action>` 小写+下划线 | `task:status_changed`, `plan:created` |
| 字段 | 驼峰 JSON + top-level envelope | `{ event, workspace_id, payload }` |
| 时间戳 | ISO 8601 UTC | `"2026-04-17T10:30:00Z"` |

### 2.2 事件清单

#### Project 域

| event | 触发 | 主要字段 | 订阅方 |
|-------|------|---------|-------|
| `project:created` | Project 创建 | project_id, channel_id, creator_owner_id | 前端、System Agent |
| `project:status_changed` | status 变更 | project_id, from, to | 前端 |
| `plan:created` | Plan 创建 | plan_id, project_id, thread_id | 前端 |
| `plan:approval_changed` | approval_status 变更 | plan_id, from, to, actor_id | 前端、SchedulerService |
| `run:started` | ProjectRun 启动 | run_id, plan_id | 前端 |
| `run:completed` / `run:failed` / `run:cancelled` | Run 终态 | run_id, plan_id, reason | 前端 |
| `task:status_changed` | Task 状态变更 | task_id, run_id, from, to | 前端、SchedulerService |
| `task:agent_assigned` | Agent 分配 | task_id, agent_id | 前端 |
| `slot:activated` | Slot waiting→ready | slot_id, task_id, slot_type | 前端、InboxService |
| `slot:submitted` | Slot → submitted | slot_id, task_id | SchedulerService、前端 |
| `slot:decision` | Slot approved/revision_requested/rejected | slot_id, review_id, decision | SchedulerService、前端 |
| `execution:claimed` | Execution → claimed | execution_id, runtime_id, agent_id | 前端 |
| `execution:started` | Execution → running | execution_id, context_ref | 前端 |
| `execution:completed` / `execution:failed` | 终态 | execution_id, result / error | SchedulerService、前端 |
| `execution:progress` | 流式进度 | execution_id, progress_payload | 前端 |
| `artifact:created` | Artifact 创建 | artifact_id, task_id, version | 前端 |
| `review:submitted` | Review 创建 | review_id, artifact_id, decision | 前端、SchedulerService |

#### Account 域

| event | 触发 | 主要字段 | 订阅方 |
|-------|------|---------|-------|
| `agent:created` | Agent 创建 | agent_id, owner_id, agent_type | 前端 |
| `agent:status_changed` | status 变更 | agent_id, from, to | 前端、SchedulerService |
| `agent:identity_card_updated` | identity_card 变更 | agent_id, updated_fields | 前端 |
| `runtime:online` / `runtime:offline` / `runtime:degraded` | status 变更 | runtime_id, status | 前端、SchedulerService |
| `impersonation:started` | impersonation_session 创建 | session_id, owner_id, agent_id | activity_log 写入 |
| `impersonation:ended` | session 结束 | session_id | 同上 |

#### Session / Channel 域

| event | 触发 | 主要字段 | 订阅方 |
|-------|------|---------|-------|
| `channel:created` | Channel 创建 | channel_id, workspace_id | 前端 |
| `channel:member_added` / `channel:member_removed` | 成员变更 | channel_id, member_id | 前端 |
| `thread:created` | Thread 创建 | thread_id, channel_id, root_message_id | 前端 |
| `thread:status_changed` | status 变更 | thread_id, from, to | 前端 |
| `message:created` | 消息发送 | message_id, channel_id, thread_id, sender_id | 前端、MediationService |
| `message:updated` / `message:deleted` | 消息变更 | message_id | 前端 |
| `thread_context_item:created` / `deleted` | Context 条目变更 | item_id, thread_id | 前端 |

#### Inbox 域

| event | 触发 | 主要字段 |
|-------|------|---------|
| `inbox:item_created` | InboxItem 创建 | item_id, recipient_id, type |
| `inbox:item_read` | 已读 | item_id |
| `inbox:item_resolved` | 已处理 | item_id, resolution |

### 2.3 WebSocket 消息信封

```json
{
  "event": "task:status_changed",
  "workspace_id": "<uuid>",
  "timestamp": "2026-04-17T10:30:00Z",
  "payload": {
    "task_id": "<uuid>",
    "run_id": "<uuid>",
    "from": "running",
    "to": "under_review"
  },
  "routing": {
    "channel_ids": ["<uuid>"],
    "user_ids": ["<uuid>"]
  }
}
```

`routing` 由 Hub 计算，前端无需感知。

### 2.4 事件可靠性

| 级别 | 机制 |
|------|------|
| WebSocket 推送 | best-effort，断线重连后前端主动拉取最新状态（不依赖事件补发） |
| 内部事件总线 | 同进程 channel 广播，订阅者错误不影响发布者 |
| 持久化审计 | 所有关键事件同时写入 `activity_log`（见 §3），断连期间的真相在 activity_log |

---

## 3. Activity Log 审计日志

### 3.1 Schema

```sql
CREATE TABLE activity_log (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  event_type          TEXT NOT NULL,            -- 见 §3.2 枚举
  actor_id            UUID,                     -- 操作发起者（user / agent / system）
  actor_type          TEXT,                     -- 'member' | 'agent' | 'system'
  effective_actor_id  UUID,                     -- impersonation 时的显示身份
  effective_actor_type TEXT,
  real_operator_id    UUID,                     -- impersonation 时的实际操作者
  real_operator_type  TEXT,
  related_project_id  UUID,                     -- 关联 project（冗余便于查询）
  related_plan_id     UUID,
  related_task_id     UUID,
  related_slot_id     UUID,
  related_execution_id UUID,
  related_channel_id  UUID,
  related_thread_id   UUID,
  related_agent_id    UUID,
  related_runtime_id  UUID,
  payload             JSONB NOT NULL DEFAULT '{}',  -- 事件特定数据
  retention_class     TEXT NOT NULL DEFAULT 'permanent',
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_activity_log_workspace_time ON activity_log(workspace_id, created_at DESC);
CREATE INDEX idx_activity_log_project ON activity_log(related_project_id, created_at DESC);
CREATE INDEX idx_activity_log_task ON activity_log(related_task_id, created_at DESC);
CREATE INDEX idx_activity_log_event_type ON activity_log(workspace_id, event_type, created_at DESC);
```

### 3.2 event_type 枚举

```
# Project / Plan / Run
project:created / status_changed / archived
plan:created / approval_changed / updated / rejected
run:started / completed / failed / cancelled

# Task / Slot / Execution
task:status_changed / agent_assigned / agent_reassigned / retried / cancelled
slot:activated / submitted / approved / revision_requested / rejected / expired / skipped
execution:created / claimed / started / completed / failed / timed_out / cancelled

# Artifact / Review
artifact:created / archived
review:submitted / decision_applied

# Account
agent:created / updated / archived / suspended / resumed
agent:identity_card_auto_generated / manual_edit
runtime:registered / heartbeat_missed / online / offline / degraded / removed
impersonation:started / ended / message_sent

# Session / Channel
channel:created / archived / member_added / member_removed
thread:created / archived
thread_context_item:created / deleted

# Inbox
inbox:item_created / item_resolved
```

### 3.3 写入约定

| 规则 | 说明 |
|------|------|
| 服务层写入 | 不在 handler 层写；SchedulerService / ArtifactService / SlotService 在事务内写入 |
| 单条事务 | activity_log 与业务表写入在**同一事务**，保证一致性 |
| 不阻塞主流程 | 写入失败 = 事务回滚；不容忍"审计丢失" |
| 查询隔离 | 对外 API 按 `workspace_id + 对象级权限` 过滤，member 只能看自己相关 |

### 3.4 查询 API

| 端点 | 说明 |
|------|------|
| `GET /api/activity-log?project_id=X` | 查项目维度审计流 |
| `GET /api/activity-log?task_id=X` | 查 Task 完整历史 |
| `GET /api/activity-log?event_type=slot:%25` | 按前缀过滤 |
| `GET /api/activity-log?actor_id=X` | 查某人操作记录（admin 可跨人） |

### 3.5 与 WebSocket 事件的关系

| 维度 | WebSocket 事件 | Activity Log |
|------|---------------|--------------|
| 持久性 | 非持久 | 永久（`retention_class='permanent'`） |
| 送达 | best-effort | 保证一致 |
| 用途 | 前端实时 UI | 审计、历史回放、Debug |
| 订阅 | WS 连接 | API 查询 |

**设计准则：** 所有写入 activity_log 的事件也应该发布 WebSocket 事件（如果前端关心）；但 WebSocket 事件不一定都要写 activity_log（如低价值的进度流）。

---

## 4. PlanGeneratorService

从 Thread 讨论生成 Plan + Task + Slot 的 LLM 服务。

### 4.1 职责边界

| 范围 | 说明 |
|------|------|
| ✅ 输入 | Thread 消息、项目上下文、可用 Agent 的 Identity Card |
| ✅ 输出 | Plan 草稿（title、task_brief、Task 列表、Slot 定义、依赖关系） |
| ✅ 产出验证 | DAG 环检查、Agent 能力覆盖检查、Slot 类型合法性 |
| ❌ 不做 | Plan 审批、Execution 调度、Artifact 创建 |

### 4.2 接口

```go
type PlanGeneratorService struct {
    Queries  *db.Queries
    LLM      LLMClient
    Registry *provider.Registry
}

type GeneratePlanRequest struct {
    ThreadID       uuid.UUID
    VersionID      uuid.UUID
    CreatedBy      uuid.UUID
    ExtraHints     string  // Owner 可选的额外约束
}

type GeneratePlanResult struct {
    Plan       PlanDraft
    Tasks      []TaskDraft
    Slots      []SlotDraft
    Warnings   []string  // 能力不足、Agent 不可用等软警告
    TokenUsage TokenUsage
}

func (s *PlanGeneratorService) Generate(ctx, req) (*GeneratePlanResult, error)
```

### 4.3 生成流程

```
1. 收集上下文
   a. Thread 消息（全量，由 ThreadContextItem 补充结构化信息）
   b. Project 基本信息 + 现有 Plan 摘要
   c. Workspace 内可用 Agent 列表 + identity_card（只看本 User 可用的）

2. 构造 LLM 提示
   a. System prompt: "你是项目计划助手..."
   b. Context: 步骤 1 汇总
   c. Output schema: JSON schema 约束输出结构

3. 调用 LLM（优先使用 scope='project' 的 System Agent）
   a. 一次调用，温度较低（0.2-0.4）
   b. 流式返回前端（显示生成进度）

4. 解析并验证输出
   a. JSON schema 校验
   b. DAG 环检查（见 §10）
   c. Agent 能力覆盖：每个 Task 的 required_skills 是否被 primary_assignee 的 identity_card.skills 覆盖
   d. Slot 类型合法性：collaboration_mode 与 Slot 序列匹配

5. 返回结构化 draft（不落库，由 handler 保存）
```

### 4.4 LLM 输出 Schema

```jsonc
{
  "plan": {
    "title": "增加登录方式",
    "task_brief": "...",
    "expected_output": "...",
    "constraints": "..."
  },
  "tasks": [
    {
      "local_id": "T1",
      "title": "产品 PRD 书写",
      "step_order": 1,
      "depends_on": [],
      "primary_assignee_agent_id": "<uuid>",
      "fallback_agent_ids": ["<uuid>"],
      "required_skills": ["product_writing"],
      "collaboration_mode": "human_input_agent_exec",
      "acceptance_criteria": "..."
    }
  ],
  "slots": [
    {
      "task_local_id": "T1",
      "slot_type": "human_input",
      "slot_order": 1,
      "participant_type": "member",
      "participant_id": "<user_id>",
      "trigger": "before_execution",
      "blocking": true
    }
  ]
}
```

### 4.5 错误模式

| 错误 | 处理 |
|------|------|
| LLM 返回非 JSON / schema 不合法 | 重试 1 次，仍失败 → 返回 `PLAN_GENERATION_MALFORMED` |
| DAG 含环 | 返回 `PLAN_DAG_CYCLE` + cycle 路径 |
| Agent 能力不足 | 降级为 warning，Plan 仍创建，UI 高亮提示 |
| LLM 超时 | 返回 `PLAN_GENERATION_TIMEOUT` |

### 4.6 成本控制

单次 Plan 生成最大 token 上限：50k input + 4k output（可配）。超过则截断 Thread（保留 root message + 最近 N 条 + Summary context item）。

---

## 5. MediationService 会话调解服务

### 5.1 职责

| 职责 | 说明 |
|------|------|
| 消息路由 | 根据 @提及、能力匹配、SLA 规则分配回复者 |
| 自动摘要 | 定期生成 Thread 摘要（写入 ThreadContextItem） |
| SLA 升级 | 回复超时升级为通知 |
| 防噪声 | 防洪、防循环自动回复 |

> MediationService 是服务端组件，依赖 `scope='conversation'` 的 System Agent 执行 LLM 任务。

### 5.2 路由决策流

```
Message 创建
  │
  ├── 是系统/AI 摘要消息？
  │     → 是：不触发路由
  │     → 否：↓
  │
  ├── 含 @mention？
  │     → 是：直接分配给 mentioned actor（如在线），创建 reply_slot
  │     → 否：↓
  │
  ├── Thread 关联 Plan？
  │     → 是：分配给 Plan 相关 Agent（primary_assignee）
  │     → 否：↓
  │
  ├── Thread 关联 Issue？
  │     → 是：分配给 Issue assignee
  │     → 否：↓
  │
  └── 能力匹配（回退）
        根据 Thread context + Channel 内 Agent identity_card 打分
        选择 top-1 Agent
```

### 5.3 reply_slot（新概念）

轻量 reply 槽，表示"某个 Agent 欠一个回复"：

```sql
CREATE TABLE reply_slot (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  thread_id       UUID NOT NULL REFERENCES thread(id) ON DELETE CASCADE,
  channel_id      UUID NOT NULL,
  trigger_message_id UUID,
  assigned_to     UUID NOT NULL,
  assigned_type   TEXT NOT NULL,  -- 'member' | 'agent'
  status          TEXT NOT NULL DEFAULT 'pending',  -- pending/responded/expired/escalated
  sla_tier        TEXT NOT NULL DEFAULT 'fallback', -- fallback/warning/critical
  responded_at    TIMESTAMPTZ,
  expires_at      TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_reply_slot_pending ON reply_slot(status, expires_at)
  WHERE status = 'pending';
```

### 5.4 SLA 升级时序

| 阶段 | 默认时长 | 动作 |
|------|---------|------|
| 创建 | T+0 | reply_slot (status=pending, sla_tier=fallback) |
| fallback 升级 | T+300s | 若 Agent 未响应，转给备选 Agent |
| warning 升级 | T+600s | 通知 Owner（inbox_item: type=reply_slow） |
| critical 升级 | T+900s | 通知 admin + 记录 SLA 违约 |

### 5.5 防循环规则

| 规则 | 说明 |
|------|------|
| Agent 不回复 Agent 超 3 轮 | 超过 3 次连续 agent→agent 消息，停止自动回复，通知人工 |
| 24h 频率限制 | 同一 Agent 对同一 Thread 自动回复 ≤ 50 次/24h |
| 自我对话禁止 | Agent 不触发对自己消息的回复 |

### 5.6 auto_reply_config 统一入口

Agent 上的 `auto_reply_config` 仅作为**输入**，所有自动回复**必须经过 MediationService 决策**。message handler 不直接触发 Agent 回复。

---

## 6. CloudExecutorService 详细规格

扩展 Project PRD §10.3 的简述。

### 6.1 职责

| 职责 | 说明 |
|------|------|
| 轮询 cloud Execution | 查询 `status='queued' AND runtime.mode='cloud'` |
| SDK session 管理 | 创建、维护、关闭 Claude Agent SDK session |
| Sandbox 管理 | 分配/释放沙箱资源 |
| MCP 工具转发 | Agent 调用 MCP 工具时转回平台后端 |
| Lease 续约 | 防止任务被重复领取 |
| 失败恢复 | 进程重启后恢复未完成任务 |

### 6.2 架构

```
                      CloudExecutorService (服务进程)
                                │
           ┌────────────────────┼────────────────────┐
           │                    │                    │
    轮询器 (Poller)       Session 管理器      Lease 监控器
           │                    │                    │
           ▼                    ▼                    ▼
    query queued Exec    AgentSDK.Session    刷新 lease_expires_at
    FOR UPDATE SKIP      Sandbox Pool       清理 stale lease
```

### 6.3 Session 生命周期

```
1. claim
   a. SELECT ... FROM execution WHERE status='queued' AND ... FOR UPDATE SKIP LOCKED LIMIT 1
   b. UPDATE execution SET status='claimed', lease_expires_at=now()+5min
   c. INSERT INTO activity_log (event_type='execution:claimed')

2. allocate_session
   a. 创建 SDK session: sdkClient.CreateSession({ agent_id, task_payload })
   b. 分配 sandbox: sandboxPool.Allocate({ execution_id })
   c. UPDATE execution SET context_ref = {...}

3. start
   a. UPDATE execution SET status='running', started_at=now()
   b. session.Run(prompt) → 异步执行

4. stream (期间)
   a. MCP 工具调用 → 后端权限校验 → 返回数据
   b. 进度上报 → execution:progress WS 事件
   c. 消息上报 → message:created WS 事件
   d. Lease 定期续约（每 2 分钟）

5. complete / fail
   a. session.Result 返回
   b. 创建 Artifact（如有）
   c. UPDATE execution SET status='completed'/'failed', completed_at=now()
   d. 释放 sandbox（或延后 TTL 清理）
   e. 关闭 SDK session
```

### 6.4 崩溃恢复

```
服务重启后：
  1. SELECT * FROM execution WHERE status='claimed' AND lease_expires_at < now()
  2. 对每条过期 lease 的 Execution:
     a. SDK session 是否还在？查 session_registry
        - 在 → 重新接管（继续监听 message/result channel）
        - 不在 → UPDATE status='failed', error='lease_expired_orphan'
     b. Sandbox 是否还在？
        - 在 → 释放
        - 不在 → 无动作
  3. 写入 activity_log
```

### 6.5 配置

| 参数 | 默认 | 说明 |
|------|------|------|
| `poll_interval` | 2s | 轮询间隔 |
| `max_concurrent_sessions` | 20 | 服务端并发上限 |
| `lease_duration` | 5min | 单次 lease |
| `lease_renewal` | 2min | 续约间隔 |
| `sandbox_idle_ttl` | 30min | 空闲 sandbox 回收 |
| `session_timeout` | 30min | 单次 session 上限（与 Task.timeout_rule 取小） |

### 6.6 配额与限流

| 维度 | 默认上限 | 说明 |
|------|---------|------|
| workspace 并发 cloud execution | 10 | Account 层可配 |
| User 并发 cloud execution | 3 | MVP 固定 |
| SDK token/分钟 | 100k | 由 §12 成本配额控制 |

---

## 7. MCP 工具契约

Account PRD §8.8 列出 17 个工具，本节给出契约（input schema + output schema + 权限检查）。

### 7.1 通用规则

| 规则 | 说明 |
|------|------|
| 工具调用方 | Claude Agent SDK（cloud）或 myteam CLI（local） |
| 鉴权 | `workspace_id` + `user_id` + `agent_id`（从 SDK session / CLI token 解出） |
| 权限检查位置 | 后端 handler 层，每次调用都查 |
| 错误格式 | `{ code, message, retriable }` |
| 审计 | 每次调用写 activity_log：`mcp_tool:<name>` |

### 7.2 工具清单（契约摘要）

| 工具 | Input 关键字段 | Output 关键字段 | 权限检查 |
|------|--------------|---------------|---------|
| `get_issue` | issue_id | issue 完整结构 | User 可见 Issue |
| `list_issue_comments` | issue_id, limit, offset | comment 数组 | 同上 |
| `create_comment` | issue_id, body | comment | User 有 Issue 评论权限 |
| `update_issue_status` | issue_id, status | updated issue | User 有 Issue 编辑权限 |
| `list_assigned_projects` | status filter | project 数组 | User 分配的 Project |
| `get_project` | project_id | project 完整结构 | User 可见 Project |
| `search_project_context` | project_id, query | context item 数组 | 同上 |
| `list_project_files` | project_id, path_prefix | file_index 数组 | 同上 |
| `download_attachment` | attachment_id | bytes + metadata | Channel 成员 或 Attachment 所在上下文可见 |
| `upload_artifact` | task_id, slot_id, execution_id, content/file | artifact | Agent 为 Task 的 actual_agent_id 或 primary_assignee |
| `complete_task` | task_id, result | updated task | 同上 |
| `request_approval` | task_id, slot_id, context | inbox_item | 同上 |
| `read_file` | project_id, file_index_id / path | file content | User 可见 |
| `apply_patch` | project_id, patch | commit_ref | User 有 Project 写权限 + risk check |
| `create_pr` | project_id, branch, title, body | pr_url | 同上 |
| `checkout_repo` | project_id | cwd path | **仅 local** |
| `local_file_read` | path | content | **仅 local** + Daemon 允许路径 |

### 7.3 工具定义示例（`upload_artifact`）

```jsonc
// Input Schema
{
  "type": "object",
  "required": ["task_id", "slot_id", "execution_id"],
  "properties": {
    "task_id": { "type": "string", "format": "uuid" },
    "slot_id": { "type": "string", "format": "uuid" },
    "execution_id": { "type": "string", "format": "uuid" },
    "artifact_type": {
      "type": "string",
      "enum": ["document", "design", "code_patch", "report", "file", "plan_doc"]
    },
    "title": { "type": "string" },
    "summary": { "type": "string" },
    "content": { "type": "object" },         // headless 时必填
    "file": {
      "type": "object",
      "properties": {
        "path": { "type": "string" },
        "content_b64": { "type": "string" }
      }
    }                                         // 带文件时必填
  },
  "oneOf": [{ "required": ["content"] }, { "required": ["file"] }]
}

// Output Schema
{
  "type": "object",
  "properties": {
    "artifact_id": { "type": "string", "format": "uuid" },
    "version": { "type": "integer" },
    "file_index_id": { "type": "string", "format": "uuid" },
    "file_snapshot_id": { "type": "string", "format": "uuid" }
  }
}
```

### 7.4 本地 vs 云端差异

| 工具 | 云端行为 | 本地行为 |
|------|---------|---------|
| `read_file` | HTTP 请求平台后端 | CLI 内部复用相同 API |
| `apply_patch` | 云端 checkout + commit + push | 本地 git 操作 + Daemon 上报 |
| `checkout_repo` | **拒绝**，返回 `TOOL_NOT_AVAILABLE_IN_CLOUD` | 本地 Daemon 分配工作目录 |
| `local_file_read` | **拒绝**，同上 | Daemon 允许路径内读取 |

---

## 8. Inbox / 通知模型

Project PRD §10.6 扩展了字段，本节完整定义。

### 8.1 Schema 扩展

现有 inbox_item 假设：
```sql
-- 已有字段（假设）
id, workspace_id, recipient_id, issue_id, type, title, body, read_at, created_at
```

**新增字段：**

```sql
ALTER TABLE inbox_item
  ADD COLUMN plan_id UUID REFERENCES plan(id) ON DELETE CASCADE,
  ADD COLUMN task_id UUID REFERENCES task(id) ON DELETE CASCADE,
  ADD COLUMN slot_id UUID REFERENCES participant_slot(id) ON DELETE CASCADE,
  ADD COLUMN thread_id UUID REFERENCES thread(id) ON DELETE SET NULL,
  ADD COLUMN channel_id UUID REFERENCES channel(id) ON DELETE SET NULL,
  ADD COLUMN action_required BOOLEAN DEFAULT FALSE,
  ADD COLUMN severity TEXT DEFAULT 'info',   -- info/warning/critical
  ADD COLUMN resolved_at TIMESTAMPTZ,
  ADD COLUMN resolution TEXT,                -- 'approved' / 'rejected' / 'dismissed' / 'auto_resolved'
  ADD COLUMN resolution_by UUID;

CREATE INDEX idx_inbox_item_unresolved ON inbox_item(recipient_id, created_at DESC)
  WHERE resolved_at IS NULL;
```

### 8.2 type 枚举

```
# Project 链路
human_input_needed       — human_input slot 激活
review_needed            — human_review slot 激活
task_attention_needed    — Task → needs_attention
plan_approval_needed     — Plan 提交审批
run_completed            — Run 完成
run_failed               — Run 失败

# Session / Channel 链路
reply_slow               — MediationService SLA warning
mention                  — 消息提及
dm_received              — DM 消息

# Account 链路
impersonation_expiring   — impersonation 快到期
agent_suspended          — 自己的 Agent 被 admin 暂停
runtime_offline          — 自己的 Runtime 离线

# 系统
system_announcement      — 系统公告
```

### 8.3 解决路径

| 操作 | 字段更新 |
|------|---------|
| 用户点击 "批准" | resolved_at=now(), resolution='approved', resolution_by=user_id |
| 用户点击 "拒绝" | resolution='rejected' |
| 用户点击 "忽略" | resolution='dismissed' |
| 系统自动解决（上下游已满足） | resolution='auto_resolved' |
| 用户标记已读 | read_at=now()（不影响 resolved_at） |

### 8.4 API

| 端点 | 方法 | 说明 |
|------|------|------|
| `GET /api/inbox?unresolved=true` | GET | 未解决的 inbox items |
| `POST /api/inbox/{id}/resolve` | POST | 标记已解决 |
| `POST /api/inbox/{id}/read` | POST | 标记已读 |
| `POST /api/inbox/mark-all-read` | POST | 全部已读 |

---

## 9. Agent 匹配与评分

Project PRD §6.4 列出规则，本节给出具体算法。

### 9.1 候选集筛选

```
输入: Task
输出: 可调度的 Agent 列表

1. 从 Task.primary_assignee_id + fallback_agent_ids 取候选
2. 对每个候选 Agent：
   a. Agent 必须 idle 或 busy 且并发未满
   b. 绑定的 Runtime 必须 online 或 degraded
   c. Runtime.current_load < Runtime.concurrency_limit
   d. Agent 拥有执行任务的权限（见 Account PRD 8.7）
3. 不满足的候选淘汰
```

### 9.2 评分公式

候选集内选 Agent：

```
score(agent) = 
    w1 × skill_coverage    // 0~1，required_skills 被 agent.identity_card.skills 覆盖比例
  + w2 × load_factor       // 0~1，1 - (runtime.current_load / runtime.concurrency_limit)
  + w3 × freshness         // 0~1，last_active_at 的新鲜度（24h 内=1，7d=0.5，更旧=0）
  + w4 × primary_bonus     // primary_assignee=1.0, fallback=0.5
  + w5 × history_success   // 0~1，该 Agent 在同 Project 的历史成功率
```

**默认权重：**

| w | 值 | 含义 |
|---|---|------|
| w1 (skill) | 0.35 | 能力匹配 |
| w2 (load) | 0.20 | 负载均衡 |
| w3 (freshness) | 0.10 | 活跃度 |
| w4 (primary) | 0.25 | 尊重 Plan 指定 |
| w5 (history) | 0.10 | 历史表现 |

### 9.3 Tie-breaking

| 规则 | 优先级 |
|------|-------|
| primary_assignee 优先 | 1 |
| runtime online > degraded | 2 |
| current_load 更低 | 3 |
| agent.created_at 更早 | 4 |

### 9.4 退化场景

| 场景 | 处理 |
|------|------|
| 候选全部不满足 | Task → needs_attention，通知 Agent Owner |
| skill 覆盖 < 0.5 | 选择但在 activity_log 记 warning |
| 无 fallback 且 primary 离线 | 等待 5 分钟（由 timeout_rule 决定），超时升级 |

---

## 10. DAG 校验

Project PRD 使用 `Task.depends_on` 构建 DAG，但未给出校验算法。

### 10.1 校验时机

| 时机 | 校验 |
|------|------|
| Plan 创建 / 更新 | 全量校验 |
| Task 新增 / 修改 depends_on | 增量校验（仅受影响 Task） |
| Run 启动 | 再次校验（兜底） |

### 10.2 校验规则

| 规则 | 错误码 |
|------|-------|
| depends_on 中 UUID 必须存在同 plan | `DAG_UNKNOWN_TASK` |
| 不允许自环：task.id ∉ depends_on | `DAG_SELF_REF` |
| 不允许循环依赖（DFS 检测） | `DAG_CYCLE` |
| step_order 单调性（可选）：depends_on 中 Task 的 step_order < 当前 Task | `DAG_STEP_ORDER` warning |

### 10.3 环检测算法

```go
func detectCycle(tasks []Task) (path []uuid.UUID, ok bool) {
    // Kahn 算法或 DFS + 白灰黑三色标记
    color := map[uuid.UUID]int{} // 0=white, 1=gray, 2=black
    var dfs func(id uuid.UUID, stack []uuid.UUID) []uuid.UUID
    dfs = func(id uuid.UUID, stack []uuid.UUID) []uuid.UUID {
        if color[id] == 1 {
            // 在栈中，环
            idx := slices.Index(stack, id)
            return stack[idx:]
        }
        if color[id] == 2 { return nil }
        color[id] = 1
        for _, dep := range depMap[id] {
            if cycle := dfs(dep, append(stack, id)); cycle != nil {
                return cycle
            }
        }
        color[id] = 2
        return nil
    }
    for _, t := range tasks {
        if cycle := dfs(t.ID, nil); cycle != nil {
            return cycle, false
        }
    }
    return nil, true
}
```

### 10.4 可视化要求

前端从 `GET /api/plans/{id}/dag` 获取结构，用图形库（如 dagre-d3 或 ReactFlow）渲染。环错误时高亮环路径。

---

## 11. 密钥与凭证管理

三份主 PRD 均未涉及，但 cloud Execution 和 Agent 调用都依赖密钥。

### 11.1 密钥类型

| 密钥 | 用途 | 归属 | 存储 |
|------|------|------|------|
| Anthropic API Key | Claude Agent SDK 调用 | Workspace 级（cloud runtime） | 后端环境变量 + Workspace secret 表 |
| OpenAI / 其他 LLM Key | 同上 | Workspace 级 | 同上 |
| PAT (Personal Access Token) | Daemon 注册、CLI 使用 | User 级 | 后端 hashed |
| GitHub / GitLab Token | `create_pr` 工具 | Project / Workspace 级 | 同上 |
| JWT (session) | 浏览器/CLI 会话 | User 级 | Cookie / local keychain |

### 11.2 Workspace Secret 表

```sql
CREATE TABLE workspace_secret (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id   UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  key            TEXT NOT NULL,           -- e.g. 'anthropic_api_key', 'github_token'
  value_encrypted BYTEA NOT NULL,         -- 使用服务端主密钥加密
  created_by     UUID NOT NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  rotated_at     TIMESTAMPTZ,
  UNIQUE (workspace_id, key)
);
```

### 11.3 访问规则

| 角色 | 可见 |
|------|------|
| owner | 可读可写（不显示明文，仅回显 masked） |
| admin | 可读可写 |
| member | 不可见 |
| Agent | 不可见（通过服务端中转使用，不直接暴露） |

### 11.4 加密方案

| 层级 | 方案 |
|------|------|
| 传输 | TLS（与平台其他 API 一致） |
| 存储 | AES-256-GCM，主密钥从环境变量 `MYTEAM_SECRET_KEY` 读取 |
| 轮换 | 手动触发；旧密钥解密 → 新密钥重加密 |

### 11.5 Cloud Execution 使用流程

```
CloudExecutorService 需要调用 Claude SDK
  → Queries.GetWorkspaceSecret(workspace_id, 'anthropic_api_key')
  → 服务端解密
  → 作为 SDK 客户端初始化参数
  → Agent 不接触明文
```

### 11.6 PAT 管理

Daemon 启动需要 admin+ 的 PAT。PAT 存储：

```sql
CREATE TABLE personal_access_token (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  token_hash   TEXT NOT NULL,           -- bcrypt hash
  scopes       TEXT[] NOT NULL DEFAULT '{}',
  last_used_at TIMESTAMPTZ,
  expires_at   TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at   TIMESTAMPTZ
);
```

---

## 12. 成本与配额

### 12.1 成本追踪

每次 Execution 记录 token / 费用：

```sql
ALTER TABLE execution
  ADD COLUMN cost_input_tokens INTEGER DEFAULT 0,
  ADD COLUMN cost_output_tokens INTEGER DEFAULT 0,
  ADD COLUMN cost_usd NUMERIC(10,4) DEFAULT 0,
  ADD COLUMN cost_provider TEXT;       -- 'anthropic' / 'openai' / ...
```

每次 PlanGeneratorService / MediationService 调用同样记录（通过 activity_log.payload）。

### 12.2 配额表

```sql
CREATE TABLE workspace_quota (
  workspace_id   UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
  max_monthly_usd          NUMERIC(10,2) DEFAULT 100.00,
  max_concurrent_cloud_exec INTEGER DEFAULT 10,
  max_monthly_plan_gen     INTEGER DEFAULT 200,
  current_monthly_usd      NUMERIC(10,2) DEFAULT 0,
  current_month            DATE NOT NULL,    -- 月度重置基准
  updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 12.3 强制机制

```
Execution claim 前：
  1. 检查 workspace_quota.current_monthly_usd < max_monthly_usd
  2. 检查当前并发 cloud execution < max_concurrent_cloud_exec
  3. 任一失败 → Execution → failed, error='QUOTA_EXCEEDED'
     → Task → needs_attention
     → 通知 admin

月度重置：定时任务每月 1 日重置 current_monthly_usd 并更新 current_month
```

### 12.4 用户可见

| 范围 | 前端展示 |
|------|---------|
| admin | Workspace 总用量 + 配额 + 按 User/Project/Agent 分解 |
| member | 自己的 Agent / Project 用量 |

### 12.5 MVP vs Post-MVP

| 范围 | 内容 |
|------|------|
| MVP | 字段预留 + 基本追踪 + 硬上限拒绝 |
| Post-MVP | 预算预警、按 Project 分摊、计费导出 |

---

## 13. 错误码分类

### 13.1 命名规则

```
<DOMAIN>_<ERROR_NAME>
```

全大写 + 下划线。Domain：PROJECT、PLAN、TASK、SLOT、EXECUTION、ARTIFACT、REVIEW、AGENT、RUNTIME、CHANNEL、THREAD、MCP、QUOTA、AUTH、DAG、PLAN_GEN。

### 13.2 错误码清单（节选）

| 错误码 | HTTP | retriable | 含义 |
|-------|------|:---------:|------|
| `AUTH_UNAUTHORIZED` | 401 | 否 | 未登录 |
| `AUTH_FORBIDDEN` | 403 | 否 | 无权限 |
| `PROJECT_NOT_FOUND` | 404 | 否 | Project 不存在 |
| `PLAN_NOT_APPROVED` | 409 | 否 | 启动执行前 Plan 未批准 |
| `PLAN_HAS_ACTIVE_RUN` | 409 | 否 | Plan 有活跃 Run 时不能直接改为 rejected |
| `PLAN_GEN_MALFORMED` | 500 | 是 | LLM 输出 schema 不合法 |
| `PLAN_GEN_TIMEOUT` | 504 | 是 | LLM 超时 |
| `DAG_CYCLE` | 400 | 否 | DAG 含环 |
| `DAG_UNKNOWN_TASK` | 400 | 否 | depends_on 引用未知 Task |
| `TASK_NOT_SCHEDULABLE` | 409 | 否 | 调度条件不满足（见 9.4） |
| `SLOT_NOT_READY` | 409 | 否 | Slot 未激活 |
| `SLOT_ALREADY_SUBMITTED` | 409 | 否 | 重复提交 |
| `EXECUTION_LEASE_EXPIRED` | 410 | 否 | Lease 过期，CloudExecutor 接管失败 |
| `AGENT_NOT_AVAILABLE` | 503 | 是 | Agent status 不可调度 |
| `RUNTIME_OFFLINE` | 503 | 是 | Runtime 离线 |
| `RUNTIME_OVERLOADED` | 429 | 是 | Runtime 达并发上限 |
| `ARTIFACT_INVALID` | 400 | 否 | 关联 FileIndex access_scope 不对齐等 |
| `REVIEW_ALREADY_DECIDED` | 409 | 否 | Slot 已审批，重复决策 |
| `MCP_TOOL_NOT_AVAILABLE` | 404 | 否 | 当前 runtime 不支持此工具（如 cloud 调 local_file_read） |
| `MCP_PERMISSION_DENIED` | 403 | 否 | Agent 权限不足 |
| `QUOTA_EXCEEDED` | 429 | 否（需配额上调） | 月度 USD 上限 |
| `QUOTA_CONCURRENT_LIMIT` | 429 | 是 | 并发超限 |
| `IMPERSONATION_NOT_OWN_AGENT` | 403 | 否 | 不是自己的 Agent |
| `IMPERSONATION_EXPIRED` | 410 | 否 | session 过期 |

### 13.3 错误响应格式

```json
{
  "error": {
    "code": "DAG_CYCLE",
    "message": "Circular dependency detected in plan tasks",
    "retriable": false,
    "details": {
      "cycle_path": ["task-id-1", "task-id-2", "task-id-3"]
    }
  }
}
```

### 13.4 前端处理约定

| retriable | 前端 |
|-----------|------|
| true | 自动重试（指数退避，最多 3 次），失败后显示 "暂时无法使用" |
| false | 直接显示具体错误提示 |

---

## 14. 可观测性

### 14.1 指标（Metrics）

使用 Prometheus 风格的指标（通过现有 logger + 增补 metrics middleware）。

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `execution_duration_seconds` | histogram | runtime_mode, provider, status | 单次 Execution 耗时 |
| `execution_count_total` | counter | status | Execution 计数 |
| `scheduler_queue_depth` | gauge | priority | 等待调度的 Task 数 |
| `runtime_load_ratio` | gauge | runtime_id | current_load / concurrency_limit |
| `plan_gen_duration_seconds` | histogram | status | PlanGenerator 调用时长 |
| `plan_gen_token_usage` | histogram | direction (input/output) | Token 消耗 |
| `mediation_reply_latency_seconds` | histogram | sla_tier | reply_slot 响应延迟 |
| `ws_connected_clients` | gauge | workspace_id | WebSocket 连接数 |
| `ws_event_published_total` | counter | event_type | 事件分发计数 |
| `mcp_tool_calls_total` | counter | tool, result | MCP 工具调用 |
| `quota_usage_ratio` | gauge | workspace_id, dimension | 配额使用比例 |
| `activity_log_writes_total` | counter | event_type | 审计日志写入 |

### 14.2 日志（Logs）

沿用现有 `slog`，补充字段：

| 字段 | 说明 |
|------|------|
| `request_id` | 请求级 trace ID |
| `workspace_id` | 多租户维度 |
| `user_id` / `agent_id` | 主体 |
| `execution_id` / `task_id` | 业务对象 |

### 14.3 追踪（Tracing）

MVP 不强求 OpenTelemetry。通过 `request_id` 贯穿 activity_log + slog + WS 事件实现逻辑追踪。

### 14.4 告警建议（Post-MVP）

| 告警 | 阈值 |
|------|------|
| Execution 失败率 | > 10% / 5min |
| Scheduler 队列深度 | > 100 / 持续 5min |
| Runtime offline 超 N 台 | N = workspace 内 Runtime 数 × 50% |
| Quota 使用 > 80% | 单个 workspace |
| WS 事件发布失败率 | > 1% / 5min |

---

## 15. MVP 范围

### 15.1 包含

| 项 | 说明 |
|----|------|
| Event Catalog | 本 PRD §2 定义，各服务按清单发布 |
| Activity Log schema | §3 完整实现（表 + event_type + API） |
| PlanGeneratorService | §4 完整实现（含 DAG 校验、Agent 能力检查） |
| MediationService | §5 MVP：@mention 路由 + 基本 SLA，复杂能力匹配延后 |
| CloudExecutorService | §6 完整实现（含 lease、崩溃恢复） |
| MCP 工具契约 | §7 所有 17 个工具的后端 handler + 权限检查 |
| Inbox 扩展 | §8 完整（新字段 + type 枚举 + resolve API） |
| Agent 匹配评分 | §9 完整（含默认权重） |
| DAG 校验 | §10 完整实现 |
| Workspace Secret 表 + PAT 表 | §11 完整（含加密） |
| 成本字段预留 + 基本配额 | §12 MVP 范围 |
| 错误码分类 | §13 全部 |
| 基础指标 | §14.1 前 8 个指标 |

### 15.2 不包含（Post-MVP）

| 项 | 说明 |
|----|------|
| WebSocket 事件补发 | 断线后依赖 API 拉取，不补发 |
| 跨服务分布式追踪 | 依赖 request_id 逻辑追踪 |
| MediationService 高级能力匹配 | 用 identity_card + embedding 向量 |
| Plan Generator 多 LLM Provider 对比 | 固定 Anthropic |
| 配额预警 / 预算告警 | 仅硬拒绝 |
| 成本按 Project / User 分摊 | 仅 Workspace 维度 |
| 告警规则自动化 | 手动配 Prometheus alertmanager |
| Secrets 密钥轮换自动化 | 手动触发 |

---

## 16. 与主 PRD 的交叉引用

| 本 PRD 章节 | 主 PRD 交叉引用 |
|------------|----------------|
| §2 Event Catalog | 覆盖 Project PRD §6/§10、Account PRD §2-5、Session PRD §2/§5 中所有 "发布事件" |
| §3 Activity Log | 覆盖 Project PRD §10.5、Account PRD §5.3/§8.6 中的审计引用 |
| §4 PlanGeneratorService | 详化 Project PRD §6.2/§9.1 |
| §5 MediationService | 详化 Session PRD §5/§6 Phase 5 |
| §6 CloudExecutorService | 详化 Project PRD §10.3 |
| §7 MCP 工具契约 | 详化 Account PRD §8.8 |
| §8 Inbox | 详化 Project PRD §10.6 |
| §9 Agent 匹配 | 详化 Project PRD §6.4 |
| §10 DAG 校验 | 详化 Project PRD §6.6 |
| §11 密钥管理 | 填补三份 PRD 的空白 |
| §12 成本配额 | 填补三份 PRD 的空白 |
| §13 错误码 | 规范三份 PRD 的错误处理 |
| §14 可观测性 | 填补三份 PRD 的空白 |
