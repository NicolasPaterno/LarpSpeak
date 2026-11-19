package domain

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"
)

func TestToken(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	otherPub, otherPriv, _ := ed25519.GenerateKey(nil)
	now := time.Unix(1_000_000, 0)
	tok := SignToken(priv, Claims{Sub: "ana", Typ: TokenAccess, Iat: now.Unix(), Exp: now.Unix() + 300})

	if c, err := VerifyToken(pub, tok, TokenAccess, now); err != nil || c.Sub != "ana" {
		t.Fatalf("valid token rejected: %v %+v", err, c)
	}
	parts := strings.Split(tok, ".")
	bad := map[string]func() (string, ed25519.PublicKey, string, time.Time){
		"wrong typ": func() (string, ed25519.PublicKey, string, time.Time) { return tok, pub, TokenSSE, now },
		"expired": func() (string, ed25519.PublicKey, string, time.Time) {
			return tok, pub, TokenAccess, now.Add(300 * time.Second)
		},
		"other key": func() (string, ed25519.PublicKey, string, time.Time) { return tok, otherPub, TokenAccess, now },
		"forged key": func() (string, ed25519.PublicKey, string, time.Time) {
			return SignToken(otherPriv, Claims{Sub: "ana", Typ: TokenAccess, Exp: now.Unix() + 9}), pub, TokenAccess, now
		},
		"alg none": func() (string, ed25519.PublicKey, string, time.Time) {
			return b64.EncodeToString([]byte(`{"alg":"none"}`)) + "." + parts[1] + ".", pub, TokenAccess, now
		},
		"alg HS256": func() (string, ed25519.PublicKey, string, time.Time) {
			return b64.EncodeToString([]byte(`{"alg":"HS256"}`)) + "." + parts[1] + "." + parts[2], pub, TokenAccess, now
		},
		"tampered": func() (string, ed25519.PublicKey, string, time.Time) {
			return parts[0] + "." + b64.EncodeToString([]byte(`{"sub":"bob","typ":"access","exp":9999999999}`)) + "." + parts[2], pub, TokenAccess, now
		},
		"two parts": func() (string, ed25519.PublicKey, string, time.Time) {
			return parts[0] + "." + parts[1], pub, TokenAccess, now
		},
		"garbage sig": func() (string, ed25519.PublicKey, string, time.Time) {
			return parts[0] + "." + parts[1] + ".!!", pub, TokenAccess, now
		},
	}
	for name, f := range bad {
		tk, k, typ, at := f()
		if _, err := VerifyToken(k, tk, typ, at); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPassword(t *testing.T) {
	a := Account{Salt: []byte("0123456789abcdef"), Iter: 1000}
	a.PwHash = HashPassword("correct horse", a.Salt, a.Iter)
	if !a.CheckPassword("correct horse") || a.CheckPassword("wrong horse") {
		t.Fatal("password check broken")
	}
	if !ValidUsername("ana_01") || ValidUsername("Ana") || ValidUsername("ab") || ValidPassword("short") {
		t.Fatal("validation broken")
	}
}
