// Package pubkey fetches call-signaling's token public key.
package pubkey

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// Key holds the public key once loaded; nil until then (offers get 503).
type Key struct {
	v atomic.Pointer[ed25519.PublicKey]
}

func (k *Key) Get() ed25519.PublicKey {
	if p := k.v.Load(); p != nil {
		return *p
	}
	return nil
}

// Load polls GET <signalingURL>/auth/public-key with backoff until it works (R6):
// the relay keeps allocating calls meanwhile; only browser offers wait for it.
func (k *Key) Load(ctx context.Context, signalingURL string, log *slog.Logger) {
	c := &http.Client{Timeout: 3 * time.Second}
	for wait := 500 * time.Millisecond; ctx.Err() == nil; wait = min(wait*2, 10*time.Second) {
		key, err := fetch(c, signalingURL+"/auth/public-key")
		if err == nil {
			k.v.Store(&key)
			log.Info("jwt public key loaded", "from", signalingURL)
			return
		}
		log.Warn("public key fetch failed, retrying", "err", err, "wait", wait.String())
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

func fetch(c *http.Client, url string) (ed25519.PublicKey, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct{ Alg, Key string }
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&body) != nil || body.Alg != "EdDSA" {
		return nil, fmt.Errorf("bad answer: HTTP %d", resp.StatusCode)
	}
	b, err := base64.RawURLEncoding.DecodeString(body.Key)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("bad key")
	}
	return ed25519.PublicKey(b), nil
}
