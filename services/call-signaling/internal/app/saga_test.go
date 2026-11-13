package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/larpspeak/call-signaling/internal/config"
	"github.com/larpspeak/call-signaling/internal/domain"
)

// fakeStore keeps calls in memory, records each write and its outgoing
// message types, and returns errs[method] instead of writing when set.
type fakeStore struct {
	calls map[string]*domain.Call
	users map[string]string
	ops   []string // "Method:Type,Type"
	errs  map[string]error
	seen  map[string]bool // processed msgIDs
}

func newFake(calls ...domain.Call) *fakeStore {
	f := &fakeStore{calls: map[string]*domain.Call{}, users: map[string]string{}, errs: map[string]error{}, seen: map[string]bool{}}
	for _, c := range calls {
		f.calls[c.ID] = &c
		for _, p := range c.Participants {
			f.users[p] = c.ID
		}
	}
	return f
}

func (f *fakeStore) write(method, msgID string, out ...domain.Outgoing) error {
	var types []string
	for _, o := range out {
		types = append(types, o.Envelope.Type)
	}
	f.ops = append(f.ops, method+":"+strings.Join(types, ","))
	if msgID != "" && f.seen[msgID] {
		return domain.ErrDuplicate
	}
	if err := f.errs[method]; err != nil {
		return err
	}
	if msgID != "" {
		f.seen[msgID] = true
	}
	return nil
}

func (f *fakeStore) Get(_ context.Context, id string) (*domain.Call, error) {
	c, ok := f.calls[id]
	if !ok {
		return nil, nil
	}
	cp := *c
	cp.Participants = slices.Clone(c.Participants)
	return &cp, nil
}
func (f *fakeStore) Pending(context.Context) (out []domain.Call, _ error) {
	for _, c := range f.calls {
		if c.Status == domain.StatusPending {
			out = append(out, *c)
		}
	}
	return out, nil
}
func (f *fakeStore) ActiveInvites(context.Context, string) ([]domain.Call, error) { return nil, nil }
func (f *fakeStore) CurrentCall(_ context.Context, u string) (string, error)      { return f.users[u], nil }
func (f *fakeStore) ForgetUser(_ context.Context, u, _ string)                    { delete(f.users, u) }
func (f *fakeStore) AddInvite(_ context.Context, id, u string) error {
	f.calls[id].Invited = append(f.calls[id].Invited, u)
	return nil
}
func (f *fakeStore) Create(_ context.Context, c domain.Call, out domain.Outgoing) error {
	if err := f.write("Create", "", out); err != nil {
		return err
	}
	f.calls[c.ID], f.users[c.CallerID] = &c, c.ID
	return nil
}
func (f *fakeStore) Activate(_ context.Context, msgID string, c domain.Call, out domain.Outgoing) error {
	if err := f.write("Activate", msgID, out); err != nil {
		return err
	}
	c.Status = domain.StatusActive
	f.calls[c.ID] = &c
	return nil
}
func (f *fakeStore) MoveRelay(_ context.Context, msgID, id, host string, port int) error {
	return f.write("MoveRelay", msgID)
}
func (f *fakeStore) Fail(_ context.Context, msgID, id, _ string, out domain.Outgoing) error {
	if f.calls[id].Status != domain.StatusPending {
		f.errs["Fail"] = domain.ErrStateChanged
	}
	if err := f.write("Fail", msgID, out); err != nil {
		return err
	}
	f.calls[id].Status, f.calls[id].Taken = domain.StatusFailed, 0
	return nil
}
func (f *fakeStore) Join(_ context.Context, id, user string, out domain.Outgoing) error {
	if err := f.write("Join", "", out); err != nil {
		return err
	}
	c := f.calls[id]
	c.Participants, c.Taken = append(c.Participants, user), c.Taken+1
	f.users[user] = id
	return nil
}
func (f *fakeStore) Leave(_ context.Context, msgID string, c *domain.Call, user string, last bool, _ int64, out ...domain.Outgoing) error {
	if err := f.write("Leave", msgID, out...); err != nil {
		return err
	}
	s := f.calls[c.ID]
	s.Participants, s.Taken = slices.DeleteFunc(s.Participants, func(p string) bool { return p == user }), s.Taken-1
	if last {
		s.Status = domain.StatusEnded
	}
	delete(f.users, user)
	return nil
}
func (f *fakeStore) Ack(_ context.Context, msgID string, out ...domain.Outgoing) error {
	return f.write("Ack", msgID, out...)
}

type fakeHub struct{ events []string } // "user:event"

func (h *fakeHub) Notify(user, event string, _ any) { h.events = append(h.events, user+":"+event) }

func newService(st *fakeStore) (*Service, *fakeHub) {
	h := &fakeHub{}
	return &Service{St: st, Hub: h, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cfg:   config.Config{MaxParticipants: 10, RelayTimeout: 3 * time.Second, RelayRequests: "relay-requests"},
		NowFn: time.Now}, h
}

func result(t *testing.T, typ, msgID string, payload map[string]any) domain.Envelope {
	t.Helper()
	e, err := domain.NewEnvelope(typ, "c1", payload)
	if err != nil {
		t.Fatal(err)
	}
	e.MessageID = msgID
	return e
}

func pending() domain.Call {
	return domain.Call{ID: "c1", CallerID: "ana", Invited: []string{"bob"}, Participants: []string{"ana"},
		Taken: 1, Capacity: 2, Status: domain.StatusPending, CreatedAt: time.Now().UnixMilli()}
}

func active(users ...string) domain.Call {
	return domain.Call{ID: "c1", CallerID: users[0], Participants: users, Taken: len(users), Capacity: 3, Status: domain.StatusActive}
}

func TestCreateThenActivate(t *testing.T) {
	st := newFake()
	sg, hub := newService(st)
	ctx := context.Background()
	if err := sg.Start(ctx, pending()); err != nil {
		t.Fatal(err)
	}
	e := result(t, "RelayAllocated", "m1", map[string]any{"callId": "c1", "relayId": "r1", "host": "10.0.0.1", "port": 40000})
	if err := sg.OnRelayResult(ctx, e); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Create:RelayRequested", "Activate:CallStarted"}; !slices.Equal(st.ops, want) {
		t.Fatalf("ops %v, want %v", st.ops, want)
	}
	if c := st.calls["c1"]; c.Status != domain.StatusActive || c.RelayHost != "10.0.0.1" || c.RelayPort != 40000 || c.StartedAt == 0 {
		t.Fatalf("call not activated with relay: %+v", c)
	}
	if want := []string{"ana:call_active", "bob:incoming_call"}; !slices.Equal(hub.events, want) {
		t.Fatalf("events %v, want %v", hub.events, want)
	}
}

func TestRelayFailedCompensatesOnce(t *testing.T) {
	st := newFake(pending())
	sg, hub := newService(st)
	e := result(t, "RelayFailed", "m1", map[string]any{"callId": "c1", "reason": "boom"})
	if err := sg.OnRelayResult(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if err := sg.OnRelayResult(context.Background(), e); !errors.Is(err, domain.ErrDuplicate) {
		t.Fatalf("redelivery: got %v, want ErrDuplicate", err)
	}
	if st.calls["c1"].Status != domain.StatusFailed || st.users["ana"] != "" {
		t.Fatalf("not compensated: %+v users=%v", st.calls["c1"], st.users)
	}
	if want := []string{"ana:call_failed"}; !slices.Equal(hub.events, want) {
		t.Fatalf("events %v, want %v", hub.events, want)
	}
}

func TestTimeoutSweepCompensates(t *testing.T) {
	old := pending()
	old.CreatedAt = time.Now().Add(-4 * time.Second).UnixMilli()
	fresh := pending()
	fresh.ID = "c2"
	st := newFake(old, fresh)
	sg, _ := newService(st)
	sg.sweepOnce(context.Background())
	if want := []string{"Fail:CallFailed"}; !slices.Equal(st.ops, want) {
		t.Fatalf("ops %v, want %v", st.ops, want)
	}
	if st.calls["c1"].Status != domain.StatusFailed || st.calls["c2"].Status != domain.StatusPending {
		t.Fatal("only the timed-out call must fail")
	}
}

func TestLateAllocationIsReleased(t *testing.T) {
	c := pending()
	c.Status = domain.StatusFailed
	st := newFake(c)
	sg, _ := newService(st)
	e := result(t, "RelayAllocated", "m1", map[string]any{"callId": "c1", "relayId": "r1"})
	if err := sg.OnRelayResult(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Ack:ReleaseRelayRequested"}; !slices.Equal(st.ops, want) {
		t.Fatalf("ops %v, want %v", st.ops, want)
	}
}

func TestJoinFullCall(t *testing.T) {
	c := active("ana", "bob")
	c.Capacity = 2
	sg, _ := newService(newFake(c))
	if _, err := sg.Join(context.Background(), "c1", "carl"); !errors.Is(err, domain.ErrRoomFull) {
		t.Fatalf("got %v, want ErrRoomFull", err)
	}
}

func TestJoinLostLastSeat(t *testing.T) {
	st := newFake(active("ana"))
	st.errs["Join"] = domain.ErrSeatLost
	st.calls["c1"].Capacity = 2
	sg, _ := newService(st)
	_, err := sg.Join(context.Background(), "c1", "bob")
	if !errors.Is(err, domain.ErrRoomFull) {
		t.Fatalf("got %v, want ErrRoomFull", err)
	}
}

func TestJoinMovesUserOutOfPreviousCall(t *testing.T) {
	prev := active("bob")
	prev.ID = "c0"
	st := newFake(prev, active("ana"))
	sg, hub := newService(st)
	if _, err := sg.Join(context.Background(), "c1", "bob"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Leave:ParticipantLeft,CallEnded", "Join:ParticipantJoined"}; !slices.Equal(st.ops, want) {
		t.Fatalf("ops %v, want %v", st.ops, want)
	}
	if !slices.Contains(hub.events, "bob:left_call") || !slices.Contains(hub.events, "ana:participant_joined") {
		t.Fatalf("events %v", hub.events)
	}
}

func TestLastLeaverEndsCall(t *testing.T) {
	st := newFake(active("ana", "bob"))
	sg, _ := newService(st)
	ctx := context.Background()
	if err := sg.Leave(ctx, "c1", "ana"); err != nil {
		t.Fatal(err)
	}
	if err := sg.Leave(ctx, "c1", "bob"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Leave:ParticipantLeft", "Leave:ParticipantLeft,CallEnded"}; !slices.Equal(st.ops, want) {
		t.Fatalf("ops %v, want %v", st.ops, want)
	}
	if err := sg.Leave(ctx, "c1", "bob"); !errors.Is(err, domain.ErrNotActive) {
		t.Fatalf("leave ended call: got %v", err)
	}
}

func TestLeaveRetriesThenGivesUp(t *testing.T) {
	st := newFake(active("ana", "bob"))
	st.errs["Leave"] = domain.ErrStateChanged
	sg, _ := newService(st)
	if err := sg.Leave(context.Background(), "c1", "ana"); !errors.Is(err, domain.ErrStateChanged) {
		t.Fatalf("got %v, want ErrStateChanged", err)
	}
	if len(st.ops) != 3 {
		t.Fatalf("%d attempts, want 3", len(st.ops))
	}
}

func TestParticipantTimedOutAlreadyGone(t *testing.T) {
	st := newFake(active("ana"))
	sg, _ := newService(st)
	e := result(t, "ParticipantTimedOut", "m1", map[string]any{"callId": "c1", "userId": "bob"})
	if err := sg.OnRelayResult(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Ack:"}; !slices.Equal(st.ops, want) {
		t.Fatalf("ops %v, want %v", st.ops, want)
	}
}

func TestInviteByNonParticipant(t *testing.T) {
	sg, hub := newService(newFake(active("ana")))
	if err := sg.Invite(context.Background(), "c1", "mallory", "bob"); !errors.Is(err, domain.ErrNotIn) {
		t.Fatalf("got %v, want ErrNotIn", err)
	}
	if len(hub.events) != 0 {
		t.Fatalf("notified: %v", hub.events)
	}
}
