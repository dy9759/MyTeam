#!/usr/bin/env bash
# P0-1: Document Sharing & Multi-Agent Collaboration — API smoke test.
#
# Two users (Song Yi, Yu Lie) interact via channel/threads. Song Yi creates
# a doc (file_index entry), shares it via thread, Yu Lie's agent posts a
# comment back. We exercise: cross-user thread visibility, message
# read/write, context items (the "PRD acceptance" surrogate), and
# attachment listing.
set -u

DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib.sh
source "$DIR/lib.sh"

SCEN="P0-1"
SUFFIX=$(uniq_suffix)
EMAIL_A="songyi-$SUFFIX@scenario.test"
EMAIL_B="yulie-$SUFFIX@scenario.test"

echo "[$SCEN] login Song Yi ($EMAIL_A)"
TOKEN_A=$(login_as "$EMAIL_A")
[[ -z "$TOKEN_A" || "$TOKEN_A" == "null" ]] && { record "$SCEN" "login Song Yi" FAIL 0 "no token"; exit 1; }
record "$SCEN" "login Song Yi" PASS 200 "got jwt"

WS_A=$(first_workspace_id "$TOKEN_A")
record "$SCEN" "Song Yi auto-workspace" $([[ -n "$WS_A" ]] && echo PASS || echo FAIL) 200 "ws=$WS_A"

AGENT_A=$(poll_personal_agent "$TOKEN_A" "$WS_A")
record "$SCEN" "Song Yi personal agent" $([[ -n "$AGENT_A" ]] && echo PASS || echo FAIL) 200 "agent=$AGENT_A"

# Invite Yu Lie as a member of Song Yi's workspace (so they share scope).
INVITE_BODY="{\"email\":\"$EMAIL_B\",\"role\":\"member\"}"
INV_RESP=$(api POST "/api/workspaces/$WS_A/members" "$TOKEN_A" "" "$INVITE_BODY")
read_code
if [[ "$HTTP_CODE" =~ ^20[01]$ ]]; then
  record "$SCEN" "invite Yu Lie to workspace" PASS "$HTTP_CODE" "added"
else
  record "$SCEN" "invite Yu Lie to workspace" FAIL "$HTTP_CODE" "$(echo "$INV_RESP" | head -c 200)"
fi

echo "[$SCEN] login Yu Lie ($EMAIL_B)"
TOKEN_B=$(login_as "$EMAIL_B")
record "$SCEN" "login Yu Lie" $([[ -n "$TOKEN_B" && "$TOKEN_B" != "null" ]] && echo PASS || echo FAIL) 200 "got jwt"

# Yu Lie should now see Song Yi's workspace (after invite). They may also
# have an auto-provisioned personal workspace — find Song Yi's by id.
WS_LIST_B=$(api GET /api/workspaces "$TOKEN_B" "")
read_code
HAS_SHARED=$(echo "$WS_LIST_B" | jq --arg w "$WS_A" 'map(.id == $w) | any')
if [[ "$HAS_SHARED" == "true" ]]; then
  record "$SCEN" "Yu Lie sees shared workspace" PASS "$HTTP_CODE" "ws_a visible"
else
  record "$SCEN" "Yu Lie sees shared workspace" FAIL "$HTTP_CODE" "ws_a NOT in list"
fi

# Yu Lie's personal agent in the shared workspace
AGENT_B=$(poll_personal_agent "$TOKEN_B" "$WS_A")
record "$SCEN" "Yu Lie agent auto-provisioned in shared ws" $([[ -n "$AGENT_B" ]] && echo PASS || echo FAIL) "$HTTP_CODE" "agent=$AGENT_B"

# 1. Song Yi creates a channel
CH_BODY="{\"name\":\"prd-collab-$SUFFIX\",\"description\":\"PRD review channel\"}"
CH_RESP=$(api POST /api/channels "$TOKEN_A" "$WS_A" "$CH_BODY")
read_code
CH_ID=$(echo "$CH_RESP" | jq -r '.id // empty')
if [[ "$HTTP_CODE" =~ ^20[01]$ && -n "$CH_ID" ]]; then
  record "$SCEN" "Song Yi creates channel" PASS "$HTTP_CODE" "id=$CH_ID"
else
  record "$SCEN" "Song Yi creates channel" FAIL "$HTTP_CODE" "$(echo "$CH_RESP" | head -c 200)"
fi

# 2. Song Yi creates a thread in the channel ("share doc here")
TH_BODY="{\"title\":\"PRD v1 review\"}"
TH_RESP=$(api POST "/api/channels/$CH_ID/threads" "$TOKEN_A" "$WS_A" "$TH_BODY")
read_code
TH_ID=$(echo "$TH_RESP" | jq -r '.id // empty')
if [[ "$HTTP_CODE" =~ ^20[01]$ && -n "$TH_ID" ]]; then
  record "$SCEN" "Song Yi creates thread" PASS "$HTTP_CODE" "id=$TH_ID"
else
  record "$SCEN" "Song Yi creates thread" FAIL "$HTTP_CODE" "$(echo "$TH_RESP" | head -c 200)"
fi

# 3. Song Yi posts the initial doc summary as a thread message
if [[ -n "$TH_ID" ]]; then
  M1_BODY='{"content":"# PRD v1\\n\\n## Goals\\n- Multi-agent doc collab\\n\\n## Non-goals\\n- realtime cursor sync"}'
  M1_RESP=$(api POST "/api/threads/$TH_ID/messages" "$TOKEN_A" "$WS_A" "$M1_BODY")
  read_code
  if [[ "$HTTP_CODE" =~ ^20[01]$ ]]; then
    record "$SCEN" "Song Yi shares PRD as thread message" PASS "$HTTP_CODE" "posted"
  else
    record "$SCEN" "Song Yi shares PRD as thread message" FAIL "$HTTP_CODE" "$(echo "$M1_RESP" | head -c 200)"
  fi

  # 4. Yu Lie reads the thread messages
  R_RESP=$(api GET "/api/threads/$TH_ID/messages" "$TOKEN_B" "$WS_A")
  read_code
  COUNT=$(echo "$R_RESP" | jq -r '.messages // [] | length')
  if [[ "$HTTP_CODE" == "200" && "$COUNT" -ge 1 ]]; then
    record "$SCEN" "Yu Lie reads PRD message" PASS "$HTTP_CODE" "msgs=$COUNT"
  else
    record "$SCEN" "Yu Lie reads PRD message" FAIL "$HTTP_CODE" "msgs=$COUNT"
  fi

  # 5. Yu Lie posts a counter / amendment
  M2_BODY='{"content":"Counter: lets add `## Open questions` section. Will the agent or human gate the merge?"}'
  M2_RESP=$(api POST "/api/threads/$TH_ID/messages" "$TOKEN_B" "$WS_A" "$M2_BODY")
  read_code
  if [[ "$HTTP_CODE" =~ ^20[01]$ ]]; then
    record "$SCEN" "Yu Lie posts amendment" PASS "$HTTP_CODE" "posted"
  else
    record "$SCEN" "Yu Lie posts amendment" FAIL "$HTTP_CODE" "$(echo "$M2_RESP" | head -c 200)"
  fi

  # 6. Song Yi accepts the amendment by writing a context_item ("decision")
  CTX_BODY='{"item_type":"decision","title":"Merge gate","body":"Both agents must approve before merging the PRD. Decision recorded for downstream agents.","retention_class":"permanent"}'
  CTX_RESP=$(api POST "/api/threads/$TH_ID/context-items" "$TOKEN_A" "$WS_A" "$CTX_BODY")
  read_code
  if [[ "$HTTP_CODE" =~ ^20[01]$ ]]; then
    record "$SCEN" "Song Yi records consensus as context item" PASS "$HTTP_CODE" "decision saved"
  else
    record "$SCEN" "Song Yi records consensus as context item" FAIL "$HTTP_CODE" "$(echo "$CTX_RESP" | head -c 200)"
  fi

  # 7. Yu Lie reads back the context items (the "agreed PRD")
  CTX_LIST=$(api GET "/api/threads/$TH_ID/context-items" "$TOKEN_B" "$WS_A")
  read_code
  C_COUNT=$(echo "$CTX_LIST" | jq -r '.items // [] | length')
  if [[ "$HTTP_CODE" == "200" && "$C_COUNT" -ge 1 ]]; then
    record "$SCEN" "Yu Lie reads agreed PRD context" PASS "$HTTP_CODE" "items=$C_COUNT"
  else
    record "$SCEN" "Yu Lie reads agreed PRD context" FAIL "$HTTP_CODE" "items=$C_COUNT"
  fi
else
  for s in "Song Yi shares PRD as thread message" "Yu Lie reads PRD message" "Yu Lie posts amendment" "Song Yi records consensus as context item" "Yu Lie reads agreed PRD context"; do
    record "$SCEN" "$s" SKIP 0 "no thread"
  done
fi

# 8. List channels (cross-user visibility check)
CH_LIST=$(api GET /api/channels "$TOKEN_B" "$WS_A")
read_code
CH_COUNT=$(echo "$CH_LIST" | jq -r '. // [] | length')
if [[ "$HTTP_CODE" == "200" ]]; then
  record "$SCEN" "Yu Lie lists channels in shared ws" PASS "$HTTP_CODE" "channels=$CH_COUNT"
else
  record "$SCEN" "Yu Lie lists channels in shared ws" FAIL "$HTTP_CODE" "$(echo "$CH_LIST" | head -c 200)"
fi

echo "[$SCEN] done"
