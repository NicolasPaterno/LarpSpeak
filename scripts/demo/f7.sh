#!/usr/bin/env bash
# F7 Resposta tardia: latência da dependência maior que o tempo de expiração.
# Esperado: compensação correta do resultado que chegou atrasado.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

step "F7: RELAY_TIMEOUT_MS=3000 no call-signaling, RELAY_LATENCY_MS=6000 no media-relay"
signaling RELAY_TIMEOUT_MS=3000
relays 2 RELAY_LATENCY_MS=6000
step "Linha do tempo ao vivo: timeout aos 3s, resposta do relay aos 6s"
m=$(mark)
id=$(new_call "ana-$RUN" 0 "bob-$RUN")
follow "call-signaling media-relay" "$id" "$m"
wait_status "$id" FAILED 15
at "GET /calls/$(short "$id") -> status=FAILED reason=$(field "$id" reason)"
ok "saga expirou e compensou a chamada"

info "aguardando a resposta atrasada do relay..."
sess() { echo "relay-sessions: $(db relay-sessions "$id" state reason owner)"; }
EVERY=0.5 watch_until 30 sess 'state=RELEASED' || true
sleep 1; unfollow

step "Evidência"
st=$($AWS dynamodb get-item --table-name relay-sessions --key "{\"pk\":{\"S\":\"$id\"}}" --query '[Item.state.S, Item.reason.S]')
kv banco "signaling-calls: $(db signaling-calls "$id" status reason)"
kv banco "relay-sessions: $(db relay-sessions "$id" state reason)"
kv log "a chamada expirou antes, o RelayAllocated chegou depois e foi compensado:"
tail_logs "call-signaling media-relay" "(compensated|late relay released|relay released|relay allocated).*$id"
[[ "$st" == RELEASED*late* ]] || fail "relay atrasado não foi liberado"
ok "F7: RelayAllocated atrasado gerou ReleaseRelayRequested e o relay foi liberado"

step "Trilha no audit-vault: a resposta atrasada não reviveu a chamada"
audit_failed "$id" "relay timeout"
ok "audit-vault: trilha FAILED por timeout, sem CallStarted"
