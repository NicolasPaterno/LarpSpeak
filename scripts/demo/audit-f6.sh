#!/usr/bin/env bash
# F6 for audit-vault: MiniStack paused for 30s during a call. Expected: "broker unreachable,
# retrying", no restart, and the call's events and audio are stored after the broker returns.
source "$(dirname "$0")/audit-lib.sh"
trap 'unfollow; docker compose unpause ministack >/dev/null 2>&1 || true' EXIT

before=$(for c in $(replicas); do docker inspect -f '{{.Name}} {{.State.StartedAt}} {{.RestartCount}}' "$c"; done)
step "F6: call with two probes, 45s of Opus each"
start_call f6 45 45
sleep 6
step "docker compose pause ministack (SQS, SNS, DynamoDB, S3 down for 30s)"
down=$(mark)
docker compose pause ministack >/dev/null
at "ministack: $(docker inspect -f '{{.State.Status}}' "$(docker compose ps -q ministack)")"
retries() { # 'broker unreachable, retrying' per replica since the pause, from the real logs
  for c in $(replicas); do printf '%s=%s ' "${c#larpspeak-}" "$(docker logs --since "$down" "$c" 2>&1 | grep -c 'broker unreachable, retrying' || true)"; done
}
p0=$SECONDS
while [ $((SECONDS - p0)) -lt 30 ]; do at "broker paused for $((SECONDS - p0))s | retries: $(retries)"; sleep 5; done
at "broker paused for $((SECONDS - p0))s | retries: $(retries)"
docker compose unpause ministack >/dev/null
up=$(mark)
at "docker compose unpause ministack: $(docker inspect -f '{{.State.Status}}' "$(docker compose ps -q ministack)")"
step "Broker back: what the vault stores after the outage (per replica, live)"
wait; probes_ok f6
id=$(call_id f6)
after_up() { for c in $(replicas); do printf '%s=%s ' "${c#larpspeak-}" "$(docker logs --since "$up" "$c" 2>&1 | grep -cE "(audit recorded|audio stored).*$id" || true)"; done
  echo "| trail: $(audit "/audit/calls/$id" | jq -r '"\(.status) \(.eventCount)"')"; }
watch_until 90 after_up 'trail: ENDED 5$' || true
wait_trail "$id" 5 90
for w in ana bob; do
  want=$(sent f6 $([ $w = ana ] && echo a || echo b)); f=$(wait_audio "$id" "$(user_of f6 $w)" $((want * 98 / 100)) 90)
  kv banco "$w: $(ogg_packets "$f") of $want packets recorded"
done
step "Evidence"
kv log "first and last retry, then the first message stored after the unpause:"
tail_logs audit-vault 'broker unreachable, retrying' "$down" | sed -n '1p;$p'
tail_logs audit-vault "(audit recorded|audio stored).*$id" "$up" | head -1
for c in $(replicas); do
  n=$(docker logs --since "$STARTED" "$c" 2>&1 | grep -c 'broker unreachable, retrying' || true)
  kv log "$c: $n retries"; [ "$n" -ge 1 ] || fail "$c logged no broker retry"
done
kv banco "audit trail $(short "$id"): $(audit "/audit/calls/$id" | jq -r '"\(.status) \(.eventCount) events"'), $(s3_keys "$id" | wc -l | tr -d ' ') S3 objects"
after=$(for c in $(replicas); do docker inspect -f '{{.Name}} {{.State.StartedAt}} {{.RestartCount}}' "$c"; done)
[ "$before" = "$after" ] || fail "a replica restarted: $before -> $after"
for c in $(replicas); do kv docker "$(pod "$c")"; done
pass "F6: vault retried through the outage without restarting; trail ENDED and audio stored after unpause"
