#!/usr/bin/env bash
# F2 Serviço morre no meio: docker kill no consumidor durante o processamento.
# Esperado: mensagem reentregue a outra réplica (ou após o reinício), efeito aplicado uma vez.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

step "F2: 2 réplicas do media-relay, alocação lenta (8s) para dar tempo de matar"
signaling RELAY_TIMEOUT_MS=90000
relays 2 RELAY_LATENCY_MS=8000
for c in $(docker compose ps -q media-relay); do info "$(pod "$c")"; done

step "Chamada criada; logs ao vivo de quem pega a mensagem (relay-requests)"
m=$(mark)
id=$(new_call "ana-$RUN" 0 "bob-$RUN")
follow "media-relay call-signaling" "(allocating relay|relay allocated|call active).*$id" "$m"
for _ in $(seq 1 200); do
  victim=$(for c in $(docker compose ps -q media-relay); do
    docker logs "$c" 2>&1 | grep -q "allocating relay.*$id" && docker inspect -f '{{.Name}}' "$c" | tr -d /
  done) || true
  [ -n "$victim" ] && break; sleep 0.2
done
[ -n "$victim" ] || fail "nenhuma réplica pegou a mensagem"
msg=$(docker logs "$victim" 2>&1 | grep "allocating relay.*$id" | head -1 | jq -r .messageId)
sleep 1
step "$victim está no meio da alocação (messageId=$(short "$msg")): docker kill"
docker kill "$victim" >/dev/null
at "docker kill $victim (SIGKILL)"
info "$(pod "$victim")"
info "a mensagem não foi apagada da fila: fica invisível até o visibility timeout (30s)"
sleep 2
at "subindo consumer de novo: docker start $victim"
docker start "$victim" >/dev/null
health() { echo "$victim: $(docker inspect -f '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' "$victim")"; }
EVERY=0.5 watch_until 30 health 'running healthy$' || fail "$victim não voltou"
info "$(pod "$victim")"

step "Aguardando a reentrega (relay-requests, direto do SQS)"
url=$(queue_url relay-requests)
queue() { echo "relay-requests: $(depth "$url") | chamada: $(field "$id" status)"; }
watch_until 90 queue 'ACTIVE$' || fail "chamada $id não chegou em ACTIVE (está $(field "$id" status))"
wait_status "$id" ACTIVE 5
unfollow

step "Evidência"
allocs=$(logs media-relay "allocating relay.*$id" | jq -r .messageId)
n=$(logs media-relay "relay allocated.*$id" | wc -l | tr -d ' ')
s=$($AWS dynamodb scan --table-name relay-sessions --filter-expression 'pk = :id' \
  --expression-attribute-values "{\":id\":{\"S\":\"$id\"}}" --output json | jq -c '[.Items[] | {owner: .owner.S, state: .state.S}]')
owner=$(echo "$s" | jq -r '.[0].owner // empty')
kv banco "signaling-calls: $(db signaling-calls "$id" status relayId)"
kv banco "relay-sessions ($id): $(echo "$s" | jq length) item(s) $s"
[ -z "$owner" ] || kv banco "owner=$owner é o container $(docker ps --filter "id=$owner" --format '{{.Names}}')"
kv log "'allocating relay' $(echo "$allocs" | grep -c .)x para messageId $(echo "$allocs" | sort -u | while read -r x; do short "$x"; done | paste -sd, -) (1a entrega morreu com $victim, 2a = reentrega)"
kv log "'relay allocated' ${n}x"
tail_logs "media-relay call-signaling" "(allocating relay|relay allocated|call active).*$id"
[ "$n" = 1 ] || fail "relay alocado $n vezes"
[ "$(echo "$allocs" | sort -u)" = "$msg" ] && [ "$(echo "$allocs" | grep -c .)" -ge 2 ] \
  || fail "esperada a mesma mensagem $msg entregue de novo após o kill: $(echo "$allocs" | paste -sd' ' -)"
[ "$(echo "$s" | jq length)" = 1 ] || fail "relay-sessions tem $(echo "$s" | jq length) itens para a chamada (esperado 1)"
ok "F2: chamada ACTIVE, 1 alocação, réplica morta não deixou efeito duplicado"
