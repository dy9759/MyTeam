#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

POSTGRES_DB="${POSTGRES_DB:-myteam}"
POSTGRES_USER="${POSTGRES_USER:-myteam}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-myteam}"

export PGPASSWORD="$POSTGRES_PASSWORD"

POSTGRES_EXEC=()

compose_postgres_id() {
  docker compose ps -q postgres 2>/dev/null | head -n 1
}

is_container_running() {
  local container_id="${1:-}"
  [ -n "$container_id" ] && [ "$(docker inspect -f '{{.State.Running}}' "$container_id" 2>/dev/null || true)" = "true" ]
}

find_published_postgres_container() {
  docker ps \
    --filter "publish=${POSTGRES_PORT}" \
    --format '{{.ID}} {{.Ports}}' \
    | awk '/->5432\/tcp/ { print $1; exit }'
}

compose_id="$(compose_postgres_id)"
if is_container_running "$compose_id"; then
  echo "==> Using PostgreSQL container from current compose project on localhost:${POSTGRES_PORT}..."
  POSTGRES_EXEC=(docker compose exec -T postgres)
else
  published_id="$(find_published_postgres_container)"
  if is_container_running "$published_id"; then
    echo "==> Reusing existing PostgreSQL container on localhost:${POSTGRES_PORT} (${published_id})..."
    POSTGRES_EXEC=(docker exec -i "$published_id")
  else
    echo "==> Ensuring PostgreSQL container is running on localhost:${POSTGRES_PORT}..."
    docker compose up -d postgres
    POSTGRES_EXEC=(docker compose exec -T postgres)
  fi
fi

echo "==> Waiting for PostgreSQL to be ready..."
until "${POSTGRES_EXEC[@]}" pg_isready -U "$POSTGRES_USER" -d postgres > /dev/null 2>&1; do
  sleep 1
done

echo "==> Ensuring database '$POSTGRES_DB' exists..."
db_exists="$("${POSTGRES_EXEC[@]}" \
  psql -U "$POSTGRES_USER" -d postgres -Atqc "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'")"

if [ "$db_exists" != "1" ]; then
  "${POSTGRES_EXEC[@]}" \
    psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 \
    -c "CREATE DATABASE \"$POSTGRES_DB\"" \
    > /dev/null
fi

echo "✓ PostgreSQL ready. Application database: $POSTGRES_DB"
