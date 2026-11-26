#!/usr/bin/env bash
# Falha manual para teste na LAN: docker kill no pod do media-relay que hospeda a chamada.
# Uso: scripts/demo/kill-call-relay.sh [callId]   (sem callId: a única sessão ALLOCATED)
# Esperado: o líder detecta em ~3 s, move a chamada para o outro pod (RelayMoved), o navegador
# recebe relay_moved, refaz a oferta e o áudio volta. O pod morto fica parado até você religá-lo.
source "$(dirname "$0")/lib.sh"
require_stack

call=${1:-}
if [ -z "$call" ]; then
  calls=$($AWS dynamodb scan --table-name relay-sessions --filter-expression '#s = :a' \
    --expression-attribute-names '{"#s":"state"}' --expression-attribute-values '{":a":{"S":"ALLOCATED"}}' \
    --query 'Items[].pk.S' | tr '\t' '\n' | grep -v '^None$' || true)
  [ "$(printf '%s' "$calls" | grep -c .)" = 1 ] || fail "informe o callId; sessões ALLOCATED: ${calls:-nenhuma}"
  call=$calls
fi

owner() { $AWS dynamodb get-item --table-name relay-sessions --key "{\"pk\":{\"S\":\"$call\"}}" --query 'Item.owner.S'; }
pod=$(owner)
[ -n "$pod" ] && [ "$pod" != None ] || fail "chamada $call sem sessão no relay"
name=$(docker inspect -f '{{.Name}}' "$pod" 2>/dev/null | tr -d /) || fail "contêiner $pod não encontrado"

step "Chamada $call está no pod $pod ($name)"
docker kill "$pod" >/dev/null
info "$(date +%T) docker kill $name"

step "Aguardando o failover (até 20 s)"
for _ in $(seq 1 40); do new=$(owner); [ "$new" != "$pod" ] && break; sleep 0.5; done
[ "$new" != "$pod" ] || fail "a chamada não saiu do pod morto"
ok "$(date +%T) chamada movida: $pod -> $new"
sleep 3 # o navegador refaz a oferta no pod novo
for c in $(docker ps -q --filter label=com.docker.compose.project=larpspeak --filter name=media-relay); do
  docker logs --since "$STARTED" "$c" 2>&1 | grep -E "relay pod dead|call moved|picked up" || true
done | jq -rc '[.time[11:23], .replica, .msg] | join("  ")' | sed 's/^/   /'
info "religar o pod morto: docker start $name"
