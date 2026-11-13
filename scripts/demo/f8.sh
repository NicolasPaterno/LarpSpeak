#!/usr/bin/env bash
# F8 Escala horizontal: subir 3 réplicas de um consumidor.
# Esperado: carga dividida entre as réplicas, sem efeito duplicado.
source "$(dirname "$0")/lib.sh"
require_stack
trap restore EXIT

step "F8: 3 réplicas do media-relay consumindo relay-requests"
relays 3
ids=()
for i in $(seq 1 30); do ids+=("$(new_call "c$i-$RUN" 0 "d$i-$RUN")"); done
for id in "${ids[@]}"; do wait_status "$id" ACTIVE 20; done
ok "30 chamadas ACTIVE"

step "Alocações por réplica"
pat=$(IFS='|'; echo "${ids[*]}")
for c in $(docker compose ps -q media-relay); do
  name=$(docker inspect -f '{{.Name}}' "$c" | tr -d /)
  info "$name: $(docker logs "$c" 2>&1 | grep 'relay allocated' | grep -cE "$pat") chamadas"
done
dup=$(logs media-relay 'relay allocated' | grep -oE "$pat" | sort | uniq -d)
[ -z "$dup" ] || fail "alocação duplicada: $dup"
ok "F8: carga dividida, cada chamada alocada uma única vez"
