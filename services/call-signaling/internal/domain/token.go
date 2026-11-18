package domain

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Token types. One signing key serves them all; typ keeps them apart, so a
// 60s SSE ticket can never be used as an access token.
const (
	TokenAccess = "access" // Bearer on the HTTP API
	TokenSSE    = "sse"    // ?ticket= on the SSE stream (EventSource cannot send headers)
	TokenJoin   = "join"   // sent to media-relay with a WebRTC offer; Cid = callId
)

var ErrTokenInvalid = errors.New("invalid token")

// Claims of a Larpspeak JWT (EdDSA / Ed25519). Only call-signaling holds the
// private key, so only call-signaling can create tokens; media-relay verifies
// with the public key.
type Claims struct {
	Sub string `json:"sub"`
	Typ string `json:"typ"`
	Cid string `json:"cid,omitempty"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

var (
	b64       = base64.RawURLEncoding
	jwtHeader = b64.EncodeToString([]byte(`{"alg":"EdDSA","typ":"JWT"}`))
)

func SignToken(key ed25519.PrivateKey, c Claims) string {
	body, _ := json.Marshal(c) // plain struct: cannot fail
	msg := jwtHeader + "." + b64.EncodeToString(body)
	return msg + "." + b64.EncodeToString(ed25519.Sign(key, []byte(msg)))
}

// VerifyToken checks signature, algorithm, expiry and type.
func VerifyToken(key ed25519.PublicKey, tok, typ string, now time.Time) (Claims, error) {
	var c Claims
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return c, ErrTokenInvalid
	}
	var h struct{ Alg string }
	hb, err := b64.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hb, &h) != nil || h.Alg != "EdDSA" { // rejects "none" and "HS256"
		return c, ErrTokenInvalid
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil || !ed25519.Verify(key, []byte(parts[0]+"."+parts[1]), sig) {
		return c, ErrTokenInvalid
	}
	body, err := b64.DecodeString(parts[1])
	if err != nil || json.Unmarshal(body, &c) != nil {
		return Claims{}, ErrTokenInvalid
	}
	if c.Typ != typ || c.Sub == "" || now.Unix() >= c.Exp {
		return Claims{}, ErrTokenInvalid
	}
	return c, nil
}
