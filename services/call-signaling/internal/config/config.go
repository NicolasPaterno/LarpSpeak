package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port            string
	MaxParticipants int
	RelayTimeout    time.Duration
	TopicArn        string
	RelayRequests   string
	RelayResults    string
	CallsTable      string
	UsersTable      string
	OutboxTable     string
	ProcessedTable  string
	AccountsTable   string
	RefreshTable    string
	JWTKeySecret    string // Secrets Manager id of the Ed25519 seed (only this service reads it)
	AccessTTL       time.Duration
	RefreshTTL      time.Duration
	CookieSecure    bool
}

// Load reads the service configuration from environment variables.
func Load() (Config, error) {
	var missing []string
	need := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			missing = append(missing, k)
		}
		return v
	}
	c := Config{
		Port:           envOr("PORT", "8080"),
		TopicArn:       need("CALL_EVENTS_TOPIC_ARN"),
		RelayRequests:  need("RELAY_REQUESTS_QUEUE"),
		RelayResults:   need("RELAY_RESULTS_QUEUE"),
		CallsTable:     need("CALLS_TABLE"),
		UsersTable:     need("USERS_TABLE"),
		OutboxTable:    need("OUTBOX_TABLE"),
		ProcessedTable: need("PROCESSED_TABLE"),
		AccountsTable:  need("ACCOUNTS_TABLE"),
		RefreshTable:   need("REFRESH_TABLE"),
		JWTKeySecret:   need("JWT_KEY_SECRET"),
		CookieSecure:   envOr("COOKIE_SECURE", "true") != "false",
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
	if c.AccessTTL, err = seconds("ACCESS_TOKEN_TTL_S", "300"); err != nil {
		return c, err
	}
	c.RefreshTTL, err = seconds("REFRESH_TTL_S", "604800") // 7 days
	return c, err
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func seconds(key, def string) (time.Duration, error) {
	n, err := strconv.Atoi(envOr(key, def))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return time.Duration(n) * time.Second, nil
}
