#!/usr/bin/env bash
# F2 for audit-vault: docker kill one of the two replicas while call events and audio chunks flow.
# Expected: whatever the dead replica held comes back after the visibility timeout (30s) and the
# survivor stores it; every event and every Opus packet is there exactly once.
source "$(dirname "$0")/audit-lib.sh"
trap 'unfollow; docker compose up -d --no-deps audit-vault >/dev/null 2>&1' EXIT

[ "$(replicas | wc -l | tr -d ' ')" -ge 2 ] || fail "needs 2 audit-vault replicas (docker compose up -d)"
step "F2: 3 calls with two probes each, 12s of Opus per probe"
for n in k1 k2 k3; do start_call $n 12 12; done
pat="$(call_id k1)|$(call_id k2)|$(call_id k3)"
sleep 6
victim=$(for c in $(replicas); do
  docker logs --since "$STARTED" "$c" 2>&1 | grep -E "(audit recorded|audio stored).*($pat)" >/dev/null && echo "$c"
done | head -1) || true
[ -n "$victim" ] || fail "no replica was storing audio of these calls"
late_since=$(( $(date +%s) + 20 )) # unix time: portable (GNU and BSD date)
stored() { # stored: messages of these calls stored per replica, from the real logs
  for c in $(docker compose ps -aq audit-vault); do
    printf '%s=%s ' "$(docker inspect -f '{{.Name}}' "$c" | sed 's#^/larpspeak-##')" \
      "$(docker logs --since "$STARTED" "$c" 2>&1 | grep -cE "(audit recorded|audio stored).*($pat)" || true)"
  done
}
at "stored so far: $(stored)"
step "$victim is consuming: docker kill"
killed=$(mark); docker kill "$victim" >/dev/null
at "docker kill $victim (SIGKILL)"; info "$(pod "$victim")"
survivor=$(replicas | grep -v "$victim" | head -1)
AUDIT=http://localhost:$(docker port "$survivor" 8080 | head -1 | cut -d: -f2) # the killed replica's API is gone too
sleep 2
at "starting the consumer again: docker start $victim"
docker start "$victim" >/dev/null
health() { echo "$victim: $(docker inspect -f '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' "$victim")"; }
EVERY=0.5 watch_until 60 health 'running healthy$' || fail "$victim did not come back"
info "$(pod "$victim")"
wait; probes_ok k1 k2 k3
at "probes done; waiting for the visibility timeout (30s) and the redeliveries (per replica, live):"
trails() { echo "stored: $(stored)| trails ENDED: $(for n in k1 k2 k3; do audit "/audit/calls/$(call_id $n)" | jq -r .status; done | grep -c ENDED)/3"; }
watch_until 90 trails 'ENDED: 3/3' || true

for n in k1 k2 k3; do
  id=$(call_id $n); wait_trail "$id" 5 90
  for w in ana bob; do
    want=$(sent $n $([ $w = ana ] && echo a || echo b)); f=$(wait_audio "$id" "$(user_of $n $w)" "$want" 90)
    got=$(ogg_packets "$f"); [ "$got" = "$want" ] || fail "$n/$w: $got packets stored, $want sent"
  done
  ids=$(audit "/audit/calls/$id" | jq -r '.events[].messageId')
  [ "$(echo "$ids" | sort -u | wc -l | tr -d ' ')" = 5 ] || fail "duplicate events in $id"
  keys=$(s3_keys "$id"); uniq=$(echo "$keys" | sed -E 's/.*-([0-9a-f-]{36})\.bin/\1/' | sort -u | wc -l | tr -d ' ')
  [ "$(echo "$keys" | wc -l | tr -d ' ')" = "$uniq" ] || fail "duplicate audio objects in $id"
  info "$id: 5 events, $(echo "$keys" | wc -l | tr -d ' ') chunks, every packet sent is in the export"
done
step "Evidence"
kv log "$victim before the kill: $(docker logs --since "$STARTED" --until "$killed" "$victim" 2>&1 | grep -cE "(audit recorded|audio stored).*($pat)") messages stored"
late=$(for c in $(replicas); do docker logs -t --since "$late_since" "$c" 2>&1; done | grep -cE "(audit recorded|audio stored).*($pat)" || true)
kv log "stored >20s after the kill (redeliveries of what $victim held): $late"
tail_logs audit-vault "(audit recorded|audio stored).*($pat)" "$late_since" | head -5
for n in k1 k2 k3; do kv banco "audit trail $(short "$(call_id $n)"): $(audit "/audit/calls/$(call_id $n)" | jq -r '"\(.status) \(.eventCount) events"'), $(s3_keys "$(call_id $n)" | wc -l | tr -d ' ') S3 objects"; done
pass "F2: replica killed mid-stream, 3 calls x 5 events and every Opus packet stored once"
