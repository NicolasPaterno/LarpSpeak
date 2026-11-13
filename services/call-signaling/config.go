package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type config struct {
	Port            string
	MaxParticipants int
	RelayTimeout    time.Duration
	TopicArn        string
	RelayRequests   string
	RelayResults    string
	CallsTable      string
	OutboxTable     string
	ProcessedTable  string
}

func loadConfig() (config, error) {
	var missing []string
	need := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			missing = append(missing, k)
		}
		return v
	}
	c := config{
		Port:           envOr("PORT", "8080"),
		TopicArn:       need("CALL_EVENTS_TOPIC_ARN"),
		RelayRequests:  need("RELAY_REQUESTS_QUEUE"),
		RelayResults:   need("RELAY_RESULTS_QUEUE"),
		CallsTable:     need("CALLS_TABLE"),
		OutboxTable:    need("OUTBOX_TABLE"),
		ProcessedTable: need("PROCESSED_TABLE"),
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("missing env vars: %v", missing)
	}
	var err error
	if c.MaxParticipants, err = strconv.Atoi(envOr("MAX_PARTICIPANTS", "10")); err != nil || c.MaxParticipants < 2 {
		return c, fmt.Errorf("MAX_PARTICIPANTS must be an integer >= 2")
	}
	ms, err := strconv.Atoi(envOr("RELAY_TIMEOUT_MS", "3000"))
	if err != nil || ms < 1 {
		return c, fmt.Errorf("RELAY_TIMEOUT_MS must be a positive integer")
	}
	c.RelayTimeout = time.Duration(ms) * time.Millisecond
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
