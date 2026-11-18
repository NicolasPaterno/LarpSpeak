package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"
	"uuid"

	"github.com/larpspeak/call-signaling/internal/domain"
)

// reuseGrace: a USED refresh token presented again this soon is two tabs
// refreshing at once, not theft; answer 401 without revoking the session.
const reuseGrace = 5 * time.Second

// Auth owns accounts and is the only place that creates tokens.
type Auth struct {
	St         AccountStore
	Key        ed25519.PrivateKey
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Iter       int // PBKDF2 iterations for new passwords
	Log        *slog.Logger
	NowFn      func() time.Time
}

// Tokens is what login and refresh return: a short JWT plus an opaque refresh token.
type Tokens struct {
	Access  string
	Refresh string
	UserID  string
}

func (a *Auth) PublicKey() ed25519.PublicKey { return a.Key.Public().(ed25519.PublicKey) }

// Mint creates a token of the given type. cid is the call id for join tokens.
func (a *Auth) Mint(user, typ, cid string, ttl time.Duration) string {
	now := a.NowFn()
	return domain.SignToken(a.Key, domain.Claims{Sub: user, Typ: typ, Cid: cid, Iat: now.Unix(), Exp: now.Add(ttl).Unix()})
}

func (a *Auth) Verify(tok, typ string) (domain.Claims, error) {
	return domain.VerifyToken(a.PublicKey(), tok, typ, a.NowFn())
}

func (a *Auth) Register(ctx context.Context, user, pw string) error {
	if !domain.ValidUsername(user) || !domain.ValidPassword(pw) {
		return domain.ErrBadCredentials
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	err := a.St.CreateAccount(ctx, domain.Account{
		UserID: user, Salt: salt, Iter: a.Iter, PwHash: domain.HashPassword(pw, salt, a.Iter), CreatedAt: a.NowFn().UnixMilli(),
	})
	if err == nil {
		a.Log.Info("account registered", "userId", user)
	}
	return err
}

// Login answers the same error for an unknown user and a wrong password, and
// spends the same PBKDF2 time on both (no username oracle).
func (a *Auth) Login(ctx context.Context, user, pw string) (Tokens, error) {
	acc, err := a.St.Account(ctx, user)
	if err != nil {
		return Tokens{}, err
	}
	if acc == nil {
		domain.HashPassword(pw, make([]byte, 16), a.Iter)
		return Tokens{}, domain.ErrBadCredentials
	}
	if !acc.CheckPassword(pw) {
		return Tokens{}, domain.ErrBadCredentials
	}
	refresh, rt := a.newRefresh(user, uuid.New().String())
	if err := a.St.StartSession(ctx, rt); err != nil {
		return Tokens{}, err
	}
	a.Log.Info("login", "userId", user, "family", rt.Family)
	return Tokens{Access: a.Mint(user, domain.TokenAccess, "", a.AccessTTL), Refresh: refresh, UserID: user}, nil
}

// Refresh rotates the refresh token. Presenting an already used token (after
// the grace window) revokes the whole login: the thief's copy dies too.
func (a *Auth) Refresh(ctx context.Context, refresh string) (Tokens, error) {
	old, err := a.St.RefreshToken(ctx, hashToken(refresh))
	if err != nil {
		return Tokens{}, err
	}
	now := a.NowFn()
	if old == nil || now.Unix() >= old.ExpiresAt {
		return Tokens{}, domain.ErrTokenInvalid
	}
	if old.Status == domain.RefreshUsed {
		if now.Sub(time.UnixMilli(old.UsedAt)) < reuseGrace {
			return Tokens{}, domain.ErrTokenInvalid
		}
		a.Log.Warn("refresh token reuse detected", "userId", old.UserID, "family", old.Family)
		if err := a.St.RevokeFamily(ctx, old.Family); err != nil {
			return Tokens{}, err
		}
		return Tokens{}, domain.ErrTokenReused
	}
	next, rt := a.newRefresh(old.UserID, old.Family)
	if err := a.St.Rotate(ctx, *old, rt, now.UnixMilli()); err != nil {
		return Tokens{}, err
	}
	return Tokens{Access: a.Mint(old.UserID, domain.TokenAccess, "", a.AccessTTL), Refresh: next, UserID: old.UserID}, nil
}

// Logout revokes the login session of the given refresh token, if any.
func (a *Auth) Logout(ctx context.Context, refresh string) error {
	rt, err := a.St.RefreshToken(ctx, hashToken(refresh))
	if err != nil || rt == nil {
		return err
	}
	a.Log.Info("logout", "userId", rt.UserID, "family", rt.Family)
	return a.St.RevokeFamily(ctx, rt.Family)
}

func (a *Auth) newRefresh(user, family string) (string, domain.RefreshToken) {
	b := make([]byte, 32)
	rand.Read(b)
	tok := base64.RawURLEncoding.EncodeToString(b)
	return tok, domain.RefreshToken{
		Hash: hashToken(tok), UserID: user, Family: family, Status: domain.RefreshActive,
		ExpiresAt: a.NowFn().Add(a.RefreshTTL).Unix(),
	}
}

func hashToken(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }

// IsAuthError reports errors that mean "log in again" rather than "server broken".
func IsAuthError(err error) bool {
	return errors.Is(err, domain.ErrTokenInvalid) || errors.Is(err, domain.ErrTokenReused) || errors.Is(err, domain.ErrBadCredentials)
}
