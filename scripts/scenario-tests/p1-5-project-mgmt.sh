#!/usr/bin/env bash
# P1-5: Dev Project Management — API smoke test.
#
# Walks: create project → create plan → create tasks (with primary_assignee_id
# pointing at the personal agent) → list tasks → fetch task detail.
# Each step records PASS/FAIL with HTTP code into $RESULTS_FILE.
set -u

DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib.sh
source "$DIR/lib.sh"

SCEN="P1-5"
SUFFIX=$(uniq_suffix)
EMAIL="p15-pm-$SUFFIX@scenario.test"

echo "[$SCEN] login as $EMAIL"
TOKEN=$(login_as "$EMAIL")
if [[ -z "$TOKEN" || "$TOKEN" == "null" ]]; then
  record "$SCEN" "login" FAIL 0 "no token returned"
  exit 1
fi
record "$SCEN" "login" PASS 200 "got jwt"

WS=$(first_workspace_id "$TOKEN")
if [[ -z "$WS" ]]; then
  record "$SCEN" "auto-workspace" FAIL 0 "no workspace auto-provisioned"
  exit 1
fi
record "$SCEN" "auto-workspace" PASS 200 "ws=$WS"

AGENT_ID=$(poll_personal_agent "$TOKEN" "$WS")
if [[ -z "$AGENT_ID" ]]; then
  record "$SCEN" "auto-personal-agent" FAIL "$HTTP_CODE" "agent never appeared"
else
  record "$SCEN" "auto-personal-agent" PASS 200 "agent=$AGENT_ID"
fi

# 1. Create project
PROJ_BODY="{\"title\":\"P1-5 Smoke $SUFFIX\",\"description\":\"scenario test\",\"schedule_type\":\"one_time\"}"
PROJ_RESP=$(api POST /api/projects "$TOKEN" "$WS" "$PROJ_BODY")
read_code
PROJ_ID=$(echo "$PROJ_RESP" | jq -r '.id // empty')
if [[ "$HTTP_CODE" =~ ^20[01]$ && -n "$PROJ_ID" ]]; then
  record "$SCEN" "create project" PASS "$HTTP_CODE" "id=$PROJ_ID"
else
  record "$SCEN" "create project" FAIL "$HTTP_CODE" "$(echo "$PROJ_RESP" | head -c 200)"
fi

# 2. Create plan
PLAN_BODY="{\"title\":\"P1-5 Plan $SUFFIX\",\"description\":\"plan body\",\"source_type\":\"project\"}"
PLAN_RESP=$(api POST /api/plans "$TOKEN" "$WS" "$PLAN_BODY")
read_code
PLAN_ID=$(echo "$PLAN_RESP" | jq -r '.id // empty')
if [[ "$HTTP_CODE" =~ ^20[01]$ && -n "$PLAN_ID" ]]; then
  record "$SCEN" "create plan" PASS "$HTTP_CODE" "id=$PLAN_ID"
else
  record "$SCEN" "create plan" FAIL "$HTTP_CODE" "$(echo "$PLAN_RESP" | head -c 200)"
fi

# 3. Create 2 tasks, second depends on first; assign agent
if [[ -n "$PLAN_ID" ]]; then
  T1_BODY="{\"plan_id\":\"$PLAN_ID\",\"title\":\"Spec the API\",\"step_order\":1,\"primary_assignee_id\":\"$AGENT_ID\",\"required_skills\":[\"writing\"]}"
  T1_RESP=$(api POST /api/tasks "$TOKEN" "$WS" "$T1_BODY")
  read_code
  T1_ID=$(echo "$T1_RESP" | jq -r '.id // empty')
  if [[ "$HTTP_CODE" =~ ^20[01]$ && -n "$T1_ID" ]]; then
    record "$SCEN" "create task #1 (with agent assignment)" PASS "$HTTP_CODE" "id=$T1_ID"
  else
    record "$SCEN" "create task #1 (with agent assignment)" FAIL "$HTTP_CODE" "$(echo "$T1_RESP" | head -c 200)"
  fi

  T2_BODY="{\"plan_id\":\"$PLAN_ID\",\"title\":\"Build the API\",\"step_order\":2,\"primary_assignee_id\":\"$AGENT_ID\",\"depends_on\":[\"$T1_ID\"],\"required_skills\":[\"coding\"]}"
  T2_RESP=$(api POST /api/tasks "$TOKEN" "$WS" "$T2_BODY")
  read_code
  T2_ID=$(echo "$T2_RESP" | jq -r '.id // empty')
  if [[ "$HTTP_CODE" =~ ^20[01]$ && -n "$T2_ID" ]]; then
    record "$SCEN" "create task #2 (with depends_on)" PASS "$HTTP_CODE" "id=$T2_ID"
  else
    record "$SCEN" "create task #2 (with depends_on)" FAIL "$HTTP_CODE" "$(echo "$T2_RESP" | head -c 200)"
  fi
else
  record "$SCEN" "create task #1 (with agent assignment)" SKIP 0 "no plan_id"
  record "$SCEN" "create task #2 (with depends_on)" SKIP 0 "no plan_id"
fi

# 4. List tasks for plan
if [[ -n "$PLAN_ID" ]]; then
  LIST_RESP=$(api GET "/api/plans/$PLAN_ID/tasks" "$TOKEN" "$WS")
  read_code
  COUNT=$(echo "$LIST_RESP" | jq -r 'if type=="array" then length else (.tasks // []) | length end')
  if [[ "$HTTP_CODE" == "200" && "$COUNT" -ge 2 ]]; then
    record "$SCEN" "list tasks by plan" PASS "$HTTP_CODE" "count=$COUNT"
  else
    record "$SCEN" "list tasks by plan" FAIL "$HTTP_CODE" "count=$COUNT body=$(echo "$LIST_RESP" | head -c 200)"
  fi
else
  record "$SCEN" "list tasks by plan" SKIP 0 "no plan_id"
fi

# 5. Get task detail
if [[ -n "${T1_ID:-}" ]]; then
  GET_RESP=$(api GET "/api/tasks/$T1_ID" "$TOKEN" "$WS")
  read_code
  STATUS=$(echo "$GET_RESP" | jq -r '.status // empty')
  if [[ "$HTTP_CODE" == "200" && -n "$STATUS" ]]; then
    record "$SCEN" "get task detail" PASS "$HTTP_CODE" "status=$STATUS"
  else
    record "$SCEN" "get task detail" FAIL "$HTTP_CODE" "$(echo "$GET_RESP" | head -c 200)"
  fi
else
  record "$SCEN" "get task detail" SKIP 0 "no task_id"
fi

# 6. List artifacts for task (expect empty array, not 404)
if [[ -n "${T1_ID:-}" ]]; then
  ART_RESP=$(api GET "/api/tasks/$T1_ID/artifacts" "$TOKEN" "$WS")
  read_code
  if [[ "$HTTP_CODE" == "200" ]]; then
    record "$SCEN" "list task artifacts" PASS "$HTTP_CODE" "endpoint reachable"
  else
    record "$SCEN" "list task artifacts" FAIL "$HTTP_CODE" "$(echo "$ART_RESP" | head -c 200)"
  fi
else
  record "$SCEN" "list task artifacts" SKIP 0 "no task_id"
fi

# 7. List task executions (expect empty, not 404)
if [[ -n "${T1_ID:-}" ]]; then
  EXEC_RESP=$(api GET "/api/tasks/$T1_ID/executions" "$TOKEN" "$WS")
  read_code
  if [[ "$HTTP_CODE" == "200" ]]; then
    record "$SCEN" "list task executions" PASS "$HTTP_CODE" "endpoint reachable"
  else
    record "$SCEN" "list task executions" FAIL "$HTTP_CODE" "$(echo "$EXEC_RESP" | head -c 200)"
  fi
else
  record "$SCEN" "list task executions" SKIP 0 "no task_id"
fi

# 8. Cancel task #2 (only PATCH path that's allowed)
if [[ -n "${T2_ID:-}" ]]; then
  CANCEL_BODY='{"status":"cancelled"}'
  CANCEL_RESP=$(api PATCH "/api/tasks/$T2_ID" "$TOKEN" "$WS" "$CANCEL_BODY")
  read_code
  NEW_STATUS=$(echo "$CANCEL_RESP" | jq -r '.status // empty')
  if [[ "$HTTP_CODE" == "200" && "$NEW_STATUS" == "cancelled" ]]; then
    record "$SCEN" "cancel task" PASS "$HTTP_CODE" "status=$NEW_STATUS"
  else
    record "$SCEN" "cancel task" FAIL "$HTTP_CODE" "got=$NEW_STATUS body=$(echo "$CANCEL_RESP" | head -c 200)"
  fi
else
  record "$SCEN" "cancel task" SKIP 0 "no task_id"
fi

# 9. Get project (round-trip)
if [[ -n "$PROJ_ID" ]]; then
  GP_RESP=$(api GET "/api/projects/$PROJ_ID" "$TOKEN" "$WS")
  read_code
  TITLE=$(echo "$GP_RESP" | jq -r '.title // empty')
  if [[ "$HTTP_CODE" == "200" && -n "$TITLE" ]]; then
    record "$SCEN" "get project (round-trip)" PASS "$HTTP_CODE" "title=$TITLE"
  else
    record "$SCEN" "get project (round-trip)" FAIL "$HTTP_CODE" "$(echo "$GP_RESP" | head -c 200)"
  fi
fi

echo "[$SCEN] done"
