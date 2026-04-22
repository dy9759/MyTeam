# Agent · Subagent · Skill Framework

Status: in production on `main` (migrations 069–074, skills bundle loader,
PlanGenerator enforcement, dedicated API + UI).

This document captures the **current** shape of the agent / subagent /
skill stack: what each layer is, how they relate, how they're populated,
and where in the codebase each invariant is enforced.

---

## 1. Core product rule

> **Agents never call skills directly. Every skill is reached through a
> subagent.**

Consequences:

- `subagent_skill` is the only live write-path between a skill and the
  thing that executes it.
- PlanGenerator is not allowed to assign a skill-bearing task to a plain
  agent; it must pick a subagent whose roster covers the required
  skills.
- The legacy `agent_skill` join is retained only for read-side
  compatibility (`ListAgentSkills`); new code must not write to it.

## 2. Three layers

| Layer       | Table              | Kind filter           | Runs?               | Scope                         |
|-------------|--------------------|-----------------------|---------------------|-------------------------------|
| Agent       | `agent`            | `kind = 'agent'`      | yes                 | workspace (or system-scoped)  |
| Subagent    | `agent`            | `kind = 'subagent'`   | no — template       | global or workspace           |
| Skill       | `skill`            | —                     | no — capability pkg | global or workspace           |

Subagent and agent share one table so a workspace can promote a subagent
template into a role agent without a copy (migration 074 does exactly
that as a seed).

```
┌─────────┐ 1       N ┌──────────┐ 1         N ┌───────┐
│ agent   │──────────▶│ subagent │────────────▶│ skill │
│(kind=   │ assignee  │(kind=    │ subagent_   │       │
│ 'agent')│           │ 'subagent│ skill       │       │
└─────────┘           └──────────┘             └───────┘
     │                                                 ▲
     └──(legacy agent_skill, read-only)────────────────┘
```

## 3. Schema evolution

Migrations that define the framework (all in `server/migrations/`):

| # | File | Role |
|---|------|------|
| 069 | `069_skills_subagents_foundation.up.sql` | `skill` + `agent` columns (`category`, `source`, `source_ref`, `is_global`), `workspace_id` nullable for globals, `agent.kind`, `subagent_skill` join table, partial unique indexes. |
| 070 | `070_backfill_task_assignees.up.sql` | Back-fills `task.primary_assignee_id` for legacy NULL tasks: match by `required_skills`, then any visible subagent, then first workspace agent. |
| 071 | `071_relax_global_skill_name_uniqueness.up.sql` | `uq_skill_global_name` becomes `WHERE is_global = true AND source <> 'bundle'` so cross-source bundles can ship duplicate names (e.g. `test-driven-development` appears in multiple upstreams). |
| 072 | `072_agent_runtime_id_nullable.up.sql` | `agent.runtime_id` nullable — subagent templates never execute. |
| 073 | `073_plan_input_context.up.sql` | `plan.input_files JSONB` + `plan.user_inputs JSONB` for plan-context capture. |
| 074 | `074_seed_role_agents_from_subagents.up.sql` | Idempotent INSERT…SELECT materializing a workspace-scoped `kind='agent'` per bundle subagent, tagged in `identity_card.seeded_from_subagent_id`. |

### Key constraints

- `uq_skill_workspace_name` — `(workspace_id, name) WHERE workspace_id IS NOT NULL`
- `uq_skill_global_name` — `(name) WHERE is_global = true AND source <> 'bundle'`
- `uq_skill_bundle_ref` — `(source_ref) WHERE source = 'bundle' AND source_ref IS NOT NULL`
- `uq_agent_bundle_ref` — same, on `agent`
- `subagent_skill PK (subagent_id, skill_id)` with `position INT DEFAULT 0`

## 4. Bundle sources

Vendored under `server/internal/skillsbundle/data/` and compiled into the
server binary via `//go:embed all:data`.

| Source | Skills | Subagent files | License |
|--------|--------|----------------|---------|
| `addyosmani/` | 21 | 3 | MIT |
| `superpowers/` | 14 | 1 | MIT |
| `everything-claude-code/` | 183 | 48 + 5 nested | MIT |

Everything-claude-code contributes extra subagent files inside nested
`skills/<slug>/agents/` dirs — the walker keys on parent-dir name
`agents`, so those are picked up automatically.

Final startup log after all three sources sync:

```
skills bundle synced skills=218 subagents≈56 skill_links=277
```

## 5. Loader pipeline

`server/internal/skillsbundle/loader.go`, booted once per process in
`server/cmd/server/main.go`:

```go
(&skillsbundle.Loader{Queries: queries}).Run(ctx)
```

`Loader.Run` is idempotent and performs a four-pass sync:

1. **`syncSkills`** — walks every `data/<source>/skills/<slug>/SKILL.md`,
   parses YAML frontmatter (`name`, `description`, `category`, `model`),
   calls `UpsertBundleSkill` keyed by relative path. Records the list of
   live refs.
2. **`syncSubagents`** — walks every `*.md` under any `agents/` dir,
   parses frontmatter, calls `UpsertBundleSubagent`. Bundle subagents
   are `is_global=true`, `owner_type='organization'`,
   `agent_type='system_agent'`.
3. **`syncSubagentSkillLinks`** — name-occurrence heuristic, scoped to
   the same upstream source. Phase 1: link a subagent → skill when the
   skill name appears as a substring in `name + description +
   instructions`. Phase 2 (catchall): any skill still orphaned gets
   linked at `position = 999` to the first subagent in its source, so
   the invariant *every skill is reachable through ≥1 subagent* holds.
4. **Prune** — `DeleteBundleSkillsNotInRefs` / `DeleteBundleSubagentsNotInRefs`
   remove rows whose `source_ref` is no longer on disk. Cascades through
   `subagent_skill`.

After the sync the loader re-runs the migration 074 logic through
`Queries.SeedRoleAgentsFromBundleSubagents`, so fresh bundle additions
get a runnable workspace agent next boot without a migration cycle.

### Malformed files

Parse errors are logged (`slog.Warn("skillsbundle: skip malformed
skill")`) and skipped. The ECC bundle currently has one such file:
`a11y-architect.md` with a duplicate `model:` key — see follow-ups.

## 6. SQL surface

Queries live under `server/pkg/db/queries/` and are code-generated via
`make sqlc`.

### `skill.sql`

- `ListSkillsCombined(workspace, category?, source?)` — union of workspace + globals
- `UpsertBundleSkill`, `ListBundleSkillRefs`, `ListBundleSkillsForLinking`, `DeleteBundleSkillsNotInRefs`
- `CreateUploadSkill` — `/api/skills/import`, `source='upload'`
- Legacy: `ListAgentSkills`, `AddAgentSkill`, `RemoveAgentSkill` — kept
  for read-only compatibility; new writes go through `subagent.sql`.

### `subagent.sql`

- `ListSubagents(workspace?, category?)`, `GetSubagent`
- `CreateWorkspaceSubagent`, `UpdateSubagent`, `DeleteSubagent`
- `UpsertBundleSubagent`, `ListBundleSubagentRefs`, `ListBundleSubagentsForLinking`, `DeleteBundleSubagentsNotInRefs`
- `LinkSubagentSkill`, `UnlinkSubagentSkill`, `UnlinkAllSubagentSkills`, `ListSubagentSkills`, `ListSkillSubagents`
- `SeedRoleAgentsFromBundleSubagents` — identical INSERT…SELECT to migration 074; called from the loader each boot.

`DeleteSubagent` refuses `source = 'bundle'` rows at the SQL layer
(`WHERE kind = 'subagent' AND source <> 'bundle'`), so the API can't
accidentally drop a vendored template.

## 7. HTTP API

Routes declared in `server/cmd/server/router.go`:

```
GET    /api/skills                            list (category/source filter)
POST   /api/skills           admin+           create manual
POST   /api/skills/import    admin+           create from upload
GET    /api/skills/{id}
PUT    /api/skills/{id}
DELETE /api/skills/{id}                       refuses source='bundle'

GET    /api/subagents                         list (scope filter)
POST   /api/subagents                         create workspace subagent
GET    /api/subagents/{id}                    hydrated (skills attached)
PATCH  /api/subagents/{id}
DELETE /api/subagents/{id}
POST   /api/subagents/{id}/skills             link {skill_id, position}
DELETE /api/subagents/{id}/skills/{skillID}   unlink
```

Handler impl in `server/internal/handler/subagent.go` and
`handler/skill.go`.

## 8. PlanGenerator integration

`server/internal/service/plan_generator.go`:

- `SubagentIdentity` struct is part of the generator's input alongside
  `AgentIdentity`.
- `GeneratePlanWithContext(ctx, chat, agents, subagents, workspaceID)` —
  the authoritative entrypoint; prompt builder (`buildContextPrompt`)
  renders both lists so the LLM can pick `primary_assignee_agent_id`
  from real IDs of either kind.
- `applyDefaultAssignees(tasks, agents, subagents)` — backfills any task
  where the LLM left the primary assignee blank. Rule:
  1. Tasks with non-empty `required_skills` → prefer a subagent whose
     roster covers those skills.
  2. No skill match → first subagent (template-driven default).
  3. No subagent candidates → first agent.
- `validateSkillSubagent(tasks, subagents)` — emits
  `WarnSkillNeedsSubagent` when a task has `required_skills` but the
  primary assignee is not a known subagent. Empty subagent list
  downgrades the error to a warning so new workspaces aren't blocked.
- `validate` wraps the default-assignee pass and the validator into the
  single exit point `PlanGeneratorService.validate(...)` — every plan
  the service returns has both behaviours applied.

## 9. Frontend surface

`apps/web/`:

| Route | Component | What it shows |
|-------|-----------|---------------|
| `/account?tab=skills` | `features/skills/components/skills-page.tsx` | Skill grid with category filter, source badge, upload dialog. |
| `/account?tab=subagents` | `features/subagents/components/subagents-page.tsx` | Subagent list + detail, create dialog, skill link/unlink. |
| `/projects/{id}` → 计划 tab | `features/projects/components/plan-stepper.tsx` | Inline-editable task stepper; assignee popover surfaces subagents before agents. |
| `/projects/{id}` → Slots tab | `features/projects/components/orchestration-graph.tsx` + `orchestration-dag.tsx` | Orchestration views; tasks carry the resolved assignee chip. |

API client methods live in `apps/web/shared/api/client.ts`:
`listSkills`, `listSubagents`, `createSubagent`, `updateSubagent`,
`linkSubagentSkill`, `unlinkSubagentSkill`, etc.

## 10. Invariants and how they are enforced

| Invariant | Enforcement point |
|-----------|-------------------|
| Skill reachable only via subagent | `validateSkillSubagent` (runtime) + loader `syncSubagentSkillLinks` (seed) |
| Every bundle skill has ≥1 wrapping subagent | Catchall phase of `syncSubagentSkillLinks` |
| Every task has an assignee | `applyDefaultAssignees` + migration 070 for legacy rows |
| Bundle rows are read-only | SQL guard in `DeleteSubagent`; API 409 on bundle skill delete |
| One system agent per workspace | Existing partial unique `uq_workspace_global_system_agent` — migration 074 sidesteps it by using `agent_type='personal_agent'` |
| Upstream bundle drift is idempotent | `source_ref` unique indexes + `DeleteBundle…NotInRefs` prune |

## 11. Open follow-ups

1. **6 subagents with empty rosters** — ECC bundle contains a handful of
   subagents whose names don't textually reference any skill and are
   not first-seen in their source, so they stay empty after the
   catchall pass. Manual curation through `/account?tab=subagents` is
   expected.
2. **ECC `a11y-architect.md` upstream bug** — duplicate `model:` YAML
   key makes the file unparsable. Loader skips it; report upstream and
   re-pull when fixed.
3. **Agent-side writes to `agent_skill`** — still possible via the old
   `Set/AddAgentSkill` endpoints. These should be deprecated in favor
   of the subagent path and eventually removed.
4. **PlanGenerator prompt hinting** — the prompt currently describes
   both lists flatly. A future pass can give the LLM explicit category
   rosters (debugging, design, review, …) so it matches more precisely
   than free-form name lookups.

## 12. File reference

### Backend

- `server/migrations/069_skills_subagents_foundation.up.sql` — schema
- `server/migrations/070_backfill_task_assignees.up.sql` — assignee backfill
- `server/migrations/071_relax_global_skill_name_uniqueness.up.sql` — cross-source bundle names
- `server/migrations/072_agent_runtime_id_nullable.up.sql` — templates don't execute
- `server/migrations/073_plan_input_context.up.sql` — plan context JSONB
- `server/migrations/074_seed_role_agents_from_subagents.up.sql` — role-agent seed
- `server/internal/skillsbundle/loader.go` — startup sync
- `server/internal/skillsbundle/data/` — vendored upstreams
- `server/pkg/db/queries/skill.sql` — skill CRUD + bundle ops
- `server/pkg/db/queries/subagent.sql` — subagent CRUD + link ops + seed
- `server/internal/handler/skill.go`, `handler/subagent.go` — HTTP
- `server/internal/service/plan_generator.go` — enforcement

### Frontend

- `apps/web/features/skills/components/skills-page.tsx`
- `apps/web/features/subagents/components/subagents-page.tsx`
- `apps/web/features/projects/components/plan-stepper.tsx`
- `apps/web/features/projects/components/orchestration-graph.tsx`
- `apps/web/features/projects/components/orchestration-dag.tsx`
- `apps/web/shared/api/client.ts` — `listSkills`, `listSubagents`, `linkSubagentSkill`, …

### Docs

- `docs/plans/skills-subagents-rollout.md` — original phased rollout
- `docs/agent-subagent-skill-framework.md` — this file (current state)

## 13. Agent Interaction Layer (migration 075)

Additive messaging layer sitting alongside the task engine: agents (or
users impersonating agents) exchange DMs, capability broadcasts, and
schema-typed events without bloating `task` with message-shaped rows.
Wire format is ported 1:1 from AgentmeshHub's `interaction` protocol.

### Data model

One table, `agent_interaction`, defined in
[075_agent_interaction.up.sql](../server/migrations/075_agent_interaction.up.sql):

- Sender: `from_id` + `from_type` (`agent` | `user` — the latter is
  populated when a human sends via the attach / 附身 flow).
- Target union — exactly one of `to_agent_id`, `channel`, `capability`,
  `session_id` (enforced at the handler for readable 400s).
- Protocol fields: `type` (`message` / `task` / `query` / `event` /
  `broadcast`), `content_type` (`text` / `json` / `file`), optional
  `schema` routing hint, `payload` JSONB, `metadata` JSONB.
- Delivery: `status` (`pending` / `delivered` / `read` / `failed`),
  `delivered_at`, `read_at`.
- Indexes cover the two hot paths — inbox (`to_agent_id, created_at
  DESC`) and sent mail (`from_id, created_at DESC`) — plus capability,
  workspace, and session lookups.

### API

Routes registered in [router.go:396](../server/cmd/server/router.go#L396)
and [router.go:388](../server/cmd/server/router.go#L388):

- `POST /api/interactions` — unified send; dispatches DM, channel,
  capability, or session target ([agent_interaction.go:117](../server/internal/handler/agent_interaction.go#L117)).
- `GET  /api/agents/{id}/inbox?after=&limit=` — pull fallback; bulk
  flips `pending` rows to `delivered` per page ([agent_interaction.go:294](../server/internal/handler/agent_interaction.go#L294)).
- `POST /api/interactions/{id}/ack?state=delivered|read` — recipient
  acks; rejects non-DM rows ([agent_interaction.go:367](../server/internal/handler/agent_interaction.go#L367)).

### Auth — `CanActAsAgent`

[agent_interaction.go:41](../server/internal/handler/agent_interaction.go#L41)
gates every send/read/ack. Three grant paths tried in order: personal
ownership (`agent.owner_id = userID`), active impersonation session,
workspace owner/admin override. Any DB error returns false silently so
the 403 never leaks the reason.

### WS broadcast + dedup

DM sends push via `Hub.PushToAgent` ([hub.go:353](../server/internal/realtime/hub.go#L353))
as `{"type":"interaction","payload":<interactionResponse>}`. The hub
extracts `payload.id` into a per-client bounded recent-push set
([hub.go:282](../server/internal/realtime/hub.go#L282)), so a reconnecting
agent doesn't re-receive messages already delivered seconds ago. When
any WS client acks synchronously, the row flips to `delivered` before
the HTTP response returns.

Capability broadcast fans out to every non-archived `kind='agent'` whose
`category` (case-insensitive) matches, each receiver getting its own WS
push against the same row ([agent_interaction.go:267](../server/internal/handler/agent_interaction.go#L267)).

### Agent consumption

Push primary, pull fallback — agents subscribe via the existing WS
connection and listen for `type="interaction"` frames; a missed push is
recovered by polling `/api/agents/{id}/inbox?after=<last_seen>`. Both
paths hit the same table, so ordering is stable.

### Known gaps

- **#75** — `target.session_id` sends skip membership check; any caller
  with a session UUID can post into it.
- **#76** — `target.channel` is persisted but has no fan-out; receivers
  never hear channel sends today.
- **#77** — no sent / conversation / by-capability query endpoints; only
  the inbox GET is exposed.
- **#78** — `POST /api/interactions` is unrate-limited; a runaway agent
  can flood the workspace.
