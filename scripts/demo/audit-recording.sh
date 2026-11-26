#!/usr/bin/env bash
# Recording end to end: two WebRTC probes talk for a few seconds -> encrypted chunks in S3
# -> GET /audit/calls/{id}/audio/{user} returns an Ogg Opus file as long as what was sent.
# A chunk moved to another participant's prefix must fail authentication (AES-GCM AAD).
source "$(dirname "$0")/audit-lib.sh"

step "Call with two probes: ana sends 8s, bob 6s of Opus"
start_call rec 8 6; wait; probes_ok rec
id=$(call_id rec); ana=$(user_of rec ana); bob=$(user_of rec bob)
info "callId=$id ana sent $(sent rec a) packets, bob $(sent rec b)"

step "Chunks in s3://$BUCKET/audio/$id/"
for _ in $(seq 1 20); do [ -n "$(s3_keys "$id" | grep "/$bob/")" ] && [ -n "$(s3_keys "$id" | grep "/$ana/")" ] && break; sleep 1; done
keys=$(s3_keys "$id"); echo "$keys" | sed 's/^/   /'
kv log "audio-chunks consumed by audit-vault (first 4 of $(vault_logs "audio stored.*$id" | wc -l | tr -d ' ')):"
tail_logs audit-vault "audio stored.*$id" | head -4
for u in "$ana" "$bob"; do echo "$keys" | grep -q "^audio/$id/$u/" || fail "no chunk for $u"; done
k=$(echo "$keys" | head -1)
magic=$($AWS s3 cp "s3://$BUCKET/$k" - | head -c4)
info "first bytes of $k: $magic"
[ "$magic" = LSA1 ] || fail "chunk does not start with LSA1 (got '$magic')"

step "Export (Bearer token) and duration"
code=$(curl -s -o /dev/null -w '%{http_code}' "$AUDIT/audit/calls/$id/audio/$ana")
info "without token: HTTP $code"; [ "$code" = 401 ] || fail "export without token answered $code"
for who in a b; do
  u=$(user_of rec $([ $who = a ] && echo ana || echo bob)); want=$(sent rec $who)
  f=$(wait_audio "$id" "$u" $((want * 98 / 100)) 20)
  info "$u: $(head -c4 "$f") file, $(ogg_packets "$f") packets = $(( $(ogg_packets "$f") / 50 ))s (sent $want = $((want / 50))s)"
  [ "$(head -c4 "$f")" = OggS ] || fail "export of $u is not Ogg"
  [ "$(ogg_packets "$f")" -le "$want" ] || fail "export of $u has more packets than were sent (duplicates?)"
done

step "Tamper: copy one of ana's chunks into bob's prefix"
src=$(echo "$keys" | grep "/$ana/" | head -1)
$AWS s3 cp "s3://$BUCKET/$src" "s3://$BUCKET/audio/$id/$bob/$(basename "$src")" >/dev/null
body=$(curl -s -w ' HTTP %{http_code}' -H "Authorization: Bearer $AUDIT_TOKEN" "$AUDIT/audit/calls/$id/audio/$bob")
info "export of $bob: $body"
echo "$body" | grep -q 'recording failed authentication.* HTTP 500' || fail "moved chunk was not rejected"
pass "recording: LSA1 chunks in S3, Ogg export matches what was sent, moved chunk -> 500 recording failed authentication"
