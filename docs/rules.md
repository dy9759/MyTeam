# My Team - Rules

This document defines the business rules that govern the Owner-Agent collaborative workflow platform. Each rule is actionable and includes specific default values for implementation.

---

## A. Permission Rules

### A1. Owner Authority over Personal Agents

- An Owner has full control over their own Personal Agents: create, edit, suspend, delete, impersonate, view conversations, assign to projects.
- An Owner **cannot** access, control, or view conversations of another Owner's Personal Agents (MVP).
- An Owner **cannot** dispatch or reassign another Owner's Personal Agent to a task (MVP).

### A2. System Agent Boundary of Autonomous Action

- The Global System Agent acts as an orchestrator. It may:
  - Auto-assign a responder agent to an unresponded message (per mediation rules in Section B).
  - Auto-generate a project plan from conversation context (requires Owner approval before execution).
  - Auto-trigger fallback agent replacement when a step fails and retry is exhausted.
  - Send notifications and escalation messages to project channels and inboxes.
- The System Agent **must not**:
  - Execute tasks itself (it dispatches, not performs).
  - Approve plans or start project runs (Owner-only).
  - Override an Owner's explicit agent assignment.
  - Access private DM conversations it is not a participant of.
- Page System Agents operate only within their page scope (e.g., Account Agent only on the account page). They cannot modify data outside their page boundary.

### A3. Cross-Organization Restrictions

- All data is scoped by `workspace_id`. No cross-workspace queries or actions.
- Agents cannot operate across workspaces. An agent belongs to exactly one workspace and one Owner within that workspace.
- Membership checks gate all access. A user must be a workspace member to perform any action.

### A4. Conversation Visibility Rules

- **DM (private):** Only the 2 participants can read/write.
- **Channel (private):** Only channel members can read/write.
- **Channel (public):** All workspace members can read; only members can write.
- **Thread:** Inherits visibility from parent channel. Any channel member can participate.
- **Owner viewing agent conversations:** An Owner can view all DMs and channel messages involving their own agents. They cannot view conversations of other Owners' agents.
- **Project channels:** All project participants (assigned agents + their owners) are auto-added as members.

---

## B. Message Assignment Rules

### B1. Response Necessity Criteria

A message triggers response assignment only if **at least one** of these conditions is met:

| Condition | Key | Description |
|-----------|-----|-------------|
| Direct question | `is_question` | Message ends with `?` or contains question patterns |
| Explicit mention | `has_mention` | Message contains `@agent_name` or `@agent_id` |
| Project-related | `project_related` | Message is in a project channel and references a project task or deliverable |
| Capability match | `matches_agent_capability` | Message topic matches an agent's `identity_card.skills` or `identity_card.capabilities` |
| SLA breach | `exceeds_sla` | A previous message in the conversation required response and has been unanswered for longer than `reply_policy.sla_timeout_seconds` |

Default `sla_timeout_seconds`: **300 seconds** (5 minutes).

If none of these conditions are met, the message is treated as informational and does not trigger assignment.

### B2. Default Assignment Priority

When a message needs a response, the System Agent selects the responder in this order:

1. **@mentioned agent** -- If the message explicitly mentions an agent, that agent is assigned. If multiple agents are mentioned, all are assigned (each responds independently).
2. **Project-assigned agent** -- If the message is in a project channel, prefer the agent assigned to the currently active step or the project's primary agent.
3. **Capability-matched agent** -- Match message content against `identity_card.capabilities` and `identity_card.skills` of agents in the channel. Select the agent with the highest relevance score.
4. **Fallback: Owner notification** -- If no agent matches or all matched agents are offline/suspended, notify the relevant Owner.

### B3. Multi-Agent Conflict Resolution

When multiple agents could respond to the same message:

- If the message contains explicit `@mentions`, only mentioned agents respond.
- If assignment is by capability match, select the **single best match** based on: (1) skill relevance score, (2) `workload_status = idle` preferred over `busy`, (3) most recently active (`last_active_at`).
- Tie-breaking: lower `agent.created_at` wins (earlier-created agent has priority).
- **Never assign more than 2 agents** to the same non-@mention message to prevent noise.

### B4. SLA Timeout Escalation Chain

When a message requiring response exceeds the SLA timeout:

1. **T + 0s:** Message flagged as needing response. Primary agent assigned.
2. **T + `sla_timeout_seconds` (default 300s):** Primary agent has not responded. System Agent tries next agent by priority (B2 rules).
3. **T + `sla_timeout_seconds * 2` (default 600s):** Fallback agent has not responded. System Agent sends `warning` severity notification to the Owner.
4. **T + `sla_timeout_seconds * 3` (default 900s):** Owner has not responded. System Agent sends `critical` severity notification to the Owner.

### B5. Anti-Noise Rules

- **No agent-to-agent reply loops:** If an agent's message triggers another agent's response criteria, the second agent may respond **once**. A third agent response in the same thread within 60 seconds is suppressed. Agents must not auto-reply to other agents' auto-replies.
- **Flood protection in group chat:** No single agent may send more than **5 messages per minute** in a channel. Messages beyond this rate are queued and batched.
- **System Agent messages:** System Agent notifications (assignment, status updates) are rate-limited to **10 per minute per channel**.
- **Self-reply suppression:** An agent must not respond to its own messages.

---

## C. Impersonation Rules

### C1. When Allowed

- Only an Owner may impersonate their **own** Personal Agents. No exceptions.
- System Agents and Page System Agents **cannot** be impersonated.
- An Owner cannot impersonate another Owner's agent.

### C2. Display Rules

- Every impersonated message must have `is_impersonated = true` on the `message` record.
- The UI must display a visible "sent by Owner" indicator on impersonated messages.
- The `is_impersonated` flag is visible to **all participants** in the conversation (it is not hidden from anyone).
- The indicator must show which Owner is impersonating (not just that it was impersonated).

### C3. Audit Logging Requirements

Every impersonation action logs to `activity_log` with:

| Field | Value |
|-------|-------|
| action | `impersonation_send` |
| actor_id | Owner's user ID (the real sender) |
| target_id | Agent ID (the impersonated identity) |
| resource_type | `message` |
| resource_id | The created message ID |
| metadata | `{ conversation_id, content_preview }` |

### C4. Revocation Mechanism

- An Owner can end impersonation at any time by toggling the impersonation mode off.
- There is no session timeout for impersonation; it remains active until explicitly ended.
- When impersonation ends, subsequent messages from the Owner are sent under the Owner's own identity.
- Workspace admins can disable impersonation for a workspace via workspace settings (future).

---

## D. Project Creation Rules

### D1. Eligible Conversations

A project can be created from **any** conversation type:

- DM -- Owner creates a project referencing DM context.
- Channel -- Owner creates a project from channel discussion.
- Thread -- Owner creates a project from a specific thread.

The source conversation is recorded in `project.source_conversations` as `[{ conversation_id, type, snapshot_at }]`.

### D2. Context Reference Granularity

When creating a project, the Owner selects the context scope:

| Scope | Description | `source_conversations` entry |
|-------|-------------|------------------------------|
| Whole conversation | All messages in the conversation up to snapshot time | `{ conversation_id, type: "full", snapshot_at }` |
| Message range | Specific message range | `{ conversation_id, type: "range", start_message_id, end_message_id, snapshot_at }` |
| Snapshot | Point-in-time snapshot of conversation | `{ conversation_id, type: "snapshot", snapshot_at }` |

The System Agent uses the selected context to generate the task brief.

### D3. Task Brief Template Structure

The task brief (`plan.task_brief`) must contain:

```
## Objective
[What needs to be accomplished - generated from conversation context]

## Background
[Relevant conversation context and decisions made]

## Scope
[What is in scope and explicitly out of scope]

## Success Criteria
[Measurable outcomes that define completion]

## Constraints
[Time, resource, or technical constraints]

## References
[Links to source conversations, files, or external resources]
```

### D4. Required Fields for Project Creation

| Field | Required | Default |
|-------|----------|---------|
| `title` | Yes | -- |
| `description` | No | Generated from conversation context |
| `creator_owner_id` | Yes (auto-filled) | Current user |
| `source_conversations` | Yes (at least one) | -- |
| `schedule_type` | No | `one_time` |
| `task_brief` | Yes | Auto-generated, editable by Owner |

### D5. Auto-Channel Creation Rules

When a project is created, a dedicated channel is automatically created:

- **Naming convention:** `proj-{project-title-slug}` (lowercased, spaces replaced with hyphens, max 50 characters, truncated with `...` if longer).
- **Initial members:** Creator Owner + all agents assigned in the plan + their Owners + System Agent.
- **Channel type:** `conversation_type = 'channel'`, `visibility = 'private'`, `project_id` set.
- **Purpose:** All project-related discussions, status updates, and result notifications flow through this channel.
- If a channel with the same name already exists, append a numeric suffix: `proj-my-project-2`.

---

## E. Plan Generation and Editing Rules

### E1. Plan Generation Inputs

The System Agent generates a plan from:

1. **Conversation context** -- The messages referenced in `source_conversations`.
2. **Agent identity cards** -- `identity_card` JSONB for all available agents in the workspace (capabilities, skills, tools, completed projects).
3. **Task brief** -- The structured brief (Section D3).

The generated plan populates `plan.tasks` (steps) and `plan.assigned_agents` (agent assignments with fallbacks).

### E2. Owner Approval Required

- A newly generated plan has `approval_status = 'draft'`.
- The Owner must review and set `approval_status = 'approved'` before any execution can begin.
- If the Owner rejects (`approval_status = 'rejected'`), the plan is archived and a new version may be generated.
- No task dispatch occurs until `approval_status = 'approved'`.
- Plan approval is logged as `plan_approved` in `activity_log`.

### E3. Plan Modification Rules

- **Owner can edit any field:** task descriptions, step order, dependencies, assigned agents, fallback agents, estimated durations, risk points.
- **Every modification is logged** in `activity_log` with action `plan_modified`, including a before/after diff in `metadata`.
- **System Agent cannot modify** an approved plan. If re-generation is needed, a new plan version is created.
- Modifications to an approved plan reset `approval_status` to `pending_approval` (the Owner must re-approve).

### E4. Agent Assignment Rules

- **Auto-match by skills:** When generating a plan, the System Agent matches each step's `required_skills` against agents' `identity_card.skills` and `identity_card.capabilities`.
- **Fallback agents:** Each step may have `fallback_agent_ids[]` -- agents tried in order if the primary fails.
- **Owner override:** The Owner can change any agent assignment during plan review. Owner's assignment takes precedence over auto-match.
- **Agent availability check:** At assignment time, prefer agents with `online_status = 'online'` and `workload_status = 'idle'`. Warn the Owner if the assigned agent is offline or busy.

### E5. Dependency Definition

- Dependencies are defined as `depends_on` in each task, referencing other tasks by `order` index.
- A step cannot start execution until all its `depends_on` steps have status `completed`.
- Circular dependencies are rejected at plan validation time (before approval).
- Steps with no dependencies can execute in parallel (subject to agent availability).

### E6. Re-Generation Rules

- The Owner can request plan re-generation at any time before execution starts.
- Re-generation creates a **new plan version** (new row in `plan` with incremented version, linked to a new `project_version`).
- The previous plan version is preserved (not deleted) for audit trail.
- Re-generation uses the same conversation context unless the Owner provides updated context.
- Re-generation is logged as `auto_plan_generated` in `activity_log`.

---

## F. Execution Rules

### F1. Task Dispatch

- **One task per step at a time:** For any given `workflow_step`, only one `agent_task_queue` entry may be in an active state (`queued`, `assigned`, `running`) at a time.
- Tasks are dispatched in dependency order. A step is dispatched only when all `depends_on` steps are `completed`.
- Independent steps (no dependencies between them) may be dispatched in parallel.
- Dispatch creates an `agent_task_queue` record with `workflow_step_id` and `run_id`.

### F2. Idempotency

- Re-dispatching a failed task must not produce duplicate side effects.
- Each dispatch attempt increments `retry_count` on the `project_run` or `workflow_step`.
- The agent receives the same `input_context_refs` on retry.
- If a task has partial output from a previous attempt, the new attempt receives the partial output as additional context (not as completed work).

### F3. Retry Strategy

Configurable per step via `retry_rule` JSONB:

| Parameter | Type | Default |
|-----------|------|---------|
| `max_retries` | integer | 2 |
| `retry_delay_seconds` | integer | 30 |

Behavior:
- On step failure, if `retry_count < max_retries`, wait `retry_delay_seconds` then re-dispatch to the **same agent**.
- Retries use exponential backoff: actual delay = `retry_delay_seconds * 2^(retry_count - 1)`. First retry: 30s, second retry: 60s.
- After `max_retries` exhausted, proceed to replacement strategy (F4).

### F4. Replacement Strategy

When retries are exhausted for the primary agent:

1. Try `fallback_agent_ids` in order. Each fallback agent gets the same `retry_rule` applied (resets retry count).
2. If all fallback agents fail, execute `owner_escalation_policy`:
   - Send `critical` notification to `escalate_to` Owner.
   - `action_type = 'replace_agent'`.
   - Owner can: manually assign a new agent, retry with the same agent, skip the step, or cancel the run.
3. If `owner_escalation_policy.escalate_after_seconds` elapses without Owner action, send a follow-up `critical` notification.

Default `escalate_after_seconds`: **600 seconds** (10 minutes).

### F5. Timeout Strategy

Configurable per step via `timeout_rule` JSONB:

| Parameter | Type | Default |
|-----------|------|---------|
| `max_duration_seconds` | integer | 1800 (30 minutes) |
| `action` | string | `"retry"` |

Timeout detection:
- A step is timed out if: `now() - step_started_at > max_duration_seconds` AND no heartbeat received.
- An agent is considered stuck if: `online_status = 'offline'` for > 60 seconds while task status is `running`.

Actions on timeout:
- `"retry"`: Re-dispatch the step (follows retry strategy in F3).
- `"fail"`: Mark step as `failed`, proceed to replacement strategy (F4).
- `"escalate"`: Skip retry, immediately notify Owner.

### F6. Context Inheritance on Agent Replacement

When a replacement agent takes over a failed step:

- The replacement agent receives the original `input_context_refs` (same inputs as the original agent).
- If the original agent produced partial output, it is included in the replacement agent's context as `previous_partial_output`.
- The replacement agent's results are recorded under the replacement agent's ID (`actual_agent_id` on the workflow step).
- An `agent_replaced` entry is logged in `activity_log` with: original agent ID, replacement agent ID, reason, step ID.

---

## G. File Rules

### G1. File Ownership

- The uploader (`uploader_identity_id` + `uploader_identity_type`) owns the file record.
- If an agent uploads a file, the agent's Owner (`agent.owner_id`) is set as `file_index.owner_id`.
- The Owner can view, download, and manage all files uploaded by their agents.

### G2. File Snapshot for Project References

- When a project plan references a file, a `file_snapshot` is created at that point in time.
- The snapshot stores an immutable copy of the file at `storage_path`.
- If the original file is updated later, existing project references still point to the old snapshot.
- A new snapshot is created only if the project explicitly re-references the updated file.

### G3. File Deletion Impact

- Deleting a message that contains a file attachment unbinds the file from that message but does **not** delete the file from `file_index`.
- The file remains accessible via the file index and any project snapshots that reference it.
- Actual file deletion (from storage) is an explicit admin action, not triggered by message deletion.
- File snapshots are never auto-deleted. They persist as long as the referencing project exists.

### G4. Permission Inheritance

- **Private files:** Only the uploader and the uploader's Owner can access.
- **Conversation files:** `access_scope.type = 'conversation'`. All members of the source channel can access.
- **Project files:** `access_scope.type = 'project'`. All project channel members can access.
- **Organization files:** `access_scope.type = 'organization'`. All workspace members can access.

### G5. Cross-Owner Visibility

- Files in shared channels (channels with members from multiple Owners) are visible to **all channel participants**.
- An agent's files in a shared project are visible to all project participants, regardless of which Owner created the agent.
- Files in private DMs are visible only to the DM participants.

---

## H. Notification Rules

### H1. Business Events (info severity)

| Event | `inbox_item.type` | Severity | Target |
|-------|-------------------|----------|--------|
| Project created | `project_created` | info | Creator Owner |
| Task assigned to agent | `task_assigned` | info | Agent's Owner |
| Run completed successfully | `run_completed` | info | Creator Owner + project channel |
| Plan approval requested | `plan_approval_requested` | info | Creator Owner |
| Plan approved | `plan_approved` | info | Project channel |

### H2. Anomaly Events (warning/critical severity)

| Event | `inbox_item.type` | Severity | Target |
|-------|-------------------|----------|--------|
| Agent went offline during run | `agent_offline_during_run` | warning | Agent's Owner |
| Step failed (retrying) | `step_failed` | warning | Agent's Owner + project channel |
| Step timed out | `step_timeout` | warning | Agent's Owner |
| All retries exhausted | `step_failed` | critical | Agent's Owner |
| All fallback agents failed | `agent_replacement_needed` | critical | Creator Owner |
| Run failed | `run_failed` | critical | Creator Owner + project channel |

### H3. Notification Targets

- **Primary target:** The Owner most relevant to the event (agent's Owner for agent events, project creator for project events).
- **Secondary target:** The project's dedicated channel (for project-related events).
- Notifications are delivered to both `inbox_item` (for the inbox UI) and as a message in the relevant channel.

### H4. Escalation Priority

Notifications are processed in priority order: `critical` > `warning` > `info`.

- `critical` notifications are displayed prominently (badge, sound if enabled, top of inbox).
- `warning` notifications appear in inbox with a warning indicator.
- `info` notifications appear in inbox as standard items.

### H5. Action-Required Deadlines

- Notifications with `action_required = true` have a `deadline` timestamp.
- Default deadline by action type:
  - `approve` (plan approval): **24 hours**
  - `retry`: **1 hour**
  - `replace_agent`: **30 minutes**
  - `acknowledge`: **No deadline** (informational acknowledgment)
- When `deadline` expires and `resolution_status` is still `pending`:
  - Set `resolution_status = 'expired'`.
  - Create a new `critical` severity notification escalating to workspace admins.
  - For `replace_agent` expiry: System Agent attempts next available agent automatically if `fallback_agent_ids` has remaining entries.

---

## I. Audit Rules

### I1. Impersonation Audit

All impersonation actions are logged with:
- Real identity: the Owner who initiated the impersonation (`actor_id`).
- Impersonated identity: the Agent being impersonated (`target_id`).
- Action performed: message content, channel, timestamp.
- Action type: `impersonation_send`.

### I2. Plan Modification Audit

All plan modifications are logged with:
- `action = 'plan_modified'`
- `metadata.before`: JSON snapshot of the field(s) before modification.
- `metadata.after`: JSON snapshot of the field(s) after modification.
- `metadata.fields_changed`: list of field names that were modified.
- `actor_id`: the Owner who made the modification.

### I3. Agent Replacement Audit

All agent replacements are logged with:
- `action = 'agent_replaced'`
- `metadata.original_agent_id`: the agent that was replaced.
- `metadata.replacement_agent_id`: the new agent.
- `metadata.reason`: `'retry_exhausted'` / `'agent_offline'` / `'owner_manual'` / `'timeout'`.
- `metadata.step_id`: the workflow step affected.

### I4. System Agent Autonomous Actions Audit

All System Agent autonomous actions are logged:
- `auto_assignment`: message assignment with `metadata.conversation_id`, `metadata.message_id`, `metadata.assigned_agent_id`.
- `auto_plan_generated`: plan generation with `metadata.project_id`, `metadata.plan_id`, `metadata.source_conversations`.
- Auto-triggered fallback with `metadata.trigger_reason`.

### I5. Retention Policy

- Audit logs (`activity_log`) are retained **indefinitely**. No automatic deletion.
- No TTL or expiration is applied to audit records.
- Storage management (archiving old records to cold storage) is deferred to a future phase.

### I6. Access Control

- **Workspace admins** can view all audit logs within their workspace.
- **Owners** can view audit logs related to their own agents and projects they created.
- **Agents** cannot view audit logs.
- Audit logs are read-only. No user can modify or delete audit entries.

---

## J. Metrics Rules

### J1. Metric Definitions

#### Agent Response Rate

```
agent_response_rate = responded / (responded + expired_sla)
```

- `responded`: messages where the assigned agent sent a reply within `sla_timeout_seconds`.
- `expired_sla`: messages where the assigned agent did not reply within `sla_timeout_seconds`.
- Scope: per agent, per time window.

#### Task Completion Rate

```
task_completion_rate = completed / (completed + failed + cancelled)
```

- Counts `workflow_step` final statuses within a time window.
- Scope: per agent, per project, per workspace.

#### Average Task Duration

```
average_task_duration = avg(completed_at - started_at)
```

- Only includes steps with status `completed`.
- Unit: seconds. Display in human-readable format (e.g., "2h 15m").
- Scope: per agent, per project, per workspace.

#### Timeout Rate

```
timeout_rate = timed_out / total_dispatched
```

- `timed_out`: steps that entered `timeout` state at least once.
- `total_dispatched`: all steps that were dispatched (entered `assigned` or `running` state).
- Scope: per agent, per workspace.

#### Plan Modification Rate

```
plan_modification_rate = modified_plans / total_auto_generated_plans
```

- `modified_plans`: plans with at least one `plan_modified` audit log entry after auto-generation.
- `total_auto_generated_plans`: plans with an `auto_plan_generated` audit log entry.
- Scope: per workspace.

#### Execution Deviation Rate

```
execution_deviation_rate = abs(actual_steps - planned_steps) / planned_steps
```

- `planned_steps`: number of steps in the approved plan.
- `actual_steps`: number of steps actually executed (including added/skipped steps).
- A deviation rate of 0 means execution matched the plan exactly.
- Scope: per project run.

### J2. Collection Strategy

- **Event-driven:** Metrics are computed from events emitted via the internal events bus (`internal/events/`).
- Key events: `step.completed`, `step.failed`, `step.timeout`, `message.assigned`, `message.responded`, `plan.modified`, `plan.generated`.
- **Aggregation:** Metrics are aggregated per workspace per time window (daily, weekly, monthly).
- Raw event data is retained; aggregates are computed on read or via periodic batch jobs.

### J3. Display

- Metrics are displayed on the metrics page (Phase 5).
- API endpoint: `GET /api/metrics` with query params for `workspace_id`, `agent_id`, `project_id`, `time_range`.
- Time ranges: `last_24h`, `last_7d`, `last_30d`, `all_time`.
- Metrics are workspace-scoped; cross-workspace aggregation is not supported.
