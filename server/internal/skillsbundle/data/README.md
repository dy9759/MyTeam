# Skills + Subagents Bundle

This directory ships curated, read-only **skills** and **subagents** that
are seeded into MyTeam at server startup (global scope, `source = 'bundle'`).
Workspaces see these alongside any uploads (`source = 'upload'`) or
manual entries (`source = 'manual'`).

## Layout

```
skills-bundle/
├── <source>/
│   ├── LICENSE                (upstream license, required)
│   ├── UPSTREAM-README.md     (upstream README for provenance)
│   ├── skills/
│   │   └── <skill-slug>/SKILL.md
│   └── agents/
│       └── <agent-slug>.md
```

Each `<source>` directory (e.g. `addyosmani/`, `superpowers/`) tracks a
single upstream provider. Splitting by source preserves attribution and
license boundaries.

## Provenance

| Source | Upstream | License |
|---|---|---|
| `addyosmani/` | https://github.com/addyosmani/agent-skills | MIT |
| `superpowers/` | https://github.com/obra/superpowers | MIT |

The upstream LICENSE file is copied verbatim into each `<source>/LICENSE`
and **must stay intact**. If the upstream repo changes license terms, the
bundle needs to be re-synced or dropped.

## File formats

### `SKILL.md`

YAML frontmatter + markdown body:

```yaml
---
name: debugging-and-error-recovery
description: Guides systematic root-cause debugging...
---

# ...body...
```

### Agents (subagents) `<slug>.md`

```yaml
---
name: code-reviewer
description: |
  Use this agent when...
model: inherit
---

# ...body (system prompt for the subagent)...
```

## Invocation rule

A skill **cannot** be called directly by a workspace agent — the
PlanGenerator and the task-execution runtime resolve skills through a
**subagent**. Bundle skills are only attached to bundle-shipped subagents
or to user-defined subagents via the `subagent_skill` join table.

This is enforced in:

- `subagent_skill` schema (no `agent_skill` link for bundle skills)
- `PlanGenerator.GeneratePlanWithContext` — rejects direct skill
  references on a task's `primary_assignee` that is not a subagent
- `/api/subagents/:id/skills` endpoint

## Resync workflow

1. Pull upstream changes into each `<source>/` directory (overwrite)
2. Bump the bundle version noted in `server/internal/service/skills_bundle.go`
3. Restart the server — the loader upserts by `source_ref`; removed
   bundle entries are soft-detached from any subagent that still
   references them (keeps user configurations stable)
