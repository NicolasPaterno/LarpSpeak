#!/usr/bin/env bash
# Helpers for the audit-vault end-to-end scenarios (scripts/demo/audit-*.sh, `make test_audit`).
# Real calls only: bin/webrtc-probe logs in, joins over WebRTC like the browser and sends Opus.
source "$(dirname "$0")/lib.sh"

AUDIT=${AUDIT:-http://localhost:8083}
AUDIT_TOKEN=${AUDIT_API_TOKEN:-larpspeak-audit-demo}
BUCKET=larpspeak-audio
PROBE=bin/webrtc-probe
D=$(mktemp -d) # probe outputs

fail() { printf '\033[1;31mFAIL: %s\033[0m\n' "$*"; exit 1; }
pass() { printf '\033[1;32mPASS: %s\033[0m\n' "$*"; }

[ -x "$PROBE" ] || (cd tools/webrtc-probe && go build -o ../../bin/webrtc-probe .) || fail "could not build $PROBE (needs Go)"
require_stack

# audit PATH: GET on the audit API with the bearer token.
audit() { curl -s -H "Authorization: Bearer $AUDIT_TOKEN" "$AUDIT$1"; }

# start_call NAME SECONDS_A SECONDS_B: ana-NAME creates a call with bob-NAME, both send Opus.
# Runs in the background; the callId lands in $D/NAME.id. Finish with `wait`.
start_call() {
  local a="ana-$1-$RUN" b="bob-$1-$RUN"
  $PROBE -user "$a" -create "$b" -seconds "$2" >"$D/$1.a" 2>&1 &
  for _ in $(seq 1 50); do grep -q '^callId ' "$D/$1.a" && break; sleep 0.2; done
  awk '/^callId /{print $2}' "$D/$1.a" >"$D/$1.id"
  [ -s "$D/$1.id" ] || fail "probe $a did not create a call: $(cat "$D/$1.a")"
  $PROBE -user "$b" -call "$(cat "$D/$1.id")" -seconds "$3" >"$D/$1.b" 2>&1 &
}
call_id() { cat "$D/$1.id"; }
user_of() { echo "$2-$1-$RUN"; }                                   # user_of NAME ana|bob
sent() { sed -nE 's/.*done: ([0-9]+) Opus packets.*/\1/p' "$D/$1.$2"; } # sent NAME a|b
probes_ok() { # probes_ok NAME...: every probe finished its stream
  for n in "$@"; do for s in a b; do grep -q 'done:' "$D/$n.$s" || fail "probe $n.$s: $(tail -2 "$D/$n.$s")"; done; done
}

# wait_trail CALL EVENTS SECONDS: until the trail is ENDED with EVENTS events.
wait_trail() {
  for _ in $(seq 1 "$3"); do
    [ "$(audit "/audit/calls/$1" | jq -r '"\(.status) \(.eventCount)"')" = "ENDED $2" ] && return 0
    sleep 1
  done
  fail "trail of $1: $(audit "/audit/calls/$1" | jq -c '{status, eventCount, types: [.events[]?.type]}')"
}

# ogg_packets FILE: Opus packets (20 ms each) in an Ogg file, from the last granule position.
ogg_packets() {
  python3 - "$1" <<'EOF'
import struct, sys
b = open(sys.argv[1], 'rb').read()
assert b[:4] == b'OggS', 'not an Ogg file'
i = b.rfind(b'OggS')
print((struct.unpack_from('<q', b, i + 6)[0] - 312) // 960)
EOF
}

# wait_audio CALL USER PACKETS SECONDS: until the export of USER holds PACKETS packets; prints the file.
wait_audio() {
  local f="$D/$1-$2.opus" n=0
  for _ in $(seq 1 "$4"); do
    if [ "$(curl -s -o "$f" -w '%{http_code}' -H "Authorization: Bearer $AUDIT_TOKEN" "$AUDIT/audit/calls/$1/audio/$2")" = 200 ]; then
      n=$(ogg_packets "$f") && [ "$n" -ge "$3" ] && { echo "$f"; return 0; }
    fi
    sleep 1
  done
  fail "recording of $2 in $1: $n of $3 packets"
}

s3_keys() { $AWS s3api list-objects-v2 --bucket "$BUCKET" --prefix "audio/$1/" --query 'Contents[].Key' | tr '\t' '\n' | grep -v '^None$' || true; }

# replicas: container names of the running audit-vault replicas.
replicas() { docker compose ps --format '{{.Name}}' audit-vault; }
# vault_logs PATTERN: log lines (one per message) of every audit-vault replica since the scenario started, prefixed by container.
vault_logs() {
  for c in $(docker compose ps -aq audit-vault); do
    docker logs --since "$STARTED" "$c" 2>&1 | grep -E "$1" | sed "s/^ */$(docker inspect -f '{{.Name}}' "$c" | tr -d /) /" || true
  done
}
