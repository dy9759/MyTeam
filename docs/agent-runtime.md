# Agent and Runtime Relationship

MyTeam separates `provider`, `runtime`, and `agent` on purpose.

That split is the difference between "what software can run", "where work can run right now", and "which collaborator identity the workspace sees."

This is close to the layering used by OpenAgents:

- the OpenAgents connector manages installed agent runtimes and daemon lifecycle separately from agent creation and workspace connection
- the OpenAgents network model keeps execution transport separate from agent identity and collaboration state

MyTeam applies the same idea to a task-centric product.

## Three Layers

### 1. Provider

A provider is a static registry definition for a coding-agent family such as `claude`, `codex`, or `opencode`.

Provider definitions describe:

- executable name
- environment variables
- default model env var
- instruction file (`CLAUDE.md` or `AGENTS.md`)
- capability metadata

In MyTeam, this lives in the provider registry, not in workspace data.

### 2. Runtime

A runtime is a workspace-scoped execution endpoint.

It answers:

> Where can this work execute right now?

A runtime is usually registered by a daemon, but the model also allows cloud runtimes.

Runtime state includes:

- `workspace_id`
- `daemon_id`
- `runtime_mode`
- `provider`
- `status`
- heartbeat / last-seen data
- device and metadata

A runtime is infrastructure, not a teammate identity.

### 3. Agent

An agent is a workspace-scoped collaborator identity.

It answers:

> Who is doing the work, and how should that collaborator behave?

Agent state includes:

- name and description
- instructions
- identity card
- tools and triggers
- visibility and ownership
- current `runtime_id`

An agent is the thing users assign, mention, review, impersonate, and see in conversation history.

## Cardinality Rules

The current MyTeam model is:

- one provider can appear in many runtimes
- one daemon host can register multiple runtimes, usually one per provider
- one runtime can back zero, one, or many agents
- one agent binds to exactly one runtime at a time
- one task snapshots both `agent_id` and `runtime_id` when it is enqueued

This means multiple specialized agents can share the same machine and CLI installation while still keeping separate identities, instructions, and audit trails.

## Why Runtime and Agent Are Separate

This split gives MyTeam properties that a single merged object cannot provide cleanly:

- a runtime can go offline without deleting the agent identity
- an agent can be retargeted to a different runtime later
- several agents can share the same execution host but keep different roles
- routing, scheduling, and health checks can operate on runtime state
- collaboration, permissions, and identity cards can operate on agent state

In short:

- runtime = execution capacity
- agent = collaboration identity

## Current Execution Flow

### 1. Register runtimes

The daemon probes the local machine for supported providers and registers one runtime per available provider for each watched workspace.

### 2. Create an agent

The owner creates an agent and binds it to a `runtime_id`.

This is the moment where a workspace identity becomes executable.

### 3. Enqueue work

When an issue or mention becomes runnable, MyTeam copies the agent's current `runtime_id` into `agent_task_queue`.

This is important: tasks do not look up the runtime dynamically at claim time.

They carry a runtime snapshot taken when the task was created.

### 4. Claim by runtime

Each runtime polls only for tasks whose queued `runtime_id` matches itself.

The runtime then claims tasks while still respecting each agent's own concurrency rules.

### 5. Execute in runtime-specific environment

The daemon creates an execution environment, injects runtime-specific instruction context, runs the provider CLI, and reports results back into MyTeam.

## Product Implications

This model should be visible in the UI:

- the Runtime page should show which agents are bound to a runtime
- creating an agent from a runtime should prefill that `runtime_id`
- the Account page should explain that choosing a runtime is binding an execution endpoint, not choosing the agent's identity

## Design Rule for Future Work

When adding new capabilities, keep the boundary intact:

- add execution-host concerns to runtime
- add identity / collaboration concerns to agent
- snapshot routing decisions onto tasks when work is queued

Do not collapse runtime and agent into one object unless the product intentionally gives up multi-agent-per-runtime execution.
