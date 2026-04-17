# Session/Channel 重构 PRD

> Version: 1.0 | Date: 2026-04-17

---

## 1. 背景与目标

### 1.1 现状问题

| # | 问题 | 说明 |
|---|------|------|
| 1 | Session 与 Channel/Thread 功能重叠 | 两者都提供多轮讨论、共享上下文、参与者追踪，但数据模型完全独立 |
| 2 | Message 多态路由 | message 可指向 channel_id、session_id、recipient_id 三种目标，增加查询和路由复杂度 |
| 3 | Thread.id = root message ID | thread 创建依赖已有消息，Plan 创建时产生鸡蛋问题 |
| 4 | Session.context 是黑盒 JSONB | files、snippets、decisions、summary 塞在一个 JSONB 字段，无法独立查询和关联 |
| 5 | 前端双概念 | `/session`（统一聊天页）和 `/sessions`（协作会话页）概念混淆 |

### 1.2 重构目标

1. **废弃 session 表** — 迁移到 Channel/Thread 模型
2. **Thread.id 解耦** — 独立 UUID，不依赖 root message
3. **结构化上下文** — session.context JSONB → thread_context_item 表
4. **统一消息路由** — message 仅指向 channel_id + thread_id，去掉 session_id
5. **前端统一** — 去掉 `/sessions` 路由，统一到 `/session`

### 1.3 与其他 PRD 的关系

| PRD | 关系 |
|-----|------|
| Account PRD | `scope='conversation'` 的 System Agent 负责消息路由/摘要/分配（原 scope='session'） |
| Project PRD | `Plan.thread_id` FK → thread（解耦后的 Thread），`Project.channel_id` FK → channel |
| 本文档 | 拥有 session 废弃、thread 重构、context 迁移、前端切换的完整定义 |

### 1.4 范围边界

**本 PRD 涉及的 "session"：** 产品协作会话（`session` 表 + `session_participant` 表 + `message.session_id`）

**不涉及的 "session"：** 以下概念名称含 session 但与本次迁移无关，保留不变：
- `remote_session` — Agent 远程执行会话
- `agent_task_queue.session_id` — Provider 会话恢复 ID（未来可改名 `provider_session_id`）
- Daemon `PriorSessionID` — CLI 会话续接标识
- `impersonation_session` — 附身会话记录

---

## 2. 核心模型

### 2.1 目标模型

```
Channel (access boundary + collaboration room)
  │
  ├── conversation_type: 'dm' | 'channel'
  ├── visibility: 'private' | 'public'
  ├── project_id (optional → Project)
  │
  └── Thread (focused discussion object)
       ├── id: gen_random_uuid()（解耦，不再 = root message ID）
       ├── channel_id → Channel
       ├── root_message_id (optional → Message)
       ├── issue_id (optional → Issue)
       ├── plan_id (反查 Plan.thread_id)
       ├── title, status, metadata
       │
       └── ThreadContextItem (structured context)
            ├── item_type: 'decision' | 'file' | 'code_snippet' | 'summary' | 'reference'
            ├── title, body, metadata
            └── source_message_id (optional)
```

**设计原则：**
- **Channel** = 访问边界和协作空间
- **Thread** = 聚焦讨论/工作对象
- **Message** = 可见的沟通/事件流
- **Slot / Review / ActionRequest** = 工作流状态真相（来自 Project PRD）
- 聊天消息**反映**状态变化，不**驱动**状态变化

### 2.2 消息路由简化

**现有（三路多态）：**
```
message → channel_id    (Channel 消息)
message → session_id    (Session 消息)
message → recipient_id  (DM 消息)
```

**目标（两路）：**
```
message → channel_id    (Channel/DM 消息)
message → thread_id     (Thread 回复，channel_id 通过 thread.channel_id 获取)
```

> `recipient_id` 保留用于 DM 路由查找（DM 是 conversation_type='dm' 的 Channel），但 message 始终写 channel_id。

---

## 3. Schema 变更

### 3.1 Thread 表增强

| 字段 | 类型 | 变化 | 说明 |
|------|------|------|------|
| `id` | UUID PK | **DEFAULT 改为 gen_random_uuid()** | 不再等于 root message ID |
| `channel_id` | UUID FK → channel | 不变 | |
| `workspace_id` | UUID FK → workspace | **新增** | 冗余，便于查询 |
| `root_message_id` | UUID FK → message | **新增** | 可选，原始触发消息 |
| `issue_id` | UUID FK → issue | **新增** | Issue 关联（替代 session.issue_id） |
| `title` | TEXT | 不变 | |
| `status` | TEXT DEFAULT 'active' | **新增** | `active` \| `archived` |
| `created_by` | UUID | **新增** | 创建者 |
| `created_by_type` | TEXT | **新增** | `member` \| `agent` \| `system` |
| `metadata` | JSONB DEFAULT '{}' | **新增** | 扩展元数据 |
| `reply_count` | INTEGER DEFAULT 0 | 不变，**语义修正** | 仅计人/agent 回复，不计系统消息 |
| `last_reply_at` | TIMESTAMPTZ | 不变 | 最近人/agent 回复时间 |
| `last_activity_at` | TIMESTAMPTZ | **新增** | 最近任何活动时间（含系统消息） |
| `created_at` | TIMESTAMPTZ | 不变 | |

**历史数据处理：** 现有 thread 行的 `root_message_id` 回填为 `id`（因为历史 thread.id = root message ID）。不重写历史 ID。

### 3.2 ThreadContextItem 表（新建）

替代 session.context JSONB。每条上下文信息独立一行。

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | UUID PK | |
| `workspace_id` | UUID FK → workspace | |
| `thread_id` | UUID FK → thread | |
| `item_type` | TEXT | `decision` \| `file` \| `code_snippet` \| `summary` \| `reference` |
| `title` | TEXT | 标题 |
| `body` | TEXT | 内容 |
| `metadata` | JSONB DEFAULT '{}' | 扩展（如 file 的 name/size/content_type，snippet 的 language） |
| `source_message_id` | UUID FK → message | 可选，来源消息 |
| `retention_class` | TEXT DEFAULT 'ttl' | 保留策略：`permanent` \| `ttl` \| `temp`（见 §7 数据生命周期） |
| `expires_at` | TIMESTAMPTZ | 过期时间（`retention_class='ttl'` 时由业务逻辑写入） |
| `created_by` | UUID | |
| `created_by_type` | TEXT | `member` \| `agent` \| `system` |
| `created_at` | TIMESTAMPTZ | |

**retention_class 默认策略：**

| item_type | 默认 retention_class | 说明 |
|-----------|---------------------|------|
| `decision` | `permanent` | 关键决策，长期保留 |
| `file` / `reference` | `ttl` | 文件引用，可随 FileIndex 清理 |
| `code_snippet` | `ttl` | 讨论中的代码片段 |
| `summary`（用户创建） | `permanent` | 人工摘要 |
| `summary`（系统自动生成） | `ttl` | 定期重算，旧摘要可清理 |

**与 session.context 的映射：**

| session.context 字段 | → ThreadContextItem |
|---------------------|---------------------|
| `topic` | thread.title（若缺失）+ item_type='summary' |
| `summary` | item_type='summary' |
| `decisions[]` | 每条 → item_type='decision'，title=decision, body=by+at |
| `files[]` | 每条 → item_type='file'，metadata={name, content} |
| `code_snippets[]` | 每条 → item_type='code_snippet'，metadata={language}, body=code |

### 3.3 SessionMigrationMap 表（新建，迁移用）

| 字段 | 类型 | 说明 |
|------|------|------|
| `session_id` | UUID PK | 原 session ID |
| `channel_id` | UUID FK → channel | 迁移到的 Channel |
| `thread_id` | UUID FK → thread | 迁移到的 Thread |
| `migrated_at` | TIMESTAMPTZ | 迁移时间 |

### 3.4 Channel 表（不变）

Channel 表结构保持不变。`conversation_type` 保留 `'dm'` | `'channel'`。

> **不再用 `conversation_type = 'thread'`。** Thread 是独立表，不是 Channel 的子类型。如有历史数据 `conversation_type = 'thread'`，迁移时清理。

### 3.5 Message 表变更

| 字段 | 变化 | 说明 |
|------|------|------|
| `session_id` | **废弃** | 兼容期保留，迁移后删除 |
| `thread_id` | 不变 | 成为 Thread 回复的主要标识 |
| `channel_id` | 不变 | 所有消息必须有 channel_id |

---

## 4. reply_count 语义

### 4.1 规则

| 消息类型 | 计入 reply_count | 更新 last_reply_at | 更新 last_activity_at |
|---------|:---------------:|:------------------:|:--------------------:|
| 人类消息 | 是 | 是 | 是 |
| Agent 消息 | 是 | 是 | 是 |
| 系统通知（Plan 创建、Slot 激活、状态变更） | 否 | 否 | 是 |
| 自动生成摘要 | 否 | 否 | 是 |

### 4.2 实现

```go
func (h *Handler) incrementThreadCounters(ctx, threadID, senderType) {
    // 所有消息都更新 last_activity_at
    h.Queries.UpdateThreadLastActivity(ctx, threadID)
    
    // 仅人/agent 消息更新 reply_count 和 last_reply_at
    if senderType == "member" || senderType == "agent" {
        h.Queries.IncrementThreadReplyCount(ctx, threadID)
    }
}
```

---

## 5. System Agent scope='conversation'

Account PRD 定义的 `scope='conversation'` System Agent（原 `scope='session'`），职责：

| 职责 | 说明 |
|------|------|
| 消息路由 | 根据 @ 提及、能力匹配、SLA 规则分配回复者 |
| 自动摘要 | 定期生成 Thread 讨论摘要（写入 ThreadContextItem） |
| 回复分配 | MediationService 的 AI 调解能力 |
| 升级通知 | SLA 超时时通知 User |
| 防噪声 | 执行防洪、防循环规则 |

> 此 Agent 自动加入所有 workspace 内的 Channel（作为 channel_member），用于接收实时消息和触发路由。

---

## 6. 迁移流程

### Phase 0: 术语冻结

```
1. 创建本 PRD
2. Account PRD: scope='session' → scope='conversation' ✓（已完成）
3. 冻结：不再新增任何使用 session 表的功能
4. 代码中 session 相关注释标记 // DEPRECATED: migrating to channel/thread
```

### Phase 1: 加表加列（无破坏性）

```sql
-- Thread 表增强
ALTER TABLE thread
  ALTER COLUMN id SET DEFAULT gen_random_uuid(),
  ADD COLUMN workspace_id UUID REFERENCES workspace(id),
  ADD COLUMN root_message_id UUID REFERENCES message(id) ON DELETE SET NULL,
  ADD COLUMN issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
  ADD COLUMN created_by UUID,
  ADD COLUMN created_by_type TEXT DEFAULT 'member',
  ADD COLUMN status TEXT NOT NULL DEFAULT 'active',
  ADD COLUMN metadata JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN last_activity_at TIMESTAMPTZ;

-- 回填 workspace_id 和 root_message_id
UPDATE thread SET
  workspace_id = (SELECT workspace_id FROM channel WHERE channel.id = thread.channel_id),
  root_message_id = thread.id;  -- 历史 thread.id = root message ID

-- ThreadContextItem 表
CREATE TABLE thread_context_item (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  thread_id UUID NOT NULL REFERENCES thread(id) ON DELETE CASCADE,
  item_type TEXT NOT NULL CHECK (item_type IN ('decision','file','code_snippet','summary','reference')),
  title TEXT,
  body TEXT,
  metadata JSONB NOT NULL DEFAULT '{}',
  source_message_id UUID REFERENCES message(id) ON DELETE SET NULL,
  retention_class TEXT NOT NULL DEFAULT 'ttl'
    CHECK (retention_class IN ('permanent','ttl','temp')),
  expires_at TIMESTAMPTZ,
  created_by UUID,
  created_by_type TEXT DEFAULT 'system',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_thread_context_item_thread ON thread_context_item(thread_id);
CREATE INDEX idx_thread_context_item_expires ON thread_context_item(expires_at)
  WHERE retention_class = 'ttl' AND expires_at IS NOT NULL;

-- SessionMigrationMap 表
CREATE TABLE session_migration_map (
  session_id UUID PRIMARY KEY,
  channel_id UUID NOT NULL REFERENCES channel(id),
  thread_id UUID NOT NULL REFERENCES thread(id),
  migrated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### Phase 2: 新 Thread API

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/channels/{channelID}/threads` | POST | 创建 Thread（独立 UUID，可选 root_message_id） |
| `/api/threads/{threadID}` | GET | 获取 Thread 详情 |
| `/api/threads/{threadID}/messages` | GET | 获取 Thread 消息 |
| `/api/threads/{threadID}/messages` | POST | 在 Thread 中发消息 |
| `/api/threads/{threadID}/context` | GET | 获取 Thread 上下文 |
| `/api/threads/{threadID}/context-items` | POST | 添加上下文项 |
| `/api/threads/{threadID}/context-items/{id}` | DELETE | 删除上下文项 |

**兼容期：** `message.session_id` 写入仍然接受。后端查 `session_migration_map`，转写为 `channel_id + thread_id`，并在 `message.metadata` 中保存 `source_session_id`。

### Phase 3: 数据迁移

每个 legacy session 迁移为 1 个 private channel + 1 个 thread：

```
for each session:
  1. 创建 Channel:
     - workspace_id = session.workspace_id
     - name = "session-<short-id>-<slug-title>"
     - visibility = 'private'
     - conversation_type = 'channel'

  2. 迁移参与者:
     - session_participant → channel_member
     - 添加 conversation scope System Agent（若路由需要）

  3. 创建 Thread:
     - channel_id = 新 Channel
     - title = session.title
     - issue_id = session.issue_id
     - status = session.status 映射 ('active'→'active', 其他→'archived')
     - metadata = { max_turns, current_turn, source_session_id }

  4. 迁移消息:
     - message.session_id = session.id → 设 channel_id + thread_id
     - 保留原始 timestamps 和 sender 信息
     - message.metadata 加 source_session_id

  5. 迁移 Context:
     - session.context.topic → thread.title (若缺失)
     - session.context.summary → thread_context_item (type='summary')
     - session.context.decisions[] → 逐条 thread_context_item (type='decision')
     - session.context.files[] → 逐条 thread_context_item (type='file')
     - session.context.code_snippets[] → 逐条 thread_context_item (type='code_snippet')

  6. 写入 session_migration_map

  7. 重算 thread.reply_count 和 last_reply_at
```

### Phase 4: 前端切换

| 现有 | 目标 | 处理 |
|------|------|------|
| `/sessions` 路由 | 删除 | 重定向到 `/session` |
| `/sessions/[id]` 路由 | 删除 | 查 migration_map → 重定向到 Channel + Thread 视图 |
| `features/sessions/store.ts` | 删除 | 功能迁入 `features/channels/` 或 `features/messaging/` |
| `SessionContextPanel` | 替换 | → `ThreadContextPanel`（读 ThreadContextItem） |
| `AutoDiscussionToggle` | 替换 | → Thread/Channel 策略 UI |
| `Session` 类型 | 删除 | 从 `shared/types/messaging.ts` 移除 |
| `SessionParticipant` 类型 | 删除 | 同上 |
| `RemoteSessionsList` | 保留 | 移到 `features/runtimes/`，与本次迁移无关 |

### Phase 5: MediationService 进化

```
现有路径：
  Message → MediationService → 基于 channel 的启发式路由

目标路径：
  Message → MediationService:
    1. 解析上下文: workspace_id, channel_id, thread_id, project_id, plan_id, issue_id, mentions
    2. 匹配路由规则:
       - @ 提及 → 直接分配
       - Plan Thread → 分配 Plan 关联的 Agent
       - Issue Thread → 分配 Issue 关联的 Agent
       - Channel → 能力匹配 + SLA 规则
    3. 创建 reply_slot（含 thread_id）
    4. SLA 升级: T+300s fallback → T+600s warning → T+900s critical
```

**防重复回复：** 自动回复由 MediationService 统一管理。`auto_reply_config` 中的 triggers 作为输入，但实际发送由 MediationService 决策，避免 message handler 和 mediation 双重触发。

### Phase 6: 清理

```
确认迁移完成后：
  1. message.session_id 写入拒绝（返回 400 + migration 提示）
  2. session 相关 API 端点移除（或保留只读 + 重定向一个版本周期）
  3. DROP TABLE session_participant
  4. DROP TABLE session
  5. ALTER TABLE message DROP COLUMN session_id
  6. 清理代码中 DEPRECATED 标记
```

---

## 7. 数据生命周期

### 7.1 三类保留策略

与 Project PRD §11 保持一致：

| retention_class | 含义 | ThreadContextItem 场景 |
|----------------|------|----------------------|
| `permanent` | 永久保留 | 人工创建的 decision、summary |
| `ttl` | 到期清理 | 自动摘要、file/code_snippet 引用 |
| `temp` | 短期临时 | 提示、缓存、临时引用 |

### 7.2 TTL 策略建议（Post-MVP 后台作业实现）

| item_type | retention_class | 默认 TTL | 清理行为 |
|-----------|----------------|---------|---------|
| `decision` | `permanent` | — | 不清理 |
| `summary`（用户创建） | `permanent` | — | 不清理 |
| `summary`（系统自动） | `ttl` | 30d | 超期删除；新摘要自动重算 |
| `file` / `reference` | `ttl` | 随引用的 FileIndex | 若 FileIndex 已清理，联动删除 |
| `code_snippet` | `ttl` | 90d | 超期删除 |

### 7.3 联动规则

| 规则 | 说明 |
|------|------|
| ThreadContextItem 引用 Artifact | 只能删 ThreadContextItem 引用，不能删已批准的 Artifact 本体 |
| ThreadContextItem 引用 FileIndex | 若 FileIndex 被清理，关联的 ThreadContextItem 随之清理 |
| Thread 归档不触发 Context 清理 | Thread 归档时 ThreadContextItem 保留，用于历史查询 |
| Thread 物理删除（罕见）联动 | `ON DELETE CASCADE` 删除所有 ThreadContextItem |

### 7.4 MVP vs Post-MVP

| 范围 | 内容 |
|------|------|
| **MVP** | Schema 字段预留：`retention_class`、`expires_at`、索引 |
| **MVP** | 创建时按 item_type 写入默认 retention_class |
| **MVP** | 迁移时 session.context 的每条数据按映射规则写入 retention_class |
| **Post-MVP** | 后台清理作业：扫描 `expires_at < now()` 且 `retention_class='ttl'` 的条目 |
| **Post-MVP** | Summary 自动重算 + 旧 summary 替换逻辑 |
| **Post-MVP** | FileIndex 联动清理 |

> **迁移时的 retention_class 写入：**
> - `session.context.decisions[]` → 新 ThreadContextItem 的 `retention_class = 'permanent'`
> - `session.context.files[]` / `code_snippets[]` → `retention_class = 'ttl'`，`expires_at = NULL`（等后台作业建立后再填充）
> - `session.context.summary`（人工） → `retention_class = 'permanent'`

---

## 8. 与 Project PRD 的集成点

| 集成 | 说明 |
|------|------|
| Plan.thread_id | FK → 增强后的 thread 表（独立 UUID） |
| Project Channel 中的 Thread | 每个 Plan 对应一个 Thread，Plan 讨论在此进行 |
| ThreadContextItem | Plan 生成时的 context_snapshot 可引用 Thread 中的 context_item |
| System Agent 加入 Channel | scope='project' Agent 自动加入项目 Channel（见 Account PRD） |
| scope='conversation' Agent | 自动加入所有 Channel，负责消息路由和 SLA |
| inbox_item | Slot 激活时通知通过 inbox_item 投递，含 thread_id 引用 |

---

## 9. 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| 数据泄露 | 每 session 独立 channel 避免参与者可见性合并 | 迁移为 private channel |
| Thread ID 兼容 | 旧代码假设 thread.id = message.id | 不重写历史 ID，仅改变未来行为 |
| Runtime session 混淆 | `remote_session` / `provider_session_id` 与本次无关 | PRD 明确排除 |
| 双重自动回复 | message handler 和 mediation 都触发回复 | MediationService 成为唯一路由者 |
| 前端回退 | `/sessions` 删除后旧书签失效 | 保留重定向一个版本周期 |
| reply_count 语义变更 | 历史数据含系统消息计数 | 迁移时重算 reply_count |

---

## 10. MVP 范围

### 包含

| 项目 | 说明 |
|------|------|
| Thread.id 解耦 | gen_random_uuid() + root_message_id |
| Thread 增强字段 | workspace_id, issue_id, status, metadata, last_activity_at |
| ThreadContextItem 表 | 结构化上下文 |
| ThreadContextItem 生命周期字段预留 | retention_class、expires_at、索引 |
| 默认 retention_class 写入 | 按 item_type 映射（见 §3.2 / §7.2） |
| Session → Channel/Thread 数据迁移 | 含 migration_map |
| 迁移时 retention_class 写入 | decision=permanent、file/snippet=ttl、summary 按来源区分 |
| reply_count 语义修正 | 不计系统消息 |
| 新 Thread API | 独立创建、消息、上下文 CRUD |
| 前端 /sessions 移除 | 重定向 + store 清理 |
| scope='conversation' Agent | 消息路由 + SLA |

### 不包含

| 项目 | 说明 |
|------|------|
| Thread 级别 ACL | 当前无 thread 级权限，靠 channel 成员控制 |
| Channel 合并/拆分 | 延后（MyTeam 架构文档标注 MVP 后） |
| 半公开频道（邀请码） | 延后 |
| 高级能力匹配路由 | MediationService 先用简单规则 |
| session_migration_map 清理 | 迁移完成后保留一段时间，不急于删除 |
| ThreadContextItem 后台清理作业 | Post-MVP：扫描 `expires_at < now()` + retention_class='ttl' |
| Summary 自动重算 | Post-MVP：定期重算 + 替换旧 summary |
| FileIndex 联动清理 | Post-MVP：file/reference 类型 context item 跟随 FileIndex 清理 |
