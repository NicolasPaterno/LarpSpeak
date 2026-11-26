#!/usr/bin/env bash
# Falha surpresa: docker kill no call-signaling (única réplica) com uma chamada WebRTC em andamento
# e logo depois de criar outra chamada.
# Esperado:
#   - enquanto está fora: API indisponível (nginx 502), contêiner não volta sozinho;
#   - o áudio da chamada em andamento continua (vai direto ao media-relay);
#   - a chamada criada antes da queda termina coerente, por um de dois caminhos corretos:
#     A) o varredor vence: FAILED "relay timeout" e o RelayAllocated atrasado é liberado;
#     B) o relay responde antes do varredor: a chamada é ativada uma única vez;
#     nos dois, a sessão do relay termina RELEASED e nada é aplicado duas vezes.
# Precisa do ambiente em modo voz (RELAY_HOST=127.0.0.1 make start) por causa do webrtc-probe.
source "$(dirname "$0")/lib.sh"
require_stack
PROBE=bin/webrtc-probe
[ -x "$PROBE" ] || (cd tools/webrtc-probe && go build -o ../../bin/webrtc-probe .) || fail "não consegui compilar $PROBE (precisa de Go)"
CS=$(docker compose ps -q call-signaling)
D=$(mktemp -d)
trap 'docker compose up -d --no-deps call-signaling >/dev/null 2>&1' EXIT

step "1. Chamada WebRTC real: ana e bob mandam Opus por 30 s"
$PROBE -user "ana-$RUN" -create "bob-$RUN" -seconds 30 >"$D/a" 2>&1 &
for _ in $(seq 1 50); do grep -q '^callId ' "$D/a" && break; sleep 0.2; done
live=$(awk '/^callId /{print $2}' "$D/a")
[ -n "$live" ] || fail "o probe não criou a chamada: $(tail -2 "$D/a")"
$PROBE -user "bob-$RUN" -call "$live" -seconds 30 >"$D/b" 2>&1 &
info "chamada em andamento: $live"
sleep 8

step "2. carl cria uma chamada nova e o call-signaling é morto em seguida"
token "carl-$RUN" >/dev/null
id=$(new_call "carl-$RUN")
docker kill "$CS" >/dev/null
info "callId=$id (PENDING; RelayRequested ainda no outbox)"

step "3. Com o call-signaling fora do ar"
sleep 3
state=$(docker inspect -f '{{.State.Status}}' "$CS")
web=$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/api/health)
info "contêiner: $state | API pelo nginx: HTTP $web"
[ "$state" = exited ] || fail "o contêiner deveria continuar parado (está $state)"
[ "$web" = 502 ] || fail "o nginx deveria responder 502 (respondeu $web)"
sleep 4

step "4. call-signaling de volta"
docker compose up -d --no-deps call-signaling >/dev/null 2>&1
until curl -sf "$API/health" >/dev/null; do sleep 0.5; done
ok "no ar de novo"

# Logs by container: the voice-mode relays are not all in the base compose file.
clogs() { docker logs --since "$STARTED" "$1" 2>&1 | grep -E "$2" || true; }
relay_logs() { for c in $(docker ps -q --filter label=com.docker.compose.project=larpspeak --filter name=media-relay); do clogs "$c" "$1"; done; }
session() { $AWS dynamodb get-item --table-name relay-sessions --key "{\"pk\":{\"S\":\"$1\"}}" --query 'Item.state.S'; }

step "5. Destino da chamada criada antes da queda"
# Na volta há uma corrida legítima: o varredor (a cada 1 s) compensa a chamada PENDING mais velha que
# RELAY_TIMEOUT_MS, mas a resposta do relay pode chegar antes. Os dois caminhos são corretos; o script
# confere que o estado final é coerente e que nada foi aplicado duas vezes.
for _ in $(seq 1 75); do st=$(field "$id" status); [ "$st" != PENDING ] && break; sleep 0.2; done
info "status: $st"
case "$st" in
  FAILED)
    [ "$(field "$id" reason)" = "relay timeout" ] || fail "motivo: $(field "$id" reason)"
    ok "caminho A: o varredor venceu, FAILED relay timeout"
    info "aguardando o RelayAllocated atrasado (até 60 s: pode estar preso no long poll do processo morto)"
    for _ in $(seq 1 60); do [ -n "$(clogs "$CS" "late relay released.*$id")" ] && break; sleep 1; done
    [ -n "$(clogs "$CS" "late relay released.*$id")" ] || fail "o RelayAllocated atrasado não foi liberado"
    [ "$(clogs "$CS" "call active.*$id" | wc -l | tr -d ' ')" = 0 ] || fail "chamada ativada depois de compensada"
    ;;
  ACTIVE|ENDED)
    ok "caminho B: o relay respondeu antes do varredor, chamada ativada"
    [ "$(clogs "$CS" "call active.*$id" | wc -l | tr -d ' ')" = 1 ] || fail "a chamada deveria ser ativada exatamente 1 vez"
    [ "$st" = ENDED ] || api "carl-$RUN" POST "/calls/$id/leave" >/dev/null
    wait_status "$id" ENDED 15
    ;;
  *) fail "status inesperado: $st" ;;
esac

step "6. Nenhum relay ficou alocado e nenhum efeito foi duplicado"
for _ in $(seq 1 20); do [ "$(session "$id")" != ALLOCATED ] && break; sleep 0.5; done
{ clogs "$CS" "$id"; relay_logs "$id"; } | jq -rc '[.time[11:23], .service, .msg, .reason // ""] | join("  ")' | sort | sed 's/^/   /'
info "relay-sessions: $(session "$id")"
[ "$(session "$id")" != ALLOCATED ] || fail "a sessão do relay ficou ALLOCATED"
[ "$(relay_logs "relay allocated.*$id" | wc -l | tr -d ' ')" = 1 ] || fail "relay alocado mais de uma vez"
ok "relay liberado; 1 alocação; duplicatas descartadas pelo marcador"

step "7. A chamada em andamento não perdeu o áudio"
wait
for p in a b; do
  sent=$(sed -nE 's/.*done: ([0-9]+) Opus packets.*/\1/p' "$D/$p")
  recv=$(sed -nE 's/.*received ([0-9]+) RTP packets.*/\1/p' "$D/$p")
  info "$p: enviou $sent, recebeu $recv"
  [ -n "$recv" ] && [ "$recv" -ge $((sent * 9 / 10)) ] || fail "$p recebeu menos de 90% do áudio ($recv de $sent)"
done
ok "Falha surpresa: call-signaling caiu e voltou; áudio contínuo, chamada pendente resolvida sem efeito duplicado, relay liberado"
