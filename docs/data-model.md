# My Team - Core Data Model

## Overview

Six core objects form the foundation of the Owner-Agent collaborative workflow platform:

1. **Identity** - Who participates (Organization, Owner, Agent)
2. **Conversation** - Where they communicate (DM, Channel, Thread)
3. **Project** - What they plan (Project, Version, Plan, Run)
4. **Execution** - How tasks are dispatched and tracked
5. **File** - What artifacts are produced and shared
6. **Notification** - How events are surfaced and escalated

---

## 1. Identity

### Hierarchy

```
Organization
  └── Owner (1 per user)
        └── Personal Agent (0..N)
  └── System Agent (Global, 1 per organization)
  └── Page System Agent (1 per feature page)
```

### Tables

#### `organization` (NEW)

| Column | Type | Description |
|--------|------|-------------|
| id | UUID PK | |
| name | TEXT NOT NULL | Organization display name |
| slug | TEXT UNIQUE NOT NULL | URL-safe identifier |
| description | TEXT | |
| settings | JSONB DEFAULT '{}' | |
| created_at | TIMESTAMPTZ | |
| updated_at | TIMESTAMPTZ | |

> Note: In MVP, `organization` maps 1:1 to the existing `workspace` table. We may either rename `workspace` → `organization` or keep `workspace` as the DB name and alias it in the application layer. Decision deferred to implementation.

#### `agent` (EXTEND existing)

New/modified columns:

| Column | Type | Description |
|--------|------|-------------|
| agent_type | TEXT NOT NULL DEFAULT 'personal_agent' | 'personal_agent' / 'system_agent' / 'page_system_agent' |
| organization_id | UUID FK → workspace | Maps to workspace_id for now |
| online_status | TEXT DEFAULT 'offline' | 'online' / 'offline' |
| workload_status | TEXT DEFAULT 'idle' | 'idle' / 'busy' / 'blocked' / 'degraded' / 'suspended' |
| identity_card | JSONB DEFAULT '{}' | See Identity Card schema below |
| accessible_files_scope | JSONB DEFAULT '[]' | File access boundaries |
| allowed_channels_scope | JSONB DEFAULT '[]' | Channel access boundaries |
| last_active_at | TIMESTAMPTZ | Last heartbeat or action |

#### Identity Card Schema (JSONB)

```json
{
  "capabilities": ["code_generation", "code_review", "testing"],
  "tools": ["claude_code", "codex", "shell"],
  "skills": ["golang", "typescript", "sql"],
  "subagents": [],
  "completed_projects": [
    { "project_id": "uuid", "title": "string", "completed_at": "timestamp" }
  ],
  "description_auto": "Auto-generated description based on task history",
  "description_manual": "Owner-edited description override"
}
```

### Notes

- `owner` is not a separate table. An Owner is a `user` who has `member.role = 'owner'` in a workspace.
- `agent.owner_id` (existing) links the agent to its owning user/member.
- System Agent: `agent_type = 'system_agent'`, created via existing `GetOrCreateSystemAgent` handler.
- Page System Agent: `agent_type = 'page_system_agent'`, `name` indicates the page scope (e.g., "account_agent", "project_agent").

---

## 2. Conversation (Unified)

### Design Decision

Unify DM, Channel, and Thread into a single `conversation` abstraction. The existing `channel` and `message` tables will be extended rather than replaced to minimize migration risk.

#### `channel` (EXTEND existing → acts as `conversation`)

New columns:

| Column | Type | Description |
|--------|------|-------------|
| conversation_type | TEXT NOT NULL DEFAULT 'channel' | 'dm' / 'channel' / 'thread' |
| parent_conversation_id | UUID FK → channel | Thread parent; NULL for dm/channel |
| visibility | TEXT DEFAULT 'private' | 'private' / 'public' / 'semi_public' (existing column, expand values) |
| invite_code | TEXT | For semi_public channels (MVP deferred) |
| reply_policy | JSONB DEFAULT '{}' | `{ require_response: bool, sla_timeout_seconds: int }` |
| auto_assignment_policy | JSONB DEFAULT '{}' | `{ enabled: bool, prefer_project_agents: bool }` |
| project_id | UUID FK → project | Project-linked channel |
| linked_project_ids | UUID[] DEFAULT '{}' | Referenced projects |

#### `thread` (NEW)

| Column | Type | Description |
|--------|------|-------------|
| id | UUID PK | Same as root message ID |
| channel_id | UUID FK → channel | Parent channel |
| title | TEXT | Thread title (optional) |
| reply_count | INTEGER DEFAULT 0 | |
| last_reply_at | TIMESTAMPTZ | |
| created_at | TIMESTAMPTZ | |

#### `message` (EXTEND existing)

New columns:

| Column | Type | Description |
|--------|------|-------------|
| thread_id | UUID FK → thread | NULL if not in a thread |
| is_impersonated | BOOLEAN DEFAULT FALSE | Owner sent as agent |

### `channel_member` (existing, unchanged)

Already supports `member_type` for polymorphic membership (member/agent).

### MVP Scope

- DM: `conversation_type = 'dm'`, exactly 2 participants
- Channel: `conversation_type = 'channel'`, N participants
- Thread: `conversation_type = 'thread'`, `parent_conversation_id` set
- DM upgrade to Channel: change `conversation_type` from 'dm' to 'channel', open membership
- **No merge/split** in MVP

---

## 3. Project (Four-Layer)

### `project` (NEW)

| Column | Type | Description |
|--------|------|-------------|
| id | UUID PK | |
| workspace_id | UUID FK → workspace | |
| title | TEXT NOT NULL | |
| description | TEXT | |
| status | TEXT DEFAULT 'not_started' | 'not_started' / 'running' / 'paused' / 'completed' / 'failed' / 'archived' |
| schedule_type | TEXT DEFAULT 'one_time' | 'one_time' / 'scheduled' / 'recurring' |
| cron_expr | TEXT | For scheduled/recurring |
| source_conversations | JSONB DEFAULT '[]' | `[{ conversation_id, type, snapshot_at }]` |
| channel_id | UUID FK → channel | Auto-created project channel |
| creator_owner_id | UUID NOT NULL | |
| created_at | TIMESTAMPTZ | |
| updated_at | TIMESTAMPTZ | |

### `project_version` (NEW)

| Column | Type | Description |
|--------|------|-------------|
| id | UUID PK | |
| project_id | UUID FK → project | |
| parent_version_id | UUID FK → project_version | NULL for root version |
| version_number | INTEGER NOT NULL | |
| branch_name | TEXT | |
| fork_reason | TEXT | |
| plan_snapshot | JSONB | Frozen plan at this version |
| workflow_snapshot | JSONB | Frozen workflow at this version |
| version_status | TEXT DEFAULT 'active' | 'active' / 'archived' |
| created_by | UUID | |
| created_at | TIMESTAMPTZ | |

### `plan` (EXTEND existing)

New columns:

| Column | Type | Description |
|--------|------|-------------|
| project_id | UUID FK → project | |
| version_id | UUID FK → project_version | |
| task_brief | TEXT | Task brief / requirements document |
| assigned_agents | JSONB DEFAULT '[]' | `[{ task_order, agent_id, fallback_agent_ids }]` |
| risk_points | TEXT | |
| approval_status | TEXT DEFAULT 'draft' | 'draft' / 'pending_approval' / 'approved' / 'rejected' |
| approved_by | UUID | |
| approved_at | TIMESTAMPTZ | |

### `project_run` (NEW)

| Column | Type | Description |
|--------|------|-------------|
| id | UUID PK | |
| plan_id | UUID FK → plan | |
| project_id | UUID FK → project | |
| status | TEXT DEFAULT 'pending' | 'pending' / 'running' / 'paused' / 'completed' / 'failed' / 'cancelled' |
| start_at | TIMESTAMPTZ | |
| end_at | TIMESTAMPTZ | |
| step_logs | JSONB DEFAULT '[]' | |
| output_refs | JSONB DEFAULT '[]' | `[{ file_id, file_name, type }]` |
| failure_reason | TEXT | |
| retry_count | INTEGER DEFAULT 0 | |
| created_at | TIMESTAMPTZ | |

### Relationships

```
Project 1──N ProjectVersion
ProjectVersion 1──1 Plan (via version_id)
Plan 1──N ProjectRun
ProjectRun 1──N WorkflowStep tasks (via run_id on agent_task_queue)
Project 1──1 Channel (project's dedicated channel)
```

### Rules

- Fork creates a new ProjectVersion with snapshot of current plan + workflow
- Execution results belong to Run → Plan → Version
- One active Run per project at a time (MVP)
- Scheduled/recurring: reuse Plan, create new Run each time
- Project creation auto-creates Channel with all participants

---

## 4. Execution

### `workflow_step` (EXTEND existing)

New columns:

| Column | Type | Description |
|--------|------|-------------|
| run_id | UUID FK → project_run | Which run this step belongs to |
| owner_escalation_policy | JSONB DEFAULT '{}' | `{ escalate_after_seconds, escalate_to }` |
| timeout_rule | JSONB DEFAULT '{}' | `{ max_duration_seconds, action: "retry"/"fail"/"escalate" }` |
| retry_rule | JSONB DEFAULT '{}' | `{ max_retries, retry_delay_seconds }` |
| human_approval_required | BOOLEAN DEFAULT FALSE | |
| input_context_refs | JSONB DEFAULT '[]' | Input context references |
| output_refs | JSONB DEFAULT '[]' | Output artifact references |
| actual_agent_id | UUID FK → agent | Agent that actually executed (may differ from agent_id if fallback used) |

### `agent_task_queue` (EXTEND existing)

New columns:

| Column | Type | Description |
|--------|------|-------------|
| workflow_step_id | UUID FK → workflow_step | Link to workflow step |
| run_id | UUID FK → project_run | Link to project run |

### Task State Machine

```
pending → queued → assigned → running → completed
                       ↓          ↓
                   waiting_input  blocked → retrying → running
                                     ↓          ↓
                                  timeout    failed → cancelled
```

See `docs/state-machines.md` for transition rules.

---

## 5. File

### `file_index` (NEW)

| Column | Type | Description |
|--------|------|-------------|
| id | UUID PK | |
| workspace_id | UUID FK → workspace | |
| uploader_identity_id | UUID NOT NULL | Member or agent who uploaded |
| uploader_identity_type | TEXT NOT NULL | 'member' / 'agent' |
| owner_id | UUID NOT NULL | Owning member |
| source_type | TEXT NOT NULL | 'conversation' / 'project' / 'external' |
| source_id | UUID NOT NULL | Conversation ID, project ID, etc. |
| file_name | TEXT NOT NULL | |
| file_size | BIGINT | |
| content_type | TEXT | |
| storage_path | TEXT | S3 key |
| access_scope | JSONB DEFAULT '{}' | `{ type: "private"/"conversation"/"project"/"organization" }` |
| channel_id | UUID | Source channel if from conversation |
| project_id | UUID | Source project if from project |
| created_at | TIMESTAMPTZ | |

### `file_snapshot` (NEW)

| Column | Type | Description |
|--------|------|-------------|
| id | UUID PK | |
| file_id | UUID FK → file | Original file |
| snapshot_at | TIMESTAMPTZ NOT NULL | When snapshot was taken |
| storage_path | TEXT NOT NULL | S3 key for snapshot copy |
| referenced_by | JSONB DEFAULT '[]' | `[{ type: "project"/"plan", id }]` |
| created_at | TIMESTAMPTZ | |

### Rules

- Agent files are visible to their Owner by default
- Cross-Owner channel files are shared with all channel participants
- Project file references use FileSnapshot (immutable)
- Message deletion unbinds file but doesn't delete it

---

## 6. Notification / Escalation

### `inbox_item` (EXTEND existing)

The existing `inbox_item` table already covers most notification needs. New fields:

| Column | Type | Description |
|--------|------|-------------|
| action_required | BOOLEAN DEFAULT FALSE | Requires user action |
| action_type | TEXT | 'approve' / 'retry' / 'replace_agent' / 'acknowledge' |
| deadline | TIMESTAMPTZ | Action deadline |
| resolution_status | TEXT DEFAULT 'pending' | 'pending' / 'resolved' / 'expired' |
| related_project_id | UUID FK → project | |
| related_run_id | UUID FK → project_run | |
| related_conversation_id | UUID FK → channel | |

### New `inbox_item.type` values

- `plan_approval_requested` - Plan needs owner approval
- `agent_offline_during_run` - Agent went offline during execution
- `step_failed` - Workflow step failed
- `step_timeout` - Workflow step timed out
- `agent_replacement_needed` - All fallback agents failed
- `run_completed` - Project run finished
- `run_failed` - Project run failed

### Audit Logging

Extend existing `activity_log` with new action types:

- `impersonation_send` - Owner sent message as agent
- `plan_modified` - Owner modified auto-generated plan
- `agent_replaced` - Agent replaced during execution
- `auto_assignment` - System Agent auto-assigned responder
- `auto_plan_generated` - System Agent generated plan
- `plan_approved` - Owner approved plan
- `plan_rejected` - Owner rejected plan
- `run_started` - Project execution started
- `run_cancelled` - Project execution cancelled

---

## Index Strategy

```sql
-- Project
CREATE INDEX idx_project_workspace ON project(workspace_id);
CREATE INDEX idx_project_status ON project(workspace_id, status);
CREATE INDEX idx_project_creator ON project(creator_owner_id);

-- Project Version
CREATE INDEX idx_project_version_project ON project_version(project_id);

-- Project Run
CREATE INDEX idx_project_run_project ON project_run(project_id);
CREATE INDEX idx_project_run_status ON project_run(status);

-- Thread
CREATE INDEX idx_thread_channel ON thread(channel_id);

-- File Index
CREATE INDEX idx_file_index_workspace ON file_index(workspace_id);
CREATE INDEX idx_file_index_owner ON file_index(owner_id);
CREATE INDEX idx_file_index_source ON file_index(source_type, source_id);
CREATE INDEX idx_file_index_project ON file_index(project_id);

-- File Snapshot
CREATE INDEX idx_file_snapshot_file ON file_snapshot(file_id);

-- Agent extensions
CREATE INDEX idx_agent_type ON agent(workspace_id, agent_type);
CREATE INDEX idx_agent_online ON agent(workspace_id, online_status);

-- Task queue extensions
CREATE INDEX idx_task_queue_workflow_step ON agent_task_queue(workflow_step_id);
CREATE INDEX idx_task_queue_run ON agent_task_queue(run_id);
```
