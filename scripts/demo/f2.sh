#!/usr/bin/env bash
# F2 Serviço morre no meio: docker kill no consumidor durante o processamento.
# Esperado: mensagem reentregue a outra réplica (ou após o reinício), efeito aplicado uma vez.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

step "F2: 2 réplicas do media-relay, alocação lenta (8s) para dar tempo de matar"
signaling RELAY_TIMEOUT_MS=90000
relays 2 RELAY_LATENCY_MS=8000

id=$(new_call "ana-$RUN" 0 "bob-$RUN"); info "callId=$id"
for _ in $(seq 1 200); do
  victim=$(for c in $(docker compose ps -q media-relay); do
    docker logs "$c" 2>&1 | grep -q "allocating relay.*$id" && docker inspect -f '{{.Name}}' "$c" | tr -d /
  done) || true
  [ -n "$victim" ] && break; sleep 0.2
done
[ -n "$victim" ] || fail "nenhuma réplica pegou a mensagem"
step "$victim está processando a mensagem: docker kill"
docker kill "$victim" >/dev/null
info "a mensagem não foi apagada da fila: volta após o visibility timeout (30s)..."

wait_status "$id" ACTIVE 90
step "Efeito aplicado uma única vez"
logs media-relay "relay allocated.*$id"
n=$(logs media-relay "relay allocated.*$id" | wc -l | tr -d ' ')
[ "$n" = 1 ] || fail "relay alocado $n vezes"
curl -s "$API/calls/$id" | jq '{status, relayId, relayHost}'
ok "F2: chamada ACTIVE, 1 alocação, réplica morta não deixou efeito duplicado"
