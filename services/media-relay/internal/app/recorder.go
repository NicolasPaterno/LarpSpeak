package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"math"
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

// cutChunk is a finished track waiting for the sender goroutine.
type cutChunk struct {
	key trackKey
	t   *track
}

type track struct {
	start, last time.Time
	bytes       int
	frames      []audioFrame
}

// Recorder cuts the Opus audio of each participant into chunks of a few seconds
// and publishes each chunk as an AudioChunk event; audit-vault encrypts and
// stores them. Recording must never hurt the call: Record only appends to memory,
// publishing runs in Run, and when the broker is slow or down chunks wait in a
// bounded queue; once it is full the incoming (newest) chunk is dropped and counted.
type Recorder struct {
	Sink      ChunkSink
	Queue     string
	Log       *slog.Logger
	Interval  time.Duration    // chunk length
	MaxBytes  int              // cut earlier when a chunk reaches this many payload bytes
	Now       func() time.Time // injectable for tests
	RetryBase time.Duration    // first wait between publish attempts, doubles each time
	MaxTries  int
	// DrainTimeout bounds the shutdown flush: pending chunks are retried until it expires.
	DrainTimeout time.Duration

	mu       sync.Mutex
	tracks   map[trackKey]*track
	stopping bool // shutdown began: new packets are not buffered anymore (guarded by mu)
	done     bool // Run returned: nobody reads out anymore (guarded by mu)
	out      chan cutChunk
	dropped  atomic.Int64
}

// NewRecorder returns a recorder with production defaults. It does nothing until Run is started.
func NewRecorder(sink ChunkSink, queue string, log *slog.Logger, interval time.Duration) *Recorder {
	return &Recorder{
		Sink: sink, Queue: queue, Log: log, Interval: interval,
		// 96 KiB of Opus becomes ~128 KiB of base64: under the 256 KiB SQS message limit
		MaxBytes: 96 << 10, Now: time.Now, RetryBase: 250 * time.Millisecond, MaxTries: 6,
		DrainTimeout: 5 * time.Second,
		tracks:       map[trackKey]*track{}, out: make(chan cutChunk, 512),
	}
}

// Dropped is how many chunks were lost because the publish queue was full, the
// shutdown flush ran out of time, or a chunk was cut after Run had returned.
func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

// Record adds one Opus packet of userID in callID. Safe on a nil Recorder (recording off).
func (r *Recorder) Record(callID, userID string, ts uint32, payload []byte) {
	if r == nil || len(payload) == 0 {
		return
	}
	now := r.Now()
	key := trackKey{callID, userID}
	r.mu.Lock()
	if r.stopping { // shutting down: the last flush already ran, audio from now on is not recorded
		r.mu.Unlock()
		return
	}
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

// enqueue hands a finished chunk to the sender goroutine. It never blocks and never
// encodes: building the envelope and marshalling it happen in Run, off the audio path.
func (r *Recorder) enqueue(key trackKey, t *track) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		n := r.dropped.Add(1)
		r.Log.Warn("audio chunk dropped: recorder stopped", "correlationId", key.call, "userId", key.user, "dropped", n)
		return
	}
	select {
	case r.out <- cutChunk{key, t}:
	default: // never block the audio path
		n := r.dropped.Add(1)
		r.Log.Warn("audio chunk dropped: publish queue full", "correlationId", key.call, "userId", key.user, "dropped", n)
	}
}

// encode builds the AudioChunk envelope of a finished chunk.
func encode(c cutChunk) (string, error) {
	env, err := domain.NewEnvelope("AudioChunk", c.key.call, audioChunk{
		CallID: c.key.call, UserID: c.key.user, StartMs: c.t.start.UnixMilli(),
		DurationMs: c.t.last.Sub(c.t.start).Milliseconds(), Frames: c.t.frames,
	})
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(env)
	return string(body), err
}

// Run publishes chunks until ctx ends, then flushes what is buffered and gives the
// broker DrainTimeout to take the rest. It returns when everything was tried.
// Idle tracks are cut by their own ticker so a publish stuck in retries does not
// hold back the safety net.
func (r *Recorder) Run(ctx context.Context) {
	if r == nil {
		return
	}
	tickerDone := make(chan struct{})
	go func() {
		defer close(tickerDone)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				r.flushIdle(false)
			case <-ctx.Done():
				return
			}
		}
	}()
	var unsent []string // chunks whose retries were cut short by shutdown
	for {
		select {
		case c := <-r.out:
			body, err := encode(c)
			if err != nil {
				r.Log.Error("audio chunk not built", "correlationId", c.key.call, "userId", c.key.user, "err", err)
				continue
			}
			if r.send(ctx, body, r.MaxTries) != nil && ctx.Err() != nil {
				unsent = append(unsent, body)
			}
		case <-ctx.Done():
			<-tickerDone
			r.mu.Lock()
			r.stopping = true
			r.mu.Unlock()
			r.flushIdle(true)
			r.drain(unsent)
			r.mu.Lock()
			r.done = true
			if n := int64(len(r.out)); n > 0 { // cut while the drain was finishing: never read
				r.dropped.Add(n)
				r.Log.Error("audio chunks lost at shutdown: cut after the final flush", "lost", n, "dropped", r.dropped.Load())
			}
			r.mu.Unlock()
			return
		}
	}
}

// drain publishes what is left at shutdown, retrying each chunk with backoff until
// the shared DrainTimeout expires; whatever is still unsent then counts as Dropped.
func (r *Recorder) drain(bodies []string) {
	ctx, cancel := context.WithTimeout(context.Background(), r.DrainTimeout)
	defer cancel()
	for {
		select {
		case c := <-r.out:
			body, err := encode(c)
			if err != nil {
				r.Log.Error("audio chunk not built", "correlationId", c.key.call, "userId", c.key.user, "err", err)
				continue
			}
			bodies = append(bodies, body)
			continue
		default:
		}
		if len(bodies) == 0 {
			return
		}
		if r.send(ctx, bodies[0], math.MaxInt) != nil {
			n := int64(len(bodies)) // what is still in r.out is counted by Run after drain
			r.dropped.Add(n)
			r.Log.Error("audio chunks lost at shutdown: publish deadline expired", "lost", n, "dropped", r.dropped.Load())
			return
		}
		bodies = bodies[1:]
	}
}

// send publishes one chunk, retrying with backoff (R6: the broker may be down for a while).
func (r *Recorder) send(ctx context.Context, body string, tries int) error {
	wait := r.RetryBase
	for i := 1; ; i++ {
		err := r.Sink.Send(ctx, r.Queue, body)
		if err == nil {
			return nil
		}
		if i >= tries || ctx.Err() != nil {
			r.Log.Error("audio chunk not published", "attempts", i, "err", err)
			return err
		}
		r.Log.Warn("audio chunk publish failed, retrying", "attempt", i, "err", err)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
		}
		wait = min(wait*2, 5*time.Second)
	}
}
