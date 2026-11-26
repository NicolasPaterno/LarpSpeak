#!/usr/bin/env bash
# F8 Escala horizontal: subir 3 réplicas de um consumidor.
# Esperado: carga dividida entre as réplicas, sem efeito duplicado.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

step "F8: 3 réplicas do media-relay consumindo relay-requests"
relays 3
for c in $(docker compose ps -q media-relay); do info "$(pod "$c")"; done

step "30 chamadas criadas em sequência; placar ao vivo de 'relay allocated' por réplica"
m=$(mark); idf=$(mktemp); errf=$(mktemp)
(for i in $(seq 1 30); do new_call "c$i-$RUN" 0 "d$i-$RUN" 2>>"$errf" >>"$idf" || exit 1; done) &
maker=$!
tally() {
  local t=0 n c
  for c in $(docker compose ps -q media-relay); do
    n=$(docker logs --since "$m" "$c" 2>&1 | grep -c 'relay allocated' || true); t=$((t + n))
    printf '%s=%s ' "$(docker inspect -f '{{.Name}}' "$c" | tr -d / | sed 's/^larpspeak-//')" "$n"
  done
  echo "| total $t/30"
}
watch_until 120 tally 'total 30/30' || fail "alocações: $(tally)"
wait $maker || fail "POST /calls falhou: $(tail -1 "$errf")"
ids=(); while read -r x; do ids+=("$x"); done <"$idf"
info "$(grep -c '202 Accepted' "$errf") x POST /calls -> 202 Accepted"
[ "${#ids[@]}" = 30 ] || fail "${#ids[@]} callIds (esperado 30)"
for id in "${ids[@]}"; do wait_status "$id" ACTIVE 20; done
ok "30 chamadas ACTIVE"

step "Evidência"
pat=$(IFS='|'; echo "${ids[*]}")
for c in $(docker compose ps -q media-relay); do
  name=$(docker inspect -f '{{.Name}}' "$c" | tr -d /)
  n=$(docker logs "$c" 2>&1 | grep 'relay allocated' | grep -cE "$pat" || true)
  kv log "$name: $n chamadas"; [ "$n" -ge 1 ] || fail "$name não recebeu nenhuma chamada"
  replicas=$((${replicas:-0} + 1))
done
own=$($AWS dynamodb scan --table-name relay-sessions --output json | jq -r --arg p "$pat" '.Items[] | select(.pk.S | test($p)) | .owner.S' | sort | uniq -c)
while read -r n h; do kv banco "relay-sessions owner=$h ($(docker ps -a --filter "id=$h" --format '{{.Names}}')): $n sessões"; done <<<"$own"
tail_logs media-relay "relay allocated.*(${ids[0]}|${ids[1]}|${ids[2]})"
[ "$replicas" = 3 ] || fail "esperadas 3 réplicas do media-relay, há $replicas"
dup=$(logs media-relay 'relay allocated' | grep -oE "$pat" | sort | uniq -d)
[ -z "$dup" ] || fail "alocação duplicada: $dup"
ok "F8: carga dividida, cada chamada alocada uma única vez"
