// media-relay: allocates audio relays (simulated unstable dependency), forwards
// UDP audio between call participants, frees relays when calls end and reports
// participants whose client disappeared.
//
// main is the composition root: rules live in internal/app, AWS and network
// code in internal/adapter.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/larpspeak/media-relay/internal/adapter/dynamo"
	"github.com/larpspeak/media-relay/internal/adapter/httpapi"
	"github.com/larpspeak/media-relay/internal/adapter/messaging"
	"github.com/larpspeak/media-relay/internal/adapter/pubkey"
	"github.com/larpspeak/media-relay/internal/adapter/udp"
	"github.com/larpspeak/media-relay/internal/adapter/webrtc"
	"github.com/larpspeak/media-relay/internal/app"
	"github.com/larpspeak/media-relay/internal/config"
)

func main() {
	hostname, _ := os.Hostname()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "media-relay", "replica", hostname)
	cfg, err := config.Load()
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
	st := &dynamo.Store{DB: dynamodb.NewFromConfig(awsCfg), Cfg: cfg}
	b := messaging.New(sqs.NewFromConfig(awsCfg), log)
	r := &app.Relay{Sessions: st, Nodes: st, Prober: httpapi.Prober{}, Log: log, Cfg: cfg, ID: hostname,
		Host: advertisedHost(hostname), ProbeAddr: net.JoinHostPort(containerIP(hostname), cfg.Port)}
	// Fault injection for demos: `docker kill -s USR1 <pod>` pauses/resumes this pod's
	// pings, as if it lost its link to DynamoDB while still forwarding audio.
	usr1 := make(chan os.Signal, 1)
	signal.Notify(usr1, syscall.SIGUSR1)
	go func() {
		for range usr1 {
			r.PingsPaused.Store(!r.PingsPaused.Load())
			log.Warn("fault injection: pings toggled", "paused", r.PingsPaused.Load())
		}
	}()

	go messaging.RelayOutbox(ctx, st, b)
	var consumers sync.WaitGroup
	consumers.Go(func() { b.Consume(ctx, cfg.RelayRequests, r.OnRelayRequest) }) // competing consumers across replicas (R4)
	consumers.Go(func() { b.Consume(ctx, cfg.CallEvents, r.OnCallEvent) })
	go r.Sweep(ctx)
	go r.Failover(ctx)
	go udp.Serve(ctx, conn, r, log)

	// Browser audio: WebRTC on its own UDP port (DTLS/STUN would be ambiguous on the legacy UDP port).
	rtcConn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: cfg.WebRTCPort})
	if err != nil {
		log.Error("webrtc udp listen", "err", err)
		os.Exit(1)
	}
	sfu, err := webrtc.New(rtcConn, os.Getenv("RELAY_HOST"), r, log)
	if err != nil {
		log.Error("webrtc setup", "err", err)
		os.Exit(1)
	}
	// Call recording: Opus packets of every browser go to audit-vault as AudioChunk events,
	// outside the audio path (see app.Recorder). Off unless AUDIO_CHUNKS_QUEUE is set.
	if cfg.AudioChunksQueue != "" {
		rec := app.NewRecorder(b, cfg.AudioChunksQueue, log, cfg.AudioChunk)
		sfu.SetRecorder(rec)
		consumers.Go(func() { rec.Run(ctx) }) // on shutdown it flushes buffered audio before Wait returns
		log.Info("call recording on", "queue", cfg.AudioChunksQueue, "chunk", cfg.AudioChunk.String())
	}
	var key pubkey.Key
	go key.Load(ctx, cfg.SignalingURL, log)

	mux := http.NewServeMux()
	httpapi.Register(mux, r, hostname, sfu, key.Get)
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: mux}
	go func() { <-ctx.Done(); _ = srv.Shutdown(context.Background()); _ = conn.Close() }()
	log.Info("listening", "http", srv.Addr, "udp", cfg.UDPPort, "webrtc", cfg.WebRTCPort, "host", r.Host,
		"latency", cfg.Latency.String(), "failureRate", cfg.FailureRate, "participantTimeout", cfg.ParticipantTimeout.String())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
	consumers.Wait() // let the last long poll finish and its messages be handled (see Bus.Consume)
	log.Info("stopped")
}

// advertisedHost is where participants send audio: RELAY_HOST if set, else this container's IP.
func advertisedHost(hostname string) string {
	if h := os.Getenv("RELAY_HOST"); h != "" {
		return h
	}
	return containerIP(hostname)
}

func containerIP(hostname string) string {
	if ips, err := net.LookupHost(hostname); err == nil && len(ips) > 0 {
		return ips[0]
	}
	return hostname
}
