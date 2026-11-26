#!/usr/bin/env bash
# F3 for audit-vault: the same real message (same messageId) delivered 5 times -> one effect.
# The vault is stopped while a real call runs, one CallStarted and one AudioChunk are taken
# from the queues as the producers wrote them, and each is sent 4 more times (5 deliveries, like f3.sh).
source "$(dirname "$0")/audit-lib.sh"
trap 'unfollow; docker compose up -d --no-deps audit-vault >/dev/null 2>&1' EXIT

step "F3: audit-vault stopped, real call with two probes (3s)"
docker compose stop audit-vault >/dev/null 2>&1
start_call dup 3 3; wait; probes_ok dup
id=$(call_id dup); info "callId=$id"
peek() { # peek QUEUE TYPE: body of a message of TYPE in QUEUE, every message left in the queue
  # Each message is received at most ONCE (long visibility while scanning, then made visible again):
  # every receive counts toward maxReceiveCount=5, so polling with visibility 0 would push the call's
  # other real messages into the DLQ.
  local url body="" receipts=() batch r
  url=$(queue_url "$1")
  for _ in $(seq 1 30); do
    batch=$($AWS sqs receive-message --queue-url "$url" --max-number-of-messages 10 --visibility-timeout 120 \
      --wait-time-seconds 1 --output json)
    if [ "$(echo "$batch" | jq '.Messages | length' 2>/dev/null || echo 0)" -eq 0 ] 2>/dev/null || [ -z "$batch" ]; then
      [ -n "$body" ] && break; continue
    fi
    while read -r r; do receipts+=("$r"); done < <(echo "$batch" | jq -r '.Messages[].ReceiptHandle')
    [ -z "$body" ] && body=$(echo "$batch" | jq -c '.Messages[].Body | fromjson? | select(.type=="'"$2"'" and .correlationId=="'"$id"'")' | head -1)
  done
  for r in "${receipts[@]}"; do
    $AWS sqs change-message-visibility --queue-url "$url" --receipt-handle "$r" --visibility-timeout 0 >/dev/null
  done
  [ -n "$body" ] || fail "no $2 of $id in $1"
  echo "$body"
}
ev=$(peek audit-vault-call-events CallStarted); ch=$(peek audio-chunks AudioChunk)
evid=$(echo "$ev" | jq -r .messageId); chid=$(echo "$ch" | jq -r .messageId)
step "Sending CallStarted $evid and AudioChunk $chid 4 more times each (5 deliveries)"
for i in 1 2 3 4; do
  at "resend $i: CallStarted SQS MessageId=$($AWS sqs send-message --queue-url "$(queue_url audit-vault-call-events)" --message-body "$ev" --query MessageId) | AudioChunk SQS MessageId=$($AWS sqs send-message --queue-url "$(queue_url audio-chunks)" --message-body "$ch" --query MessageId)"
done
at "starting audit-vault; its consumption of these two messageIds, live:"
follow audit-vault "($evid|$chid)"
docker compose up -d --no-deps audit-vault >/dev/null 2>&1
wait_trail "$id" 5 60
for _ in $(seq 1 60); do [ "$(vault_logs "duplicate ignored.*($evid|$chid)" | wc -l | tr -d ' ')" -ge 8 ] && break; sleep 1; done

unfollow
step "Evidence"
rec=$(vault_logs "audit recorded.*$evid" | wc -l | tr -d ' '); st=$(vault_logs "audio stored.*$chid" | wc -l | tr -d ' ')
dup=$(vault_logs "duplicate ignored.*($evid|$chid)" | wc -l | tr -d ' ')
n=$(audit "/audit/calls/$id" | jq '[.events[] | select(.messageId=="'"$evid"'")] | length')
objs=$(s3_keys "$id" | grep -c "$chid" || true)
kv banco "audit trail: $(audit "/audit/calls/$id" | jq -r '"\(.status) \(.eventCount) events"') | S3 objects with $(short "$chid"): $objs"
kv log "CallStarted: $rec recorded, $n in the trail | AudioChunk: $st stored, $objs S3 object | duplicates ignored: $dup"
[ "$rec" = 1 ] && [ "$n" = 1 ] && [ "$st" = 1 ] && [ "$objs" = 1 ] && [ "$dup" = 8 ] || fail "expected 1 effect each and 8 duplicates"
pass "F3: 5 deliveries of each real message, 1 effect, 'duplicate ignored' x8"
