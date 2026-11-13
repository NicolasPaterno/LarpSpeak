// Package app holds the media-relay rules: relay allocation, audio forwarding
// decisions, participant timeouts and pod failover. It reaches the outside only
// through the ports below.
package app

import (
	"context"

	"github.com/larpspeak/media-relay/internal/domain"
)

// SessionStore persists relay sessions. Write methods taking a msgID also mark
// that message processed in the same transaction and fail with
// domain.ErrDuplicate on a redelivery; domain.ErrReleased means the session
// guard failed (already released, never allocated, or already exists).
type SessionStore interface {
	Get(ctx context.Context, callID string) (*domain.Session, error) // nil if missing
	Allocated(ctx context.Context) ([]domain.Session, error)
	Allocate(ctx context.Context, msgID string, s domain.Session, out domain.Outgoing) error
	Ack(ctx context.Context, msgID string, out ...domain.Outgoing) error
	Release(ctx context.Context, msgID, callID, reason string) error
	Member(ctx context.Context, msgID, callID, op, user string) error // op: "ADD" or "DELETE"
	Emit(ctx context.Context, out domain.Outgoing) error
}

// NodeStore persists relay pods and the leader lease. Leader writes are fenced:
// they fail unless leader still holds the lease in term.
type NodeStore interface {
	Ping(ctx context.Context, n domain.Node) error
	Nodes(ctx context.Context) ([]domain.Node, error)
	Lease(ctx context.Context) (*domain.Lease, error) // nil if nobody ever led
	Claim(ctx context.Context, id string, old *domain.Lease) (term int64, err error)
	Renew(ctx context.Context, id string, term int64) error
	MarkDead(ctx context.Context, n domain.Node, leader string, term int64) error
	ForgetNode(ctx context.Context, id string)
	Move(ctx context.Context, leader string, term int64, s domain.Session, to domain.Node, out domain.Outgoing) error
}

// Prober talks to other relay pods directly.
type Prober interface {
	AnswersHealth(addr, id string) bool // a pod at addr answers /health as replica id
	NotifyAdopt(addr, callID string)    // best effort: start forwarding callID now
}
