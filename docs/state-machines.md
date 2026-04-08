# My Team - State Machine Definitions

Reference document for all state machines in the Owner-Agent collaborative workflow platform. Each section defines valid states, transitions, triggers, side effects, and constraints.

---

## 1. Agent Lifecycle State Machine

Tracks an agent's availability and capacity for task dispatch.

### States

| State | Description |
|-------|-------------|
| `offline` | Agent process is not running or has not sent a heartbeat within the liveness window |
| `online` | Agent process is registered and heartbeating, but has not yet declared readiness |
| `idle` | Agent is online and ready to accept tasks |
| `busy` | Agent is actively executing one or more tasks |
| `blocked` | Agent is stuck: execution timed out, dependency unmet, or tool failure |
| `degraded` | Agent is online but operating at reduced capacity (e.g., some tools unavailable) |
| `suspended` | Agent is manually paused by its Owner; cannot receive new tasks |

### Transitions

```
                          +--------------------------------------------------+
                          |                                                  |
                          v                                                  |
 +--------+  register  +---------+  ready   +------+  task_dispatched  +------+
 |offline |----------->| online  |--------->| idle |------------------>| busy |
 +--------+            +---------+          +------+                   +------+
     ^                      |                  ^ ^                       |  |
     |                      |                  | |                       |  |
     |  heartbeat_lost      |                  | |  task_completed       |  |  execution_stuck
     +----------------------+                  | +----------------------+  |
     |                                         |                          v
     |                                         |  owner_intervenes   +---------+
     |                                         +---------------------|blocked  |
     |                                         |                     +---------+
     |                                         |                          |
     |                                         |  tools_recovered         |  heartbeat_timeout
     |                                         |                          +---------> offline
     |                                    +----------+
     |                                    | degraded |
     |                                    +----------+
     |                                         ^
     |                                         | tools_partially_unavailable
     |                                         |
     +-------- any active state -------> +-----------+
               owner_suspends            | suspended |
                                         +-----------+
```

### Transition Table

| From | To | Trigger | Who/What | Condition |
|------|----|---------|----------|-----------|
| `offline` | `online` | Agent process registers or heartbeat resumes | Agent daemon | Heartbeat received after being offline |
| `online` | `idle` | Agent declares ready | Agent daemon | Registration handshake complete |
| `online` | `offline` | No heartbeat within liveness window | System (heartbeat monitor) | No heartbeat for > 60 seconds |
| `idle` | `busy` | Task dispatched to agent | Scheduler (System Agent) | Agent matched to a queued task |
| `busy` | `idle` | All assigned tasks completed/failed | Agent daemon | No remaining active tasks |
| `busy` | `blocked` | Execution stuck | System (timeout monitor) | No heartbeat for > 60s while running, or dependency unmet |
| `blocked` | `idle` | Owner intervenes or System Agent replaces | Owner / System Agent | Manual fix, task reassigned, or dependency resolved |
| `blocked` | `offline` | Heartbeat completely lost | System (heartbeat monitor) | No heartbeat for > 120s after entering blocked |
| `idle` | `degraded` | Partial tool failure detected | Agent daemon self-report | Agent reports reduced capability |
| `busy` | `degraded` | Tool becomes unavailable mid-execution | Agent daemon self-report | Non-critical tool lost; execution continues at reduced capacity |
| `degraded` | `idle` | Tools recovered | Agent daemon self-report | All tools operational again |
| `degraded` | `offline` | Heartbeat lost | System (heartbeat monitor) | No heartbeat for > 60s |
| Any active | `suspended` | Owner suspends agent | Owner (manual) | Owner explicitly pauses the agent |
| `suspended` | `idle` | Owner resumes agent | Owner (manual) | Owner explicitly resumes and agent is heartbeating |
| `suspended` | `offline` | Agent process stops while suspended | System | Heartbeat lost while suspended |

### Dispatch Rules

| Agent State | Can Receive New Tasks? | Notes |
|-------------|----------------------|-------|
| `offline` | No | Not reachable |
| `online` | No | Not yet ready |
| `idle` | Yes | Preferred dispatch target |
| `busy` | Conditional | Only if concurrency limit not reached (MVP: 1 task at a time) |
| `blocked` | No | Cannot accept until unblocked |
| `degraded` | Conditional | Only tasks matching remaining capabilities |
| `suspended` | No | Owner must resume first |

---

## 2. Project Status State Machine

Tracks the lifecycle of a project from creation to completion or archival.

### States

| State | Description |
|-------|-------------|
| `not_started` | Project created, plan may be in draft or pending approval |
| `running` | Active execution in progress (at least one Run is running) |
| `paused` | Execution paused by Owner; can be resumed |
| `completed` | All runs finished successfully |
| `failed` | Active run failed and no automatic recovery possible |
| `archived` | Project moved to archive; read-only |

### Transitions

```
 +--------------+   run_started    +---------+   all_steps_done   +-----------+
 | not_started  |----------------->| running |-------------------->| completed |
 +--------------+                  +---------+                     +-----------+
       |                            |      ^                            |
       |                            |      |                            |
       |                  owner_    |      | owner_                     | owner_archives
       |                  pauses    |      | resumes                    v
       |                            v      |                      +-----------+
       |                          +--------+                      | archived  |
       |                          | paused |                      +-----------+
       |                          +--------+                           ^
       |                            |                                  |
       |                            | owner_archives                   |
       |                            +--------------------->+-----------+
       |                                                        ^
       |                                                        |
       |   critical_step_failed    +---------+  owner_archives  |
       +-------------------------->| failed  |------------------+
                                   +---------+
                                        ^
                                        |
                              +---------+
                              | running | (critical_step_failed)
                              +---------+
```

### Transition Table

| From | To | Trigger | Who/What | Side Effects |
|------|----|---------|----------|-------------|
| `not_started` | `running` | Plan approved and run started | Owner (approves plan) / System Agent (starts run) | Creates ProjectRun, dispatches first tasks, broadcasts `project.status_changed` WS event |
| `running` | `paused` | Owner pauses execution | Owner (manual) | Pauses all active tasks in current Run, broadcasts `project.status_changed` |
| `paused` | `running` | Owner resumes execution | Owner (manual) | Resumes paused tasks, broadcasts `project.status_changed` |
| `running` | `completed` | All steps in active Run completed successfully | System (automatic) | Sets Run status to `completed`, sends `run_completed` notification to Owner, broadcasts `project.status_changed` |
| `running` | `failed` | Critical step failed after exhausting retries and fallbacks | System (automatic) | Sets Run status to `failed`, sends `run_failed` critical notification to Owner, broadcasts `project.status_changed` |
| `not_started` | `failed` | N/A - invalid transition | -- | Projects cannot fail before they start |
| `completed` | `archived` | Owner archives project | Owner (manual) | Project becomes read-only, project channel archived |
| `failed` | `archived` | Owner archives project | Owner (manual) | Project becomes read-only |
| `failed` | `running` | Owner fixes issues and restarts | Owner (manual) | Creates new Run from same Plan, dispatches tasks |
| `paused` | `archived` | Owner archives project | Owner (manual) | Cancels current Run, project becomes read-only |
| `not_started` | `archived` | Owner archives project | Owner (manual) | Project becomes read-only |

### Rules

- Only **Owner** can approve a plan and trigger the first run.
- Only **Owner** can pause, resume, and archive.
- **System Agent** triggers `running` -> `completed` and `running` -> `failed` automatically.
- Recurring/scheduled projects cycle: `running` -> `completed` -> `running` (new Run created on schedule via `cron_expr`).
- A project in `archived` cannot return to any other state.

---

## 3. Plan Approval State Machine

Controls the review gate between plan generation and execution.

### States

| State | Description |
|-------|-------------|
| `draft` | Plan is being authored or auto-generated; not yet ready for review |
| `pending_approval` | Plan submitted for Owner review |
| `approved` | Owner approved the plan; eligible for execution |
| `rejected` | Owner rejected the plan; needs revision |

### Transitions

```
 +-------+  submit_for_approval   +-------------------+
 | draft |----------------------->| pending_approval  |
 +-------+                        +-------------------+
     ^                               |             |
     |                               |             |
     |  revise (after rejection)     |             |
     +-------------------------------+             |
     |                               |             |
     |                     owner_    |             | owner_
     |                     rejects   |             | approves
     |                               v             v
     |                          +----------+  +----------+
     +<-------------------------| rejected |  | approved |
        author revises          +----------+  +----------+
```

### Transition Table

| From | To | Trigger | Who Can Trigger | Conditions |
|------|----|---------|----------------|------------|
| `draft` | `pending_approval` | Author submits plan for review | System Agent (auto-generated plan) or Owner (manual plan) | Plan must have at least one task with an assigned agent |
| `pending_approval` | `approved` | Owner approves the plan | **Owner only** | Owner reviews tasks, agents, dependencies, and accepts |
| `pending_approval` | `rejected` | Owner rejects the plan | **Owner only** | Owner provides rejection reason |
| `rejected` | `draft` | Author revises plan | System Agent (re-generates) or Owner (edits manually) | Rejection reason addressed |
| `approved` | `draft` | Plan needs modification after approval | Owner (manual) | Resets approval; Run must not be active |

### Rules

- **Only the Owner** can move a plan to `approved`. System Agent cannot approve its own plans.
- System Agent **can** submit plans for approval (`draft` -> `pending_approval`).
- A plan in `approved` state is the only valid input for creating a ProjectRun.
- If the Owner modifies an `approved` plan, it reverts to `draft` and requires re-approval.
- Rejection must include a `rejection_reason` field to guide revision.
- Approval sets `approved_by` (Owner ID) and `approved_at` (timestamp).

---

## 4. Project Run State Machine

Tracks a single execution attempt of an approved plan.

### States

| State | Description |
|-------|-------------|
| `pending` | Run created but not yet started (e.g., scheduled for future) |
| `running` | Steps are being dispatched and executed |
| `paused` | Owner paused execution; active steps suspended |
| `completed` | All steps finished successfully |
| `failed` | Run failed; critical step could not be recovered |
| `cancelled` | Owner or System cancelled the run |

### Transitions

```
 +---------+  start_time_reached   +---------+  all_steps_done    +-----------+
 | pending |---------------------->| running |-------------------->| completed |
 +---------+                       +---------+                     +-----------+
      |                             |      ^
      |                             |      |
      | owner_cancels     owner_    |      | owner_resumes
      |                   pauses    |      |
      v                             v      |
 +-----------+                   +--------+
 | cancelled |                   | paused |
 +-----------+                   +--------+
      ^                             |
      |   owner_cancels             |
      +-----------------------------+
      |
      |   critical_failure
      +-----------------------------+
                                    |
                              +---------+
                              | running |
                              +---------+
                                    |
                                    | critical_step_failed_no_recovery
                                    v
                              +---------+
                              | failed  |
                              +---------+
                                    |
                                    | owner_cancels
                                    v
                              +-----------+
                              | cancelled |
                              +-----------+
```

### Transition Table

| From | To | Trigger | Who/What | Side Effects |
|------|----|---------|----------|-------------|
| `pending` | `running` | Start time reached or immediate start | System (scheduler) / Owner (manual start) | Dispatches first batch of tasks (steps with no dependencies), records `start_at` |
| `running` | `completed` | All steps completed successfully | System (automatic) | Records `end_at`, aggregates `output_refs` from all steps, sends `run_completed` notification |
| `running` | `failed` | Critical step failed after retries + fallbacks exhausted | System (automatic) | Records `end_at`, `failure_reason`, sends `run_failed` critical notification |
| `running` | `paused` | Owner pauses | Owner (manual) | Suspends dispatch of new steps; running steps continue to completion but no new ones start |
| `paused` | `running` | Owner resumes | Owner (manual) | Resumes step dispatch |
| `pending` | `cancelled` | Owner cancels before start | Owner (manual) | No tasks dispatched |
| `running` | `cancelled` | Owner cancels during execution | Owner (manual) | All active tasks cancelled, sends notifications |
| `paused` | `cancelled` | Owner cancels while paused | Owner (manual) | All pending tasks cancelled |
| `failed` | `cancelled` | Owner acknowledges failure and cancels | Owner (manual) | Cleanup; no further retries |

### Automatic Completion Logic

A Run transitions to `completed` when:
1. Every step in the plan has status `completed` or `skipped`.
2. No step is in `running`, `queued`, `assigned`, `retrying`, or `blocked`.

### Automatic Failure Logic

A Run transitions to `failed` when:
1. A step marked as `critical: true` (or any step if no criticality flag) reaches `failed` status.
2. All retries for that step are exhausted (`retry_count >= retry_rule.max_retries`).
3. All fallback agents have been tried and failed.
4. Owner escalation has been triggered but not resolved within the escalation deadline.

### Rules

- One active Run per project at a time (MVP).
- Scheduled/recurring projects: when a Run completes, the scheduler creates a new `pending` Run for the next cron interval.
- `retry_count` on the Run tracks how many times the entire Run has been restarted (distinct from per-step retries).

---

## 5. Task / WorkflowStep State Machine

The most detailed state machine. Tracks each individual step from plan dispatch through execution to completion or failure.

### States

| State | Description |
|-------|-------------|
| `pending` | Step exists in the plan but is not yet eligible for dispatch (dependencies unmet) |
| `queued` | Dependencies satisfied; step is waiting in the dispatch queue |
| `assigned` | An agent has been matched and notified |
| `running` | Agent is actively executing the step |
| `waiting_input` | Step is paused waiting for human input or approval |
| `blocked` | Step is stuck: agent offline, heartbeat lost, or dependency failed |
| `retrying` | Step failed and is being retried (with same or different agent) |
| `timeout` | Step exceeded `max_duration_seconds` without completion |
| `completed` | Step finished successfully with output |
| `failed` | Step failed permanently (retries + fallbacks exhausted) |
| `cancelled` | Step was cancelled by Owner or system |

### ASCII Diagram

```
                                                    +-------------+
                                                    |  completed  |
                                                    +-------------+
                                                          ^
                                                          | agent_reports_success
                                                          |
 +---------+  deps_met  +--------+  agent_matched  +----------+  agent_starts  +---------+
 | pending |----------->| queued |---------------->| assigned |--------------->| running |
 +---------+            +--------+                 +----------+                +---------+
                             ^                                                  |  |  |  |
                             |                                                  |  |  |  |
                             |  fallback_agent_assigned                         |  |  |  |
                             +--------------------------------------------------+  |  |  |
                                                                                   |  |  |
                                           +------------------+  needs_human_input |  |  |
                                           | waiting_input    |<-------------------+  |  |
                                           +------------------+                       |  |
                                             |  input_received                        |  |
                                             +-----> running                          |  |
                                                                                      |  |
                             +----------+  no_heartbeat_N_seconds                     |  |
                             | blocked  |<--------------------------------------------+  |
                             +----------+                                                |
                               |      |                                                  |
                  retry_       |      | dep_failed_                                      |
                  triggered    |      | permanently           exceeds_max_duration       |
                               v      v                                                  |
                          +----------+                       +---------+                 |
                          | retrying |                       | timeout |<----------------+
                          +----------+                       +---------+
                            |      |                           |
               retry_starts |      | max_retries_exceeded      | timeout_action
               (new attempt)|      |                           |
                            v      v                           v
                         running  failed                  retry / fail / escalate
                                    ^
                                    |
                              +----------+
                              | timeout  | (if action = "fail")
                              +----------+

 +----------------------------------------------------------------------+
 | Any active state (pending/queued/assigned/running/waiting_input/     |
 | blocked/retrying/timeout) ----owner_or_system_cancels----> cancelled |
 +----------------------------------------------------------------------+
```

### Transition Table

| From | To | Trigger | Who/What | Details |
|------|----|---------|----------|---------|
| `pending` | `queued` | All dependency steps completed | Scheduler (automatic) | Scheduler checks `dependency_ids[]`; all must be `completed` or `skipped` |
| `queued` | `assigned` | Agent matched to step | Scheduler (automatic) | Picks `assignee_agent_id` if idle; otherwise tries `fallback_agent_ids[]` in order |
| `assigned` | `running` | Agent acknowledges and starts execution | Agent daemon | Agent sends `step.started` event; `started_at` timestamp recorded |
| `running` | `completed` | Agent reports success | Agent daemon | Agent sends `step.completed` with `output_refs`; triggers downstream dependency check |
| `running` | `waiting_input` | Step requires human input or `human_approval_required = true` | Agent daemon / System | Sends notification to Owner; step paused until input received |
| `waiting_input` | `running` | Owner provides input or approval | Owner (manual) | Resumes execution with provided input |
| `running` | `blocked` | No heartbeat for N seconds | System (heartbeat monitor) | N = `timeout_rule.max_duration_seconds` or default 60s; marks step blocked |
| `running` | `blocked` | Agent goes offline (agent `online_status` = `offline`) | System (agent monitor) | Agent offline > 60s while step is running |
| `running` | `timeout` | Exceeds `max_duration_seconds` | System (timeout monitor) | See Timeout Calculation below |
| `blocked` | `retrying` | Retry triggered | System (automatic) or Owner (manual) | Waits `retry_rule.retry_delay_seconds` before attempting |
| `blocked` | `failed` | Dependency step failed permanently | System (automatic) | Upstream step reached `failed` and cannot be recovered |
| `retrying` | `running` | Retry attempt starts successfully | Agent daemon | Same or replacement agent begins new attempt; `retry_count` incremented |
| `retrying` | `failed` | Max retries exceeded | System (automatic) | `retry_count >= retry_rule.max_retries`; triggers fallback flow |
| `timeout` | `retrying` | `timeout_rule.action = "retry"` | System (automatic) | Follows retry mechanics |
| `timeout` | `failed` | `timeout_rule.action = "fail"` | System (automatic) | Step permanently failed |
| `timeout` | `waiting_input` | `timeout_rule.action = "escalate"` | System (automatic) | Escalates to Owner via `owner_escalation_policy` |
| `failed` | `queued` | Fallback agent assigned | System (automatic) | Next agent from `fallback_agent_ids[]`; inherits `input_context_refs` |
| `failed` | `queued` | Owner manually retries with different agent | Owner (manual) | Owner selects replacement agent |
| Any active | `cancelled` | Owner or system cancels | Owner (manual) / System (Run cancelled) | All resources released; agent notified to stop |

### Heartbeat Rules

An agent is considered **alive** if:
1. The agent's `last_active_at` timestamp is within the liveness window (default: 60 seconds).
2. The agent has sent at least one progress event (`step.progress`) within `timeout_rule.max_duration_seconds`.

A step is marked `blocked` when:
- The assigned agent's `online_status` transitions to `offline` while the step is `running`.
- No heartbeat received for > 60 seconds and the step is `running`.
- A dependency step that this step was waiting on transitions to `failed`.

### Timeout Calculation

```
timeout_threshold = step.started_at + step.timeout_rule.max_duration_seconds

IF now() > timeout_threshold AND step.status = 'running':
    IF no progress event received since started_at:
        step.status = 'timeout'
    ELIF last_progress_at + max_duration_seconds < now():
        step.status = 'timeout'
```

Default `max_duration_seconds` if not specified: **3600** (1 hour).

The timeout monitor runs on a polling interval (default: every 30 seconds) and checks all steps in `running` state.

### Retry Mechanics

```
ON step entering 'retrying':
    IF step.retry_count >= step.retry_rule.max_retries:
        step.status = 'failed'
        TRIGGER fallback_flow
    ELSE:
        WAIT retry_rule.retry_delay_seconds
        step.retry_count += 1
        Re-dispatch step to same agent (or fallback if agent offline)
        step.status = 'running'
```

Default `retry_rule`:
- `max_retries`: 2
- `retry_delay_seconds`: 30

Retry delay uses fixed delay (not exponential backoff in MVP).

### Fallback Agent Assignment Flow

```
ON step.status = 'failed' AND fallback_agent_ids is not empty:
    FOR agent_id IN step.fallback_agent_ids (in order):
        IF agent.online_status = 'online' AND agent.workload_status IN ('idle', 'degraded'):
            step.actual_agent_id = agent_id
            step.retry_count = 0  (reset for new agent)
            step.status = 'queued'
            BREAK
    IF no fallback agent available:
        TRIGGER owner_escalation
```

When a fallback agent takes over:
- `actual_agent_id` updated to the new agent.
- `input_context_refs` inherited from original step.
- Original agent's attempt recorded in `step_logs` with failure reason.
- New agent gets a fresh retry budget (`retry_count` reset to 0).

### Owner Escalation Trigger Conditions

Owner escalation is triggered when **any** of the following occur:

1. **All fallback agents exhausted**: Primary agent + all `fallback_agent_ids` have failed.
2. **Escalation timeout**: Step has been in `blocked` or `retrying` for longer than `owner_escalation_policy.escalate_after_seconds`.
3. **Human approval required**: Step has `human_approval_required = true` and reaches the execution point requiring approval.
4. **Timeout with escalate action**: `timeout_rule.action = "escalate"`.

Escalation creates an `inbox_item` with:
- `severity`: `critical`
- `action_required`: `true`
- `action_type`: `retry` | `replace_agent` | `acknowledge`
- `deadline`: `now() + owner_escalation_policy.escalate_after_seconds` (for response SLA)
- `target_identity_ids`: `[owner_escalation_policy.escalate_to]`

Owner response options:
1. **Retry with same agent** - resets step to `retrying`
2. **Replace agent** - assigns a new agent, resets step to `queued`
3. **Skip step** - marks step as `skipped`, unblocks downstream dependencies
4. **Cancel run** - transitions entire Run to `cancelled`

### Anti-Duplication Rule

Each workflow step can have at most **one active task** at any time. "Active" means status is one of: `queued`, `assigned`, `running`, `waiting_input`, `blocked`, `retrying`. Before dispatching a retry or fallback, the system must verify no other active task exists for the same step.

---

## 6. Conversation State Machine

Conversations have a simpler lifecycle focused on type transitions and archival.

### DM-to-Channel Upgrade

```
 +------+  owner_adds_participants  +---------+
 |  dm  |-------------------------->| channel |
 +------+                           +---------+
```

- **Trigger**: Owner adds a third participant to a DM.
- **Who**: Owner only.
- **Side effects**: `conversation_type` changes from `dm` to `channel`. All existing messages preserved. Participants list opens to N members.
- **Irreversible**: A channel cannot revert to a DM.

### Thread Lifecycle

```
              first_reply_to_message
 (no thread) --------------------------> +----------+
                                          | created  |
                                          +----------+
                                               |
                                               | replies continue
                                               v
                                          +----------+
                                          |  active  |
                                          +----------+
                                               |
                                               | parent_channel_archived
                                               v
                                          +----------+
                                          | archived |
                                          +----------+
```

- **Creation**: A thread is created when the first reply is made to a message in a channel. The thread ID equals the root message ID.
- **Active**: Thread accepts new replies. `reply_count` and `last_reply_at` updated on each reply.
- **Archived**: When the parent channel is archived (e.g., project completed and archived), all threads in that channel are also archived. Archived threads are read-only.
- Threads do **not** have independent deletion. They exist as long as their parent channel exists.

### Channel Archival

```
 +----------+  owner_archives   +----------+
 |  active  |------------------->| archived |
 +----------+                    +----------+
```

- **Trigger**: Owner archives channel, or project channel auto-archives when project is archived.
- **Side effects**: Channel becomes read-only. All threads archived. Files remain accessible but no new uploads.
- **Irreversible** in MVP. Archived channels can be viewed but not reactivated.

---

## Cross-Machine Dependencies

The state machines interact with each other. Key dependency flows:

```
Agent goes offline
  --> All 'running' steps for that agent --> 'blocked'
    --> If blocked step is critical --> Run may eventually --> 'failed'
      --> Project status --> 'failed'
        --> Owner escalation notification created

Plan approved
  --> Project status: 'not_started' --> 'running'
    --> Run status: 'pending' --> 'running'
      --> Steps: 'pending' --> 'queued' (for steps with no dependencies)
        --> Agent status: 'idle' --> 'busy'

All steps completed
  --> Run status: 'running' --> 'completed'
    --> Project status: 'running' --> 'completed'
      --> Agent status: 'busy' --> 'idle' (if no other tasks)
        --> Notification: run_completed sent to Owner
```
