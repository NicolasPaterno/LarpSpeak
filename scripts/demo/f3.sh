#!/usr/bin/env bash
# F3 Mensagem duplicada: republicar a mesma mensagem (mesmo messageId) 5 vezes.
# Esperado: um único efeito.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

step "F3: media-relay parado, a chamada fica PENDING esperando o relay"
signaling RELAY_TIMEOUT_MS=60000
relays 0
info "réplicas do media-relay rodando: $(docker compose ps -q media-relay | wc -l | tr -d ' ')"
id=$(new_call "ana-$RUN" 0 "bob-$RUN"); info "callId=$id status=$(field "$id" status)"

msg=$(uuidgen | tr A-Z a-z)
body=$(envelope RelayAllocated "$id" "{\"callId\":\"$id\",\"relayId\":\"$(uuidgen | tr A-Z a-z)\",\"host\":\"127.0.0.1\",\"port\":40000}" "$msg")
url=$(queue_url relay-results)
step "Publicando o mesmo RelayAllocated 5x (messageId=$msg); consumo ao vivo do call-signaling"
follow call-signaling "$msg|call active.*$id"
for i in 1 2 3 4 5; do at "envio $i/5 -> SQS MessageId=$($AWS sqs send-message --queue-url "$url" --message-body "$body" --query MessageId) (envelope messageId=$(short "$msg"))"; done
for _ in $(seq 1 30); do # all 5 copies handled?
  [ "$(logs call-signaling "duplicate ignored.*$msg" | wc -l | tr -d ' ')" -ge 4 ] && break; sleep 0.5
done
unfollow

step "Evidência"
active=$(logs call-signaling "call active.*$id" | wc -l | tr -d ' ')
dups=$(logs call-signaling "duplicate ignored.*$msg" | wc -l | tr -d ' ')
kv banco "signaling-calls: $(db signaling-calls "$id" status relayId)"
kv banco "signaling-processed: $(db signaling-processed "$msg")"
kv log "efeitos ('call active'): $active | 'duplicate ignored' messageId=$(short "$msg"): $dups"
tail_logs call-signaling "$msg|call active.*$id"
[ "$active" = 1 ] && [ "$dups" = 4 ] || fail "esperado 1 efeito e 4 duplicatas"
ok "F3: 5 entregas, 1 efeito"
# The forged RelayAllocated has no relay session, so no relay would ever time the call out: leave it.
api "ana-$RUN" POST "/calls/$id/leave" >/dev/null
