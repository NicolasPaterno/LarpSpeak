#!/usr/bin/env bash
# F5 Corrida pelo recurso: 100 requisições simultâneas para 10 unidades.
# Esperado: exatamente 10 sucessos; as outras 90 recusadas.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

step "F5: chamada com 11 lugares = criador + 10 vagas livres"
signaling MAX_PARTICIPANTS=11
id=$(new_call "host-$RUN" 11); wait_status "$id" ACTIVE 15
info "callId=$id"

step "100 contas fazem login (antes da corrida, fora do tempo medido)"
for i in $(seq 1 100); do token "u$i-$RUN" >/dev/null & done; wait

step "100 joins simultâneos"
seq 1 100 | xargs -P 100 -I{} sh -c "curl -s -o /dev/null -w '%{http_code}\n' -XPOST -H \"Authorization: Bearer \$(cat $TOKENS/u{}-$RUN)\" $API/calls/$id/join" \
  | sort | uniq -c | sed 's/^/   /'
call_json "$id" | jq '{taken, capacity, participants: (.participants | length)}'
[ "$(field "$id" taken)" = 11 ] || fail "lugares ocupados: $(field "$id" taken)"
ok "F5: 10 joins aceitos (200), 90 recusados (409), nenhum lugar a mais"
