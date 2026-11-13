package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type config struct {
	Port               string
	UDPPort            int
	Latency            time.Duration // R5: simulated slowness of relay allocation
	FailureRate        float64       // R5: probability that an allocation fails
	ParticipantTimeout time.Duration // a participant sending no packets this long is gone
	RelayRequests      string
	RelayResults       string
	CallEvents         string
	SessionsTable      string
	NodesTable         string
	OutboxTable        string
	ProcessedTable     string
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
		RelayRequests:  need("RELAY_REQUESTS_QUEUE"),
		RelayResults:   need("RELAY_RESULTS_QUEUE"),
		CallEvents:     need("CALL_EVENTS_QUEUE"),
		SessionsTable:  need("SESSIONS_TABLE"),
		NodesTable:     need("NODES_TABLE"),
		OutboxTable:    need("OUTBOX_TABLE"),
		ProcessedTable: need("PROCESSED_TABLE"),
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("missing env vars: %v", missing)
	}
	var err error
	if c.UDPPort, err = strconv.Atoi(envOr("UDP_PORT", "40000")); err != nil {
		return c, fmt.Errorf("UDP_PORT must be an integer")
	}
	if c.FailureRate, err = strconv.ParseFloat(envOr("RELAY_FAILURE_RATE", "0"), 64); err != nil || c.FailureRate < 0 || c.FailureRate > 1 {
		return c, fmt.Errorf("RELAY_FAILURE_RATE must be between 0 and 1")
	}
	if c.Latency, err = millis("RELAY_LATENCY_MS", "0"); err != nil {
		return c, err
	}
	if c.ParticipantTimeout, err = millis("PARTICIPANT_TIMEOUT_MS", "15000"); err != nil {
		return c, err
	}
	return c, nil
}

func millis(key, def string) (time.Duration, error) {
	ms, err := strconv.Atoi(envOr(key, def))
	if err != nil || ms < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer (ms)", key)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
