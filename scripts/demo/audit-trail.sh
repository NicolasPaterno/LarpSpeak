#!/usr/bin/env bash
# Audit trail of a real call: CallStarted, ParticipantJoined, ParticipantLeft x2, CallEnded,
# status ENDED, nobody present, every event's stored envelope verified.
source "$(dirname "$0")/audit-lib.sh"

step "Call with two probes (3s)"
start_call trail 3 3; wait; probes_ok trail
id=$(call_id trail); info "callId=$id"
wait_trail "$id" 5 30
t=$(audit "/audit/calls/$id")
step "Evidence"
kv banco "audit trail: $(echo "$t" | jq -r '"status=\(.status) present=\(.present | length) events=\(.eventCount)"')"
echo "$t" | jq -r '.events[] | "   │ \(.occurredAt // "") \(.type) verified=\(.verified) messageId=\(.messageId[0:8])"'
kv log "producer (call-signaling) and consumer (audit-vault) of the same call:"
tail_logs "call-signaling audit-vault" "(call active|participant joined|participant left|call ended|audit recorded).*$id"
types=$(echo "$t" | jq -r '[.events[].type] | sort | join(",")')
[ "$types" = "CallEnded,CallStarted,ParticipantJoined,ParticipantLeft,ParticipantLeft" ] || fail "event types: $types"
[ "$(echo "$t" | jq -r .status)" = ENDED ] || fail "status $(echo "$t" | jq -r .status)"
[ "$(echo "$t" | jq '[.events[] | select(.verified != true)] | length')" = 0 ] || fail "unverified events"
[ "$(echo "$t" | jq '.present | length')" = 0 ] || fail "present after the end: $(echo "$t" | jq -c .present)"
[ "$(echo "$t" | jq '[.events[].messageId] | unique | length')" = 5 ] || fail "duplicate messageIds"
pass "trail: 5 events from the real call, ENDED, all verified, present=[]"
