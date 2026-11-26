#!/usr/bin/env bash
# F4 Mensagem venenosa: publicar payload inválido.
# Esperado: N tentativas, depois DLQ; as demais mensagens seguem normalmente.
source "$(dirname "$0")/lib.sh"
require_stack

url=$(queue_url relay-results); dlq=$(queue_url relay-results-dlq)
$AWS sqs purge-queue --queue-url "$dlq" >/dev/null 2>&1 || true
max=$($AWS sqs get-queue-attributes --queue-url "$url" --attribute-names RedrivePolicy VisibilityTimeout --output json |
  jq -r '.Attributes | "\(.RedrivePolicy | fromjson | .maxReceiveCount) \(.VisibilityTimeout)"')
vis=${max#* }; max=${max% *}
step "F4: publicando um payload inválido em relay-results (maxReceiveCount=$max, visibility timeout ${vis}s)"
sqs_id=$($AWS sqs send-message --queue-url "$url" --message-body "isto nao e um envelope $RUN" --query MessageId)
at "send-message -> SQS MessageId=$sqs_id body='isto nao e um envelope $RUN'"

step "Enquanto isso, outras mensagens seguem normalmente"
id=$(new_call "ana-$RUN" 0 "bob-$RUN"); wait_status "$id" ACTIVE 15
at "chamada $(short "$id") ACTIVE"
tail_logs "call-signaling media-relay" "(call requested|relay allocated|call active).*$id"
ok "chamada $id ficou ACTIVE"

step "Tentativas: cada recebimento do call-signaling vira uma linha de log real"
attempts() { # count and last line come from the same read of the logs
  local l n d
  l=$(tail_logs call-signaling "poison message"); n=$(echo "$l" | grep -c . || true)
  d=$($AWS sqs get-queue-attributes --queue-url "$dlq" --attribute-names ApproximateNumberOfMessages --query Attributes.ApproximateNumberOfMessages)
  printf 'tentativa %s/%s | relay-results: %s | relay-results-dlq: %s mensagem(ns)\n' "$n" "$max" "$(depth "$url")" "$d"
  [ "$n" = 0 ] || echo "$l" | tail -1
}
EVERY=2 watch_until 240 attempts 'dlq: [1-9]' || fail "mensagem não chegou à DLQ"
tries=$(logs call-signaling "poison message" | wc -l | tr -d ' ')

step "Evidência"
dm=$($AWS sqs receive-message --queue-url "$dlq" --attribute-names All --output json | jq -c '.Messages[0] // {}')
kv banco "relay-results-dlq: MessageId=$(echo "$dm" | jq -r .MessageId) body='$(echo "$dm" | jq -r .Body)'"
kv banco "relay-results-dlq: ApproximateReceiveCount=$(echo "$dm" | jq -r .Attributes.ApproximateReceiveCount) (inclui esta leitura)"
kv banco "signaling-calls ($(short "$id")): $(db signaling-calls "$id" status relayId)"
kv log "$tries linhas 'poison message left for DLQ' (uma por recebimento, ~${vis}s entre elas):"
tail_logs call-signaling "poison message" | awk '{printf "   %d/'"$max"' %s\n", NR, $0}'
[ "$(echo "$dm" | jq -r .MessageId)" = "$sqs_id" ] || fail "a mensagem na DLQ não é a venenosa publicada ($sqs_id)"
[ "$tries" = "$max" ] || fail "$tries tentativas, esperado maxReceiveCount=$max"
ok "F4: $tries tentativas, mensagem na DLQ, sistema seguiu funcionando"
