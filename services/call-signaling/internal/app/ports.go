// Package app holds the call-signaling rules: the call-setup saga and the
// channel lifecycle. It reaches the outside only through the ports below.
package app

import (
	"context"

	"github.com/larpspeak/call-signaling/internal/domain"
)

// CallStore persists calls. Every write method is one atomic unit of work:
// state change + outbox messages (+ processed-message marker when msgID != "",
// failing with domain.ErrDuplicate on a redelivery).
type CallStore interface {
	Get(ctx context.Context, id string) (*domain.Call, error) // consistent read; nil if missing
	Pending(ctx context.Context) ([]domain.Call, error)
	ActiveInvites(ctx context.Context, user string) ([]domain.Call, error)
	CurrentCall(ctx context.Context, user string) (string, error)
	ForgetUser(ctx context.Context, user, callID string)  // best effort
	AddInvite(ctx context.Context, id, user string) error // ErrNotActive if the call is not active
	MakeGroup(ctx context.Context, id string) error       // direct -> group; no-op if already a group

	// Create: [call, RelayRequested, claim creator]. ErrStateChanged: creator is in another call.
	Create(ctx context.Context, c domain.Call, out domain.Outgoing) error
	// Activate: PENDING -> ACTIVE with c.Relay*/StartedAt, plus CallStarted. ErrStateChanged if not PENDING.
	Activate(ctx context.Context, msgID string, c domain.Call, out domain.Outgoing) error
	// MoveRelay points an ACTIVE call to a new relay pod. ErrStateChanged if not ACTIVE.
	MoveRelay(ctx context.Context, msgID, id, host string, port int) error
	// Fail (compensation): PENDING -> FAILED, seats freed, plus CallFailed. ErrStateChanged if not PENDING.
	Fail(ctx context.Context, msgID, id, reason string, out domain.Outgoing) error
	// Join takes a seat (R2). ErrSeatLost: full, ended or already inside. ErrStateChanged: user claimed by another call.
	Join(ctx context.Context, id, user string, out domain.Outgoing) error
	// Leave frees a seat (pinned on c.Taken; last ends the call). ErrStateChanged: counter moved, read again.
	Leave(ctx context.Context, msgID string, c *domain.Call, user string, last bool, now int64, out ...domain.Outgoing) error
	// Ack marks msgID processed, emitting out with it.
	Ack(ctx context.Context, msgID string, out ...domain.Outgoing) error
}

// Notifier pushes an event to a connected user.
type Notifier interface {
	Notify(userID, event string, data any)
}

// AccountStore persists logins and refresh tokens.
type AccountStore interface {
	CreateAccount(ctx context.Context, a domain.Account) error // ErrTaken if the username exists
	Account(ctx context.Context, user string) (*domain.Account, error)
	// StartSession stores a new login family and its first refresh token.
	StartSession(ctx context.Context, t domain.RefreshToken) error
	RefreshToken(ctx context.Context, hash string) (*domain.RefreshToken, error) // nil if missing
	// Rotate marks old USED and stores next, atomically and only while old is
	// ACTIVE, unexpired and its family not revoked. ErrTokenInvalid otherwise.
	Rotate(ctx context.Context, old domain.RefreshToken, next domain.RefreshToken, nowMs int64) error
	RevokeFamily(ctx context.Context, family string) error
}
