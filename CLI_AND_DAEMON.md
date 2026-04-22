# CLI and Agent Daemon Guide

The `myteam` CLI connects your local machine to MyTeam. It handles authentication, workspace management, issue tracking, and runs the agent daemon that executes AI tasks locally.

## Installation

### Homebrew (macOS/Linux)

```bash
brew tap MyAIOSHub/tap
brew install myteam
```

### Build from Source

```bash
git clone https://github.com/MyAIOSHub/MyTeam.git
cd myteam
make build
cp server/bin/myteam /usr/local/bin/myteam
```

### Update

```bash
myteam update
```

This auto-detects your installation method (Homebrew or manual) and upgrades accordingly.

## Quick Start

```bash
# 1. Authenticate (opens browser for login)
myteam login

# 2. Start the agent daemon
myteam daemon start

# 3. Done — agents in your watched workspaces can now execute tasks on your machine
```

`myteam login` automatically discovers all workspaces you belong to and adds them to the daemon watch list.

## Authentication

### Browser Login

```bash
myteam login
```

Opens your browser for OAuth authentication, creates a 90-day personal access token, and auto-configures your workspaces.

### Token Login

```bash
myteam login --token
```

Authenticate by pasting a personal access token directly. Useful for headless environments.

### Check Status

```bash
myteam auth status
```

Shows your current server, user, and token validity.

### Logout

```bash
myteam auth logout
```

Removes the stored authentication token.

## Agent Daemon

The daemon is the local agent runtime. It detects available AI CLIs on your machine, registers them with the MyTeam server, and executes tasks when agents are assigned work.

### Start

```bash
myteam daemon start
```

By default, the daemon runs in the background and logs to `~/.myteam/daemon.log`.

To run in the foreground (useful for debugging):

```bash
myteam daemon start --foreground
```

### Stop

```bash
myteam daemon stop
```

### Status

```bash
myteam daemon status
myteam daemon status --output json
```

Shows PID, uptime, detected agents, and watched workspaces.

### Logs

```bash
myteam daemon logs              # Last 50 lines
myteam daemon logs -f           # Follow (tail -f)
myteam daemon logs -n 100       # Last 100 lines
```

### Supported Agents

The daemon auto-detects these AI CLIs on your PATH:

| CLI | Command | Description |
|-----|---------|-------------|
| [Claude Code](https://docs.anthropic.com/en/docs/claude-code) | `claude` | Anthropic's coding agent |
| [Codex](https://github.com/openai/codex) | `codex` | OpenAI's coding agent |

You need at least one installed. The daemon registers each detected CLI as an available runtime.

### How It Works

1. On start, the daemon detects installed agent CLIs and registers a runtime for each agent in each watched workspace
2. It polls the server at a configurable interval (default: 3s) for claimed tasks
3. When a task arrives, it creates an isolated workspace directory, spawns the agent CLI, and streams results back
4. Heartbeats are sent periodically (default: 15s) so the server knows the daemon is alive
5. On shutdown, all runtimes are deregistered

### Configuration

Daemon behavior is configured via flags or environment variables:

| Setting | Flag | Env Variable | Default |
|---------|------|--------------|---------|
| Poll interval | `--poll-interval` | `MYTEAM_DAEMON_POLL_INTERVAL` | `3s` |
| Heartbeat interval | `--heartbeat-interval` | `MYTEAM_DAEMON_HEARTBEAT_INTERVAL` | `15s` |
| Agent timeout | `--agent-timeout` | `MYTEAM_AGENT_TIMEOUT` | `2h` |
| Max concurrent tasks | `--max-concurrent-tasks` | `MYTEAM_DAEMON_MAX_CONCURRENT_TASKS` | `20` |
| Daemon ID | `--daemon-id` | `MYTEAM_DAEMON_ID` | hostname |
| Device name | `--device-name` | `MYTEAM_DAEMON_DEVICE_NAME` | hostname |
| Runtime name | `--runtime-name` | `MYTEAM_AGENT_RUNTIME_NAME` | `Local Agent` |
| Workspaces root | — | `MYTEAM_WORKSPACES_ROOT` | `~/myteam_workspaces` |

Agent-specific overrides:

| Variable | Description |
|----------|-------------|
| `MYTEAM_CLAUDE_PATH` | Custom path to the `claude` binary |
| `MYTEAM_CLAUDE_MODEL` | Override the Claude model used |
| `MYTEAM_CODEX_PATH` | Custom path to the `codex` binary |
| `MYTEAM_CODEX_MODEL` | Override the Codex model used |

### Self-Hosted Server

When connecting to a self-hosted MyTeam instance, point the CLI to your server before logging in:

```bash
export MYTEAM_APP_URL=https://app.example.com
export MYTEAM_SERVER_URL=wss://api.example.com/ws

myteam login
myteam daemon start
```

Or set them persistently:

```bash
myteam config set app_url https://app.example.com
myteam config set server_url wss://api.example.com/ws
```

### Profiles

Profiles let you run multiple daemons on the same machine — for example, one for production and one for a staging server.

```bash
# Start a daemon for the staging server
myteam --profile staging login
myteam --profile staging daemon start

# Default profile runs separately
myteam daemon start
```

Each profile gets its own config directory (`~/.myteam/profiles/<name>/`), daemon state, health port, and workspace root.

## Workspaces

### List Workspaces

```bash
myteam workspace list
```

Watched workspaces are marked with `*`. The daemon only processes tasks for watched workspaces.

### Watch / Unwatch

```bash
myteam workspace watch <workspace-id>
myteam workspace unwatch <workspace-id>
```

### Get Details

```bash
myteam workspace get <workspace-id>
myteam workspace get <workspace-id> --output json
```

### List Members

```bash
myteam workspace members <workspace-id>
```

## Issues

### List Issues

```bash
myteam issue list
myteam issue list --status in_progress
myteam issue list --priority urgent --assignee "Agent Name"
myteam issue list --limit 20 --output json
```

Available filters: `--status`, `--priority`, `--assignee`, `--limit`.

### Get Issue

```bash
myteam issue get <id>
myteam issue get <id> --output json
```

### Create Issue

```bash
myteam issue create --title "Fix login bug" --description "..." --priority high --assignee "Lambda"
```

Flags: `--title` (required), `--description`, `--status`, `--priority`, `--assignee`, `--parent`, `--due-date`.

### Update Issue

```bash
myteam issue update <id> --title "New title" --priority urgent
```

### Assign Issue

```bash
myteam issue assign <id> --to "Lambda"
myteam issue assign <id> --unassign
```

### Change Status

```bash
myteam issue status <id> in_progress
```

Valid statuses: `backlog`, `todo`, `in_progress`, `in_review`, `done`, `blocked`, `cancelled`.

### Comments

```bash
# List comments
myteam issue comment list <issue-id>

# Add a comment
myteam issue comment add <issue-id> --content "Looks good, merging now"

# Reply to a specific comment
myteam issue comment add <issue-id> --parent <comment-id> --content "Thanks!"

# Delete a comment
myteam issue comment delete <comment-id>
```

### Execution History

```bash
# List all execution runs for an issue
myteam issue runs <issue-id>
myteam issue runs <issue-id> --output json

# View messages for a specific execution run
myteam issue run-messages <task-id>
myteam issue run-messages <task-id> --output json

# Incremental fetch (only messages after a given sequence number)
myteam issue run-messages <task-id> --since 42 --output json
```

The `runs` command shows all past and current executions for an issue, including running tasks. The `run-messages` command shows the detailed message log (tool calls, thinking, text, errors) for a single run. Use `--since` for efficient polling of in-progress runs.

## Configuration

### View Config

```bash
myteam config show
```

Shows config file path, server URL, app URL, and default workspace.

### Set Values

```bash
myteam config set server_url wss://api.example.com/ws
myteam config set app_url https://app.example.com
myteam config set workspace_id <workspace-id>
```

## Other Commands

```bash
myteam version              # Show CLI version and commit hash
myteam update               # Update to latest version
myteam agent list           # List agents in the current workspace
```

## Output Formats

Most commands support `--output` with two formats:

- `table` — human-readable table (default for list commands)
- `json` — structured JSON (useful for scripting and automation)

```bash
myteam issue list --output json
myteam daemon status --output json
```
