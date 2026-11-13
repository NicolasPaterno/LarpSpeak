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
	lastAudio    time.Time
	lastRefresh  time.Time // last reload of participants from DynamoDB
}

type relay struct {
	st   *store
	log  *slog.Logger
	cfg  config
	host string // address participants send UDP audio to

	mu   sync.Mutex
	live map[string]*liveSession // by callId
}

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
			ls.participants[ref.UserID] = true
		} else {
			delete(ls.participants, ref.UserID)
			delete(ls.addrs, ref.UserID)
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
		ls.participants = map[string]bool{}
		for _, p := range ss.Participants {
			ls.participants[p] = true
		}
		for u := range ls.addrs {
			if !ls.participants[u] {
				delete(ls.addrs, u)
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

	ss := session{CallID: ref.CallID, RelayID: uuid.New().String(), Host: r.host, Port: r.cfg.UDPPort, Participants: ref.Participants, State: stateAllocated}
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
	ls := &liveSession{relayID: ss.RelayID, participants: map[string]bool{}, addrs: map[string]*net.UDPAddr{}, lastAudio: time.Now()}
	for _, p := range ss.Participants {
		ls.participants[p] = true
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
		ls.lastAudio = time.Now()
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

// sweep closes relays with no audio for RELAY_IDLE_TIMEOUT_MS (abandoned calls)
// and drops local sessions that another replica already released.
// ponytail: one GetItem per live session every 2s; fine for demo-sized load.
// ponytail: if the owning replica dies, its relays are not idle-closed; add a
// max call duration in call-signaling if that matters.
func (r *relay) sweep(ctx context.Context) {
	for ctx.Err() == nil {
		time.Sleep(2 * time.Second)
		r.mu.Lock()
		snapshot := make(map[string]liveSession, len(r.live))
		for id, ls := range r.live {
			snapshot[id] = *ls
		}
		r.mu.Unlock()

		for callID, ls := range snapshot {
			if time.Since(ls.lastAudio) > r.cfg.IdleTimeout {
				r.closeIdle(ctx, callID, ls.relayID)
				continue
			}
			r.refresh(ctx, callID) // drops sessions released elsewhere, syncs participants
		}
	}
}

func (r *relay) closeIdle(ctx context.Context, callID, relayID string) {
	closed, err := newEnvelope("RelayClosed", callID, map[string]string{"callId": callID, "relayId": relayID, "reason": "idle"})
	if err != nil {
		return
	}
	out, err := r.st.outbox(r.cfg.RelayResults, closed)
	if err != nil {
		return
	}
	// Item 0 is the state guard here, so "duplicate" means someone already released it.
	switch err := r.st.commit(ctx, r.st.release(callID, "idle"), out); {
	case err == nil:
		r.log.Warn("relay closed: no audio", "correlationId", callID, "idleTimeout", r.cfg.IdleTimeout.String())
		r.untrack(callID)
	case errors.Is(err, errDuplicate):
		r.untrack(callID)
	default:
		r.log.Warn("idle close failed, retrying", "correlationId", callID, "err", err)
	}
}
