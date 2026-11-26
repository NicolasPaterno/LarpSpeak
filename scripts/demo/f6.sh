#!/usr/bin/env bash
# F6 Broker fora do ar: parar o MiniStack por 30 segundos.
# Esperado: serviços reconectam sozinhos, outbox drena, nenhuma mensagem perdida.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

signaling RELAY_TIMEOUT_MS=90000
relays 2 RELAY_LATENCY_MS=2000
pods() { for c in $(docker compose ps -q call-signaling media-relay); do docker inspect -f '{{.Name}} {{.State.StartedAt}} {{.RestartCount}}' "$c"; done; }
before=$(pods)
for c in $(docker compose ps -q call-signaling media-relay); do info "$(pod "$c")"; done
step "F6: chamada criada; o relay vai responder com o broker fora do ar"
id=$(new_call "ana-$RUN" 0 "bob-$RUN"); info "callId=$id"
sleep 0.5
carl=$(token "carl-$RUN") # log in before the broker (and DynamoDB) goes away
step "docker compose pause ministack (SQS, SNS, DynamoDB fora por 30s)"
info "pause, não stop: o MiniStack guarda tudo em memória e um stop apagaria filas e tabelas"
down=$(mark)
docker compose pause ministack >/dev/null
at "ministack: $(docker inspect -f '{{.State.Status}}' "$(docker compose ps -q ministack)")"
code=$(curl -s -o /dev/null -w '%{http_code} em %{time_total}s' -XPOST -H "Authorization: Bearer $carl" "$API/calls" -d '{}')
at "POST /calls durante a queda: HTTP $code (falha rápida, cliente tenta depois)"
retries() { # retries: retry warnings per pod since the pause, read from the real logs
  for c in $(docker compose ps -q call-signaling media-relay); do
    printf '%s=%s ' "$(docker inspect -f '{{.Name}}' "$c" | sed 's#^/larpspeak-##')" \
      "$(docker logs --since "$down" "$c" 2>&1 | grep -cE 'retrying|will retry' || true)"
  done
}
p0=$SECONDS
while [ $((SECONDS - p0)) -lt 30 ]; do at "broker pausado há $((SECONDS - p0))s | avisos de retry: $(retries)"; sleep 5; done
at "broker pausado há $((SECONDS - p0))s | avisos de retry: $(retries)"
docker compose unpause ministack >/dev/null
up=$(mark)
at "docker compose unpause ministack: $(docker inspect -f '{{.State.Status}}' "$(docker compose ps -q ministack)")"

step "Broker de volta: reconexão e entrega do que ficou no outbox (ao vivo)"
follow "call-signaling media-relay" "(relay allocated|call active).*$id" "$up"
wait_status "$id" ACTIVE 60
at "GET /calls/$(short "$id") -> ACTIVE"
ok "a chamada criada antes da queda ficou ACTIVE: nenhuma mensagem perdida"
outbox() { echo "signaling-outbox=$(count signaling-outbox) relay-outbox=$(count relay-outbox)"; }
watch_until 15 outbox '^signaling-outbox=0 relay-outbox=0$' || true
unfollow

step "Evidência"
for s in call-signaling media-relay; do
  kv log "$s: $(logs $s 'retrying|retry' | wc -l | tr -d ' ') avisos de retry; primeiro e último:"
  tail_logs $s 'retrying|retry' "$down" | sed -n '1p;$p'
done
for t in signaling-outbox relay-outbox; do
  for _ in $(seq 1 10); do n=$(count $t); [ "$n" = 0 ] && break; sleep 1; done
  kv banco "$t: $n itens"; [ "$n" = 0 ] || fail "$t não drenou ($n itens)"
done
kv banco "signaling-calls: $(db signaling-calls "$id" status relayId)"
after=$(pods)
[ "$before" = "$after" ] || fail "um serviço Go reiniciou: $before -> $after"
for c in $(docker compose ps -q call-signaling media-relay); do kv docker "$(pod "$c")"; done
id2=$(new_call "dave-$RUN" 0 "eve-$RUN"); wait_status "$id2" ACTIVE 30
at "nova chamada $(short "$id2") -> ACTIVE"
ok "F6: serviços reconectaram sozinhos e novas chamadas funcionam"
