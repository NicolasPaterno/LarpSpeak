package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// saga orchestrates call setup: reserve seats -> request relay -> activate,
// compensating (release seats, notify caller, publish CallFailed) when the relay
// fails, times out, or answers too late.
type saga struct {
	st    *store
	hub   *hub
	log   *slog.Logger
	cfg   config
	nowFn func() time.Time
}

type relayRequested struct {
	CallID       string   `json:"callId"`
	RoomID       string   `json:"roomId"`
	Participants []string `json:"participants"`
}

type relayResult struct {
	CallID  string `json:"callId"`
	RelayID string `json:"relayId"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Reason  string `json:"reason"`
}

// start is step 1: seats + call + RelayRequested outbox entry, atomically.
func (sg *saga) start(ctx context.Context, c call) error {
	if c.Seats > sg.cfg.RoomCapacity {
		return errRoomFull
	}
	put, err := sg.st.putCall(c)
	if err != nil {
		return err
	}
	e, err := newEnvelope("RelayRequested", c.ID, relayRequested{c.ID, c.RoomID, c.Participants})
	if err != nil {
		return err
	}
	out, err := sg.st.outbox(sg.cfg.RelayRequests, e)
	if err != nil {
		return err
	}
	codes, err := sg.st.commit(ctx, sg.st.reserveSeats(c.RoomID, c.Seats), put, out)
	if conditionFailed(codes, 0) {
		return errRoomFull
	}
	return err
}

// onRelayResult handles RelayAllocated / RelayFailed from media-relay.
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

	case e.Type == "RelayAllocated" && c.Status == statusPending:
		now := sg.nowFn().UnixMilli()
		started, err := newEnvelope("CallStarted", c.ID, map[string]any{
			"callId": c.ID, "roomId": c.RoomID, "kind": c.Kind, "relayId": r.RelayID, "participants": c.Participants,
		})
		if err != nil {
			return err
		}
		out, err := sg.st.outbox(destTopic, started)
		if err != nil {
			return err
		}
		set := map[string]types.AttributeValue{"relayId": str(r.RelayID), "relayHost": str(r.Host), "relayPort": num(int64(r.Port)), "startedAt": num(now)}
		if err := sg.commitOnce(ctx, dedupe, sg.st.transition(c.ID, statusPending, statusActive, set), out); err != nil {
			return err
		}
		sg.log.Info("call active", "correlationId", c.ID, "relayId", r.RelayID)
		relay := map[string]any{"callId": c.ID, "relayHost": r.Host, "relayPort": r.Port}
		sg.hub.notify(c.CallerID, "call_active", relay)
		for _, p := range c.Participants {
			if p != c.CallerID {
				sg.hub.notify(p, "incoming_call", map[string]any{"callId": c.ID, "from": c.CallerID, "relayHost": r.Host, "relayPort": r.Port})
			}
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
		sg.log.Warn("late relay released", "correlationId", c.ID, "status", c.Status, "relayId", r.RelayID)
		return sg.commitOnce(ctx, dedupe, out)

	default: // RelayFailed for a call that is no longer pending: nothing to undo.
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
	items := append(extra,
		sg.st.transition(c.ID, statusPending, statusFailed, map[string]types.AttributeValue{"reason": str(reason)}),
		sg.st.releaseSeats(c.RoomID, c.Seats), out)
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
	sg.log.Warn("call compensated", "correlationId", c.ID, "reason", reason, "seatsReleased", c.Seats)
	sg.hub.notify(c.CallerID, "call_failed", map[string]string{"callId": c.ID, "reason": reason})
	return nil
}

// end hangs up an active call and publishes CallEnded (consumed by media-relay and audit-vault).
func (sg *saga) end(ctx context.Context, c *call) error {
	now := sg.nowFn().UnixMilli()
	ended, err := newEnvelope("CallEnded", c.ID, map[string]any{
		"callId": c.ID, "relayId": c.RelayID, "durationMs": now - c.StartedAt, "reason": "hangup",
	})
	if err != nil {
		return err
	}
	out, err := sg.st.outbox(destTopic, ended)
	if err != nil {
		return err
	}
	codes, err := sg.st.commit(ctx,
		sg.st.transition(c.ID, statusActive, statusEnded, map[string]types.AttributeValue{"endedAt": num(now)}),
		sg.st.releaseSeats(c.RoomID, c.Seats), out)
	if conditionFailed(codes, 0) {
		return errStateChanged
	}
	return err
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
