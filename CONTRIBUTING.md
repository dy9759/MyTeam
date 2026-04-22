# Contributing Guide

This guide documents the local development workflow for contributors working on the MyTeam codebase.

It covers:

- first-time setup
- day-to-day development in the main checkout
- isolated worktree development
- the checkout-local PostgreSQL model
- testing and verification
- troubleshooting and destructive reset options

## Development Model

Local development uses one PostgreSQL container per checkout and one database per checkout.

- the main checkout usually uses `.env` and `POSTGRES_DB=myteam`
- each Git worktree uses its own `.env.worktree`
- the main checkout keeps PostgreSQL on `localhost:5432`
- each worktree gets its own PostgreSQL host port and database name
- backend and frontend ports are still unique per worktree

This keeps Docker simple while isolating schema, data, and local port bindings.

## Prerequisites

- Node.js `v20+`
- `pnpm` `v10.28+`
- Go `v1.26+`
- Docker

## Important Rules

- The main checkout should use `.env`.
- A worktree should use `.env.worktree`.
- Do not copy `.env` into a worktree directory.

Why:

- the current command flow prefers `.env` over `.env.worktree`
- if a worktree contains `.env`, it can accidentally point back to the main database

## Environment Files

### Main Checkout

Create `.env` once:

```bash
cp .env.example .env
```

By default, `.env` points to:

```bash
POSTGRES_DB=myteam
POSTGRES_PORT=5432
DATABASE_URL=postgres://myteam:myteam@localhost:5432/myteam?sslmode=disable
PORT=8080
FRONTEND_PORT=3000
```

### Worktree

Generate `.env.worktree` from inside the worktree:

```bash
make worktree-env
```

That generates values like:

```bash
POSTGRES_DB=myteam_my_feature_702
POSTGRES_PORT=18431
PORT=18782
FRONTEND_PORT=13702
DATABASE_URL=postgres://myteam:myteam@localhost:18431/myteam_my_feature_702?sslmode=disable
```

Notes:

- `POSTGRES_DB` is unique per worktree
- `POSTGRES_PORT` is unique per worktree
- backend and frontend ports are derived from the worktree path hash
- `make worktree-env` refuses to overwrite an existing `.env.worktree`

To regenerate a worktree env file:

```bash
FORCE=1 make worktree-env
```

## First-Time Setup

### Main Checkout

From the main checkout:

```bash
cp .env.example .env
make setup-main
```

What `make setup-main` does:

- installs JavaScript dependencies with `pnpm install`
- ensures the PostgreSQL container for this checkout is running
- creates the application database if it does not exist
- runs all migrations against that database

Start the app:

```bash
make start-main
```

Stop the app processes:

```bash
make stop-main
```

This does not stop PostgreSQL.

### Worktree

From the worktree directory:

```bash
make worktree-env
make setup-worktree
```

What `make setup-worktree` does:

- uses `.env.worktree`
- ensures the worktree PostgreSQL container is running on its generated host port
- creates the worktree database if it does not exist
- runs migrations against the worktree database

Start the worktree app:

```bash
make start-worktree
```

Stop the worktree app processes:

```bash
make stop-worktree
```

## Recommended Daily Workflow

### Main Checkout

Use the main checkout when you want a stable local environment for `main`.

```bash
make start-main
make stop-main
make check-main
```

### Feature Worktree

Use a worktree when you want isolated data and separate app ports.

```bash
git worktree add ../myteam-feature -b feat/my-change main
cd ../myteam-feature
make worktree-env
make setup-worktree
make start-worktree
```

After that, day-to-day commands are:

```bash
make start-worktree
make stop-worktree
make check-worktree
```

## Running Main and Worktree at the Same Time

This is a first-class workflow.

Example:

- main checkout
  - database: `myteam`
  - backend: `8080`
  - frontend: `3000`
- worktree checkout
  - database: `myteam_my_feature_702`
  - backend: generated worktree port such as `18782`
  - frontend: generated worktree port such as `13702`

Both checkouts use:

- separate PostgreSQL containers for each checkout
- separate PostgreSQL host ports

They do not share application data because each uses a different database.

## Cleaning Up a Worktree

When you are done with a worktree, verify what Git still knows about it first:

```bash
git worktree list
```

Then remove the worktree by path from a different checkout:

```bash
git worktree remove ../myteam-feature
```

If a worktree directory was deleted manually or Git still shows a stale entry, prune the leftovers:

```bash
git worktree prune
```

Use `git worktree list` again after cleanup to confirm the entry is gone.

## Command Reference

### Checkout-Local Infrastructure

Start the PostgreSQL container for this checkout:

```bash
make db-up
```

Stop the PostgreSQL container for this checkout:

```bash
make db-down
```

Important:

- `make db-down` stops the container but keeps the Docker volume
- your local databases are preserved

### App Lifecycle

Main checkout:

```bash
make setup-main
make start-main
make stop-main
make check-main
```

Worktree:

```bash
make worktree-env
make setup-worktree
make start-worktree
make stop-worktree
make check-worktree
```

Generic targets for the current checkout:

```bash
make setup
make start
make stop
make check
make dev
make test
make migrate-up
make migrate-down
```

These generic targets require a valid env file in the current directory.

## How Database Creation Works

Database creation is automatic.

The following commands all ensure the target database exists before they continue:

- `make setup`
- `make start`
- `make dev`
- `make test`
- `make migrate-up`
- `make migrate-down`
- `make check`

That logic lives in `scripts/ensure-postgres.sh`.

## Testing

Run all local checks:

```bash
make check-main
```

Or from a worktree:

```bash
make check-worktree
```

This runs:

1. TypeScript typecheck
2. TypeScript unit tests
3. Go tests
4. Playwright E2E tests

Notes:

- Go tests create their own fixture data
- E2E tests create their own workspace and issue fixtures
- the check flow starts backend/frontend only if they are not already running

## Local Codex Daemon

Run the local daemon:

```bash
make daemon
```

The daemon authenticates using the CLI's stored token (`myteam login`).
It registers runtimes for all watched workspaces from the CLI config.

## Troubleshooting

### Missing Env File

If you see:

```text
Missing env file: .env
```

or:

```text
Missing env file: .env.worktree
```

then create the expected env file first.

Main checkout:

```bash
cp .env.example .env
```

Worktree:

```bash
make worktree-env
```

### Check Which Database a Checkout Uses

Inspect the env file:

```bash
cat .env
cat .env.worktree
```

Look for:

- `POSTGRES_DB`
- `DATABASE_URL`
- `PORT`
- `FRONTEND_PORT`

### List All Local Databases in Shared PostgreSQL

```bash
docker compose exec -T postgres psql -U myteam -d postgres -At -c "select datname from pg_database order by datname;"
```

### Worktree Is Accidentally Using the Main Database

Check whether the worktree contains `.env`.

It should not.

The safe worktree setup is:

```bash
make worktree-env
make setup-worktree
make start-worktree
```

### App Stops but PostgreSQL Keeps Running

That is expected.

- `make stop`
- `make stop-main`
- `make stop-worktree`

only stop backend/frontend processes.

To stop the PostgreSQL container for this checkout:

```bash
make db-down
```

## Destructive Reset

If you want to stop PostgreSQL and keep your local databases:

```bash
make db-down
```

If you want to wipe all local PostgreSQL data for this repo:

```bash
docker compose down -v
```

Warning:

- this deletes the shared Docker volume
- this deletes the main database and every worktree database in that volume
- after that you must run `make setup-main` or `make setup-worktree` again

## Typical Flows

### Stable Main Environment

```bash
cp .env.example .env
make setup-main
make start-main
```

### Feature Worktree

```bash
git worktree add ../myteam-feature -b feat/my-change main
cd ../myteam-feature
make worktree-env
make setup-worktree
make start-worktree
```

### Return to a Previously Configured Worktree

```bash
cd ../myteam-feature
make start-worktree
```

### Validate Before Pushing

Main checkout:

```bash
make check-main
```

Worktree:

```bash
make check-worktree
```
