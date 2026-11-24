package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/larpspeak/media-relay/internal/domain"
)

// ChunkSink is where finished audio chunks go (an SQS queue in production).
type ChunkSink interface {
	Send(ctx context.Context, queue, body string) error
}

type audioFrame struct {
	TS   uint32 `json:"ts"`   // RTP timestamp (48 kHz clock)
	Data []byte `json:"data"` // one Opus packet; encoded as base64 by encoding/json
}

// audioChunk is the payload of the AudioChunk event (contracts/events/AudioChunk.v1.schema.json).
type audioChunk struct {
	CallID     string       `json:"callId"`
	UserID     string       `json:"userId"`
	StartMs    int64        `json:"startMs"` // wall clock of the first frame on this pod
	DurationMs int64        `json:"durationMs"`
	Frames     []audioFrame `json:"frames"`
}

type trackKey struct{ call, user string }

type track struct {
	start, last time.Time
	bytes       int
	frames      []audioFrame
}

// Recorder cuts the Opus audio of each participant into chunks of a few seconds
// and publishes each chunk as an AudioChunk event; audit-vault encrypts and
// stores them. Recording must never hurt the call: Record only appends to memory,
// publishing runs in Run, and when the broker is slow or down chunks wait in a
// bounded queue and the oldest news is dropped (and counted) once it is full.
type Recorder struct {
	Sink      ChunkSink
	Queue     string
	Log       *slog.Logger
	Interval  time.Duration    // chunk length
	MaxBytes  int              // cut earlier when a chunk reaches this many payload bytes
	Now       func() time.Time // injectable for tests
	RetryBase time.Duration    // first wait between publish attempts, doubles each time
	MaxTries  int

	mu      sync.Mutex
	tracks  map[trackKey]*track
	out     chan string
	dropped atomic.Int64
}

// NewRecorder returns a recorder with production defaults. It does nothing until Run is started.
func NewRecorder(sink ChunkSink, queue string, log *slog.Logger, interval time.Duration) *Recorder {
	return &Recorder{
		Sink: sink, Queue: queue, Log: log, Interval: interval,
		// 96 KiB of Opus becomes ~128 KiB of base64: under the 256 KiB SQS message limit
		MaxBytes: 96 << 10, Now: time.Now, RetryBase: 250 * time.Millisecond, MaxTries: 6,
		tracks: map[trackKey]*track{}, out: make(chan string, 512),
	}
}

// Dropped is how many chunks were lost because the publish queue was full.
func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

// Record adds one Opus packet of userID in callID. Safe on a nil Recorder (recording off).
func (r *Recorder) Record(callID, userID string, ts uint32, payload []byte) {
	if r == nil || len(payload) == 0 {
		return
	}
	now := r.Now()
	key := trackKey{callID, userID}
	r.mu.Lock()
	t := r.tracks[key]
	if t == nil {
		t = &track{start: now}
		r.tracks[key] = t
	}
	t.frames = append(t.frames, audioFrame{TS: ts, Data: bytes.Clone(payload)})
	t.bytes += len(payload)
	t.last = now
	var cut *track
	if now.Sub(t.start) >= r.Interval || t.bytes >= r.MaxBytes {
		delete(r.tracks, key)
		cut = t
	}
	r.mu.Unlock()
	if cut != nil {
		r.enqueue(key, cut)
	}
}

// Flush publishes what is buffered for a participant (they left, or their peer was replaced).
func (r *Recorder) Flush(callID, userID string) {
	if r == nil {
		return
	}
	key := trackKey{callID, userID}
	r.mu.Lock()
	t := r.tracks[key]
	delete(r.tracks, key)
	r.mu.Unlock()
	if t != nil {
		r.enqueue(key, t)
	}
}

// flushIdle cuts tracks that stopped receiving packets (safety net for peers that vanished).
func (r *Recorder) flushIdle(all bool) {
	now := r.Now()
	r.mu.Lock()
	cut := map[trackKey]*track{}
	for k, t := range r.tracks {
		if all || now.Sub(t.last) >= r.Interval {
			cut[k] = t
			delete(r.tracks, k)
		}
	}
	r.mu.Unlock()
	for k, t := range cut {
		r.enqueue(k, t)
	}
}

func (r *Recorder) enqueue(key trackKey, t *track) {
	env, err := domain.NewEnvelope("AudioChunk", key.call, audioChunk{
		CallID: key.call, UserID: key.user, StartMs: t.start.UnixMilli(),
		DurationMs: t.last.Sub(t.start).Milliseconds(), Frames: t.frames,
	})
	if err != nil {
		r.Log.Error("audio chunk not built", "correlationId", key.call, "userId", key.user, "err", err)
		return
	}
	body, err := json.Marshal(env)
	if err != nil {
		r.Log.Error("audio chunk not built", "correlationId", key.call, "userId", key.user, "err", err)
		return
	}
	select {
	case r.out <- string(body):
	default: // never block the audio path
		n := r.dropped.Add(1)
		r.Log.Warn("audio chunk dropped: publish queue full", "correlationId", key.call, "userId", key.user, "dropped", n)
	}
}

// Run publishes chunks until ctx ends, then flushes what is buffered and gives the
// broker a few seconds to take the rest. It returns when everything was tried.
func (r *Recorder) Run(ctx context.Context) {
	if r == nil {
		return
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case body := <-r.out:
			r.send(ctx, body, r.MaxTries)
		case <-tick.C:
			r.flushIdle(false)
		case <-ctx.Done():
			r.flushIdle(true)
			r.drain()
			return
		}
	}
}

func (r *Recorder) drain() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		select {
		case body := <-r.out:
			r.send(ctx, body, 1)
		default:
			return
		}
	}
}

// send publishes one chunk, retrying with backoff (R6: the broker may be down for a while).
func (r *Recorder) send(ctx context.Context, body string, tries int) {
	wait := r.RetryBase
	for i := 1; ; i++ {
		err := r.Sink.Send(ctx, r.Queue, body)
		if err == nil {
			return
		}
		if i >= tries || ctx.Err() != nil {
			r.Log.Error("audio chunk lost: publish failed", "attempts", i, "err", err)
			return
		}
		r.Log.Warn("audio chunk publish failed, retrying", "attempt", i, "err", err)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
		}
		wait = min(wait*2, 5*time.Second)
	}
}
