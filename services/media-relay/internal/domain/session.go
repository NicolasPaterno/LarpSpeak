// Package domain holds the media-relay types that do not depend on AWS or the network.
package domain

import "errors"

const (
	StateAllocated = "ALLOCATED"
	StateReleased  = "RELEASED"

	NodeAlive = "ALIVE"
	NodeDead  = "DEAD"
)

var (
	ErrDuplicate = errors.New("message already processed")
	ErrReleased  = errors.New("relay already released")
)

// Session is one relay, keyed by callId (one relay per call).
type Session struct {
	CallID       string   `dynamodbav:"pk"`
	RelayID      string   `dynamodbav:"relayId"`
	Host         string   `dynamodbav:"host"`
	Port         int      `dynamodbav:"port"`
	Participants []string `dynamodbav:"participants,stringset,omitempty"`
	State        string   `dynamodbav:"state"`
	Reason       string   `dynamodbav:"reason,omitempty"`
	Owner        string   `dynamodbav:"owner"` // relay pod forwarding this call's audio
}

// Node is a relay pod in relay-nodes.
type Node struct {
	ID     string `dynamodbav:"pk"`
	Beat   int64  `dynamodbav:"beat"` // heartbeat counter: +1 per ping
	Status string `dynamodbav:"status"`
	Addr   string `dynamodbav:"addr"` // host:port of the pod's HTTP API (health check, adopt)
	Host   string `dynamodbav:"host"` // where participants send UDP audio
	Port   int    `dynamodbav:"port"`
}

// Lease is the leader row. Term is the generation clock: +1 per new leader.
type Lease struct {
	Holder string `dynamodbav:"holder"`
	Term   int64  `dynamodbav:"term"`
	Beat   int64  `dynamodbav:"beat"`
}

// Outgoing is a message written to the outbox in the same transaction as the
// state change that produced it. Dest is a queue name.
type Outgoing struct {
	Dest     string
	Envelope Envelope
}
