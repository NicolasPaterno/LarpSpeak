#!/usr/bin/env bash
# Shared helpers for the failure scenarios F1-F8 (run them with `make test_caseN`).
# Each scenario changes the configuration it needs and restores the default on exit.
set -euo pipefail
cd "$(dirname "$0")/../.."

API=${API:-http://localhost:8081}
AWS=scripts/aws.sh
RUN=$RANDOM$RANDOM          # suffix that keeps user ids unique between runs
STARTED=$(date -u +%Y-%m-%dT%H:%M:%SZ)
export COMPOSE_IGNORE_ORPHANS=True

step() { printf '\n\033[1;36m== %s\033[0m\n' "$*"; }
info() { printf '   %s\n' "$*"; }
ok()   { printf '\033[1;32m✔ %s\033[0m\n' "$*"; }
fail() { printf '\033[1;31m✘ %s\033[0m\n' "$*"; exit 1; }

require_stack() {
  curl -sf "$API/health" >/dev/null || fail "call-signaling fora do ar: rode 'make start' antes"
}

# relays N [VAR=value ...]: run N media-relay pods (Docker network only) with extra env.
relays() {
  local n=$1; shift
  local voice # the voice-mode relay (media-relay-2) would compete for messages too
  voice=$(docker ps -aq --filter label=com.docker.compose.project=larpspeak --filter label=com.docker.compose.service=media-relay-2)
  [ -z "$voice" ] || docker rm -f $voice >/dev/null
  docker compose rm -sf media-relay >/dev/null 2>&1 || true
  env "$@" docker compose up -d --no-deps --scale "media-relay=$n" media-relay >/dev/null 2>&1
  sleep 5 # pods register, elect a leader and start consuming
}

# signaling [VAR=value ...]: recreate call-signaling with extra env.
signaling() {
  env "$@" docker compose up -d --no-deps call-signaling >/dev/null 2>&1
  until curl -sf "$API/health" >/dev/null; do sleep 0.5; done
}

# restore: back to what `make start` runs (voice mode when RELAY_HOST is set).
restore() {
  unfollow
  step "Restaurando a configuração padrão"
  docker compose unpause ministack >/dev/null 2>&1 || true
  signaling
  docker compose rm -sf media-relay >/dev/null 2>&1 || true
  if [ -n "${RELAY_HOST:-}" ]; then
    docker compose -f docker-compose.yml -f compose.voice.yml up -d --no-deps media-relay media-relay-2 >/dev/null 2>&1
  else
    docker compose up -d --no-deps media-relay >/dev/null 2>&1
  fi
  sleep 5 # relays register and start consuming before the next scenario
  ok "pronto"
}

# Real login (only call-signaling creates tokens). Users are registered on first use.
PW=${LARPSPEAK_PASSWORD:-larpspeak-demo}
TOKENS=$(mktemp -d)
# token USER: prints a Bearer token for USER (registers + logs in; cached for 4 min, tokens live 5).
token() {
  if [ -z "$(find "$TOKENS/$1" -mmin -4 2>/dev/null)" ]; then
    curl -s -o /dev/null -XPOST "$API/auth/register" -d "{\"username\":\"$1\",\"password\":\"$PW\"}"
    curl -s -XPOST "$API/auth/login" -d "{\"username\":\"$1\",\"password\":\"$PW\"}" | jq -r '.accessToken // empty' >"$TOKENS/$1"
  fi
  cat "$TOKENS/$1"
}
# api USER METHOD PATH [BODY]: authenticated request as USER.
api() { curl -s -H "Authorization: Bearer $(token "$1")" -X"$2" "$API$3" ${4:+-d "$4"}; }

# new_call CALLER [MAX] [INVITEE]: POST /calls, asserts 202 Accepted (R3), prints the callId.
new_call() {
  local r code id
  r=$(curl -s -w '\n%{http_code}' -H "Authorization: Bearer $(token "$1")" -XPOST "$API/calls" \
    -d "{\"maxParticipants\":${2:-0},\"calleeIds\":[${3:+\"$3\"}]}")
  code=${r##*$'\n'}; id=$(echo "${r%$'\n'*}" | jq -r .callId)
  [ "$code" = 202 ] || fail "POST /calls -> HTTP $code (esperado 202): ${r%$'\n'*}" >&2
  info "POST /calls -> 202 Accepted, callId=$id" >&2
  echo "$id"
}

# call_json CALL: GET /calls/{id} as an observer account.
call_json() { api "observer-$RUN" GET "/calls/$1"; }
field() { call_json "$1" | jq -r ".$2 // empty"; }

# wait_status CALL STATUS SECONDS
wait_status() {
  for _ in $(seq 1 $(($3 * 5))); do
    [ "$(field "$1" status)" = "$2" ] && return 0
    sleep 0.2
  done
  fail "chamada $1 não chegou em $2 (está $(field "$1" status))"
}

# --- live evidence: real timestamps, real log lines, real table state ---------------------------
# ts: local wall clock with milliseconds (HH:MM:SS.mmm), the same clock fmt_logs prints.
ts() { perl -MTime::HiRes=time -MPOSIX=strftime -e '$t=time; printf "%s.%03d\n", strftime("%H:%M:%S", localtime $t), ($t-int $t)*1000'; }
# mark: current instant in UTC with milliseconds, for `--since` (log lines from this point on).
mark() { perl -MTime::HiRes=time -MPOSIX=strftime -e '$t=time; printf "%s.%03dZ\n", strftime("%Y-%m-%dT%H:%M:%S", gmtime $t), ($t-int $t)*1000'; }
at() { printf '   [%s] %s\n' "$(ts)" "$*"; }    # at TEXT: info line stamped with the current time
kv() { printf '   %-9s %s\n' "$1:" "$2"; }       # kv LABEL TEXT: one line of an evidence block
short() { echo "${1:0:8}"; }                    # short UUID: first 8 characters, as fmt_logs prints them

JQ_SHORT='def short: tostring | gsub("(?<a>[0-9a-f]{8})-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"; .a) | .[0:90];
  def loc: capture("^(?<s>[0-9T:-]{19})\\.?(?<f>[0-9]*)Z") | "\(.s + "Z" | fromdate | strflocaltime("%H:%M:%S")).\(.f + "000" | .[0:3])";'
# fmt_logs: `docker compose logs -t` lines -> "   │ HH:MM:SS.mmm container [LEVEL] msg key=value ...".
# Go services log JSON (msg + attributes); audit-vault logs text (kept verbatim). UUIDs cut to 8 chars.
fmt_logs() {
  jq -Rr --unbuffered "$JQ_SHORT"'
    (capture("^(?<c>[^ |]+) *\\| (?<s>[0-9T:-]{19})\\.?(?<f>[0-9]*)Z (?<m>.*)$")? // empty) as $p
    | "   │ \("\($p.s).\($p.f)Z" | loc) \($p.c) "
      + ((try ($p.m | fromjson) catch null) as $l
         | if ($l | type) == "object" then
             (if $l.level == "INFO" then "" else "\($l.level) " end) + $l.msg
             + ([$l | to_entries[] | select(.key | IN("time", "level", "msg", "service", "replica") | not)
                 | " \(.key)=\(.value | short)"] | join(""))
           else $p.m | sub("^ +"; "") | short end)'
}
# tail_logs "SERVICES" PATTERN [SINCE]: log lines matching PATTERN of every pod of SERVICES since SINCE
# (default: scenario start), formatted and in time order.
tail_logs() {
  # shellcheck disable=SC2086 # SERVICES is a list
  docker compose logs -t --no-color --since "${3:-$STARTED}" $1 2>/dev/null | grep -E "$2" | fmt_logs | sort -s -k2,2 || true
}
# follow "SERVICES" PATTERN [SINCE]: print matching log lines live (background) from SINCE (default: now)
# until unfollow.
FOLLOW=()
follow() {
  # shellcheck disable=SC2086
  docker compose logs -f -t --no-color --since "${3:-$(mark)}" $1 2>/dev/null > >(grep --line-buffered -E "$2" | fmt_logs) &
  FOLLOW+=($!)
}
unfollow() { if [ ${#FOLLOW[@]} -gt 0 ]; then sleep 1; kill "${FOLLOW[@]}" 2>/dev/null || true; FOLLOW=(); fi; }
trap unfollow EXIT # scripts that set their own EXIT trap call unfollow in it (restore does)

# watch_until SECONDS STATE_FN DONE_REGEX: run STATE_FN every ${EVERY:-1}s and print each new state
# with its timestamp (nothing is printed while the state stays the same); 0 once it matches DONE_REGEX.
watch_until() {
  local end=$((SECONDS + $1)) s last=
  while [ $SECONDS -lt $end ]; do
    s=$($2 2>/dev/null) || true
    [ "$s" = "$last" ] || { at "$s"; last=$s; }
    [[ $s =~ $3 ]] && return 0
    sleep "${EVERY:-1}"
  done
  return 1
}

# db TABLE PK [FIELD...]: one DynamoDB item read straight from the table, as "key=value ..." (only FIELDs if given).
db() {
  local t=$1 k=$2; shift 2
  $AWS dynamodb get-item --table-name "$t" --key "{\"pk\":{\"S\":\"$k\"}}" --output json |
    jq -rs --args "$JQ_SHORT"'.[0].Item // null | if . == null then "(nenhum item)" else
      to_entries | map(select($ARGS.positional == [] or (.key | IN($ARGS.positional[]))))
      | map("\(.key)=\(.value | .S // .N // ((.L // []) | map(.S // .N) | join(",")) | short)") | join(" ") end' "$@"
}
# count TABLE: items in a DynamoDB table (full scan).
count() { $AWS dynamodb scan --table-name "$1" --select COUNT --query Count; }
# depth QUEUE_URL: "visible=N inflight=M" straight from SQS.
depth() {
  $AWS sqs get-queue-attributes --queue-url "$1" --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible \
    --output json | jq -r '.Attributes | "visible=\(.ApproximateNumberOfMessages) inflight=\(.ApproximateNumberOfMessagesNotVisible)"'
}
# pod CONTAINER: "name status started=HH:MM:SS.mmm [finished=... exit=N] restarts=N" from docker inspect.
pod() {
  docker inspect "$1" | jq -r "$JQ_SHORT"'.[0] | "\(.Name | ltrimstr("/")) \(.State.Status) started=\(.State.StartedAt | loc)"
    + (if .State.Running then "" else " finished=\(.State.FinishedAt | loc) exit=\(.State.ExitCode)" end) + " restarts=\(.RestartCount)"'
}

# logs SERVICE PATTERN: matching log lines of every pod since the scenario started.
logs() { docker compose logs --no-log-prefix --since "$STARTED" "$1" 2>/dev/null | grep -E "$2" || true; }

queue_url() { $AWS sqs get-queue-url --queue-name "$1" --query QueueUrl; }

envelope() { # envelope TYPE CORRELATION PAYLOAD_JSON [MESSAGE_ID]
  jq -cn --arg id "${4:-$(uuidgen | tr A-Z a-z)}" --arg c "$2" --arg t "$1" --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --argjson p "$3" '{messageId:$id, correlationId:$c, type:$t, version:1, occurredAt:$at, payload:$p}'
}

# audit_failed CALL REASON_REGEX: audit-vault consumed CallFailed from call-events. The trail is
# FAILED with the reason, holds only that event (CallStarted never happened) and the event is verified.
audit_failed() {
  local url=${AUDIT:-http://localhost:8083} tok=${AUDIT_API_TOKEN:-larpspeak-audit-demo} t=
  for _ in $(seq 1 20); do # async: outbox -> SNS -> SQS -> consumer
    t=$(curl -s -H "Authorization: Bearer $tok" "$url/audit/calls/$1")
    [ "$(echo "$t" | jq -r '.status // empty' 2>/dev/null)" = FAILED ] && break; sleep 1
  done
  echo "$t" | jq -c '{status, reason, events: [.events[]? | {type, verified}]}' 2>/dev/null | sed 's/^/   /' || info "$t"
  [ "$(echo "$t" | jq -r '.status // empty' 2>/dev/null)" = FAILED ] || fail "trilha do audit-vault não chegou em FAILED"
  echo "$t" | jq -e --arg r "$2" '.reason | test($r)' >/dev/null || fail "motivo na trilha: $(echo "$t" | jq -r .reason)"
  [ "$(echo "$t" | jq -r '[.events[].type] | join(",")')" = CallFailed ] || fail "eventos na trilha: $(echo "$t" | jq -c '[.events[].type]')"
  [ "$(echo "$t" | jq -r '.events[0].verified')" = true ] || fail "evento da trilha não verificado"
}
