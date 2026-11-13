#!/usr/bin/env bash
# F7 Resposta tardia: latência da dependência maior que o tempo de expiração.
# Esperado: compensação correta do resultado que chegou atrasado.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

step "F7: RELAY_TIMEOUT_MS=3000 no call-signaling, RELAY_LATENCY_MS=6000 no media-relay"
signaling RELAY_TIMEOUT_MS=3000
relays 2 RELAY_LATENCY_MS=6000
id=$(new_call "ana-$RUN" 0 "bob-$RUN"); info "callId=$id"
wait_status "$id" FAILED 15
curl -s "$API/calls/$id" | jq '{status, reason}'
ok "saga expirou e compensou a chamada"

step "Aguardando a resposta atrasada do relay..."
for _ in $(seq 1 60); do
  st=$($AWS dynamodb get-item --table-name relay-sessions --key "{\"pk\":{\"S\":\"$id\"}}" --query '[Item.state.S, Item.reason.S]')
  [[ "$st" == RELEASED* ]] && break; sleep 0.5
done
info "relay-sessions: $st"
logs call-signaling "late relay released.*$id" | cut -c1-200
logs media-relay "relay released.*$id" | cut -c1-200
[[ "$st" == RELEASED*late* ]] || fail "relay atrasado não foi liberado"
ok "F7: RelayAllocated atrasado gerou ReleaseRelayRequested e o relay foi liberado"
