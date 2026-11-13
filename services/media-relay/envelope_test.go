package main

import "testing"

func TestParseEnvelope(t *testing.T) {
	valid := `{"messageId":"m1","correlationId":"c1","type":"RelayFailed","version":1,"occurredAt":"2026-11-13T10:00:00Z","payload":{"callId":"c1"}}`
	if _, err := parseEnvelope(valid); err != nil {
		t.Fatalf("valid envelope rejected: %v", err)
	}
	poison := map[string]string{
		"not json":        `garbage`,
		"no messageId":    `{"correlationId":"c1","type":"T","version":1,"occurredAt":"2026-11-13T10:00:00Z","payload":{}}`,
		"no version":      `{"messageId":"m1","correlationId":"c1","type":"T","occurredAt":"2026-11-13T10:00:00Z","payload":{}}`,
		"payload array":   `{"messageId":"m1","correlationId":"c1","type":"T","version":1,"occurredAt":"2026-11-13T10:00:00Z","payload":[]}`,
		"missing payload": `{"messageId":"m1","correlationId":"c1","type":"T","version":1,"occurredAt":"2026-11-13T10:00:00Z"}`,
	}
	for name, body := range poison {
		if _, err := parseEnvelope(body); err == nil {
			t.Errorf("%s: accepted, want rejection", name)
		}
	}
}

func TestParseHeader(t *testing.T) {
	c, u, ok := parseHeader([]byte("call-1 ana\nAUDIO"))
	if !ok || c != "call-1" || u != "ana" {
		t.Fatalf("got %q %q %v", c, u, ok)
	}
	for _, bad := range []string{"no newline", "onlycall\nx", " ana\nx", "call-1 \nx"} {
		if _, _, ok := parseHeader([]byte(bad)); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}
