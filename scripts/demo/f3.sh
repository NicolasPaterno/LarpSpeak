#!/usr/bin/env bash
# F3 Mensagem duplicada: republicar a mesma mensagem (mesmo messageId) 5 vezes.
# Esperado: um único efeito.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

step "F3: media-relay parado, a chamada fica PENDING esperando o relay"
signaling RELAY_TIMEOUT_MS=60000
relays 0
id=$(new_call "ana-$RUN" 0 "bob-$RUN"); info "callId=$id status=$(field "$id" status)"

msg=$(uuidgen | tr A-Z a-z)
body=$(envelope RelayAllocated "$id" "{\"callId\":\"$id\",\"relayId\":\"$(uuidgen | tr A-Z a-z)\",\"host\":\"127.0.0.1\",\"port\":40000}" "$msg")
url=$(queue_url relay-results)
step "Publicando o mesmo RelayAllocated 5x (messageId=$msg)"
for i in 1 2 3 4 5; do $AWS sqs send-message --queue-url "$url" --message-body "$body" >/dev/null; info "envio $i"; done
for _ in $(seq 1 30); do # all 5 copies handled?
  [ "$(logs call-signaling "duplicate ignored.*$msg" | wc -l | tr -d ' ')" -ge 4 ] && break; sleep 0.5
done

step "Resultado"
call_json "$id" | jq '{status, relayId}'
logs call-signaling "$msg|call active.*$id" | grep -E "duplicate|call active" | cut -c1-200
active=$(logs call-signaling "call active.*$id" | wc -l | tr -d ' ')
dups=$(logs call-signaling "duplicate ignored.*$msg" | wc -l | tr -d ' ')
info "efeitos: $active | duplicatas ignoradas: $dups"
$AWS dynamodb get-item --table-name signaling-processed --key "{\"pk\":{\"S\":\"$msg\"}}" --query Item.pk.S | sed 's/^/   processed: /'
[ "$active" = 1 ] && [ "$dups" = 4 ] || fail "esperado 1 efeito e 4 duplicatas"
ok "F3: 5 entregas, 1 efeito"
# The forged RelayAllocated has no relay session, so no relay would ever time the call out: leave it.
api "ana-$RUN" POST "/calls/$id/leave" >/dev/null
