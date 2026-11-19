package app

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/larpspeak/call-signaling/internal/domain"
)

type fakeAccounts struct {
	mu       sync.Mutex
	accounts map[string]domain.Account
	tokens   map[string]domain.RefreshToken
	revoked  map[string]bool
}

func newFakeAccounts() *fakeAccounts {
	return &fakeAccounts{accounts: map[string]domain.Account{}, tokens: map[string]domain.RefreshToken{}, revoked: map[string]bool{}}
}

func (f *fakeAccounts) CreateAccount(_ context.Context, a domain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.accounts[a.UserID]; ok {
		return domain.ErrTaken
	}
	f.accounts[a.UserID] = a
	return nil
}
func (f *fakeAccounts) Account(_ context.Context, u string) (*domain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.accounts[u]; ok {
		return &a, nil
	}
	return nil, nil
}
func (f *fakeAccounts) StartSession(_ context.Context, t domain.RefreshToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[t.Hash] = t
	return nil
}
func (f *fakeAccounts) RefreshToken(_ context.Context, h string) (*domain.RefreshToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tokens[h]; ok {
		return &t, nil
	}
	return nil, nil
}
func (f *fakeAccounts) Rotate(_ context.Context, old, next domain.RefreshToken, now int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur := f.tokens[old.Hash]
	if cur.Status != domain.RefreshActive || f.revoked[old.Family] {
		return domain.ErrTokenInvalid
	}
	cur.Status, cur.UsedAt = domain.RefreshUsed, now
	f.tokens[old.Hash], f.tokens[next.Hash] = cur, next
	return nil
}
func (f *fakeAccounts) RevokeFamily(_ context.Context, fam string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked[fam] = true
	return nil
}

func TestAuthFlow(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(nil)
	now := time.Unix(1_000_000, 0)
	a := &Auth{St: newFakeAccounts(), Key: key, AccessTTL: 5 * time.Minute, RefreshTTL: time.Hour, Iter: 1000,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), NowFn: func() time.Time { return now }}
	ctx := context.Background()

	if err := a.Register(ctx, "ana", "password1"); err != nil {
		t.Fatal(err)
	}
	if err := a.Register(ctx, "ana", "password2"); !errors.Is(err, domain.ErrTaken) {
		t.Fatalf("duplicate register: %v", err)
	}
	if _, err := a.Login(ctx, "ana", "wrong-pass"); !errors.Is(err, domain.ErrBadCredentials) {
		t.Fatalf("bad password: %v", err)
	}
	if _, err := a.Login(ctx, "nobody", "password1"); !errors.Is(err, domain.ErrBadCredentials) {
		t.Fatalf("unknown user: %v", err)
	}
	t1, err := a.Login(ctx, "ana", "password1")
	if err != nil {
		t.Fatal(err)
	}
	if c, err := a.Verify(t1.Access, domain.TokenAccess); err != nil || c.Sub != "ana" {
		t.Fatalf("access token: %v", err)
	}

	t2, err := a.Refresh(ctx, t1.Refresh) // rotation
	if err != nil || t2.Refresh == t1.Refresh {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := a.Refresh(ctx, t1.Refresh); !errors.Is(err, domain.ErrRefreshRace) { // inside grace: no revoke, cookie kept
		t.Fatalf("grace reuse: %v", err)
	}
	t3, err := a.Refresh(ctx, t2.Refresh) // session still alive after grace hit
	if err != nil {
		t.Fatalf("after grace: %v", err)
	}

	now = now.Add(10 * time.Second)
	if _, err := a.Refresh(ctx, t2.Refresh); !errors.Is(err, domain.ErrTokenReused) { // theft
		t.Fatalf("reuse: %v", err)
	}
	if _, err := a.Refresh(ctx, t3.Refresh); err == nil { // the newest token died with the family
		t.Fatal("family not revoked")
	}

	t4, _ := a.Login(ctx, "ana", "password1")
	if err := a.Logout(ctx, t4.Refresh); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Refresh(ctx, t4.Refresh); err == nil {
		t.Fatal("refresh after logout")
	}
	now = now.Add(2 * time.Hour)
	t5, _ := a.Login(ctx, "ana", "password1")
	now = now.Add(2 * time.Hour)
	if _, err := a.Refresh(ctx, t5.Refresh); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Fatalf("expired refresh: %v", err)
	}
}
