#!/usr/bin/env bash
# F8 for audit-vault: both replicas consume both queues (competing consumers), no duplicate effects.
source "$(dirname "$0")/audit-lib.sh"

[ "$(replicas | wc -l | tr -d ' ')" -ge 2 ] || fail "needs 2 audit-vault replicas"
step "F8: 4 simultaneous calls, two probes each (6s)"
for n in s1 s2 s3 s4; do start_call $n 6 6; done
pat=$(for n in s1 s2 s3 s4; do call_id $n; done | paste -sd'|' -)
step "Messages stored per replica (live, from the real logs)"
tally() {
  for c in $(replicas); do
    printf '%s: events=%s audio=%s  ' "${c#larpspeak-}" \
      "$(docker logs --since "$STARTED" "$c" 2>&1 | grep -cE "audit recorded.*($pat)" || true)" \
      "$(docker logs --since "$STARTED" "$c" 2>&1 | grep -cE "audio stored.*($pat)" || true)"
  done
}
watch_until 12 tally 'NEVER' || true # 12s: the probes stream 6s
wait; probes_ok s1 s2 s3 s4
for n in s1 s2 s3 s4; do
  wait_trail "$(call_id $n)" 5 60
  for w in ana bob; do out=$(wait_audio "$(call_id $n)" "$(user_of $n $w)" "$(sent $n $([ $w = ana ] && echo a || echo b))" 30) || fail "$out"; done
done
at "after the trails ENDED: $(tally)"
step "Evidence"
for c in $(replicas); do
  e=$(docker logs --since "$STARTED" "$c" 2>&1 | grep -E "audit recorded.*($pat)" | wc -l | tr -d ' ')
  a=$(docker logs --since "$STARTED" "$c" 2>&1 | grep -E "audio stored.*($pat)" | wc -l | tr -d ' ')
  kv log "$c: $e events, $a audio chunks"; [ $((e + a)) -ge 1 ] || fail "$c consumed nothing"
done
dup=$(vault_logs "(audit recorded|audio stored).*($pat)" | grep -oE 'messageId=[0-9a-f-]+' | sort | uniq -d)
[ -z "$dup" ] || fail "stored twice: $dup"
total=$(vault_logs "(audit recorded|audio stored).*($pat)" | wc -l | tr -d ' ')
objs=$(for n in s1 s2 s3 s4; do s3_keys "$(call_id $n)"; done | wc -l | tr -d ' ')
kv banco "$total messages stored once each: 20 events + $objs chunk objects in S3"
tail_logs audit-vault "(audit recorded|audio stored).*$(call_id s1)" | head -4
[ "$total" = $((20 + objs)) ] || fail "stored $total, expected $((20 + objs))"
pass "F8: both replicas consumed, every message stored exactly once"
