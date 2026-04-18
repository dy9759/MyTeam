#!/usr/bin/env bash
# Shared helpers for scenario API smoke tests.
# Sourced by p1-5-project-mgmt.sh / p0-1-doc-collab.sh.
#
# Records each step as a row in $RESULTS_FILE so the runner can render a
# markdown report at the end. Set BASE_URL=http://localhost:8080 and
# RESULTS_FILE=/tmp/results.tsv before sourcing.

: "${BASE_URL:=http://localhost:8080}"
: "${RESULTS_FILE:=/tmp/scenario-results.tsv}"
: "${HTTP_CODE_FILE:=/tmp/.scenario_http_code}"
HTTP_CODE=""

# Wrapper around `cat $HTTP_CODE_FILE` so callers stay readable.
read_code() { HTTP_CODE=$(cat "$HTTP_CODE_FILE" 2>/dev/null || echo 0); }

# ANSI dim/green/red for the live console line.
green()  { printf "\033[32m%s\033[0m" "$*"; }
red()    { printf "\033[31m%s\033[0m" "$*"; }
yellow() { printf "\033[33m%s\033[0m" "$*"; }

# record <scenario> <step> <status> <http_code> <note>
record() {
  local scenario="$1" step="$2" status="$3" code="$4" note="${5:-}"
  printf "%s\t%s\t%s\t%s\t%s\n" "$scenario" "$step" "$status" "$code" "$note" >> "$RESULTS_FILE"
  case "$status" in
    PASS) printf "  [%s] %s — %s (HTTP %s) %s\n" "$(green PASS)" "$scenario" "$step" "$code" "$note" ;;
    FAIL) printf "  [%s] %s — %s (HTTP %s) %s\n" "$(red FAIL)" "$scenario" "$step" "$code" "$note" ;;
    SKIP) printf "  [%s] %s — %s — %s\n" "$(yellow SKIP)" "$scenario" "$step" "$note" ;;
  esac
}

# Send verification code, verify with master code, return JWT.
# Usage: TOKEN=$(login_as <email>)
login_as() {
  local email="$1"
  curl -sS -X POST -H "Content-Type: application/json" \
    -d "{\"email\":\"$email\"}" \
    "$BASE_URL/auth/send-code" > /dev/null

  local resp
  resp=$(curl -sS -X POST -H "Content-Type: application/json" \
    -d "{\"email\":\"$email\",\"code\":\"888888\"}" \
    "$BASE_URL/auth/verify-code")
  echo "$resp" | jq -r '.token'
}

# api <method> <path> <token> [workspace_id] [body]
# Echoes the response body. Last HTTP code stored in $HTTP_CODE.
api() {
  local method="$1" path="$2" token="$3" ws="${4:-}" body="${5:-}"
  local args=(-sS -X "$method" -H "Authorization: Bearer $token" \
    -H "Content-Type: application/json" \
    -w "\n__HTTP__%{http_code}")
  [[ -n "$ws" ]] && args+=(-H "X-Workspace-ID: $ws")
  [[ -n "$body" ]] && args+=(-d "$body")
  local raw
  raw=$(curl "${args[@]}" "$BASE_URL$path")
  if [[ "$raw" =~ __HTTP__([0-9]+) ]]; then
    echo "${BASH_REMATCH[1]}" > "$HTTP_CODE_FILE"
  else
    echo "0" > "$HTTP_CODE_FILE"
  fi
  printf "%s" "${raw%__HTTP__*}"
}

# Pick first workspace id for a logged-in user.
first_workspace_id() {
  local token="$1"
  api GET /api/workspaces "$token" "" | jq -r '.[0].id // empty'
}

# Get personal agent (with up-to-5s polling for async auto-provision).
poll_personal_agent() {
  local token="$1" ws="$2"
  local deadline=$(( $(date +%s) + 8 ))
  while [[ $(date +%s) -lt $deadline ]]; do
    local resp
    resp=$(api GET /api/personal-agent "$token" "$ws")
    read_code
    if [[ "$HTTP_CODE" == "200" ]]; then
      echo "$resp" | jq -r '.id // empty'
      return 0
    fi
    sleep 0.5
  done
  return 1
}

# Generate a unique-per-run suffix.
uniq_suffix() {
  date +%s%N | tail -c 9
}
