#!/usr/bin/env bash
# F4 Mensagem venenosa: publicar payload inválido.
# Esperado: N tentativas, depois DLQ; as demais mensagens seguem normalmente.
source "$(dirname "$0")/lib.sh"
require_stack

url=$(queue_url relay-results); dlq=$(queue_url relay-results-dlq)
$AWS sqs purge-queue --queue-url "$dlq" >/dev/null 2>&1 || true
step "F4: publicando um payload inválido em relay-results"
$AWS sqs send-message --queue-url "$url" --message-body "isto nao e um envelope $RUN" >/dev/null

step "Enquanto isso, outras mensagens seguem normalmente"
id=$(new_call "ana-$RUN" 0 "bob-$RUN"); wait_status "$id" ACTIVE 15
ok "chamada $id ficou ACTIVE"

step "Aguardando as 5 tentativas (maxReceiveCount=5, visibility timeout 30s: ~2,5 min)"
for i in $(seq 1 40); do
  n=$($AWS sqs get-queue-attributes --queue-url "$dlq" --attribute-names ApproximateNumberOfMessages --query Attributes.ApproximateNumberOfMessages)
  tries=$(logs call-signaling "poison message" | wc -l | tr -d ' ')
  info "tentativas: $tries | mensagens na DLQ: $n"
  [ "$n" -ge 1 ] && break; sleep 10
done
[ "$n" -ge 1 ] || fail "mensagem não chegou à DLQ"
step "Conteúdo da relay-results-dlq"
$AWS sqs receive-message --queue-url "$dlq" --query 'Messages[].Body'
ok "F4: $tries tentativas, mensagem na DLQ, sistema seguiu funcionando"
