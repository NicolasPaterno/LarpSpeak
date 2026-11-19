package domain

import (
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"
)

func sign(key ed25519.PrivateKey, c map[string]any) string {
	body, _ := json.Marshal(c)
	msg := b64.EncodeToString([]byte(`{"alg":"EdDSA","typ":"JWT"}`)) + "." + b64.EncodeToString(body)
	return msg + "." + b64.EncodeToString(ed25519.Sign(key, []byte(msg)))
}

func TestVerifyJoin(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	_, other, _ := ed25519.GenerateKey(nil)
	now := time.Unix(1000, 0)
	ok := map[string]any{"sub": "ana", "typ": "join", "cid": "c1", "exp": 1060}
	if c, err := VerifyJoin(pub, sign(priv, ok), "c1", now); err != nil || c.Sub != "ana" {
		t.Fatalf("valid token rejected: %v", err)
	}
	for name, tok := range map[string]string{
		"other call": sign(priv, ok),
		"access typ": sign(priv, map[string]any{"sub": "ana", "typ": "access", "cid": "c2", "exp": 1060}),
		"expired":    sign(priv, map[string]any{"sub": "ana", "typ": "join", "cid": "c2", "exp": 1000}),
		"forged":     sign(other, map[string]any{"sub": "ana", "typ": "join", "cid": "c2", "exp": 1060}),
	} {
		if _, err := VerifyJoin(pub, tok, "c2", now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
