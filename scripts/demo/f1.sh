#!/usr/bin/env bash
# F1 Compensação: dependência (alocação de relay) falhando em 100%.
# Esperado: passos anteriores desfeitos, recurso liberado, cliente notificado.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

step "F1: media-relay com RELAY_FAILURE_RATE=1.0"
relays 2 RELAY_FAILURE_RATE=1.0
for c in $(docker compose ps -q media-relay); do info "$(pod "$c") RELAY_FAILURE_RATE=$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$c" | sed -n 's/^RELAY_FAILURE_RATE=//p')"; done
ana=ana-$RUN

step "ana conecta nas notificações (SSE) e cria uma chamada; passos da saga ao vivo"
sse=$(mktemp)
curl -sN -H "Authorization: Bearer $(token "$ana")" "$API/notifications" \
  > >(while IFS= read -r l; do [ -z "$l" ] || printf '%s %s\n' "$(ts)" "$l"; done >"$sse") &
sse_pid=$! # curl itself: killing it ends the timestamping loop
sleep 0.5
m=$(mark)
id=$(new_call "$ana" 0 "bob-$RUN")
follow "call-signaling media-relay" "$id" "$m"
wait_status "$id" FAILED 15
at "GET /calls/$(short "$id") -> status=FAILED"
sleep 1; unfollow
kill "$sse_pid" 2>/dev/null || true; wait "$sse_pid" 2>/dev/null || true; sleep 0.2

step "Evidência"
kv banco "signaling-calls: $(db signaling-calls "$id" status reason taken callerId)"
u=$(db signaling-users "$ana")
kv banco "signaling-users ($ana): $u"
kv log "$(logs media-relay "simulated.*$id" | wc -l | tr -d ' ') 'relay allocation failed (simulated)' | $(logs call-signaling "compensated.*$id" | wc -l | tr -d ' ') 'call compensated'"
tail_logs "media-relay call-signaling" "(simulated|failed|compensated).*$id"
kv cliente "SSE recebido por $ana (hora de chegada):"
grep -A1 'call_failed' "$sse" | sed 's/^/   │ /' || true
[ "$u" = "(nenhum item)" ] || fail "ana ainda presa à chamada"
ok "recurso liberado: ana não está mais presa a nenhuma chamada (signaling-users vazio)"
grep -q call_failed "$sse" || fail "nenhum call_failed recebido"
ok "ana recebeu call_failed"
[ -n "$(logs call-signaling "compensated.*$id")" ] || fail "sem log 'call compensated' para $id"
ok "F1: compensação executada"

step "Trilha no audit-vault (consumiu CallFailed de call-events)"
audit_failed "$id" "relay failed"
ok "audit-vault registrou a falha: trilha FAILED com o motivo"
