// media-relay: allocates audio relays (simulated unstable dependency), forwards
// UDP audio between call participants, frees relays when calls end and reports
// participants whose client disappeared.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

func main() {
	hostname, _ := os.Hostname()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "media-relay", "replica", hostname)
	cfg, err := loadConfig()
	if err != nil {
		log.Error("bad config", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The client timeout (> 10s SQS long poll) turns a frozen broker into errors we log and retry (R6).
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithHTTPClient(&http.Client{Timeout: 15 * time.Second}),
		awsconfig.WithRetryMaxAttempts(1)) // our loops retry with backoff and log each attempt
	if err != nil {
		log.Error("aws config", "err", err)
		os.Exit(1)
	}

	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: cfg.UDPPort})
	if err != nil {
		log.Error("udp listen", "err", err)
		os.Exit(1)
	}
	st := &store{db: dynamodb.NewFromConfig(awsCfg), cfg: cfg}
	b := &bus{sqs: sqs.NewFromConfig(awsCfg), log: log, urls: map[string]string{}}
	r := &relay{st: st, log: log, cfg: cfg, id: hostname, host: advertisedHost(hostname), live: map[string]*liveSession{}}

	go relayOutbox(ctx, st, b)
	go b.consume(ctx, cfg.RelayRequests, r.onRelayRequest) // competing consumers across replicas (R4)
	go b.consume(ctx, cfg.CallEvents, r.onCallEvent)
	go r.sweep(ctx)
	go r.failover(ctx)
	go r.serveUDP(ctx, conn)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "media-relay", "replica": hostname})
	})
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: mux}
	go func() { <-ctx.Done(); _ = srv.Shutdown(context.Background()); _ = conn.Close() }()
	log.Info("listening", "http", srv.Addr, "udp", cfg.UDPPort, "host", r.host,
		"latency", cfg.Latency.String(), "failureRate", cfg.FailureRate, "participantTimeout", cfg.ParticipantTimeout.String())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// advertisedHost is where participants send audio: RELAY_HOST if set, else this container's IP.
func advertisedHost(hostname string) string {
	if h := os.Getenv("RELAY_HOST"); h != "" {
		return h
	}
	if ips, err := net.LookupHost(hostname); err == nil && len(ips) > 0 {
		return ips[0]
	}
	return hostname
}

func attr(item map[string]types.AttributeValue, k string) string {
	if v, ok := item[k].(*types.AttributeValueMemberS); ok {
		return v.Value
	}
	return ""
}
