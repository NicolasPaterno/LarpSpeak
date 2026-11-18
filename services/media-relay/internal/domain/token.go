package domain

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Verify-only copy of call-signaling's token code (duplicated on purpose, like
// the envelope). Only call-signaling holds the private key; this pod has the
// public key and can check join tokens but never create them.

var ErrTokenInvalid = errors.New("invalid token")

type Claims struct {
	Sub string `json:"sub"`
	Typ string `json:"typ"`
	Cid string `json:"cid,omitempty"`
	Exp int64  `json:"exp"`
}

var b64 = base64.RawURLEncoding

// VerifyJoin checks a "join" token for callID: EdDSA signature, expiry, type, call.
func VerifyJoin(key ed25519.PublicKey, tok, callID string, now time.Time) (Claims, error) {
	var c Claims
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return c, ErrTokenInvalid
	}
	var h struct{ Alg string }
	hb, err := b64.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hb, &h) != nil || h.Alg != "EdDSA" {
		return c, ErrTokenInvalid
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil || !ed25519.Verify(key, []byte(parts[0]+"."+parts[1]), sig) {
		return c, ErrTokenInvalid
	}
	body, err := b64.DecodeString(parts[1])
	if err != nil || json.Unmarshal(body, &c) != nil || c.Typ != "join" || c.Cid != callID || c.Sub == "" || now.Unix() >= c.Exp {
		return Claims{}, ErrTokenInvalid
	}
	return c, nil
}
