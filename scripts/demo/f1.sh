#!/usr/bin/env bash
# F1 Compensação: dependência (alocação de relay) falhando em 100%.
# Esperado: passos anteriores desfeitos, recurso liberado, cliente notificado.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

step "F1: media-relay com RELAY_FAILURE_RATE=1.0"
relays 2 RELAY_FAILURE_RATE=1.0
ana=ana-$RUN

step "ana conecta nas notificações (SSE) e cria uma chamada"
sse=$(mktemp); curl -sN "$API/users/$ana/notifications" >"$sse" & sse_pid=$!
sleep 0.5
id=$(new_call "$ana" 0 "bob-$RUN"); info "callId=$id"
wait_status "$id" FAILED 15
kill $sse_pid 2>/dev/null || true; wait $sse_pid 2>/dev/null || true

step "Estado no DynamoDB (signaling-calls)"
curl -s "$API/calls/$id" | jq '{status, reason, participants, taken}'
[ -z "$($AWS dynamodb get-item --table-name signaling-users --key "{\"pk\":{\"S\":\"$ana\"}}")" ] \
  && ok "recurso liberado: ana não está mais presa a nenhuma chamada (signaling-users vazio)" \
  || fail "ana ainda presa à chamada"

step "Cliente notificado (SSE)"
grep -A1 call_failed "$sse" || fail "nenhum call_failed recebido"
ok "ana recebeu call_failed"

step "Logs"
logs media-relay "$id" | grep -E "simulated|failed" | head -3
logs call-signaling "$id" | grep -E "compensated" | head -3
ok "F1: compensação executada"
