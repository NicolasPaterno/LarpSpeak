#!/usr/bin/env bash
# F4 for audit-vault: a poison message on each of its queues goes to the -dlq after 5 receives
# while a real call is still recorded normally.
source "$(dirname "$0")/audit-lib.sh"

for q in audit-vault-call-events audio-chunks; do $AWS sqs purge-queue --queue-url "$(queue_url $q-dlq)" >/dev/null 2>&1 || true; done
step "F4: poison on audit-vault-call-events and audio-chunks"
$AWS sqs send-message --queue-url "$(queue_url audit-vault-call-events)" --message-body "not an envelope $RUN" >/dev/null
$AWS sqs send-message --queue-url "$(queue_url audio-chunks)" --message-body "not an envelope $RUN" >/dev/null

step "Meanwhile a real call is audited and recorded"
start_call poison 3 3; wait; probes_ok poison
id=$(call_id poison); wait_trail "$id" 5 30
f=$(wait_audio "$id" "$(user_of poison ana)" "$(sent poison a)" 30)
ok "call $id: trail ENDED with 5 events, recording of $(ogg_packets "$f") packets"

step "Receives of each poison message (one real log line per receive, any replica)"
eq=$(queue_url audit-vault-call-events-dlq); aq=$(queue_url audio-chunks-dlq)
mx=$($AWS sqs get-queue-attributes --queue-url "$(queue_url audit-vault-call-events)" --attribute-names RedrivePolicy --query Attributes.RedrivePolicy | jq -r .maxReceiveCount)
attempts() { # counts and last lines come from the same read of the logs
  local le la
  le=$(tail_logs audit-vault 'poison message left'); la=$(tail_logs audit-vault 'poison audio message')
  e=$($AWS sqs get-queue-attributes --queue-url "$eq" --attribute-names ApproximateNumberOfMessages --query Attributes.ApproximateNumberOfMessages)
  a=$($AWS sqs get-queue-attributes --queue-url "$aq" --attribute-names ApproximateNumberOfMessages --query Attributes.ApproximateNumberOfMessages)
  echo "events: attempt $(echo "$le" | grep -c . || true)/$mx DLQ=$e | audio: attempt $(echo "$la" | grep -c . || true)/$mx DLQ=$a"
  { echo "$le" | tail -1; echo "$la" | tail -1; } | grep . || true
}
EVERY=2 watch_until 300 attempts 'DLQ=[1-9].*DLQ=[1-9]' || true
e=$($AWS sqs get-queue-attributes --queue-url "$eq" --attribute-names ApproximateNumberOfMessages --query Attributes.ApproximateNumberOfMessages)
a=$($AWS sqs get-queue-attributes --queue-url "$aq" --attribute-names ApproximateNumberOfMessages --query Attributes.ApproximateNumberOfMessages)
step "Evidence"
[ "$e" -ge 1 ] && [ "$a" -ge 1 ] || fail "poison did not reach both DLQs"
for q in audit-vault-call-events:'poison message left' audio-chunks:'poison audio message'; do
  max=$($AWS sqs get-queue-attributes --queue-url "$(queue_url ${q%%:*})" --attribute-names RedrivePolicy --query Attributes.RedrivePolicy | jq -r .maxReceiveCount)
  tries=$(vault_logs "${q#*:}" | wc -l | tr -d ' ')
  kv log "${q%%:*}: $tries receives (maxReceiveCount=$max)"
  tail_logs audit-vault "${q#*:}" | awk '{printf "   %d/'"$max"' %s\n", NR, $0}'
  [ "$tries" = "$max" ] || fail "${q%%:*}: $tries receives, expected $max"
done
for q in audit-vault-call-events-dlq audio-chunks-dlq; do kv banco "$q: $($AWS sqs receive-message --queue-url "$(queue_url $q)" --attribute-names ApproximateReceiveCount --output json | jq -r '.Messages[0] | "body=\(.Body) ApproximateReceiveCount=\(.Attributes.ApproximateReceiveCount)"')"; done
pass "F4: poison moved to both DLQs after 5 receives, real call recorded meanwhile"
