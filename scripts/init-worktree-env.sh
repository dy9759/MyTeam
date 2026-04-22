#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env.worktree}"

if [ -f "$ENV_FILE" ] && [ "${FORCE:-0}" != "1" ]; then
  echo "Refusing to overwrite existing $ENV_FILE. Re-run with FORCE=1 if you want to regenerate it."
  exit 1
fi

worktree_name="${WORKTREE_NAME:-$(basename "$PWD")}"
slug="$(printf '%s' "$worktree_name" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//')"
if [ -z "$slug" ]; then
  slug="myteam"
fi

hash_value="$(printf '%s' "$PWD" | cksum | awk '{print $1}')"
app_offset=$((hash_value % 1000))
postgres_offset=$((hash_value % 10000))

postgres_db="myteam_${slug}_${postgres_offset}"
postgres_port=$((15432 + postgres_offset))
backend_port=$((18080 + app_offset))
frontend_port=$((13000 + app_offset))
frontend_origin="http://localhost:${frontend_port}"

cat > "$ENV_FILE" <<EOF
POSTGRES_DB=${postgres_db}
POSTGRES_USER=myteam
POSTGRES_PASSWORD=myteam
POSTGRES_PORT=${postgres_port}
DATABASE_URL=postgres://myteam:myteam@localhost:${postgres_port}/${postgres_db}?sslmode=disable

PORT=${backend_port}
JWT_SECRET=change-me-in-production
MYTEAM_SERVER_URL=ws://localhost:${backend_port}/ws
MYTEAM_APP_URL=${frontend_origin}

GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
GOOGLE_REDIRECT_URI=${frontend_origin}/auth/callback

FRONTEND_PORT=${frontend_port}
FRONTEND_ORIGIN=${frontend_origin}
NEXT_PUBLIC_API_URL=http://localhost:${backend_port}
NEXT_PUBLIC_WS_URL=ws://localhost:${backend_port}/ws
EOF

echo "Generated $ENV_FILE for worktree '$worktree_name'"
echo "  PostgreSQL: localhost:${postgres_port}"
echo "  Database: ${postgres_db}"
echo "  Backend:  http://localhost:${backend_port}"
echo "  Frontend: ${frontend_origin}"
echo ""
echo "Next steps:"
echo "  make setup-worktree"
echo "  make start-worktree"
