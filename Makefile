.PHONY: dev daemon cli multica myteam daemon-start daemon-stop daemon-status daemon-logs build test migrate-up migrate-down sqlc seed clean setup start stop check worktree-env setup-main start-main stop-main check-main setup-worktree start-worktree stop-worktree check-worktree db-up db-down setup-agent-runner

MAIN_ENV_FILE ?= .env
WORKTREE_ENV_FILE ?= .env.worktree
ENV_FILE ?= $(if $(wildcard $(MAIN_ENV_FILE)),$(MAIN_ENV_FILE),$(if $(wildcard $(WORKTREE_ENV_FILE)),$(WORKTREE_ENV_FILE),$(MAIN_ENV_FILE)))

ifneq ($(wildcard $(ENV_FILE)),)
include $(ENV_FILE)
endif

POSTGRES_DB ?= multica
POSTGRES_USER ?= multica
POSTGRES_PASSWORD ?= multica
POSTGRES_PORT ?= 5432
PORT ?= 8080
FRONTEND_PORT ?= 3000
FRONTEND_ORIGIN ?= http://localhost:$(FRONTEND_PORT)
MULTICA_APP_URL ?= $(FRONTEND_ORIGIN)
DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
NEXT_PUBLIC_API_URL ?= http://localhost:$(PORT)
NEXT_PUBLIC_WS_URL ?= ws://localhost:$(PORT)/ws
GOOGLE_REDIRECT_URI ?= $(FRONTEND_ORIGIN)/auth/callback
MULTICA_SERVER_URL ?= ws://localhost:$(PORT)/ws

export

MULTICA_ARGS ?= $(ARGS)

COMPOSE := docker compose

define REQUIRE_ENV
	@if [ ! -f "$(ENV_FILE)" ]; then \
		echo "Missing env file: $(ENV_FILE)"; \
		echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."; \
		exit 1; \
	fi
endef

# ---------- One-click commands ----------

# First-time setup: install deps, start DB, run migrations
setup:
	$(REQUIRE_ENV)
	@echo "==> Using env file: $(ENV_FILE)"
	@echo "==> Installing dependencies..."
	pnpm install
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	@echo "==> Running migrations..."
	cd server && go run ./cmd/migrate up
	@$(MAKE) setup-agent-runner || echo "(personal agent runner not installed; run 'make setup-agent-runner' manually when ready)"
	@echo ""
	@echo "✓ Setup complete! Run 'make start' to launch the app."

# Install Python deps for the personal agent runner (claude-agent-sdk).
setup-agent-runner:
	@command -v python3 >/dev/null 2>&1 || { echo "python3 is required; install Python 3.10+"; exit 1; }
	@python3 -m pip install --upgrade pip >/dev/null
	@python3 -m pip install -r server/pkg/agent_runner/requirements.txt
	@echo "✓ agent runner Python deps installed"

# Start all services (backend + frontend)
start:
	$(REQUIRE_ENV)
	@echo "Using env file: $(ENV_FILE)"
	@echo "Backend: http://localhost:$(PORT)"
	@echo "Frontend: http://localhost:$(FRONTEND_PORT)"
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	@echo "Starting backend and frontend..."
	@trap 'kill 0' EXIT; \
		(cd server && go run ./cmd/server) & \
		pnpm dev:web & \
		wait

# Stop all services
stop:
	$(REQUIRE_ENV)
	@echo "Stopping services..."
	@-lsof -ti:$(PORT) | xargs kill -9 2>/dev/null
	@-lsof -ti:$(FRONTEND_PORT) | xargs kill -9 2>/dev/null
	@echo "✓ App processes stopped. Shared PostgreSQL is still running on localhost:5432."

# Full verification: typecheck + unit tests + Go tests + E2E
check:
	$(REQUIRE_ENV)
	@ENV_FILE="$(ENV_FILE)" bash scripts/check.sh

db-up:
	@$(COMPOSE) up -d postgres

db-down:
	@$(COMPOSE) down

worktree-env:
	@bash scripts/init-worktree-env.sh .env.worktree

setup-main:
	@$(MAKE) setup ENV_FILE=$(MAIN_ENV_FILE)

start-main:
	@$(MAKE) start ENV_FILE=$(MAIN_ENV_FILE)

stop-main:
	@$(MAKE) stop ENV_FILE=$(MAIN_ENV_FILE)

check-main:
	@ENV_FILE=$(MAIN_ENV_FILE) bash scripts/check.sh

setup-worktree:
	@echo "==> Generating $(WORKTREE_ENV_FILE) with unique ports..."
	@FORCE=1 bash scripts/init-worktree-env.sh $(WORKTREE_ENV_FILE)
	@$(MAKE) setup ENV_FILE=$(WORKTREE_ENV_FILE)

start-worktree:
	@$(MAKE) start ENV_FILE=$(WORKTREE_ENV_FILE)

stop-worktree:
	@$(MAKE) stop ENV_FILE=$(WORKTREE_ENV_FILE)

check-worktree:
	@ENV_FILE=$(WORKTREE_ENV_FILE) bash scripts/check.sh

# ---------- Individual commands ----------

# Go server
dev:
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/server

daemon:
	@$(MAKE) multica MULTICA_ARGS="daemon"

cli:
	@$(MAKE) multica MULTICA_ARGS="$(MULTICA_ARGS)"

multica:
	cd server && go run ./cmd/multica $(MULTICA_ARGS)

# ---------- myteam aliases (friendlier front for the multica CLI) ----------

# Generic pass-through: `make myteam ARGS="daemon status"` equals `make multica ARGS="daemon status"`
myteam:
	@$(MAKE) multica MULTICA_ARGS="$(ARGS)"

# Common daemon shortcuts
daemon-start:
	@$(MAKE) multica MULTICA_ARGS="daemon start $(ARGS)"

daemon-stop:
	@$(MAKE) multica MULTICA_ARGS="daemon stop $(ARGS)"

daemon-status:
	@$(MAKE) multica MULTICA_ARGS="daemon status $(ARGS)"

daemon-logs:
	@$(MAKE) multica MULTICA_ARGS="daemon logs $(ARGS)"

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

build:
	cd server && go build -o bin/server ./cmd/server
	cd server && go build -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o bin/multica ./cmd/multica

test:
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go test ./...

# Database
migrate-up:
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate up

migrate-down:
	$(REQUIRE_ENV)
	@bash scripts/ensure-postgres.sh "$(ENV_FILE)"
	cd server && go run ./cmd/migrate down

sqlc:
	cd server && sqlc generate

# Cleanup
clean:
	rm -rf server/bin server/tmp
