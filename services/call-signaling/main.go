// call-signaling: call API (202 + polling), room seat control, call-setup saga,
// transactional outbox and user notifications.
//
// main is the composition root: rules live in internal/app, AWS and network
// code in internal/adapter.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/larpspeak/call-signaling/internal/adapter/dynamo"
	"github.com/larpspeak/call-signaling/internal/adapter/httpapi"
	"github.com/larpspeak/call-signaling/internal/adapter/messaging"
	"github.com/larpspeak/call-signaling/internal/adapter/sse"
	"github.com/larpspeak/call-signaling/internal/app"
	"github.com/larpspeak/call-signaling/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "call-signaling")
	cfg, err := config.Load()
	if err != nil {
		log.Error("bad config", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Endpoint and credentials come from AWS_ENDPOINT_URL / AWS_* env vars.
	// The client timeout (> 10s SQS long poll) turns a frozen broker into errors we log and retry (R6).
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithHTTPClient(&http.Client{Timeout: 15 * time.Second}),
		awsconfig.WithRetryMaxAttempts(1)) // our loops retry with backoff and log each attempt
	if err != nil {
		log.Error("aws config", "err", err)
		os.Exit(1)
	}
	st := &dynamo.Store{DB: dynamodb.NewFromConfig(awsCfg), Cfg: cfg}
	b := messaging.New(sqs.NewFromConfig(awsCfg), sns.NewFromConfig(awsCfg), log)
	h := sse.New()
	svc := &app.Service{St: st, Hub: h, Log: log, Cfg: cfg, NowFn: time.Now}
	h.Replay = svc.ReplayInvites

	go messaging.RelayOutbox(ctx, st, b, cfg.TopicArn)
	go b.Consume(ctx, cfg.RelayResults, svc.OnRelayResult)
	go svc.SweepTimeouts(ctx)

	mux := http.NewServeMux()
	(&httpapi.Handler{Svc: svc}).Register(mux)
	mux.HandleFunc("GET /users/{userId}/notifications", h.Serve)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: mux}
	go func() { <-ctx.Done(); _ = srv.Shutdown(context.Background()) }()
	log.Info("listening", "addr", srv.Addr, "maxParticipants", cfg.MaxParticipants, "relayTimeout", cfg.RelayTimeout.String())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
