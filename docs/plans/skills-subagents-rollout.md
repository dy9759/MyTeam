# Skills + Subagents — Rollout Plan

**Status:** Phase 1 landed on `main` (migration 069 + bundle files).
Phases 2–5 outline the remaining work.

## Product rule

A skill is **not** callable directly by an agent. Skills are always
wrapped by a subagent. The same subagent may be shared across workspaces
via a global template; workspaces can also create their own subagents
that compose bundle skills and user-uploaded skills.

## Phase 1 — Foundation (done)

- `server/skills-bundle/` — checked-in copy of upstream skill libraries
  - `addyosmani/` (MIT — 21 skills, 3 subagents)
  - `superpowers/` (MIT — 14 skills, 1 subagent)
- Migration `069_skills_subagents_foundation`:
  - `skill.category`, `skill.source`, `skill.source_ref`, `skill.is_global`
  - `skill.workspace_id` now nullable (globals have NULL)
  - Partial unique indexes: `(workspace_id, name) WHERE workspace_id IS NOT NULL`,
    `(name) WHERE is_global`, `(source_ref) WHERE source='bundle'`
  - `agent.kind ∈ ('agent', 'subagent')`, `agent.is_global`,
    `agent.source`, `agent.source_ref`, `agent.category`
  - `agent.workspace_id` now nullable (global subagent templates have NULL)
  - New `subagent_skill` join table

## Phase 2 — sqlc + bundle loader

- `server/pkg/db/queries/skill.sql`
  - `ListSkills(workspace, filters)` — union workspace-scoped + globals, `category` filter
  - `UpsertBundleSkill(source_ref, ...)` — idempotent upsert keyed by `source_ref`
  - `DeleteBundleSkillsNotIn(paths[])` — soft-remove bundle rows no longer in tree
- `server/pkg/db/queries/agent.sql`
  - `ListSubagents(workspace, scope)` — scope = globals | workspace | both
  - `UpsertBundleSubagent(...)`
- `server/pkg/db/queries/subagent_skill.sql` — add/remove/list links
- `server/internal/service/skills_bundle.go`
  - On startup: walk `server/skills-bundle/<source>/skills/*/SKILL.md` and
    `agents/*.md`, parse YAML frontmatter, upsert
  - Category inferred from path segment or frontmatter `category:` hint;
    fall back to top-level slug (e.g. `engineering` → all addyosmani)
  - Bundle version hash recorded in `app_metadata` so loader can short-
    circuit when bundle hasn't changed

## Phase 3 — API

- `GET  /api/skills?category=&source=&scope=` — returns merged list
- `POST /api/skills/upload` — accepts `.zip` (with `SKILL.md` + assets),
  writes to upload store, creates `source='upload'` row
- `DELETE /api/skills/:id` — only `source='manual'|'upload'` rows; bundle
  rows rejected with 409
- `GET  /api/subagents` — scope filter (`global|workspace|all`)
- `POST /api/subagents` — create workspace subagent (kind='subagent')
- `PATCH /api/subagents/:id` — rename, description, category
- `POST /api/subagents/:id/skills` — link a skill
- `DELETE /api/subagents/:id/skills/:skillID` — unlink
- `GET  /api/subagents/:id` — hydrated view with linked skills

Routes: add under protected router in `server/cmd/server/router.go`.

## Phase 4 — PlanGenerator enforcement

- `service/plan_generator.go`:
  - When emitting `TaskDraft.PrimaryAssigneeAgentID`, verify the agent's
    `kind`. Assigning a `kind='agent'` task that requires a skill is a
    hard error — the generator must resolve through a subagent.
  - New input: list of available subagents (global + workspace) with
    their linked skills. The prompt tells the LLM: "Skills are reached
    through subagents — pick a subagent whose skills cover the task."
- `materializePlanDrafts`:
  - Persist the subagent selection in `task.primary_assignee_id` when
    the subagent owns the task; the actual executing agent is still
    assigned via existing fallback mechanics when the subagent is a
    template.

## Phase 5 — Frontend

- `/skills` page:
  - Left rail: category tree (engineering, debugging, planning, design,
    review, …)
  - Grid: skill cards with description + source badge (bundle/upload)
  - Detail drawer: frontmatter + full markdown body
- `/skills/upload` (inline dialog): drag-and-drop zip or paste markdown
- `/subagents` page:
  - List of global subagents + workspace subagents
  - Create/edit: name, description, category, linked-skill multi-select
- Project detail `计划` tab already renders tasks — in Phase 4 it will
  also display the selected subagent on each task with a drill-in link.

## Risks

- **Bundle license drift** — if upstream changes license, the bundle
  must be re-synced or dropped. Loader records upstream version; CI job
  can check for drift.
- **Global row uniqueness** — a user creating a workspace skill with the
  same name as a global skill is allowed today (different partial index
  coverage). Expected — workspace overrides. Needs a UI indicator.
- **Migration idempotency** — 069 down drops all added columns, so a
  deploy that rolls back while rows with `workspace_id IS NULL` exist
  will fail on `SET NOT NULL`. The down migration intentionally does
  not silently delete those rows; rollbacks must be paired with a
  data-cleanup step.
