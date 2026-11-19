package domain

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"regexp"
)

var (
	ErrTaken          = errors.New("username taken")
	ErrBadCredentials = errors.New("invalid credentials")
	ErrTokenReused    = errors.New("refresh token reused")
	// ErrRefreshRace: another tab rotated this refresh token a moment ago. The
	// browser already holds the newer cookie, so the answer must not clear it.
	ErrRefreshRace = errors.New("refresh token just rotated")

	usernameRe = regexp.MustCompile(`^[a-z0-9_-]{3,32}$`)
)

const (
	PwIterations   = 600_000 // OWASP 2023 for PBKDF2-HMAC-SHA256
	RefreshActive  = "ACTIVE"
	RefreshUsed    = "USED"
	RefreshRevoked = "REVOKED"
)

// Account is a login. UserID is the username, so the userId strings already
// used in events and scripts stay valid.
type Account struct {
	UserID    string `dynamodbav:"pk"`
	PwHash    []byte `dynamodbav:"pwHash"`
	Salt      []byte `dynamodbav:"salt"`
	Iter      int    `dynamodbav:"iter"` // stored per row so it can be raised later
	CreatedAt int64  `dynamodbav:"createdAt"`
}

// RefreshToken is one rotation step of a login session (Family). Only the
// SHA-256 of the token is stored.
type RefreshToken struct {
	Hash      string `dynamodbav:"pk"`
	UserID    string `dynamodbav:"userId"`
	Family    string `dynamodbav:"family"`
	Status    string `dynamodbav:"status"`
	UsedAt    int64  `dynamodbav:"usedAt,omitempty"` // unix ms
	ExpiresAt int64  `dynamodbav:"expiresAt"`        // unix s (DynamoDB TTL unit)
}

func ValidUsername(u string) bool { return usernameRe.MatchString(u) }
func ValidPassword(p string) bool { return len(p) >= 8 && len(p) <= 128 }

func HashPassword(pw string, salt []byte, iter int) []byte {
	k, _ := pbkdf2.Key(sha256.New, pw, salt, iter, 32) // errors only on FIPS limits we do not hit
	return k
}

func (a *Account) CheckPassword(pw string) bool {
	return subtle.ConstantTimeCompare(HashPassword(pw, a.Salt, a.Iter), a.PwHash) == 1
}
