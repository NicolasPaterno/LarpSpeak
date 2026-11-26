package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/larpspeak/media-relay/internal/domain"
)

type fakeSink struct {
	mu     sync.Mutex
	failN  int // fail this many sends first
	bodies chan string
	calls  int
}

func (f *fakeSink) Send(_ context.Context, queue, body string) error {
	f.mu.Lock()
	f.calls++
	if f.failN > 0 {
		f.failN--
		f.mu.Unlock()
		return errors.New("broker down")
	}
	f.mu.Unlock()
	f.bodies <- queue + "\x00" + body
	return nil
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func newTestRecorder(sink ChunkSink) (*Recorder, *clock) {
	c := &clock{t: time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)}
	r := NewRecorder(sink, "audio-chunks", slog.New(slog.NewTextHandler(io.Discard, nil)), 5*time.Second)
	r.Now = c.now
	r.RetryBase = time.Millisecond
	return r, c
}

func recv(t *testing.T, f *fakeSink) (queue string, env domain.Envelope, ch audioChunk) {
	t.Helper()
	select {
	case s := <-f.bodies:
		for i := range s {
			if s[i] == 0 {
				queue = s[:i]
				s = s[i+1:]
				break
			}
		}
		var err error
		if env, err = domain.ParseEnvelope(s); err != nil {
			t.Fatalf("published envelope is invalid: %v", err)
		}
		if err = json.Unmarshal(env.Payload, &ch); err != nil {
			t.Fatalf("payload: %v", err)
		}
		return
	case <-time.After(2 * time.Second):
		t.Fatal("no chunk published")
	}
	return
}

func TestChunkIsCutAfterTheInterval(t *testing.T) {
	sink := &fakeSink{bodies: make(chan string, 8)}
	r, c := newTestRecorder(sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	r.Record("call1", "ana", 100, []byte{1, 2, 3})
	c.add(2 * time.Second)
	r.Record("call1", "ana", 1060, []byte{4, 5})
	select {
	case <-sink.bodies:
		t.Fatal("chunk published before the interval")
	case <-time.After(50 * time.Millisecond):
	}
	c.add(3 * time.Second) // 5s since the first frame
	r.Record("call1", "ana", 2020, []byte{6})

	q, env, ch := recv(t, sink)
	if q != "audio-chunks" || env.Type != "AudioChunk" || env.CorrelationID != "call1" || env.Version != 1 {
		t.Fatalf("bad envelope: queue=%q %+v", q, env)
	}
	if ch.CallID != "call1" || ch.UserID != "ana" || len(ch.Frames) != 3 || ch.DurationMs != 5000 {
		t.Fatalf("bad chunk: %+v", ch)
	}
	if ch.StartMs != time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("startMs = %d", ch.StartMs)
	}
	if ch.Frames[0].TS != 100 || string(ch.Frames[1].Data) != "\x04\x05" {
		t.Fatalf("frames lost data: %+v", ch.Frames)
	}
}

func TestChunkIsCutEarlyWhenFull(t *testing.T) {
	sink := &fakeSink{bodies: make(chan string, 8)}
	r, _ := newTestRecorder(sink)
	r.MaxBytes = 10
	go r.Run(t.Context())
	r.Record("c", "ana", 1, make([]byte, 6))
	r.Record("c", "ana", 2, make([]byte, 6))
	_, _, ch := recv(t, sink)
	if len(ch.Frames) != 2 {
		t.Fatalf("frames = %d, want 2", len(ch.Frames))
	}
}

func TestParticipantsAreRecordedSeparately(t *testing.T) {
	sink := &fakeSink{bodies: make(chan string, 8)}
	r, _ := newTestRecorder(sink)
	go r.Run(t.Context())
	r.Record("c", "ana", 1, []byte{1})
	r.Record("c", "bob", 1, []byte{2})
	r.Flush("c", "ana")
	r.Flush("c", "bob")
	got := map[string]byte{}
	for range 2 {
		_, _, ch := recv(t, sink)
		got[ch.UserID] = ch.Frames[0].Data[0]
	}
	if got["ana"] != 1 || got["bob"] != 2 {
		t.Fatalf("tracks mixed up: %v", got)
	}
}

func TestFlushPublishesTheTail(t *testing.T) {
	sink := &fakeSink{bodies: make(chan string, 8)}
	r, _ := newTestRecorder(sink)
	go r.Run(t.Context())
	r.Record("c", "ana", 1, []byte{9})
	r.Flush("c", "ana")
	r.Flush("c", "ana") // nothing left: no second chunk
	_, _, ch := recv(t, sink)
	if len(ch.Frames) != 1 {
		t.Fatalf("frames = %d", len(ch.Frames))
	}
	select {
	case <-sink.bodies:
		t.Fatal("empty chunk published")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPublishIsRetriedWhileTheBrokerIsDown(t *testing.T) {
	sink := &fakeSink{bodies: make(chan string, 8), failN: 3} // R6
	r, _ := newTestRecorder(sink)
	go r.Run(t.Context())
	r.Record("c", "ana", 1, []byte{1})
	r.Flush("c", "ana")
	recv(t, sink)
	if sink.calls != 4 {
		t.Fatalf("sends = %d, want 3 failures + 1 success", sink.calls)
	}
}

func TestRecordNeverBlocksWhenTheQueueIsFull(t *testing.T) {
	sink := &fakeSink{bodies: make(chan string, 8)}
	r, _ := newTestRecorder(sink) // Run is NOT started: nothing drains the queue
	done := make(chan struct{})
	go func() {
		for i := range 600 {
			r.Record("c", "ana", uint32(i), []byte{1})
			r.Flush("c", "ana")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Record blocked the audio path")
	}
	if r.Dropped() != 600-512 {
		t.Fatalf("dropped = %d, want %d", r.Dropped(), 600-512)
	}
}

func TestShutdownFlushesBufferedAudio(t *testing.T) {
	sink := &fakeSink{bodies: make(chan string, 8)}
	r, _ := newTestRecorder(sink)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() { r.Run(ctx); close(finished) }()
	r.Record("c", "ana", 1, []byte{1})
	cancel()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
	recv(t, sink)
}

func TestNilRecorderIsOff(t *testing.T) {
	var r *Recorder
	r.Record("c", "ana", 1, []byte{1})
	r.Flush("c", "ana")
	r.Run(context.Background())
}

func TestRecordOnlyHandsTheChunkOver(t *testing.T) {
	sink := &fakeSink{bodies: make(chan string, 8)}
	r, _ := newTestRecorder(sink) // Run is NOT started
	r.MaxBytes = 1
	r.Record("c", "ana", 1, []byte{1, 2}) // cut on the audio path
	if sink.calls != 0 {
		t.Fatalf("Record published inline: %d sends", sink.calls)
	}
	select {
	case c := <-r.out: // raw frames, not an encoded body
		if c.key.user != "ana" || len(c.t.frames) != 1 {
			t.Fatalf("bad hand-over: %+v", c)
		}
	default:
		t.Fatal("chunk not handed to the sender")
	}
}

// blockingSink holds the first Send until release is closed.
type blockingSink struct {
	fakeSink
	once    sync.Once
	release chan struct{}
}

func (b *blockingSink) Send(ctx context.Context, queue, body string) error {
	first := false
	b.once.Do(func() { first = true })
	if first {
		<-b.release
	}
	return b.fakeSink.Send(ctx, queue, body)
}

func TestIdleTracksAreCutWhileASendIsStuck(t *testing.T) {
	sink := &blockingSink{fakeSink: fakeSink{bodies: make(chan string, 8)}, release: make(chan struct{})}
	r, c := newTestRecorder(sink)
	go r.Run(t.Context())
	r.Record("c", "ana", 1, []byte{1})
	r.Flush("c", "ana") // the sender now blocks on this chunk
	r.Record("c", "bob", 1, []byte{2})
	c.add(5 * time.Second) // bob is idle
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.mu.Lock()
		n := len(r.tracks)
		r.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			close(sink.release)
			t.Fatal("idle track not cut while a send was blocked")
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(sink.release)
	got := map[string]bool{}
	for range 2 {
		_, _, ch := recv(t, &sink.fakeSink)
		got[ch.UserID] = true
	}
	if !got["ana"] || !got["bob"] {
		t.Fatalf("published %v", got)
	}
}

func TestShutdownFlushRetriesUntilTheDeadline(t *testing.T) {
	sink := &fakeSink{bodies: make(chan string, 8), failN: 4}
	r, _ := newTestRecorder(sink)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // shutdown right away: everything goes through the drain
	r.Record("c", "ana", 1, []byte{1})
	r.Run(ctx)
	recv(t, sink)
	if sink.calls != 5 || r.Dropped() != 0 {
		t.Fatalf("sends = %d dropped = %d, want 5 and 0", sink.calls, r.Dropped())
	}
}

func TestShutdownFlushCountsWhatIsLeftAsDropped(t *testing.T) {
	sink := &fakeSink{bodies: make(chan string, 8), failN: 1 << 30} // broker never comes back
	r, _ := newTestRecorder(sink)
	r.DrainTimeout = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.Record("c", "ana", 1, []byte{1})
	r.Record("c", "bob", 1, []byte{2})
	start := time.Now()
	r.Run(ctx)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("drain overran its deadline: %v", d)
	}
	if sink.calls < 2 || r.Dropped() != 2 {
		t.Fatalf("sends = %d dropped = %d, want retries and 2 dropped", sink.calls, r.Dropped())
	}
}

func TestAudioAfterShutdownIsNotBufferedAndLateChunksAreCounted(t *testing.T) {
	sink := &fakeSink{bodies: make(chan string, 8)}
	r, _ := newTestRecorder(sink)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.Run(ctx) // stopped: nobody reads the queue anymore
	r.Record("c", "ana", 1, []byte{1})
	r.mu.Lock()
	n := len(r.tracks)
	r.mu.Unlock()
	if n != 0 {
		t.Fatalf("audio buffered after shutdown: %d tracks", n)
	}
	r.enqueue(trackKey{"c", "bob"}, &track{frames: []audioFrame{{TS: 1, Data: []byte{2}}}}) // a cut racing the stop
	if r.Dropped() != 1 || len(r.out) != 0 {
		t.Fatalf("dropped = %d queued = %d, want 1 and 0", r.Dropped(), len(r.out))
	}
}
