package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/larpspeak/media-relay/internal/config"
	"github.com/larpspeak/media-relay/internal/domain"
)

// liveSession is the in-memory part of a relay owned by THIS replica:
// who may send audio and where each participant was last seen.
type liveSession struct {
	relayID      string
	participants map[string]bool
	addrs        map[string]*net.UDPAddr
	lastSeen     map[string]time.Time // last packet per participant (muted clients still send)
	reported     map[string]bool      // participants already reported as timed out
	lastRefresh  time.Time            // last reload of participants from DynamoDB
}

func (ls *liveSession) add(user string) {
	if !ls.participants[user] {
		ls.participants[user], ls.lastSeen[user] = true, time.Now()
	}
}

func (ls *liveSession) remove(user string) {
	delete(ls.participants, user)
	delete(ls.addrs, user)
	delete(ls.lastSeen, user)
	delete(ls.reported, user)
}

// Relay is one media-relay replica: it allocates relays, forwards audio for the
// calls it owns and, when it leads, moves calls away from dead pods.
type Relay struct {
	Sessions  SessionStore
	Nodes     NodeStore
	Prober    Prober
	Log       *slog.Logger
	Cfg       config.Config
	ID        string // this replica (container hostname), the session "owner"
	Host      string // address participants send UDP audio to
	ProbeAddr string // this pod's /health address inside the network (container IP:port)

	PingsPaused atomic.Bool // fault injection (SIGUSR1): simulate losing the link to DynamoDB

	mu      sync.Mutex
	live    map[string]*liveSession // by callId
	lookups map[string]time.Time    // last pickUp read per call id (guarded by mu)

	unhealthyAt atomic.Int64            // unix ns of the last failed ping/scan; adoption waits for a stable link
	term        atomic.Int64            // leader term we hold; 0 = follower
	inherited   atomic.Pointer[suspect] // previous leader, handed from elect to the watch loop
}

type callRef struct {
	CallID       string   `json:"callId"`
	RelayID      string   `json:"relayId"`
	UserID       string   `json:"userId"`
	Participants []string `json:"participants"`
}

// OnRelayRequest handles the relay-requests queue: allocate (the unstable
// dependency, R5) or release (compensation for a late allocation, F7).
func (r *Relay) OnRelayRequest(ctx context.Context, e domain.Envelope) error {
	var ref callRef
	if err := json.Unmarshal(e.Payload, &ref); err != nil || ref.CallID == "" {
		return fmt.Errorf("bad %s payload", e.Type)
	}
	switch e.Type {
	case "RelayRequested":
		return r.allocate(ctx, e, ref)
	case "ReleaseRelayRequested":
		return r.release(ctx, e, ref.CallID, "late allocation")
	}
	return fmt.Errorf("unexpected type %s on relay-requests", e.Type)
}

// OnCallEvent handles call-events fanned out by SNS. CallEnded is the event with
// several consumers: media-relay frees the relay, audit-vault records the call.
func (r *Relay) OnCallEvent(ctx context.Context, e domain.Envelope) error {
	var ref callRef
	if err := json.Unmarshal(e.Payload, &ref); err != nil || ref.CallID == "" {
		return fmt.Errorf("bad %s payload", e.Type)
	}
	switch e.Type {
	case "CallEnded":
		return r.release(ctx, e, ref.CallID, "call ended")
	case "CallFailed":
		return r.release(ctx, e, ref.CallID, "call failed")
	case "ParticipantJoined":
		return r.member(ctx, e, ref, "ADD")
	case "ParticipantLeft":
		return r.member(ctx, e, ref, "DELETE")
	}
	return nil // CallStarted: nothing to do here
}

func message(dest, typ, callID string, payload any) (domain.Outgoing, error) {
	e, err := domain.NewEnvelope(typ, callID, payload)
	return domain.Outgoing{Dest: dest, Envelope: e}, err
}

// member updates who may send audio. Any replica may receive the event: the
// DynamoDB session is updated here, and the owning replica picks the change up
// on its next refresh (or immediately, if it is this one).
func (r *Relay) member(ctx context.Context, e domain.Envelope, ref callRef, op string) error {
	if ref.UserID == "" {
		return fmt.Errorf("bad %s payload", e.Type)
	}
	err := r.Sessions.Member(ctx, e.MessageID, ref.CallID, op, ref.UserID)
	if errors.Is(err, domain.ErrReleased) { // relay gone: nothing to update
		return r.Sessions.Ack(ctx, e.MessageID)
	}
	if err != nil {
		return err
	}
	r.mu.Lock()
	if ls := r.live[ref.CallID]; ls != nil {
		if op == "ADD" {
			ls.add(ref.UserID)
		} else {
			ls.remove(ref.UserID)
		}
	}
	r.mu.Unlock()
	r.Log.Info("participants updated", "correlationId", ref.CallID, "userId", ref.UserID, "op", op)
	return nil
}

// refresh reloads a live session's participants from DynamoDB, at most every 500ms.
func (r *Relay) refresh(ctx context.Context, callID string) {
	r.mu.Lock()
	ls := r.live[callID]
	if ls == nil || time.Since(ls.lastRefresh) < 500*time.Millisecond {
		r.mu.Unlock()
		return
	}
	ls.lastRefresh = time.Now()
	r.mu.Unlock()

	ss, err := r.Sessions.Get(ctx, callID)
	if err != nil {
		return
	}
	if ss == nil || ss.State == domain.StateReleased || ss.Owner != r.ID { // released, or adopted by another pod
		r.untrack(callID)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if ls := r.live[callID]; ls != nil {
		for _, p := range ss.Participants {
			ls.add(p)
		}
		for u := range ls.participants {
			if !slices.Contains(ss.Participants, u) {
				ls.remove(u)
			}
		}
	}
}

func (r *Relay) allocate(ctx context.Context, e domain.Envelope, ref callRef) error {
	if len(ref.Participants) == 0 {
		return errors.New("RelayRequested without participants")
	}
	log := r.Log.With("correlationId", e.CorrelationID, "messageId", e.MessageID)
	log.Info("allocating relay", "latency", r.Cfg.Latency.String())
	select { // R5: simulated slow dependency
	case <-time.After(r.Cfg.Latency):
	case <-ctx.Done():
		return ctx.Err()
	}

	if rand.Float64() < r.Cfg.FailureRate { // R5: simulated failure
		out, err := message(r.Cfg.RelayResults, "RelayFailed", ref.CallID, map[string]string{"callId": ref.CallID, "reason": "simulated relay failure"})
		if err != nil {
			return err
		}
		if err := r.Sessions.Ack(ctx, e.MessageID, out); err != nil {
			return err
		}
		log.Warn("relay allocation failed (simulated)")
		return nil
	}

	ss := domain.Session{CallID: ref.CallID, RelayID: uuid.New().String(), Host: r.Host, Port: r.Cfg.UDPPort, Participants: ref.Participants,
		State: domain.StateAllocated, Owner: r.ID}
	out, err := message(r.Cfg.RelayResults, "RelayAllocated", ref.CallID, map[string]any{
		"callId": ss.CallID, "relayId": ss.RelayID, "host": ss.Host, "port": ss.Port,
		"signalAddr": r.ProbeAddr, // where call-signaling sends WebRTC offers for this call
	})
	if err != nil {
		return err
	}
	switch err := r.Sessions.Allocate(ctx, e.MessageID, ss, out); {
	case errors.Is(err, domain.ErrReleased): // session already exists for this call
		log.Warn("relay already allocated for call, ignoring")
		return r.Sessions.Ack(ctx, e.MessageID)
	case err != nil:
		return err
	}
	r.track(ss)
	log.Info("relay allocated", "relayId", ss.RelayID, "host", ss.Host, "port", ss.Port)
	return nil
}

// release frees the relay at most once, whichever replica receives the message.
func (r *Relay) release(ctx context.Context, e domain.Envelope, callID, reason string) error {
	err := r.Sessions.Release(ctx, e.MessageID, callID, reason)
	if errors.Is(err, domain.ErrReleased) { // already released or never allocated
		err = r.Sessions.Ack(ctx, e.MessageID)
	} else if err == nil {
		r.Log.Info("relay released", "correlationId", callID, "reason", reason)
	}
	r.untrack(callID)
	return err
}

func (r *Relay) track(ss domain.Session) {
	ls := &liveSession{relayID: ss.RelayID, participants: map[string]bool{}, addrs: map[string]*net.UDPAddr{},
		lastSeen: map[string]time.Time{}, reported: map[string]bool{}}
	for _, p := range ss.Participants {
		ls.add(p)
	}
	r.mu.Lock()
	if r.live == nil {
		r.live = map[string]*liveSession{}
	}
	r.live[ss.CallID] = ls
	r.mu.Unlock()
}

func (r *Relay) untrack(callID string) {
	r.mu.Lock()
	delete(r.live, callID)
	r.mu.Unlock()
}

// Forward returns where to copy an audio packet from userID in callID. The
// sender's address is learned from its packets; the packet goes to every other
// participant whose address is known. No call-signaling involved. An unknown
// call or sender gets no targets (drop) and a background lookup, in case it
// joined via another replica or the call was moved here after a failover.
func (r *Relay) Forward(ctx context.Context, callID, userID string, from *net.UDPAddr) []*net.UDPAddr {
	r.mu.Lock()
	ls := r.live[callID]
	if ls == nil || !ls.participants[userID] {
		r.mu.Unlock()
		if ls != nil {
			go r.refresh(ctx, callID) // maybe joined via another replica
		} else {
			go r.PickUp(ctx, callID) // maybe moved here by the leader after a failover
		}
		return nil // unknown call or not (yet) a participant: drop
	}
	ls.addrs[userID] = from
	ls.lastSeen[userID] = time.Now()
	var targets []*net.UDPAddr
	for u, a := range ls.addrs {
		if u != userID {
			targets = append(targets, a)
		}
	}
	r.mu.Unlock()
	return targets
}

// Touch authorizes a packet from userID in callID and records that the user is
// alive. WebRTC peers call it on every RTP/RTCP packet (a muted browser still
// sends silent Opus frames), so the participant timeout works as for UDP.
func (r *Relay) Touch(callID, userID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	ls := r.live[callID]
	if ls == nil || !ls.participants[userID] {
		return false
	}
	ls.lastSeen[userID] = time.Now()
	return true
}

// Admit checks that this pod relays callID and userID may talk in it, reading
// DynamoDB right away (a WebRTC offer can arrive before /adopt or before the
// ParticipantJoined event reached this pod).
func (r *Relay) Admit(ctx context.Context, callID, userID string) error {
	r.mu.Lock()
	ls := r.live[callID]
	ok := ls != nil && ls.participants[userID]
	r.mu.Unlock()
	if ok {
		return nil
	}
	ss, err := r.Sessions.Get(ctx, callID)
	if err != nil {
		return err
	}
	if ss == nil || ss.State != domain.StateAllocated || ss.Owner != r.ID {
		return domain.ErrNotOwner
	}
	if !slices.Contains(ss.Participants, userID) {
		return domain.ErrNotIn
	}
	r.mu.Lock()
	if ls := r.live[callID]; ls != nil {
		ls.add(userID)
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	r.track(*ss)
	return nil
}

// Sweep reports participants whose client stopped sending packets for
// PARTICIPANT_TIMEOUT_MS (crashed app, lost network). call-signaling then removes
// them like a normal leave, and the call ends only when the last one is gone.
// Silence is not a timeout: muted clients keep sending packets.
// It also drops local sessions released elsewhere and syncs participants.
// ponytail: one GetItem per live session every 2s; fine for demo-sized load.
func (r *Relay) Sweep(ctx context.Context) {
	for ctx.Err() == nil {
		time.Sleep(2 * time.Second)
		type gone struct{ callID, userID string }
		var timedOut []gone
		var calls []string
		r.mu.Lock()
		for callID, ls := range r.live {
			calls = append(calls, callID)
			for u, seen := range ls.lastSeen {
				if !ls.reported[u] && time.Since(seen) > r.Cfg.ParticipantTimeout {
					ls.reported[u] = true
					timedOut = append(timedOut, gone{callID, u})
				}
			}
		}
		r.mu.Unlock()

		for _, g := range timedOut {
			if err := r.reportTimeout(ctx, g.callID, g.userID); err != nil {
				r.Log.Warn("timeout report failed, retrying", "correlationId", g.callID, "userId", g.userID, "err", err)
				r.mu.Lock()
				if ls := r.live[g.callID]; ls != nil {
					delete(ls.reported, g.userID)
				}
				r.mu.Unlock()
			}
		}
		for _, callID := range calls {
			r.refresh(ctx, callID)
		}
	}
}

func (r *Relay) reportTimeout(ctx context.Context, callID, userID string) error {
	out, err := message(r.Cfg.RelayResults, "ParticipantTimedOut", callID, map[string]string{"callId": callID, "userId": userID})
	if err != nil {
		return err
	}
	if err := r.Sessions.Emit(ctx, out); err != nil {
		return err
	}
	r.Log.Warn("participant timed out: no packets", "correlationId", callID, "userId", userID, "timeout", r.Cfg.ParticipantTimeout.String())
	return nil
}
