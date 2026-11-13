// call-signaling: call API (202 + polling), room seat control, call-setup saga,
// transactional outbox and user notifications.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"uuid"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "call-signaling")
	cfg, err := loadConfig()
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
	st := &store{db: dynamodb.NewFromConfig(awsCfg), cfg: cfg}
	b := &bus{sqs: sqs.NewFromConfig(awsCfg), sns: sns.NewFromConfig(awsCfg), log: log, urls: map[string]string{}}
	h := newHub()
	sg := &saga{st: st, hub: h, log: log, cfg: cfg, nowFn: time.Now}

	go relayOutbox(ctx, st, b, cfg.TopicArn)
	go b.consume(ctx, cfg.RelayResults, sg.onRelayResult)
	go sg.sweepTimeouts(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "call-signaling"})
	})
	mux.HandleFunc("POST /calls", sg.handleCreate)
	mux.HandleFunc("GET /calls/{id}", sg.handleGet)
	mux.HandleFunc("POST /calls/{id}/end", sg.handleEnd)
	mux.HandleFunc("GET /users/{userId}/notifications", h.serve)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: mux}
	go func() { <-ctx.Done(); _ = srv.Shutdown(context.Background()) }()
	log.Info("listening", "addr", srv.Addr, "roomCapacity", cfg.RoomCapacity, "relayTimeout", cfg.RelayTimeout.String())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

type createCallRequest struct {
	CallerID  string   `json:"callerId"`
	CalleeIDs []string `json:"calleeIds"`
	RoomID    string   `json:"roomId"` // empty = 1:1 call in a private room
}

// handleCreate is the R3 async entry point: validate, run saga step 1, answer 202.
func (sg *saga) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createCallRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil || req.CallerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "callerId is required"})
		return
	}
	c := call{
		ID: uuid.New().String(), CallerID: req.CallerID, Status: statusPending, CreatedAt: sg.nowFn().UnixMilli(),
		Participants: append([]string{req.CallerID}, req.CalleeIDs...),
	}
	c.Seats = len(c.Participants)
	switch {
	case req.RoomID != "":
		c.Kind, c.RoomID = "group", req.RoomID
	case len(req.CalleeIDs) == 1:
		c.Kind, c.RoomID = "direct", "direct-"+c.ID
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "1:1 call needs exactly one callee; group call needs roomId"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	err := sg.start(ctx, c)
	if errors.Is(err, errRoomFull) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "room full", "roomId": c.RoomID})
		return
	}
	if err != nil {
		sg.log.Error("create call failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "try again"})
		return
	}
	sg.log.Info("call requested", "correlationId", c.ID, "roomId", c.RoomID, "seats", c.Seats)
	w.Header().Set("Location", "/calls/"+c.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"callId": c.ID, "status": c.Status, "statusUrl": "/calls/" + c.ID})
}

func (sg *saga) handleGet(w http.ResponseWriter, r *http.Request) {
	c, err := sg.st.getCall(r.Context(), r.PathValue("id"))
	switch {
	case err != nil:
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "try again"})
	case c == nil:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "call not found"})
	default:
		writeJSON(w, http.StatusOK, c)
	}
}

func (sg *saga) handleEnd(w http.ResponseWriter, r *http.Request) {
	c, err := sg.st.getCall(r.Context(), r.PathValue("id"))
	if err != nil || c == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "call not found"})
		return
	}
	if c.Status != statusActive {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call is " + c.Status})
		return
	}
	if err := sg.end(r.Context(), c, "hangup"); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "call state changed, retry"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"callId": c.ID, "status": statusEnded})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func attr(item map[string]types.AttributeValue, k string) string {
	if v, ok := item[k].(*types.AttributeValueMemberS); ok {
		return v.Value
	}
	return ""
}
