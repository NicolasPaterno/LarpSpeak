#!/usr/bin/env bash
# F5 Corrida pelo recurso: 100 requisições simultâneas para 10 unidades.
# Esperado: exatamente 10 sucessos; as outras 90 recusadas.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

step "F5: chamada com 11 lugares = criador + 10 vagas livres"
signaling MAX_PARTICIPANTS=11
id=$(new_call "host-$RUN" 11); wait_status "$id" ACTIVE 15
kv banco "signaling-calls antes: $(db signaling-calls "$id" status taken capacity)"

step "100 contas fazem login (antes da corrida, fora do tempo medido)"
for i in $(seq 1 100); do token "u$i-$RUN" >/dev/null & done; wait
info "$(find "$TOKENS" -name "u*-$RUN" -size +0 | wc -l | tr -d ' ') tokens prontos"

step "100 joins simultâneos (placar ao vivo, a cada 10 respostas)"
at "disparando 100x POST /calls/$(short "$id")/join em paralelo"
codes=$(seq 1 100 | xargs -P 100 -I{} sh -c "curl -s -o /dev/null -w '%{http_code} u{}\n' -XPOST -H \"Authorization: Bearer \$(cat $TOKENS/u{}-$RUN)\" $API/calls/$id/join" |
  while read -r c u; do
    echo "$c $u"; n=$((${n:-0} + 1)); [ "$c" = 200 ] && a=$((${a:-0} + 1)); [ "$c" = 409 ] && r=$((${r:-0} + 1))
    [ $((n % 10)) = 0 ] && at "$n/100 respostas: 200=${a:-0} 409=${r:-0}" >&2
  done)

step "Evidência"
accepted=$(echo "$codes" | grep -c '^200 ' || true)
refused=$(echo "$codes" | grep -c '^409 ' || true)
c=$(call_json "$id")
kv banco "signaling-calls depois: $(db signaling-calls "$id" status taken capacity)"
kv banco "participantes ($(echo "$c" | jq '.participants | length')): $(echo "$c" | jq -r '[.participants[] | (.userId? // .) | tostring | sub("-[0-9]+$"; "")] | join(" ")')"
kv http "$(echo "$codes" | cut -d' ' -f1 | sort | uniq -c | awk '{printf "%s=%s ", $2, $1}')"
kv http "aceitos: $(echo "$codes" | awk '$1 == 200 {sub(/-[0-9]+$/, "", $2); print $2}' | paste -sd' ' -)"
tail_logs call-signaling "participant joined.*$id" | tail -3
[ "$accepted" = 10 ] || fail "joins aceitos: $accepted (esperado 10)"
[ "$refused" = 90 ] || fail "joins recusados: $refused (esperado 90)"
[ "$(field "$id" taken)" = 11 ] || fail "lugares ocupados: $(field "$id" taken)"
[ "$(echo "$c" | jq '.participants | length')" = 11 ] || fail "participantes: $(echo "$c" | jq '.participants | length')"
ok "F5: 10 joins aceitos (200), 90 recusados (409), nenhum lugar a mais"
