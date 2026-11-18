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
  local voice # the voice-mode relay (media-relay-b) would compete for messages too
  voice=$(docker ps -aq --filter label=com.docker.compose.project=larpspeak --filter label=com.docker.compose.service=media-relay-b)
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
  step "Restaurando a configuração padrão"
  docker compose unpause ministack >/dev/null 2>&1 || true
  signaling
  docker compose rm -sf media-relay >/dev/null 2>&1 || true
  if [ -n "${RELAY_HOST:-}" ]; then
    docker compose -f docker-compose.yml -f compose.voice.yml up -d --no-deps media-relay media-relay-b >/dev/null 2>&1
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

# new_call CALLER [MAX] [INVITEE]: POST /calls, prints the callId.
new_call() {
  api "$1" POST /calls "{\"maxParticipants\":${2:-0},\"calleeIds\":[${3:+\"$3\"}]}" | jq -r .callId
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

# logs SERVICE PATTERN: matching log lines of every pod since the scenario started.
logs() { docker compose logs --no-log-prefix --since "$STARTED" "$1" 2>/dev/null | grep -E "$2" || true; }

queue_url() { $AWS sqs get-queue-url --queue-name "$1" --query QueueUrl; }

envelope() { # envelope TYPE CORRELATION PAYLOAD_JSON [MESSAGE_ID]
  jq -cn --arg id "${4:-$(uuidgen | tr A-Z a-z)}" --arg c "$2" --arg t "$1" --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --argjson p "$3" '{messageId:$id, correlationId:$c, type:$t, version:1, occurredAt:$at, payload:$p}'
}
