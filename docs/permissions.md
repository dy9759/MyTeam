# My Team - Permissions Matrix

## A. Role Hierarchy

```
Organization
  ├── Owner (1 per user, N per organization)
  │     └── Personal Agent (0..N, owned exclusively by this Owner)
  ├── System Agent - Global (1 per organization)
  └── Page System Agent (1 per feature page, scoped to that page)
```

### Role Definitions

| Role | Scope | Purpose |
|------|-------|---------|
| Owner | Organization-wide | Human user. Full management of their own agents. Approves plans, starts execution, intervenes on failures. |
| Personal Agent | Bound to one Owner | AI agent created and owned by an Owner. Executes tasks, responds to messages, participates in channels. |
| System Agent (Global) | Organization-wide | Singleton scheduler and orchestrator. Generates plans, assigns tasks, mediates unresponded messages, handles failure escalation. Not a task executor. |
| Page System Agent | Single feature page | Scoped assistant for a specific page (e.g., Account Agent, Project Agent). Limited to operations within its page boundary. |

### Key Constraints

- System Agent is an orchestrator, not an executor. It dispatches work but does not replace agents in running tasks.
- Page System Agent cannot act outside its page scope.
- Agents cannot cross Owner boundaries (MVP).
- Owners cannot dispatch another Owner's agents (MVP).

---

## B. Role Permissions Matrix

### Project and Plan Operations

| Operation | Owner | Personal Agent | System Agent (Global) | Page System Agent |
|-----------|-------|----------------|----------------------|-------------------|
| Create project | Yes | No | Yes (auto, from chat context) | No |
| Edit project metadata | Yes (own projects) | No | No | No |
| Delete/archive project | Yes (own projects) | No | No | No |
| Generate plan | No (reviews only) | No | Yes (auto) | No |
| Approve plan | Yes | No | No | No |
| Reject plan | Yes | No | No | No |
| Modify plan before approval | Yes | No | No | No |
| Fork project version | Yes | No | No | No |

### Execution Operations

| Operation | Owner | Personal Agent | System Agent (Global) | Page System Agent |
|-----------|-------|----------------|----------------------|-------------------|
| Start execution | Yes (requires approval first) | No | No | No |
| Pause execution | Yes | No | No | No |
| Cancel execution | Yes | No | No | No |
| Resume execution | Yes | No | No | No |
| Execute assigned task | No | Yes (when assigned) | No | No |
| Report task progress | No | Yes (own tasks) | No | No |
| Replace agent during execution | Yes (own agents) | No | Yes (auto, via fallback list) | No |
| Retry failed step | Yes | No | Yes (auto, per retry_rule) | No |
| Skip step | Yes | No | No | No |

### Impersonation

| Operation | Owner | Personal Agent | System Agent (Global) | Page System Agent |
|-----------|-------|----------------|----------------------|-------------------|
| Impersonate agent (send as) | Yes (own agents only) | N/A | No | No |
| End impersonation | Yes | N/A | No | No |

### Messaging and Channels

| Operation | Owner | Personal Agent | System Agent (Global) | Page System Agent |
|-----------|-------|----------------|----------------------|-------------------|
| Send message in conversation | Yes | Yes (auto/when @-mentioned) | Yes (mediation/notifications only) | Yes (within page scope) |
| Create channel | Yes | No | Yes (project channels only) | No |
| Create thread | Yes | Yes | Yes | No |
| Create DM | Yes | No | No | No |
| Upgrade DM to channel | Yes | No | No | No |
| Modify channel visibility | Yes (own channels) | No | No | No |
| Invite to channel | Yes | No | Yes (project participants) | No |
| Auto-assign message responder | No | No | Yes | No |

### Agent Management

| Operation | Owner | Personal Agent | System Agent (Global) | Page System Agent |
|-----------|-------|----------------|----------------------|-------------------|
| Create agent | Yes | No | No | No |
| Edit agent (name, config) | Yes (own agents) | No | No | No |
| Delete agent | Yes (own agents) | No | No | No |
| Suspend agent | Yes (own agents) | No | No | No |
| Resume agent | Yes (own agents) | No | No | No |
| View agent conversations | Yes (own agents) | No | No | No |
| Update identity card (auto) | No | No | Yes | No |
| Update identity card (manual) | Yes (own agents) | No | No | No |

### Files

| Operation | Owner | Personal Agent | System Agent (Global) | Page System Agent |
|-----------|-------|----------------|----------------------|-------------------|
| Upload files | Yes | Yes (in assigned tasks) | No | No |
| Delete files | Yes (own files) | No | No | No |
| View own files | Yes | Yes | No | No |
| View own agent's files | Yes | No | No | No |
| View cross-Owner channel files | Yes (if channel participant) | Yes (if channel participant) | No | No |
| Create file snapshot | No | No | Yes (auto, for project refs) | No |

### Organization

| Operation | Owner | Personal Agent | System Agent (Global) | Page System Agent |
|-----------|-------|----------------|----------------------|-------------------|
| Manage organization members | Yes (if org admin) | No | No | No |
| View organization members | Yes | No | No | No |

---

## C. Impersonation Rules

### Who Can Impersonate Whom

| Impersonator | Target | Allowed |
|-------------|--------|---------|
| Owner | Own Personal Agent | Yes |
| Owner | Another Owner's Agent | No |
| Owner | System Agent | No |
| Owner | Page System Agent | No |
| Personal Agent | Any identity | No |
| System Agent | Any identity | No |

### Visibility

- All impersonated messages carry `is_impersonated = true` in the message record.
- The impersonation indicator is **visible to all participants** in the conversation. There is no "stealth" mode.
- UI renders a distinct badge (e.g., "sent by Owner on behalf of Agent") on impersonated messages.

### Audit Requirements

Every impersonation action is logged to `activity_log` with:

| Field | Value |
|-------|-------|
| action | `impersonation_send` |
| actor_id | Owner's user ID |
| target_id | Agent ID being impersonated |
| conversation_id | Where the message was sent |
| message_id | The impersonated message |
| timestamp | When it occurred |

### Restrictions

- Owner can only impersonate agents they own (`agent.owner_id = current_user_id`).
- Impersonation is limited to sending messages. Owner cannot impersonate an agent to execute workflow tasks.
- Owner can end impersonation at any time, returning to their own identity.
- An agent that is currently `suspended` cannot be impersonated.

---

## D. Human-Machine Boundary

The following operations **always require explicit human (Owner) confirmation** and cannot be performed autonomously by any agent, including System Agent.

### Actions Requiring Human Confirmation

| Action | Trigger | Who Confirms |
|--------|---------|-------------|
| Plan approval | System Agent generates a plan and sets `approval_status = pending_approval` | Project creator Owner |
| Execution start | Plan is approved, ready to run | Project creator Owner |
| Agent replacement (manual) | Owner decides to swap an agent on a running step | Owner initiating the replacement |
| Critical agent reassignment | All fallback agents exhausted; System Agent escalates to Owner | Escalated Owner (per `owner_escalation_policy`) |
| External result publication | Execution output to be published outside the platform | Project creator Owner |

### How Confirmation Works

1. System Agent or the execution engine creates an `inbox_item` with `action_required = true`.
2. The item specifies `action_type` (approve, retry, replace_agent, acknowledge).
3. Owner receives a notification and must act before the `deadline` (if set).
4. If the deadline expires without action, `resolution_status` moves to `expired` and the system follows the escalation policy (e.g., pause execution, notify other Owners).

### What System Agent CAN Do Without Confirmation

- Auto-retry a failed step per the step's `retry_rule` (up to `max_retries`).
- Switch to a fallback agent per the step's `fallback_agent_ids` list.
- Auto-assign a message responder when no response within SLA.
- Generate a plan draft (but not approve it).
- Create a project channel and add participants.

---

## E. Cross-Owner Rules (MVP)

### Agent Isolation

- An agent belongs to exactly one Owner (`agent.owner_id`).
- An agent **cannot** be assigned tasks by another Owner.
- An agent **cannot** execute steps in a project created by another Owner, unless that Owner explicitly adds the agent to the project plan.

### Dispatch Restrictions

- Owner A **cannot** dispatch Owner B's Personal Agent to any task.
- System Agent dispatches tasks only to agents already listed in the plan's `assigned_agents`. It cannot unilaterally reassign work to another Owner's agent.

### Cross-Owner Channel Files

- In channels with participants from multiple Owners, uploaded files are **shared with all channel participants** by default.
- File `access_scope.type = 'conversation'` grants read access to all current members of that conversation.
- Leaving a channel does not revoke access to files uploaded while the user was a member (files remain in their file index).

### Cross-Owner Project Participation

- Multiple Owners can participate in the same project if the project creator invites them.
- Each Owner can only assign **their own agents** to project steps.
- Owner A cannot modify or replace Owner B's assigned agents on a step. Only Owner B (or System Agent via fallback) can replace Owner B's agents.
- The project channel includes all participating Owners and their assigned agents.

---

## F. System Agent Boundaries

### What System Agent CAN Do Autonomously

| Action | Condition |
|--------|-----------|
| Generate project plan from chat context | Triggered by user action (e.g., "create project from chat") |
| Auto-assign message responder | Message unresponded past SLA, matches assignment criteria |
| Auto-retry failed step | Within `retry_rule.max_retries` limit |
| Switch to fallback agent | Primary agent failed/offline, `fallback_agent_ids` has candidates |
| Create project channel | When a new project is created |
| Add participants to project channel | Owners and agents listed in the plan |
| Update agent identity card (auto description) | After project completion or significant event |
| Send mediation/notification messages | Escalation alerts, status updates, assignment notices |
| Mark message as "needs response" | Based on message analysis (question, @-mention, SLA breach) |

### What System Agent CANNOT Do Without Owner Approval

| Action | Escalation Path |
|--------|----------------|
| Approve or reject a plan | Creates `inbox_item` with `action_type = 'approve'` for the project Owner |
| Start or cancel execution | Requires Owner to click "Start" / "Cancel" |
| Assign an agent not in the plan's fallback list | Creates `inbox_item` with `action_type = 'replace_agent'` |
| Remove an Owner from a project | Not possible autonomously |
| Delete any data (files, messages, projects) | Not possible autonomously |
| Change channel visibility | Not possible autonomously |
| Suspend or resume an agent | Only Owners can manage agent lifecycle |
| Publish results externally | Creates `inbox_item` with `action_type = 'acknowledge'` |

### Escalation When System Agent Hits Its Boundary

```
1. Step fails
   → System Agent checks retry_rule → auto-retry (up to max_retries)

2. Retries exhausted
   → System Agent checks fallback_agent_ids → assign fallback agent

3. All fallback agents fail or are offline
   → System Agent triggers owner_escalation_policy
   → Creates inbox_item (severity: critical, action_required: true)
   → Notifies Owner via inbox + project channel message

4. Owner does not respond within deadline
   → Execution is paused automatically
   → inbox_item.resolution_status → 'expired'
   → Additional notification sent (if configured)

5. Owner responds
   → Owner can: retry with same agent, replace agent manually,
      skip step, or cancel the entire run
```

### Page System Agent Additional Constraints

- Operates only within the context of its assigned page (e.g., Account page, Project page).
- Cannot send messages outside its page-scoped conversations.
- Cannot create channels or threads.
- Cannot access files or data outside its page scope.
- Cannot participate in project execution or plan generation.
- Serves as a contextual assistant only (answering questions, surfacing relevant info within the page).
