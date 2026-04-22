# Multica To MyTeam Rename Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace remaining supported `multica` product, CLI, env var, package, and release surfaces with `myteam` while avoiding a blind global replace that would break builds, compatibility, or repository identity.

**Architecture:** Execute the rename in two phases. Phase 1 makes `myteam` the canonical external surface everywhere users interact with the product, keeps explicit compatibility shims for legacy `multica` entry points, and removes drift between UI/docs/build outputs. Phase 2 is a gated repository-identity migration for Go module paths, database/container defaults, and other deep internals that currently still encode `multica`.

**Tech Stack:** Go 1.26 + Cobra CLI, Next.js 16, Electron, pnpm workspace, Goreleaser, Docker, GitHub Actions, Markdown docs.

---

## Scope Notes

Current inventory from `rg` on 2026-04-22:

- `server/` still contains `461` `github.com/multica-ai/multica` import/module hits.
- `server/` still contains `63` `MULTICA_` env hits.
- `server/` still contains `1071` `multica` word hits.
- The repo remote is already `https://github.com/MyAIOSHub/MyTeam.git`, while `server/go.mod` still says `module github.com/multica-ai/multica/server`.
- Release/update config is inconsistent today:
  - `server/internal/cli/update.go` defaults to `dy9759/MyTeam`
  - `.goreleaser.yml` still publishes `multica` from `multica-ai/homebrew-tap`

This plan intentionally separates:

1. **Phase 1, must rename now**
   - User-facing CLI command examples
   - User-facing env vars
   - Built binary names and local runtime wiring
   - Web/Desktop onboarding text
   - Public docs and repo-level scripts
   - Release packaging names that users install or invoke directly

2. **Phase 2, gated deep rename**
   - Go module path
   - Go import paths across the repo
   - Database defaults like `postgres://multica:multica@...`
   - Compose project names and container naming
   - Legacy test fixture emails and MIME strings where semantics matter

Do **not** do a raw global search-and-replace. The plan below preserves working builds after each task.

## File Structure

### Phase 1 ownership

- `server/cmd/myteam/` — canonical CLI source tree
- `server/cmd/multica/` — compatibility entrypoint only after consolidation
- `server/internal/cli/update.go` — release repo + brew formula defaults
- `Makefile` — local run/build aliases
- `Dockerfile` — packaged binary name
- `.goreleaser.yml` — release artifact + brew formula metadata
- `apps/web/app/(dashboard)/account/page.tsx` — legacy onboarding snippet still showing `multica`
- `apps/web/app/(dashboard)/account/_components/add-agent-tab.tsx` — newer `myteam` onboarding reference; use as the source of truth
- `apps/desktop/electron/runtime-controller.ts` — desktop daemon control path already expects `server/bin/myteam`
- `package.json` — root `pnpm --filter @multica/web ...` aliases
- `apps/web/package.json` — `@multica/web` package name
- `scripts/check.sh` — root verification flow still references `@multica/web` and `/tmp/multica-*`
- `README.md`
- `README.zh-CN.md`
- `CLI_AND_DAEMON.md`
- `SELF_HOSTING.md`
- `CONTRIBUTING.md`
- `AGENTS.md`
- `CLAUDE.md`
- `.env.example`

### Phase 2 ownership

- `server/go.mod`
- every Go file importing `github.com/multica-ai/multica/...`
- `.github/workflows/ci.yml`
- `docker-compose.yml`
- `scripts/check.sh`
- `e2e/fixtures.ts`
- `e2e/helpers.ts`
- repo-level env/docs still using `postgres://multica:multica@localhost:5432/multica`

## Task 1: Make `myteam` The Only Maintained CLI Surface

**Files:**
- Modify: `server/cmd/myteam/main.go`
- Modify: `server/cmd/myteam/cmd_auth.go`
- Modify: `server/cmd/myteam/cmd_agent.go`
- Modify: `server/cmd/myteam/cmd_daemon.go`
- Modify: `server/cmd/myteam/cmd_workspace.go`
- Modify: `server/cmd/myteam/cmd_update.go`
- Modify: `server/cmd/multica/main.go`
- Modify: `Makefile`
- Modify: `Dockerfile`
- Modify: `.goreleaser.yml`
- Test: `server/cmd/myteam/cmd_auth_test.go`
- Test: `server/cmd/myteam/cmd_compat_test.go`

- [ ] **Step 1: Lock the canonical direction**

Run:

```bash
git diff --no-index server/cmd/multica server/cmd/myteam || true
```

Expected: both trees are near-duplicates, proving the repo currently pays a double-maintenance cost.

- [ ] **Step 2: Make `server/cmd/myteam` the only full implementation**

Implementation target:

```go
var rootCmd = &cobra.Command{
    Use:           "myteam",
    Short:         "MyTeam CLI — local agent runtime and management tool",
    Long:          "myteam manages local agent runtimes and provides control commands for the MyTeam platform.",
    SilenceUsage:  true,
    SilenceErrors: true,
}
```

Keep `server/cmd/myteam/*` as the canonical implementation. Do not keep evolving two full Cobra trees in parallel.

- [ ] **Step 3: Convert `server/cmd/multica` into a compatibility wrapper or compatibility build target**

Implementation target:

```go
var rootCmd = &cobra.Command{
    Use:           "multica",
    Short:         "Legacy Multica CLI compatibility wrapper",
    Long:          "multica remains available as a compatibility alias for myteam during the rename migration.",
    SilenceUsage:  true,
    SilenceErrors: true,
}
```

The wrapper must route to the same command set and behavior as `myteam`, not fork logic again.

- [ ] **Step 4: Make `myteam` the primary build artifact**

Implementation target:

```make
build:
	cd server && go build -o bin/server ./cmd/server
	cd server && go build -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o bin/myteam ./cmd/myteam
	cd server && go build -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o bin/multica ./cmd/multica
```

`bin/myteam` becomes primary. `bin/multica` remains only as a temporary compatibility artifact until Phase 1 is complete.

- [ ] **Step 5: Align Docker and release packaging with `myteam`**

Implementation target:

```dockerfile
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o bin/myteam ./cmd/myteam
COPY --from=builder /src/server/bin/myteam .
```

Implementation target:

```yaml
project_name: myteam

builds:
  - id: myteam
    main: ./cmd/myteam
    dir: server
    binary: myteam
```

- [ ] **Step 6: Add CLI compatibility tests before deleting duplicated logic**

Test target:

```go
func TestLegacyCompatibilityCommandsRemainAvailable(t *testing.T) {
    if _, _, err := authCmd.Find([]string{"login"}); err != nil {
        t.Fatalf("expected auth login command to exist: %v", err)
    }
}
```

Add coverage that proves both `myteam` and legacy `multica` entrypoints still expose the same command graph during the migration window.

- [ ] **Step 7: Verify the artifact switch**

Run:

```bash
make build
test -x server/bin/myteam
test -x server/bin/multica
server/bin/myteam version
server/bin/multica version
```

Expected: both binaries exist; `myteam` is primary; `multica` still runs only as a compatibility alias.

- [ ] **Step 8: Commit**

```bash
git add server/cmd/myteam server/cmd/multica Makefile Dockerfile .goreleaser.yml
git commit -m "refactor(cli): make myteam the canonical runtime binary"
```

## Task 2: Replace Public Env Vars And Config Defaults With `MYTEAM_*`

**Files:**
- Modify: `server/internal/cli/flags.go`
- Modify: `server/cmd/myteam/cmd_auth.go`
- Modify: `server/cmd/myteam/cmd_agent.go`
- Modify: `server/cmd/myteam/cmd_daemon.go`
- Modify: `server/cmd/myteam/cmd_repo.go`
- Modify: `.env.example`
- Modify: `Makefile`
- Test: `server/cmd/myteam/cmd_auth_test.go`

- [ ] **Step 1: Add one helper path that prefers `MYTEAM_*` and optionally falls back to `MULTICA_*`**

Implementation target:

```go
func EnvAny(keys ...string) string {
    for _, key := range keys {
        if v := strings.TrimSpace(os.Getenv(key)); v != "" {
            return v
        }
    }
    return ""
}
```

At each call site, read `MYTEAM_*` first and only fall back to `MULTICA_*` where migration compatibility is required.

- [ ] **Step 2: Update auth/server/workspace reads to prefer `MYTEAM_*`**

Implementation target:

```go
if v := cli.EnvAny("MYTEAM_TOKEN", "MULTICA_TOKEN"); v != "" {
    return v
}
```

Implementation target:

```go
val := cli.FlagOrEnvAny(cmd, "server-url", []string{"MYTEAM_SERVER_URL", "MULTICA_SERVER_URL"}, "")
```

- [ ] **Step 3: Update checked-in examples to teach only `MYTEAM_*`**

Implementation target:

```dotenv
MYTEAM_SERVER_URL=ws://localhost:8080/ws
MYTEAM_APP_URL=http://localhost:3000
```

Remove `MULTICA_*` from `.env.example` and primary docs. Compatibility support stays in code, not in examples.

- [ ] **Step 4: Add regression tests for fallback behavior**

Test target:

```go
t.Setenv("MYTEAM_SERVER_URL", "")
t.Setenv("MULTICA_SERVER_URL", "ws://localhost:19090/ws")
if got := resolveServerURL(cmd); got != "ws://localhost:19090/ws" {
    t.Fatalf("resolveServerURL() = %q", got)
}
```

The test should prove the order is `MYTEAM_*` first, `MULTICA_*` second.

- [ ] **Step 5: Verify env migration**

Run:

```bash
cd server && go test ./cmd/myteam/... -run 'Test.*Auth|Test.*Compat'
rg -n "MULTICA_" .env.example Makefile README.md README.zh-CN.md CLI_AND_DAEMON.md SELF_HOSTING.md apps/web/app
```

Expected: checked-in examples no longer teach `MULTICA_*`; any remaining `MULTICA_*` hits are compatibility-only or Phase 2 targets.

- [ ] **Step 6: Commit**

```bash
git add server/internal/cli/flags.go server/cmd/myteam .env.example Makefile
git commit -m "refactor(cli): prefer myteam environment variables"
```

## Task 3: Replace Web/Desktop Onboarding And Public Docs

**Files:**
- Modify: `apps/web/app/(dashboard)/account/page.tsx`
- Modify: `apps/web/app/(dashboard)/account/_components/add-agent-tab.tsx`
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `CLI_AND_DAEMON.md`
- Modify: `SELF_HOSTING.md`
- Modify: `CONTRIBUTING.md`
- Modify: `AGENTS.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Use the newer account onboarding component as the canonical wording**

Reference:

```tsx
<CopySnippet
  title="默认云端登录并启动 Runtime"
  code={`myteam config set app_url https://myteam.ai
myteam config set server_url https://api.myteam.ai
myteam login
myteam daemon start
myteam runtime list`}
/>
```

Update the older `apps/web/app/(dashboard)/account/page.tsx` block so it matches the `myteam` examples already present in `_components/add-agent-tab.tsx`.

- [ ] **Step 2: Replace user-facing commands and paths**

Implementation target:

```tsx
<CodeBlock code={`export MYTEAM_TOKEN="<PASTE_TOKEN>"
export MYTEAM_SERVER_URL="${serverUrl || "<server-url>"}"

myteam login
myteam daemon start
myteam runtime list`} />
```

Do the same for help text like `myteam daemon status`, `myteam daemon logs -f`, and source build paths like `server/bin/myteam`.

- [ ] **Step 3: Rewrite install/update docs to stop advertising `multica`**

Implementation target:

```md
The `myteam` CLI connects your local machine to MyTeam.
```

Implementation target:

```md
go run ./cmd/myteam daemon start
```

Every doc that teaches end users how to install, log in, or start the daemon must switch to `myteam`.

- [ ] **Step 4: Update contributor guidance that still points to `@multica/web` or `multica` CLI**

Implementation target:

```md
pnpm --filter @myteam/web exec vitest run src/path/to/file.test.ts
make cli ARGS="..."   # Run myteam CLI (e.g. make cli ARGS="config")
```

This applies to `AGENTS.md`, `CLAUDE.md`, `README*`, and `CONTRIBUTING.md`.

- [ ] **Step 5: Verify the doc surface**

Run:

```bash
rg -n "\bmultica\b|MULTICA_|@multica/web|multica-ai/tap/multica" \
  apps/web/app/(dashboard)/account \
  README.md README.zh-CN.md CLI_AND_DAEMON.md SELF_HOSTING.md CONTRIBUTING.md AGENTS.md CLAUDE.md
```

Expected: no user-facing onboarding or contributor docs still teach `multica`, except explicit compatibility notes that are intentionally marked legacy.

- [ ] **Step 6: Commit**

```bash
git add apps/web/app/'(dashboard)'/account README.md README.zh-CN.md CLI_AND_DAEMON.md SELF_HOSTING.md CONTRIBUTING.md AGENTS.md CLAUDE.md
git commit -m "docs(product): rename public multica references to myteam"
```

## Task 4: Rename Package Filters, Release Defaults, And Repo-Level Tooling

**Files:**
- Modify: `package.json`
- Modify: `apps/web/package.json`
- Modify: `scripts/check.sh`
- Modify: `server/internal/cli/update.go`
- Modify: `.goreleaser.yml`
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Rename the web workspace package**

Implementation target:

```json
{
  "name": "@myteam/web"
}
```

This is required because root scripts currently still hard-code `@multica/web`.

- [ ] **Step 2: Update root scripts and CI filters**

Implementation target:

```json
"dev:web": "pnpm --filter @myteam/web dev",
"build": "pnpm --filter @myteam/web build",
"typecheck": "pnpm --filter @myteam/web typecheck",
"test": "pnpm --filter @myteam/web test",
"lint": "pnpm --filter @myteam/web lint"
```

Implementation target:

```bash
# Root `pnpm typecheck` only filters @myteam/web — explicitly include
```

- [ ] **Step 3: Align release metadata with the actual MyTeam repo**

Implementation target:

```go
const (
    defaultReleaseRepo = "MyAIOSHub/MyTeam"
    defaultBrewFormula = "MyAIOSHub/homebrew-tap/myteam"
)
```

Do not leave `dy9759/MyTeam` in update logic while `.goreleaser.yml` still says `multica-ai/homebrew-tap`.

- [ ] **Step 4: Rename release artifacts**

Implementation target:

```yaml
brews:
  - name: myteam
    homepage: "https://github.com/MyAIOSHub/MyTeam"
    description: "MyTeam CLI — local agent runtime and management tool for the MyTeam platform"
    install: |
      bin.install "myteam"
    test: |
      system "#{bin}/myteam", "version"
```

- [ ] **Step 5: Verify workspace/tooling rename**

Run:

```bash
pnpm --filter @myteam/web typecheck
rg -n "@multica/web|dy9759/MyTeam|multica-ai/homebrew-tap|project_name: multica|binary: multica" \
  package.json apps/web/package.json scripts/check.sh server/internal/cli/update.go .goreleaser.yml .github/workflows/ci.yml
```

Expected: tooling points at `@myteam/web` and release metadata points at MyTeam-owned coordinates.

- [ ] **Step 6: Commit**

```bash
git add package.json apps/web/package.json scripts/check.sh server/internal/cli/update.go .goreleaser.yml .github/workflows/ci.yml
git commit -m "chore(release): align tooling and packaging with myteam"
```

## Task 5: Gated Deep Identity Rename For Go Module Path And Infra Defaults

**Files:**
- Modify: `server/go.mod`
- Modify: every Go file importing `github.com/multica-ai/multica/...`
- Modify: `.env.example`
- Modify: `docker-compose.yml`
- Modify: `.github/workflows/ci.yml`
- Modify: `scripts/check.sh`
- Modify: `e2e/fixtures.ts`
- Modify: `e2e/helpers.ts`

- [ ] **Step 1: Confirm the target canonical path before touching imports**

Run:

```bash
git remote -v
```

Expected today:

```text
origin  https://github.com/MyAIOSHub/MyTeam.git (fetch)
origin  https://github.com/MyAIOSHub/MyTeam.git (push)
```

If the final import path should be `github.com/MyAIOSHub/MyTeam/server`, record that decision before editing `go.mod`.

- [ ] **Step 2: Change the module path and rewrite imports in one atomic pass**

Implementation target:

```go
module github.com/MyAIOSHub/MyTeam/server
```

Use a scripted rewrite for imports. Do not hand-edit 400+ references one-by-one.

- [ ] **Step 3: Rename deep infra defaults only after module path is green**

Implementation target:

```dotenv
POSTGRES_DB=myteam
POSTGRES_USER=myteam
POSTGRES_PASSWORD=myteam
DATABASE_URL=postgres://myteam:myteam@localhost:5432/myteam?sslmode=disable
```

Implementation target:

```yaml
name: myteam
```

This is where container names, local DB defaults, and CI service values change. Keep this isolated from Phase 1 so failures are attributable.

- [ ] **Step 4: Update test fixtures and internal samples that still encode `multica`**

Implementation target:

```ts
const DATABASE_URL = process.env.DATABASE_URL ?? "postgres://myteam:myteam@localhost:5432/myteam?sslmode=disable";
```

Implementation target:

```ts
email: `e2e+${suffix}@myteam.ai`,
```

Only rename MIME values or protocol strings if they are branding-derived and not externally contracted.

- [ ] **Step 5: Run full verification for the deep rename**

Run:

```bash
pnpm typecheck
pnpm test
make test
```

Expected: package resolution, Go imports, DB defaults, and tests all pass under the new module/database identity.

- [ ] **Step 6: Stop and reassess before deleting compatibility**

Run:

```bash
rg -n "\bmultica\b|MULTICA_|github.com/multica-ai/multica|@multica/web|postgres://multica:multica@" .
```

Expected: only intentionally retained compatibility notes remain. If operational docs or installers outside this repo still reference `multica`, keep the `multica` compatibility binary one more release.

- [ ] **Step 7: Commit**

```bash
git add server/go.mod server .env.example docker-compose.yml .github/workflows/ci.yml scripts/check.sh e2e
git commit -m "refactor(repo): complete deep multica to myteam identity migration"
```

## Verification Checklist

- `server/bin/myteam` is built by default.
- Any remaining `server/bin/multica` is explicitly marked compatibility-only.
- Account onboarding UI no longer teaches `multica` commands or `MULTICA_*` vars.
- Public docs no longer instruct users to install or run `multica`.
- Root scripts no longer use `@multica/web`.
- Release metadata points to MyTeam-owned repo/tap coordinates.
- Phase 2 only starts after the module-path target is explicitly confirmed.

## Risks

- The deepest risk is changing Go module identity and import paths in the same pass as product-surface copy. That is why Phase 2 is explicitly gated.
- Build/output drift already exists today: Desktop and newer web flows expect `server/bin/myteam`, while `Makefile` still only builds `bin/multica`.
- Release config is currently split across conflicting owners (`MyAIOSHub`, `dy9759`, `multica-ai`). Fix that before publishing any renamed installer instructions.
- Some `multica` strings are not branding; they are compatibility surfaces or historical fixture data. Every remaining hit must be classified before deletion.

## Self-Review

- **Spec coverage:** The plan covers public CLI/build/output rename, env-var migration, web/docs rewrite, package/release tooling rename, and a gated deep module/infra rename.
- **Placeholder scan:** No `TODO` or `TBD` placeholders remain. The only explicit gate is the module-path decision, which is an actual prerequisite, not a filler.
- **Type consistency:** The plan consistently treats `myteam` as the canonical public surface and `multica` as compatibility-only until Phase 2 is complete.

Plan complete and saved to `docs/superpowers/plans/2026-04-22-rename-multica-to-myteam.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
