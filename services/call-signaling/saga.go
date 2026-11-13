package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// saga orchestrates call setup: create call (creator holds a seat) -> request
// relay -> activate, compensating (free seats, notify creator, publish CallFailed)
// when the relay fails, times out, or answers too late. It also handles the
// channel lifecycle afterwards: join, leave, end.
type saga struct {
	st    *store
	hub   *hub
	log   *slog.Logger
	cfg   config
	nowFn func() time.Time
}

type relayResult struct {
	CallID  string `json:"callId"`
	UserID  string `json:"userId"`
	RelayID string `json:"relayId"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Reason  string `json:"reason"`
}

// start is step 1: call (creator inside, 1 seat taken) + RelayRequested outbox entry, atomically.
func (sg *saga) start(ctx context.Context, c call) error {
	put, err := sg.st.putCall(c)
	if err != nil {
		return err
	}
	e, err := newEnvelope("RelayRequested", c.ID, map[string]any{"callId": c.ID, "participants": c.Participants})
	if err != nil {
		return err
	}
	out, err := sg.st.outbox(sg.cfg.RelayRequests, e)
	if err != nil {
		return err
	}
	if err := sg.moveOut(ctx, c.CallerID, c.ID); err != nil {
		return err
	}
	codes, err := sg.st.commit(ctx, put, out, sg.st.claimUser(c.CallerID, c.ID))
	if conditionFailed(codes, 2) {
		return errStateChanged // the creator joined another call at the same moment
	}
	return err
}

// moveOut makes a user leave whatever call they are in before entering target,
// like Discord does when you join another voice channel. Stale records (call
// already over, or user no longer inside) are just cleaned up.
func (sg *saga) moveOut(ctx context.Context, user, target string) error {
	prev, err := sg.st.currentCall(ctx, user)
	if err != nil || prev == "" || prev == target {
		return err
	}
	switch err := sg.leave(ctx, prev, user); err {
	case nil:
		sg.log.Info("participant moved", "userId", user, "from", prev, "to", target)
		sg.hub.notify(user, "left_call", map[string]string{"callId": prev, "reason": "joined another call"})
	case errNotActive, errNotIn:
		sg.st.forgetUser(ctx, user, prev)
	default:
		return err
	}
	return nil
}

// onRelayResult handles RelayAllocated / RelayFailed / RelayMoved / ParticipantTimedOut from media-relay.
func (sg *saga) onRelayResult(ctx context.Context, e envelope) error {
	var r relayResult
	if err := json.Unmarshal(e.Payload, &r); err != nil || r.CallID == "" {
		return fmt.Errorf("bad %s payload", e.Type)
	}
	c, err := sg.st.getCall(ctx, r.CallID)
	if err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("unknown call %s", r.CallID)
	}
	dedupe := sg.st.markProcessed(e.MessageID)

	switch {
	case e.Type == "RelayFailed" && c.Status == statusPending:
		return sg.compensate(ctx, c, "relay failed: "+r.Reason, dedupe)

	case e.Type == "RelayMoved" && c.Status == statusActive:
		// The relay replica died and another one adopted the call: point everyone to it.
		fields := map[string]types.AttributeValue{"relayHost": str(r.Host), "relayPort": num(int64(r.Port))}
		if err := sg.commitOnce(ctx, dedupe, sg.st.transition(c.ID, statusActive, statusActive, fields)); err != nil {
			return err
		}
		sg.log.Warn("relay moved", "correlationId", c.ID, "host", r.Host, "port", r.Port)
		for _, u := range c.Participants {
			sg.hub.notify(u, "relay_moved", map[string]any{"callId": c.ID, "relayHost": r.Host, "relayPort": r.Port})
		}
		return nil

	case e.Type == "ParticipantTimedOut":
		// The user's client stopped sending packets (crash, lost network): remove
		// them like a normal leave. Only the last one out ends the call.
		err := sg.leave(ctx, c.ID, r.UserID, dedupe)
		if errors.Is(err, errNotActive) || errors.Is(err, errNotIn) { // already gone
			return sg.commitOnce(ctx, dedupe)
		}
		if err == nil {
			sg.hub.notify(r.UserID, "left_call", map[string]string{"callId": c.ID, "reason": "connection lost"})
		}
		return err

	case e.Type == "RelayAllocated" && c.Status == statusPending:
		now := sg.nowFn().UnixMilli()
		started, err := newEnvelope("CallStarted", c.ID, map[string]any{
			"callId": c.ID, "kind": c.Kind, "capacity": c.Capacity, "relayId": r.RelayID, "participants": c.Participants,
		})
		if err != nil {
			return err
		}
		out, err := sg.st.outbox(destTopic, started)
		if err != nil {
			return err
		}
		fields := map[string]types.AttributeValue{"relayId": str(r.RelayID), "relayHost": str(r.Host), "relayPort": num(int64(r.Port)), "startedAt": num(now)}
		if err := sg.commitOnce(ctx, dedupe, sg.st.transition(c.ID, statusPending, statusActive, fields), out); err != nil {
			return err
		}
		sg.log.Info("call active", "correlationId", c.ID, "relayId", r.RelayID)
		sg.hub.notify(c.CallerID, "call_active", map[string]any{"callId": c.ID, "relayHost": r.Host, "relayPort": r.Port})
		for _, u := range c.Invited {
			sg.hub.notify(u, "incoming_call", map[string]any{"callId": c.ID, "from": c.CallerID})
		}
		return nil

	case e.Type == "RelayAllocated":
		// Late answer (F7): the saga already gave up, so give the relay back.
		rel, err := newEnvelope("ReleaseRelayRequested", c.ID, map[string]string{"callId": c.ID, "relayId": r.RelayID})
		if err != nil {
			return err
		}
		out, err := sg.st.outbox(sg.cfg.RelayRequests, rel)
		if err != nil {
			return err
		}
		if err := sg.commitOnce(ctx, dedupe, out); err != nil {
			return err
		}
		sg.log.Warn("late relay released", "correlationId", c.ID, "status", c.Status, "relayId", r.RelayID)
		return nil

	default: // RelayFailed/RelayMoved for a call no longer in that state: nothing to do.
		return sg.commitOnce(ctx, dedupe)
	}
}

// compensate undoes step 1. The status condition makes it run at most once,
// even if the timeout sweeper and a RelayFailed race each other.
func (sg *saga) compensate(ctx context.Context, c *call, reason string, extra ...types.TransactWriteItem) error {
	failed, err := newEnvelope("CallFailed", c.ID, map[string]string{"callId": c.ID, "reason": reason})
	if err != nil {
		return err
	}
	out, err := sg.st.outbox(destTopic, failed)
	if err != nil {
		return err
	}
	fields := map[string]types.AttributeValue{"reason": str(reason), "taken": num(0)} // seats freed
	items := append(extra, sg.st.transition(c.ID, statusPending, statusFailed, fields), out)
	codes, err := sg.st.commit(ctx, items...)
	if len(extra) > 0 && conditionFailed(codes, 0) {
		return errDuplicate
	}
	if conditionFailed(codes, len(extra)) {
		return errStateChanged
	}
	if err != nil {
		return err
	}
	sg.log.Warn("call compensated", "correlationId", c.ID, "reason", reason, "seatsReleased", c.Taken)
	sg.st.forgetUser(ctx, c.CallerID, c.ID)
	sg.hub.notify(c.CallerID, "call_failed", map[string]string{"callId": c.ID, "reason": reason})
	return nil
}

// join puts a user into an active call if a seat is free (R2).
func (sg *saga) join(ctx context.Context, id, user string) (*call, error) {
	e, err := newEnvelope("ParticipantJoined", id, map[string]string{"callId": id, "userId": user})
	if err != nil {
		return nil, err
	}
	out, err := sg.st.outbox(destTopic, e)
	if err != nil {
		return nil, err
	}
	// Check the target first, so a full or finished call does not kick the user out of their current one.
	if err := sg.canJoin(ctx, id, user); err != nil {
		return nil, err
	}
	if err := sg.moveOut(ctx, user, id); err != nil {
		return nil, err
	}
	codes, err := sg.st.commit(ctx, sg.st.join(id, user), out, sg.st.claimUser(user, id))
	if conditionFailed(codes, 0) { // lost a race for the last seat (or the call just ended)
		if err := sg.canJoin(ctx, id, user); err != nil {
			return nil, err
		}
		return nil, errRoomFull
	}
	if conditionFailed(codes, 2) {
		return nil, errStateChanged // the same user joined another call at the same moment
	}
	if err != nil {
		return nil, err
	}
	c, err := sg.st.getCall(ctx, id)
	if err != nil || c == nil {
		return nil, fmt.Errorf("reading call after join: %w", err)
	}
	sg.log.Info("participant joined", "correlationId", id, "userId", user, "taken", c.Taken, "capacity", c.Capacity)
	sg.broadcast(c, user, "participant_joined")
	return c, nil
}

// canJoin says whether user could join call id right now.
func (sg *saga) canJoin(ctx context.Context, id, user string) error {
	c, err := sg.st.getCall(ctx, id)
	switch {
	case err != nil:
		return err
	case c == nil || c.Status != statusActive:
		return errNotActive
	case c.has(user):
		return errAlreadyIn
	case c.Taken >= c.Capacity:
		return errRoomFull
	}
	return nil
}

// leave frees the user's seat; the last one out ends the call (the only way a
// call ends). Retries when someone else joined/left between our read and our
// write. extra items (a dedupe marker) go first in the transaction.
func (sg *saga) leave(ctx context.Context, id, user string, extra ...types.TransactWriteItem) error {
	for range 3 {
		c, err := sg.st.getCall(ctx, id)
		switch {
		case err != nil:
			return err
		case c == nil || c.Status != statusActive:
			return errNotActive
		case !c.has(user):
			return errNotIn
		}
		now := sg.nowFn().UnixMilli()
		last := c.Taken == 1
		left, err := newEnvelope("ParticipantLeft", id, map[string]string{"callId": id, "userId": user})
		if err != nil {
			return err
		}
		items := append(slices.Clone(extra), sg.st.leave(id, user, c.Taken, last, now), sg.st.releaseUser(user, id))
		out, err := sg.st.outbox(destTopic, left)
		if err != nil {
			return err
		}
		items = append(items, out)
		if last {
			ended, err := newEnvelope("CallEnded", id, map[string]any{
				"callId": id, "relayId": c.RelayID, "durationMs": now - c.StartedAt, "reason": "everyone left",
			})
			if err != nil {
				return err
			}
			if out, err = sg.st.outbox(destTopic, ended); err != nil {
				return err
			}
			items = append(items, out)
		}
		codes, err := sg.st.commit(ctx, items...)
		if len(extra) > 0 && conditionFailed(codes, 0) {
			return errDuplicate
		}
		if conditionFailed(codes, len(extra)) {
			continue // counter moved under us: read again
		}
		if err != nil {
			return err
		}
		sg.log.Info("participant left", "correlationId", id, "userId", user, "callEnded", last)
		c.Participants = slices.DeleteFunc(c.Participants, func(p string) bool { return p == user })
		sg.broadcast(c, user, "participant_left")
		if last {
			sg.log.Info("call ended", "correlationId", id, "reason", "everyone left")
		}
		return nil
	}
	return errStateChanged
}

// replayInvites resends invitations the user missed while not connected:
// active calls that invited them and that they have not joined yet.
func (sg *saga) replayInvites(ctx context.Context, user string) {
	calls, err := sg.st.activeInvites(ctx, user)
	if err != nil {
		sg.log.Warn("invite replay failed", "userId", user, "err", err)
		return
	}
	for i := range calls {
		if !calls[i].has(user) {
			sg.hub.notify(user, "incoming_call", map[string]any{"callId": calls[i].ID, "from": calls[i].CallerID})
		}
	}
}

// broadcast tells everyone still in the call that user joined or left.
func (sg *saga) broadcast(c *call, user, event string) {
	for _, u := range c.Participants {
		if u != user {
			sg.hub.notify(u, event, map[string]any{"callId": c.ID, "userId": user, "participants": c.Participants})
		}
	}
}

// sweepTimeouts compensates calls that waited longer than RELAY_TIMEOUT_MS.
func (sg *saga) sweepTimeouts(ctx context.Context) {
	for ctx.Err() == nil {
		time.Sleep(time.Second)
		calls, err := sg.st.pendingCalls(ctx)
		if err != nil {
			sg.log.Warn("timeout sweep failed, retrying", "err", err)
			continue
		}
		deadline := sg.nowFn().Add(-sg.cfg.RelayTimeout).UnixMilli()
		for i := range calls {
			if calls[i].CreatedAt < deadline {
				if err := sg.compensate(ctx, &calls[i], "relay timeout"); err != nil && err != errStateChanged {
					sg.log.Warn("timeout compensation failed", "correlationId", calls[i].ID, "err", err)
				}
			}
		}
	}
}

// commitOnce runs a transaction whose first item is the processed-message marker.
func (sg *saga) commitOnce(ctx context.Context, items ...types.TransactWriteItem) error {
	codes, err := sg.st.commit(ctx, items...)
	if conditionFailed(codes, 0) {
		return errDuplicate
	}
	if conditionFailed(codes, 1) {
		return errStateChanged
	}
	return err
}
