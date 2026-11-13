package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// envelope is the shared message format defined in contracts/envelope.schema.json.
type envelope struct {
	MessageID     string          `json:"messageId"`
	CorrelationID string          `json:"correlationId"`
	Type          string          `json:"type"`
	Version       int             `json:"version"`
	OccurredAt    time.Time       `json:"occurredAt"`
	Payload       json.RawMessage `json:"payload"`
}

func newEnvelope(typ, correlationID string, payload any) (envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return envelope{}, err
	}
	return envelope{
		MessageID:     newID(),
		CorrelationID: correlationID,
		Type:          typ,
		Version:       1,
		OccurredAt:    time.Now().UTC(),
		Payload:       raw,
	}, nil
}

// parseEnvelope rejects anything that does not match the contract, so poison
// messages are never acknowledged and end up in the DLQ.
func parseEnvelope(body string) (envelope, error) {
	var e envelope
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		return e, fmt.Errorf("invalid json: %w", err)
	}
	switch {
	case e.MessageID == "", e.CorrelationID == "", e.Type == "":
		return e, errors.New("missing messageId, correlationId or type")
	case e.Version < 1, e.OccurredAt.IsZero():
		return e, errors.New("missing version or occurredAt")
	case len(e.Payload) == 0 || e.Payload[0] != '{':
		return e, errors.New("payload must be an object")
	}
	return e, nil
}

// newID returns a random UUID v4.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
