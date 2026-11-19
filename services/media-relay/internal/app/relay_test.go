package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/larpspeak/media-relay/internal/config"
	"github.com/larpspeak/media-relay/internal/domain"
)

// fakeStore implements SessionStore and NodeStore in memory. It records each
// write as "Method:detail" and returns errs[method] instead of writing when set.
type fakeStore struct {
	mu       sync.Mutex
	sessions map[string]domain.Session
	nodes    []domain.Node
	ops      []string
	errs     map[string]error
}

func (f *fakeStore) op(method, detail string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, method+":"+detail)
	return f.errs[method]
}

func (f *fakeStore) Get(_ context.Context, id string) (*domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[id]; ok {
		return &s, nil
	}
	return nil, nil
}
func (f *fakeStore) Allocated(context.Context) (out []domain.Session, _ error) {
	for _, s := range f.sessions {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b domain.Session) int { return strings.Compare(a.CallID, b.CallID) })
	return out, nil
}
func (f *fakeStore) Allocate(_ context.Context, _ string, s domain.Session, out domain.Outgoing) error {
	return f.op("Allocate", out.Envelope.Type)
}
func (f *fakeStore) Ack(_ context.Context, _ string, out ...domain.Outgoing) error {
	d := ""
	for _, o := range out {
		d += o.Envelope.Type
	}
	return f.op("Ack", d)
}
func (f *fakeStore) Release(_ context.Context, _, callID, _ string) error {
	return f.op("Release", callID)
}
func (f *fakeStore) Member(_ context.Context, _, callID, op, user string) error {
	return f.op("Member", op+" "+user)
}
func (f *fakeStore) Emit(_ context.Context, out domain.Outgoing) error {
	return f.op("Emit", out.Envelope.Type)
}
func (f *fakeStore) Ping(context.Context, domain.Node) error                     { return nil }
func (f *fakeStore) Nodes(context.Context) ([]domain.Node, error)                { return f.nodes, nil }
func (f *fakeStore) Lease(context.Context) (*domain.Lease, error)                { return nil, nil }
func (f *fakeStore) Claim(context.Context, string, *domain.Lease) (int64, error) { return 1, nil }
func (f *fakeStore) Renew(context.Context, string, int64) error                  { return nil }
func (f *fakeStore) ForgetNode(_ context.Context, id string)                     { _ = f.op("ForgetNode", id) }
func (f *fakeStore) MarkDead(_ context.Context, n domain.Node, leader string, term int64) error {
	return f.op("MarkDead", n.ID+" by "+leader+" term "+strconv.FormatInt(term, 10))
}
func (f *fakeStore) Move(_ context.Context, leader string, term int64, s domain.Session, to domain.Node, out domain.Outgoing) error {
	return f.op("Move", s.CallID+" to "+to.ID+" term "+strconv.FormatInt(term, 10)+" "+out.Envelope.Type)
}

type fakeProber struct{ healthy map[string]bool }

func (p fakeProber) AnswersHealth(_, id string) bool { return p.healthy[id] }
func (p fakeProber) NotifyAdopt(string, string)      {}

func newRelay(st *fakeStore) *Relay {
	if st.errs == nil {
		st.errs = map[string]error{}
	}
	if st.sessions == nil {
		st.sessions = map[string]domain.Session{}
	}
	return &Relay{Sessions: st, Nodes: st, Prober: fakeProber{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cfg: config.Config{RelayResults: "relay-results", UDPPort: 40000, ParticipantTimeout: time.Minute}, ID: "pod-a", Host: "10.0.0.1"}
}

func envelope(t *testing.T, typ string, payload map[string]any) domain.Envelope {
	t.Helper()
	e, err := domain.NewEnvelope(typ, "c1", payload)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func addr(port int) *net.UDPAddr { return &net.UDPAddr{IP: net.IPv4(10, 0, 0, 9), Port: port} }

func TestForward(t *testing.T) {
	r := newRelay(&fakeStore{})
	r.track(domain.Session{CallID: "c1", Participants: []string{"ana", "bob"}})
	ctx := context.Background()

	if got := r.Forward(ctx, "c1", "ana", addr(1)); len(got) != 0 {
		t.Fatalf("bob not seen yet, got targets %v", got)
	}
	if got := r.Forward(ctx, "c1", "bob", addr(2)); len(got) != 1 || got[0].Port != 1 {
		t.Fatalf("bob -> ana: got %v", got)
	}
	if got := r.Forward(ctx, "c1", "mallory", addr(3)); got != nil {
		t.Fatalf("intruder forwarded to %v", got)
	}
	if got := r.Forward(ctx, "c1", "ana", addr(1)); len(got) != 1 || got[0].Port != 2 {
		t.Fatalf("intruder address learned: ana -> %v", got)
	}
	if got := r.Forward(ctx, "unknown", "ana", addr(1)); got != nil {
		t.Fatalf("unknown call forwarded to %v", got)
	}
}

func TestAllocateSimulatedFailure(t *testing.T) {
	st := &fakeStore{}
	r := newRelay(st)
	r.Cfg.FailureRate = 1
	e := envelope(t, "RelayRequested", map[string]any{"callId": "c1", "participants": []string{"ana"}})
	if err := r.OnRelayRequest(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Ack:RelayFailed"}; !slices.Equal(st.ops, want) {
		t.Fatalf("ops %v, want %v", st.ops, want)
	}
}

func TestAllocateTracksSession(t *testing.T) {
	st := &fakeStore{}
	r := newRelay(st)
	e := envelope(t, "RelayRequested", map[string]any{"callId": "c1", "participants": []string{"ana"}})
	if err := r.OnRelayRequest(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Allocate:RelayAllocated"}; !slices.Equal(st.ops, want) {
		t.Fatalf("ops %v, want %v", st.ops, want)
	}
	if r.live["c1"] == nil || !r.live["c1"].participants["ana"] {
		t.Fatal("allocated session not tracked")
	}
}

func TestReleaseAlreadyReleased(t *testing.T) {
	st := &fakeStore{errs: map[string]error{"Release": domain.ErrReleased}}
	r := newRelay(st)
	r.track(domain.Session{CallID: "c1"})
	e := envelope(t, "CallEnded", map[string]any{"callId": "c1"})
	if err := r.OnCallEvent(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Release:c1", "Ack:"}; !slices.Equal(st.ops, want) {
		t.Fatalf("ops %v, want %v", st.ops, want)
	}
	if r.live["c1"] != nil {
		t.Fatal("released session still tracked")
	}
}

func TestReleaseDuplicate(t *testing.T) {
	st := &fakeStore{errs: map[string]error{"Release": domain.ErrDuplicate}}
	r := newRelay(st)
	e := envelope(t, "CallEnded", map[string]any{"callId": "c1"})
	if err := r.OnCallEvent(context.Background(), e); !errors.Is(err, domain.ErrDuplicate) {
		t.Fatalf("got %v, want ErrDuplicate", err)
	}
}

// silent returns a watch state where every node was last seen 4s ago with its current beat.
func silent(nodes ...domain.Node) map[string]pingSeen {
	seen := map[string]pingSeen{}
	for _, n := range nodes {
		seen[n.ID] = pingSeen{n.Beat, time.Now().Add(-4 * time.Second)}
	}
	return seen
}

func TestWatchNodes(t *testing.T) {
	me := domain.Node{ID: "pod-a", Beat: 7, Status: domain.NodeAlive}
	b := domain.Node{ID: "pod-b", Beat: 3, Status: domain.NodeAlive}
	ctx := context.Background()

	t.Run("silent and no health answer: dead", func(t *testing.T) {
		st := &fakeStore{nodes: []domain.Node{me, b}}
		r := newRelay(st)
		r.watchNodes(ctx, silent(me, b), 2, nil)
		if want := []string{"MarkDead:pod-b by pod-a term 2", "ForgetNode:pod-b"}; !slices.Equal(st.ops, want) {
			t.Fatalf("ops %v, want %v", st.ops, want)
		}
	})
	t.Run("silent but answers health: alive", func(t *testing.T) {
		st := &fakeStore{nodes: []domain.Node{me, b}}
		r := newRelay(st)
		r.Prober = fakeProber{healthy: map[string]bool{"pod-b": true}}
		r.watchNodes(ctx, silent(me, b), 2, nil)
		if len(st.ops) != 0 {
			t.Fatalf("ops %v, want none", st.ops)
		}
	})
	t.Run("beat moved: alive", func(t *testing.T) {
		st := &fakeStore{nodes: []domain.Node{me, b}}
		r := newRelay(st)
		seen := silent(me, b)
		seen["pod-b"] = pingSeen{2, seen["pod-b"].at}
		r.watchNodes(ctx, seen, 2, nil)
		if len(st.ops) != 0 {
			t.Fatalf("ops %v, want none", st.ops)
		}
	})
	t.Run("inherited suspect is checked at once", func(t *testing.T) {
		st := &fakeStore{nodes: []domain.Node{me, b}}
		r := newRelay(st)
		r.watchNodes(ctx, map[string]pingSeen{}, 2, &suspect{"pod-b", time.Now().Add(-4 * time.Second)})
		if want := []string{"MarkDead:pod-b by pod-a term 2", "ForgetNode:pod-b"}; !slices.Equal(st.ops, want) {
			t.Fatalf("ops %v, want %v", st.ops, want)
		}
	})
	t.Run("own link just unhealthy: no verdict", func(t *testing.T) {
		st := &fakeStore{nodes: []domain.Node{me, b}}
		r := newRelay(st)
		r.unhealthyAt.Store(time.Now().UnixNano())
		r.watchNodes(ctx, silent(me, b), 2, nil)
		if len(st.ops) != 0 {
			t.Fatalf("ops %v, want none", st.ops)
		}
	})
}

func TestRedistributeLeastConnections(t *testing.T) {
	st := &fakeStore{sessions: map[string]domain.Session{
		"c1": {CallID: "c1", Owner: "pod-dead"},
		"c2": {CallID: "c2", Owner: "pod-dead"},
		"c3": {CallID: "c3", Owner: "pod-b"},
	}}
	r := newRelay(st)
	r.ID = "pod-leader"
	alive := []domain.Node{{ID: "pod-b"}, {ID: "pod-c"}, {ID: "pod-d"}}
	r.redistribute(context.Background(), []string{"pod-dead"}, alive, 3)
	// pod-b already has 1 call; pod-c and pod-d tie at 0, so the first (pod-c) wins, then pod-d.
	want := []string{"Move:c1 to pod-c term 3 RelayMoved", "Move:c2 to pod-d term 3 RelayMoved", "ForgetNode:pod-dead"}
	if !slices.Equal(st.ops, want) {
		t.Fatalf("ops %v, want %v", st.ops, want)
	}
}

func TestRedistributeKeepsDeadNodeWhenMoveFails(t *testing.T) {
	st := &fakeStore{sessions: map[string]domain.Session{"c1": {CallID: "c1", Owner: "pod-dead"}},
		errs: map[string]error{"Move": errors.New("fenced")}}
	r := newRelay(st)
	r.redistribute(context.Background(), []string{"pod-dead"}, []domain.Node{{ID: "pod-b"}}, 3)
	if want := []string{"Move:c1 to pod-b term 3 RelayMoved"}; !slices.Equal(st.ops, want) {
		t.Fatalf("ops %v, want %v", st.ops, want)
	}
}

// Admit decides whether this pod accepts a browser's WebRTC offer. It reads
// DynamoDB right away, because the offer can arrive before /adopt or before the
// ParticipantJoined event reached this pod.
func TestAdmit(t *testing.T) {
	ctx := context.Background()
	st := &fakeStore{sessions: map[string]domain.Session{
		"other":    {CallID: "other", State: domain.StateAllocated, Owner: "pod-b", Participants: []string{"ana"}},
		"released": {CallID: "released", State: domain.StateReleased, Owner: "pod-a", Participants: []string{"ana"}},
		"mine":     {CallID: "mine", State: domain.StateAllocated, Owner: "pod-a", Participants: []string{"ana"}},
		"live":     {CallID: "live", State: domain.StateAllocated, Owner: "pod-a", Participants: []string{"ana", "bob"}},
	}}
	r := newRelay(st)
	r.track(domain.Session{CallID: "live", Participants: []string{"ana"}}) // bob joined via another replica: not here yet

	for _, c := range []struct {
		call, user string
		want       error
	}{
		{"missing", "ana", domain.ErrNotOwner},  // no session at all
		{"other", "ana", domain.ErrNotOwner},    // owned by another pod (e.g. moved by failover)
		{"released", "ana", domain.ErrNotOwner}, // call already over
		{"mine", "mallory", domain.ErrNotIn},    // this pod's call, but not a participant
		{"mine", "ana", nil},                    // this pod's call, not loaded yet: picked up
		{"live", "ana", nil},                    // already live here
		{"live", "bob", nil},                    // live, participant only in DynamoDB so far
	} {
		if err := r.Admit(ctx, c.call, c.user); !errors.Is(err, c.want) {
			t.Errorf("Admit(%s, %s) = %v, want %v", c.call, c.user, err, c.want)
		}
	}
	if !r.Touch("mine", "ana") {
		t.Error("admitted call was not picked up")
	}
	if !r.Touch("live", "bob") {
		t.Error("participant read from DynamoDB was not added to the live session")
	}
	if r.Touch("mine", "mallory") || r.Touch("other", "ana") {
		t.Error("refused offers must not leave the user able to send audio")
	}
}

// Touch authorizes every WebRTC packet and refreshes the participant's last
// sign of life, which the participant timeout (Sweep) relies on.
func TestTouch(t *testing.T) {
	r := newRelay(&fakeStore{})
	if r.Touch("c1", "ana") {
		t.Fatal("call not relayed here was accepted")
	}
	r.track(domain.Session{CallID: "c1", Participants: []string{"ana", "bob"}})
	old := time.Now().Add(-time.Hour)
	r.live["c1"].lastSeen["ana"] = old

	if !r.Touch("c1", "ana") {
		t.Fatal("participant refused")
	}
	if !r.live["c1"].lastSeen["ana"].After(old) {
		t.Fatal("Touch did not refresh lastSeen")
	}
	if r.Touch("c1", "mallory") {
		t.Fatal("non-participant accepted")
	}

	r.live["c1"].remove("bob") // ParticipantLeft applied
	if r.Touch("c1", "bob") {
		t.Fatal("user who left still accepted")
	}
	r.untrack("c1") // relay released (call ended) or call moved away
	if r.Touch("c1", "ana") {
		t.Fatal("released call still accepted")
	}
}
