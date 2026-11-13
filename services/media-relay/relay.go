package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"slices"
	"sync"
	"time"
	"uuid"
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

type relay struct {
	st   *store
	log  *slog.Logger
	cfg  config
	id   string // this replica (container hostname), the session "owner"
	host string // address participants send UDP audio to

	mu   sync.Mutex
	live map[string]*liveSession // by callId

	healthySince time.Time // last time a DynamoDB call failed; takeovers wait for a stable link
}

const (
	heartbeatEvery = 2 * time.Second
	orphanAfter    = 6 * time.Second // a session without heartbeat this long is taken over
)

type callRef struct {
	CallID       string   `json:"callId"`
	RelayID      string   `json:"relayId"`
	UserID       string   `json:"userId"`
	Participants []string `json:"participants"`
}

// onRelayRequest handles the relay-requests queue: allocate (the unstable
// dependency, R5) or release (compensation for a late allocation, F7).
func (r *relay) onRelayRequest(ctx context.Context, e envelope) error {
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

// onCallEvent handles call-events fanned out by SNS. CallEnded is the event with
// several consumers: media-relay frees the relay, audit-vault records the call.
func (r *relay) onCallEvent(ctx context.Context, e envelope) error {
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

// member updates who may send audio. Any replica may receive the event: the
// DynamoDB session is updated here, and the owning replica picks the change up
// on its next refresh (or immediately, if it is this one).
func (r *relay) member(ctx context.Context, e envelope, ref callRef, op string) error {
	if ref.UserID == "" {
		return fmt.Errorf("bad %s payload", e.Type)
	}
	dedupe := r.st.markProcessed(e.MessageID)
	err := r.st.commit(ctx, dedupe, r.st.member(ref.CallID, op, ref.UserID))
	if errors.Is(err, errReleased) { // relay gone: nothing to update
		return r.st.commit(ctx, dedupe)
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
	r.log.Info("participants updated", "correlationId", ref.CallID, "userId", ref.UserID, "op", op)
	return nil
}

// refresh reloads a live session's participants from DynamoDB, at most every 500ms.
func (r *relay) refresh(ctx context.Context, callID string) {
	r.mu.Lock()
	ls := r.live[callID]
	if ls == nil || time.Since(ls.lastRefresh) < 500*time.Millisecond {
		r.mu.Unlock()
		return
	}
	ls.lastRefresh = time.Now()
	r.mu.Unlock()

	ss, err := r.st.getSession(ctx, callID)
	if err != nil {
		return
	}
	if ss == nil || ss.State == stateReleased {
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

func (r *relay) allocate(ctx context.Context, e envelope, ref callRef) error {
	if len(ref.Participants) == 0 {
		return errors.New("RelayRequested without participants")
	}
	log := r.log.With("correlationId", e.CorrelationID, "messageId", e.MessageID)
	log.Info("allocating relay", "latency", r.cfg.Latency.String())
	select { // R5: simulated slow dependency
	case <-time.After(r.cfg.Latency):
	case <-ctx.Done():
		return ctx.Err()
	}
	dedupe := r.st.markProcessed(e.MessageID)

	if rand.Float64() < r.cfg.FailureRate { // R5: simulated failure
		failed, err := newEnvelope("RelayFailed", ref.CallID, map[string]string{"callId": ref.CallID, "reason": "simulated relay failure"})
		if err != nil {
			return err
		}
		out, err := r.st.outbox(r.cfg.RelayResults, failed)
		if err != nil {
			return err
		}
		if err := r.st.commit(ctx, dedupe, out); err != nil {
			return err
		}
		log.Warn("relay allocation failed (simulated)")
		return nil
	}

	ss := session{CallID: ref.CallID, RelayID: uuid.New().String(), Host: r.host, Port: r.cfg.UDPPort, Participants: ref.Participants,
		State: stateAllocated, Owner: r.id, HeartbeatAt: time.Now().UnixMilli()}
	put, err := r.st.putSession(ss)
	if err != nil {
		return err
	}
	ok, err := newEnvelope("RelayAllocated", ref.CallID, map[string]any{"callId": ss.CallID, "relayId": ss.RelayID, "host": ss.Host, "port": ss.Port})
	if err != nil {
		return err
	}
	out, err := r.st.outbox(r.cfg.RelayResults, ok)
	if err != nil {
		return err
	}
	switch err := r.st.commit(ctx, dedupe, put, out); {
	case errors.Is(err, errReleased): // session already exists for this call
		log.Warn("relay already allocated for call, ignoring")
		return r.st.commit(ctx, dedupe)
	case err != nil:
		return err
	}
	r.track(ss)
	log.Info("relay allocated", "relayId", ss.RelayID, "host", ss.Host, "port", ss.Port)
	return nil
}

// release frees the relay at most once, whichever replica receives the message.
func (r *relay) release(ctx context.Context, e envelope, callID, reason string) error {
	dedupe := r.st.markProcessed(e.MessageID)
	err := r.st.commit(ctx, dedupe, r.st.release(callID, reason))
	if errors.Is(err, errReleased) { // already released or never allocated
		err = r.st.commit(ctx, dedupe)
	} else if err == nil {
		r.log.Info("relay released", "correlationId", callID, "reason", reason)
	}
	r.untrack(callID)
	return err
}

func (r *relay) track(ss session) {
	ls := &liveSession{relayID: ss.RelayID, participants: map[string]bool{}, addrs: map[string]*net.UDPAddr{},
		lastSeen: map[string]time.Time{}, reported: map[string]bool{}}
	for _, p := range ss.Participants {
		ls.add(p)
	}
	r.mu.Lock()
	r.live[ss.CallID] = ls
	r.mu.Unlock()
}

func (r *relay) untrack(callID string) {
	r.mu.Lock()
	delete(r.live, callID)
	r.mu.Unlock()
}

// serveUDP forwards audio. Packet = "<callId> <userId> [anything]\n" + payload;
// the relay only reads callId and userId and forwards the packet untouched.
// The sender's address is learned from its packets; the packet is copied to
// every other participant whose address is known. No call-signaling involved.
func (r *relay) serveUDP(ctx context.Context, conn *net.UDPConn) {
	buf := make([]byte, 64*1024)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			r.log.Error("udp read failed", "err", err)
			return
		}
		callID, userID, ok := parseHeader(buf[:n])
		if !ok {
			continue
		}
		r.mu.Lock()
		ls := r.live[callID]
		if ls == nil || !ls.participants[userID] {
			r.mu.Unlock()
			if ls != nil {
				go r.refresh(ctx, callID) // maybe joined via another replica
			}
			continue // unknown call or not (yet) a participant: drop
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
		for _, t := range targets {
			_, _ = conn.WriteToUDP(buf[:n], t)
		}
	}
}

func parseHeader(p []byte) (callID, userID string, ok bool) {
	line, _, found := bytes.Cut(p, []byte("\n"))
	f := bytes.Fields(line)
	if !found || len(f) < 2 {
		return "", "", false
	}
	return string(f[0]), string(f[1]), true
}

// sweep reports participants whose client stopped sending packets for
// PARTICIPANT_TIMEOUT_MS (crashed app, lost network). call-signaling then removes
// them like a normal leave, and the call ends only when the last one is gone.
// Silence is not a timeout: muted clients keep sending packets.
// It also drops local sessions released elsewhere and syncs participants.
// ponytail: one GetItem per live session every 2s; fine for demo-sized load.
func (r *relay) sweep(ctx context.Context) {
	r.healthySince = time.Now()
	for ctx.Err() == nil {
		time.Sleep(heartbeatEvery)
		r.heartbeat(ctx)
		r.adoptOrphans(ctx)
		type gone struct{ callID, userID string }
		var timedOut []gone
		var calls []string
		r.mu.Lock()
		for callID, ls := range r.live {
			calls = append(calls, callID)
			for u, seen := range ls.lastSeen {
				if !ls.reported[u] && time.Since(seen) > r.cfg.ParticipantTimeout {
					ls.reported[u] = true
					timedOut = append(timedOut, gone{callID, u})
				}
			}
		}
		r.mu.Unlock()

		for _, g := range timedOut {
			if err := r.reportTimeout(ctx, g.callID, g.userID); err != nil {
				r.log.Warn("timeout report failed, retrying", "correlationId", g.callID, "userId", g.userID, "err", err)
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

// heartbeat refreshes heartbeatAt on every session this replica owns. A session
// that another replica took over (we were frozen too long) is dropped locally.
func (r *relay) heartbeat(ctx context.Context) {
	r.mu.Lock()
	calls := make([]string, 0, len(r.live))
	for id := range r.live {
		calls = append(calls, id)
	}
	r.mu.Unlock()
	now := time.Now().UnixMilli()
	for _, callID := range calls {
		switch err := r.st.heartbeat(ctx, callID, r.id, now); {
		case errors.Is(err, errReleased):
			r.log.Warn("session no longer ours, dropping", "correlationId", callID)
			r.untrack(callID)
		case err != nil:
			r.healthySince = time.Now()
		}
	}
}

// adoptOrphans takes over sessions whose owner stopped heartbeating (replica
// died) and announces the new address so clients move their audio here.
func (r *relay) adoptOrphans(ctx context.Context) {
	if time.Since(r.healthySince) < orphanAfter {
		return // after an outage everyone looks stale: give live owners time to heartbeat first
	}
	orphans, err := r.st.orphans(ctx, time.Now().Add(-orphanAfter).UnixMilli())
	if err != nil {
		r.healthySince = time.Now()
		return
	}
	for _, old := range orphans {
		moved, err := newEnvelope("RelayMoved", old.CallID, map[string]any{
			"callId": old.CallID, "relayId": old.RelayID, "host": r.host, "port": r.cfg.UDPPort,
		})
		if err != nil {
			continue
		}
		out, err := r.st.outbox(r.cfg.RelayResults, moved)
		if err != nil {
			continue
		}
		// Item 0 is the takeover guard: "duplicate" here means another replica won.
		if err := r.st.commit(ctx, r.st.takeover(old, r.id, r.host, r.cfg.UDPPort, time.Now().UnixMilli()), out); err != nil {
			continue
		}
		deadOwner := old.Owner
		old.Host, old.Port, old.Owner = r.host, r.cfg.UDPPort, r.id
		r.track(old)
		r.log.Warn("relay taken over from dead replica", "correlationId", old.CallID, "from", deadOwner, "host", r.host, "port", r.cfg.UDPPort)
	}
}

func (r *relay) reportTimeout(ctx context.Context, callID, userID string) error {
	e, err := newEnvelope("ParticipantTimedOut", callID, map[string]string{"callId": callID, "userId": userID})
	if err != nil {
		return err
	}
	out, err := r.st.outbox(r.cfg.RelayResults, e)
	if err != nil {
		return err
	}
	if err := r.st.commit(ctx, out); err != nil {
		return err
	}
	r.log.Warn("participant timed out: no packets", "correlationId", callID, "userId", userID, "timeout", r.cfg.ParticipantTimeout.String())
	return nil
}
