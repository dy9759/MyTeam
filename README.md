# MyTeam

[![CI](https://github.com/MyAIOSHub/MyTeam/actions/workflows/ci.yml/badge.svg)](https://github.com/MyAIOSHub/MyTeam/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![GitHub stars](https://img.shields.io/github/stars/MyAIOSHub/MyTeam?style=social)](https://github.com/MyAIOSHub/MyTeam)

AI-native task management for humans and coding agents.

[中文说明](README.zh-CN.md) · [Self-Hosting](SELF_HOSTING.md) · [CLI and Daemon](CLI_AND_DAEMON.md) · [Contributing](CONTRIBUTING.md)

MyTeam gives your team a shared workspace where humans and agents can pick up work, run sessions, manage projects, and collaborate through local or remote runtimes.

## What You Get

- Agent-first issues, projects, and execution sessions
- Local daemon support for CLI-based coding agents
- Web app and desktop control plane in the same repository
- PostgreSQL + pgvector backend with realtime updates over WebSocket
- Built-in support for files, comments, workspace members, and agent runtimes

## Quick Start

### Prerequisites

- Node.js 22
- pnpm
- Go 1.26.1
- Docker / Docker Compose

### Run Locally

```bash
git clone https://github.com/MyAIOSHub/MyTeam.git
cd MyTeam
cp .env.example .env
make setup
make start
```

Once the app is up:

- Web app: `http://localhost:3000`
- API server: `http://localhost:8080`

### Run the Local Daemon

```bash
make daemon
```

Useful shortcuts:

```bash
make daemon-status
make daemon-logs
make myteam ARGS="daemon stop"
```

If you prefer to invoke the CLI directly:

```bash
cd server
go run ./cmd/myteam daemon start
```

The daemon auto-detects locally available coding CLIs such as `codex` and `claude` when they are installed.

### Run the Desktop App

```bash
pnpm --filter @myteam/desktop dev
```

The desktop app expects the backend to be running locally.

## Project Layout

| Path | Purpose |
| --- | --- |
| `apps/web/` | Next.js web client |
| `apps/desktop/` | Electron desktop app |
| `server/` | Go API server, CLI, daemon, sqlc queries, and migrations |
| `e2e/` | Playwright end-to-end tests |
| `scripts/` | Local setup, verification, and environment helpers |

## Common Commands

| Command | Description |
| --- | --- |
| `make setup` | Install dependencies, start PostgreSQL, and run migrations |
| `make start` | Start the backend and web app together |
| `make daemon` | Start the local daemon |
| `make build` | Build the Go server and CLI binaries |
| `make test` | Run backend Go tests |
| `pnpm typecheck` | Run web TypeScript type checks |
| `pnpm test` | Run web unit tests |
| `pnpm --filter @myteam/desktop typecheck` | Run desktop type checks |
| `pnpm --filter @myteam/desktop test` | Run desktop unit tests |
| `make check` | Run the full verification pipeline |
| `make worktree-env` | Generate a worktree-specific `.env.worktree` |

## Development Notes

- CI runs frontend build/typecheck/tests, desktop typecheck/tests, and backend Go tests against PostgreSQL with pgvector.
- For isolated local development, use `make setup-worktree` and `make start-worktree`.
- Some environment variables and legacy internal entry points still use the `MYTEAM_` prefix or the `myteam` binary name. Day-to-day use can go through the `myteam` entry points documented above.

## Additional Docs

- [SELF_HOSTING.md](SELF_HOSTING.md)
- [CLI_AND_DAEMON.md](CLI_AND_DAEMON.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
