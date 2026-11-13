package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"
)

// Envelope is the shared message format defined in contracts/envelope.schema.json.
type Envelope struct {
	MessageID     string          `json:"messageId"`
	CorrelationID string          `json:"correlationId"`
	Type          string          `json:"type"`
	Version       int             `json:"version"`
	OccurredAt    time.Time       `json:"occurredAt"`
	Payload       json.RawMessage `json:"payload"`
}

func NewEnvelope(typ, correlationID string, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		MessageID:     uuid.New().String(),
		CorrelationID: correlationID,
		Type:          typ,
		Version:       1,
		OccurredAt:    time.Now().UTC(),
		Payload:       raw,
	}, nil
}

// ParseEnvelope rejects anything that does not match the contract, so poison
// messages are never acknowledged and end up in the DLQ.
func ParseEnvelope(body string) (Envelope, error) {
	var e Envelope
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
