#!/usr/bin/env bash
# F6 Broker fora do ar: parar o MiniStack por 30 segundos.
# Esperado: serviços reconectam sozinhos, outbox drena, nenhuma mensagem perdida.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

signaling RELAY_TIMEOUT_MS=90000
relays 2 RELAY_LATENCY_MS=2000
step "F6: chamada criada; o relay vai responder com o broker fora do ar"
id=$(new_call "ana-$RUN" 0 "bob-$RUN"); info "callId=$id"
sleep 0.5
step "docker compose pause ministack (SQS, SNS, DynamoDB fora por 30s)"
docker compose pause ministack >/dev/null
code=$(curl -s -o /dev/null -w '%{http_code}' -XPOST "$API/calls" -d "{\"callerId\":\"carl-$RUN\"}")
info "POST /calls durante a queda: HTTP $code (falha rápida, cliente tenta depois)"
for s in 30 20 10; do info "${s}s..."; sleep 10; done
docker compose unpause ministack >/dev/null
step "Broker de volta"

wait_status "$id" ACTIVE 60
ok "a chamada criada antes da queda ficou ACTIVE: nenhuma mensagem perdida"
step "Logs de reconexão"
for s in call-signaling media-relay; do
  info "$s: $(logs $s 'retrying|retry' | wc -l | tr -d ' ') avisos de retry"; logs $s 'retrying|retry' | head -2 | cut -c1-180
done
sleep 3
step "Outbox drenado"
for t in signaling-outbox relay-outbox; do info "$t: $($AWS dynamodb scan --table-name $t --select COUNT --query Count) itens"; done
id2=$(new_call "dave-$RUN" 0 "eve-$RUN"); wait_status "$id2" ACTIVE 30
ok "F6: serviços reconectaram sozinhos e novas chamadas funcionam"
