// Package domain holds the call-signaling types and rules that do not depend
// on AWS or the network.
package domain

import (
	"errors"
	"slices"
)

const (
	StatusPending = "PENDING"
	StatusActive  = "ACTIVE"
	StatusFailed  = "FAILED"
	StatusEnded   = "ENDED"

	DestTopic = "call-events" // outbox destination: SNS topic; anything else is an SQS queue name
)

var (
	ErrRoomFull     = errors.New("call is full")
	ErrNotActive    = errors.New("call is not active")
	ErrAlreadyIn    = errors.New("user already in call")
	ErrNotIn        = errors.New("user not in call")
	ErrDuplicate    = errors.New("message already processed")
	ErrStateChanged = errors.New("call state changed concurrently")
	ErrSeatLost     = errors.New("seat update rejected") // join condition failed: full, ended, or already inside
)

// Call is a voice channel: up to Capacity participants talk at the same time.
// Taken is the seat counter guarded by conditional writes (R2).
type Call struct {
	ID           string   `json:"callId" dynamodbav:"pk"`
	Kind         string   `json:"kind" dynamodbav:"kind"`
	CallerID     string   `json:"callerId" dynamodbav:"callerId"`
	Invited      []string `json:"invited,omitempty" dynamodbav:"invited,omitempty"`
	Participants []string `json:"participants" dynamodbav:"participants,stringset,omitempty"`
	Taken        int      `json:"taken" dynamodbav:"taken"`
	Capacity     int      `json:"capacity" dynamodbav:"capacity"`
	Status       string   `json:"status" dynamodbav:"status"`
	Reason       string   `json:"reason,omitempty" dynamodbav:"reason,omitempty"`
	RelayID      string   `json:"relayId,omitempty" dynamodbav:"relayId,omitempty"`
	RelayHost    string   `json:"relayHost,omitempty" dynamodbav:"relayHost,omitempty"`
	RelayPort    int      `json:"relayPort,omitempty" dynamodbav:"relayPort,omitempty"`
	CreatedAt    int64    `json:"createdAt" dynamodbav:"createdAt"` // unix ms
	StartedAt    int64    `json:"startedAt,omitempty" dynamodbav:"startedAt,omitempty"`
	EndedAt      int64    `json:"endedAt,omitempty" dynamodbav:"endedAt,omitempty"`
}

func (c *Call) Has(user string) bool { return slices.Contains(c.Participants, user) }

// Outgoing is a message written to the outbox in the same transaction as the
// state change that produced it. Dest is a queue name or DestTopic.
type Outgoing struct {
	Dest     string
	Envelope Envelope
}
