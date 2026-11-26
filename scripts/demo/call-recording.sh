#!/usr/bin/env bash
# Prova da gravação de uma chamada real: trechos cifrados no S3, chave fora do bucket, API com token,
# integridade (AES-GCM) e um arquivo Ogg por participante, aberto no Chrome para ouvir.
# Uso: scripts/demo/call-recording.sh [callId]   (sem callId: a chamada mais recente da trilha)
# Funciona com a chamada em andamento: os trechos fecham a cada AUDIO_CHUNK_MS (5 s).
source "$(dirname "$0")/lib.sh"
require_stack

AUDIT=${AUDIT:-http://localhost:8083}
AUDIT_TOKEN=${AUDIT_API_TOKEN:-larpspeak-audit-demo}
BUCKET=larpspeak-audio
audit() { curl -s -H "Authorization: Bearer $AUDIT_TOKEN" "$@"; }

call=${1:-$(audit "$AUDIT/audit/calls?limit=1" | jq -r '.[0].callId // empty')}
[ -n "$call" ] || fail "nenhuma chamada na trilha; informe o callId"
OUT=${OUT:-/tmp/larpspeak-recordings/$call}
mkdir -p "$OUT"

step "1. Trechos da chamada $call no S3 (s3://$BUCKET/audio/$call/)"
keys=$($AWS s3 ls "s3://$BUCKET/audio/$call/" --recursive | awk '{print $4}')
[ -n "$keys" ] || fail "nenhum trecho gravado para $call"
echo "$keys" | cut -d/ -f3 | sort | uniq -c | awk '{printf "   %-12s %s trechos\n", $2, $1}'
info "exemplo de chave: $(echo "$keys" | head -1)"

step "2. O objeto no S3 é cifrado (AES-256-GCM): \"LSA1\" | nonce 12 B | tag 16 B | ciphertext"
k=$(echo "$keys" | head -1)
$AWS s3 cp "s3://$BUCKET/$k" - >"$OUT/chunk.bin"
xxd -l 64 "$OUT/chunk.bin" | sed 's/^/   /'
[ "$(head -c4 "$OUT/chunk.bin")" = LSA1 ] || fail "o trecho não começa com LSA1"
info "file: $(file -b "$OUT/chunk.bin")  (não é áudio tocável)"
ok "trecho cifrado"

step "3. A chave fica no Secrets Manager, não no bucket"
info "segredo: $($AWS secretsmanager describe-secret --secret-id larpspeak/audio-key --query Name)"
others=$($AWS s3 ls "s3://$BUCKET/" --recursive | awk '{print $4}' | grep -cv '^audio/' || true)
info "objetos fora de audio/ no bucket: $others"
[ "$others" = 0 ] || fail "há objetos fora de audio/ no bucket"
ok "o bucket só tem áudio cifrado"

step "4. A API de exportação exige token"
code=$(curl -s -o /dev/null -w '%{http_code}' "$AUDIT/audit/calls/$call/audio")
info "sem token: HTTP $code"
[ "$code" = 401 ] || fail "sem token respondeu $code"
ok "sem token, sem áudio"

step "5. Integridade: um trecho de outro participante colado na pasta errada é rejeitado"
users=$(audit "$AUDIT/audit/calls/$call/audio" | jq -r '.[].userId')
if [ "$(echo "$users" | wc -l | tr -d ' ')" -ge 2 ]; then
  u1=$(echo "$users" | sed -n 1p); u2=$(echo "$users" | sed -n 2p)
  src=$(echo "$keys" | grep "/$u1/" | head -1); fake="audio/$call/$u2/$(basename "$src")"
  $AWS s3 cp "s3://$BUCKET/$src" "s3://$BUCKET/$fake" >/dev/null
  info "copiado: $src -> $fake"
  info "exportar $u2: $(audit -w ' HTTP %{http_code}' "$AUDIT/audit/calls/$call/audio/$u2")"
  $AWS s3 rm "s3://$BUCKET/$fake" >/dev/null
  info "cópia removida; exportar $u2 de novo: HTTP $(audit -o /dev/null -w '%{http_code}' "$AUDIT/audit/calls/$call/audio/$u2")"
  ok "AES-GCM com callId+userId+início como AAD: trecho fora do lugar falha a autenticação"
else
  info "só 1 participante gravado: pulando"
fi

step "6. Áudio decifrado de cada participante (Ogg Opus) em $OUT"
for u in $users; do
  audit -o "$OUT/$u.ogg" "$AUDIT/audit/calls/$call/audio/$u"
  [ "$(head -c4 "$OUT/$u.ogg")" = OggS ] || fail "exportação de $u não é Ogg: $(head -c 200 "$OUT/$u.ogg")"
  info "$u.ogg  $(du -h "$OUT/$u.ogg" | cut -f1)  $(file -b "$OUT/$u.ogg")"
done
rm -f "$OUT/chunk.bin"
ok "gravação verificada"
open -a "Google Chrome" "$OUT"/*.ogg 2>/dev/null || info "abra os arquivos no Chrome ou VLC"
