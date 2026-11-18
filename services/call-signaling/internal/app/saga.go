package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/larpspeak/call-signaling/internal/config"
	"github.com/larpspeak/call-signaling/internal/domain"
)

// Service orchestrates call setup: create call (creator holds a seat) -> request
// relay -> activate, compensating (free seats, notify creator, publish CallFailed)
// when the relay fails, times out, or answers too late. It also handles the
// channel lifecycle afterwards: join, leave, end.
type Service struct {
	St    CallStore
	Hub   Notifier
	Log   *slog.Logger
	Cfg   config.Config
	NowFn func() time.Time
	Media MediaSignaler // WebRTC offers to the owning relay pod
}

type relayResult struct {
	CallID  string `json:"callId"`
	UserID  string `json:"userId"`
	RelayID string `json:"relayId"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Signal  string `json:"signalAddr"` // optional (v1 additive): owning pod's HTTP addr
	Reason  string `json:"reason"`
}

func message(dest, typ, callID string, payload any) (domain.Outgoing, error) {
	e, err := domain.NewEnvelope(typ, callID, payload)
	return domain.Outgoing{Dest: dest, Envelope: e}, err
}

// Get returns a call, or nil if it does not exist.
func (s *Service) Get(ctx context.Context, id string) (*domain.Call, error) { return s.St.Get(ctx, id) }

// Start is step 1: call (creator inside, 1 seat taken) + RelayRequested outbox entry, atomically.
func (s *Service) Start(ctx context.Context, c domain.Call) error {
	out, err := message(s.Cfg.RelayRequests, "RelayRequested", c.ID, map[string]any{"callId": c.ID, "participants": c.Participants})
	if err != nil {
		return err
	}
	if err := s.moveOut(ctx, c.CallerID, c.ID); err != nil {
		return err
	}
	return s.St.Create(ctx, c, out)
}

// moveOut makes a user leave whatever call they are in before entering target,
// like Discord does when you join another voice channel. Stale records (call
// already over, or user no longer inside) are just cleaned up.
func (s *Service) moveOut(ctx context.Context, user, target string) error {
	prev, err := s.St.CurrentCall(ctx, user)
	if err != nil || prev == "" || prev == target {
		return err
	}
	switch err := s.leave(ctx, prev, user, ""); err {
	case nil:
		s.Log.Info("participant moved", "userId", user, "from", prev, "to", target)
		s.Hub.Notify(user, "left_call", map[string]string{"callId": prev, "reason": "joined another call"})
	case domain.ErrNotActive, domain.ErrNotIn:
		s.St.ForgetUser(ctx, user, prev)
	default:
		return err
	}
	return nil
}

// OnRelayResult handles RelayAllocated / RelayFailed / RelayMoved / ParticipantTimedOut from media-relay.
func (s *Service) OnRelayResult(ctx context.Context, e domain.Envelope) error {
	var r relayResult
	if err := json.Unmarshal(e.Payload, &r); err != nil || r.CallID == "" {
		return fmt.Errorf("bad %s payload", e.Type)
	}
	c, err := s.St.Get(ctx, r.CallID)
	if err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("unknown call %s", r.CallID)
	}
	msgID := e.MessageID

	switch {
	case e.Type == "RelayFailed" && c.Status == domain.StatusPending:
		return s.compensate(ctx, c, "relay failed: "+r.Reason, msgID)

	case e.Type == "RelayMoved" && c.Status == domain.StatusActive:
		// The relay replica died and another one adopted the call: point everyone to it.
		if err := s.St.MoveRelay(ctx, msgID, c.ID, r.Host, r.Port, r.Signal); err != nil {
			return err
		}
		s.Log.Warn("relay moved", "correlationId", c.ID, "host", r.Host, "port", r.Port)
		for _, u := range c.Participants {
			s.Hub.Notify(u, "relay_moved", map[string]any{"callId": c.ID, "relayHost": r.Host, "relayPort": r.Port})
		}
		return nil

	case e.Type == "ParticipantTimedOut":
		// The user's client stopped sending packets (crash, lost network): remove
		// them like a normal leave. Only the last one out ends the call.
		err := s.leave(ctx, c.ID, r.UserID, msgID)
		if errors.Is(err, domain.ErrNotActive) || errors.Is(err, domain.ErrNotIn) { // already gone
			return s.St.Ack(ctx, msgID)
		}
		if err == nil {
			s.Hub.Notify(r.UserID, "left_call", map[string]string{"callId": c.ID, "reason": "connection lost"})
		}
		return err

	case e.Type == "RelayAllocated" && c.Status == domain.StatusPending:
		now := s.NowFn().UnixMilli()
		out, err := message(domain.DestTopic, "CallStarted", c.ID, map[string]any{
			"callId": c.ID, "kind": c.Kind, "capacity": c.Capacity, "relayId": r.RelayID, "participants": c.Participants,
		})
		if err != nil {
			return err
		}
		c.RelayID, c.RelayHost, c.RelayPort, c.RelaySignal, c.StartedAt = r.RelayID, r.Host, r.Port, r.Signal, now
		if err := s.St.Activate(ctx, msgID, *c, out); err != nil {
			return err
		}
		s.Log.Info("call active", "correlationId", c.ID, "relayId", r.RelayID)
		s.Hub.Notify(c.CallerID, "call_active", map[string]any{"callId": c.ID, "relayHost": r.Host, "relayPort": r.Port})
		for _, u := range c.Invited {
			s.Hub.Notify(u, "incoming_call", map[string]any{"callId": c.ID, "from": c.CallerID})
		}
		return nil

	case e.Type == "RelayAllocated":
		// Late answer (F7): the saga already gave up, so give the relay back.
		out, err := message(s.Cfg.RelayRequests, "ReleaseRelayRequested", c.ID, map[string]string{"callId": c.ID, "relayId": r.RelayID})
		if err != nil {
			return err
		}
		if err := s.St.Ack(ctx, msgID, out); err != nil {
			return err
		}
		s.Log.Warn("late relay released", "correlationId", c.ID, "status", c.Status, "relayId", r.RelayID)
		return nil

	default: // RelayFailed/RelayMoved for a call no longer in that state: nothing to do.
		return s.St.Ack(ctx, msgID)
	}
}

// compensate undoes step 1. The status condition makes it run at most once,
// even if the timeout sweeper and a RelayFailed race each other.
func (s *Service) compensate(ctx context.Context, c *domain.Call, reason, msgID string) error {
	out, err := message(domain.DestTopic, "CallFailed", c.ID, map[string]string{"callId": c.ID, "reason": reason})
	if err != nil {
		return err
	}
	if err := s.St.Fail(ctx, msgID, c.ID, reason, out); err != nil {
		return err
	}
	s.Log.Warn("call compensated", "correlationId", c.ID, "reason", reason, "seatsReleased", c.Taken)
	s.St.ForgetUser(ctx, c.CallerID, c.ID)
	s.Hub.Notify(c.CallerID, "call_failed", map[string]string{"callId": c.ID, "reason": reason})
	return nil
}

// Join puts a user into an active call if a seat is free (R2).
func (s *Service) Join(ctx context.Context, id, user string) (*domain.Call, error) {
	out, err := message(domain.DestTopic, "ParticipantJoined", id, map[string]string{"callId": id, "userId": user})
	if err != nil {
		return nil, err
	}
	// Check the target first, so a full or finished call does not kick the user out of their current one.
	if err := s.canJoin(ctx, id, user); err != nil {
		return nil, err
	}
	if err := s.moveOut(ctx, user, id); err != nil {
		return nil, err
	}
	err = s.St.Join(ctx, id, user, out)
	if errors.Is(err, domain.ErrSeatLost) { // lost a race for the last seat (or the call just ended)
		if err := s.canJoin(ctx, id, user); err != nil {
			return nil, err
		}
		return nil, domain.ErrRoomFull
	}
	if err != nil {
		return nil, err
	}
	c, err := s.St.Get(ctx, id)
	if err != nil || c == nil {
		return nil, fmt.Errorf("reading call after join: %w", err)
	}
	s.Log.Info("participant joined", "correlationId", id, "userId", user, "taken", c.Taken, "capacity", c.Capacity)
	if c.Taken > 2 {
		s.makeGroup(ctx, c)
	}
	s.broadcast(c, user, "participant_joined")
	return c, nil
}

// makeGroup turns a direct (1:1) call into a group call, like Discord does when
// a third person enters a DM call. Best effort: it is only a label, and the
// next join or invite retries it.
func (s *Service) makeGroup(ctx context.Context, c *domain.Call) {
	if c.Kind != domain.KindDirect {
		return
	}
	if err := s.St.MakeGroup(ctx, c.ID); err != nil {
		s.Log.Warn("call kind update failed", "correlationId", c.ID, "err", err)
		return
	}
	c.Kind = domain.KindGroup
	s.Log.Info("call became group", "correlationId", c.ID)
}

// canJoin says whether user could join call id right now.
func (s *Service) canJoin(ctx context.Context, id, user string) error {
	c, err := s.St.Get(ctx, id)
	switch {
	case err != nil:
		return err
	case c == nil || c.Status != domain.StatusActive:
		return domain.ErrNotActive
	case c.Has(user):
		return domain.ErrAlreadyIn
	case c.Taken >= c.Capacity:
		return domain.ErrRoomFull
	}
	return nil
}

// Leave frees the user's seat; the last one out ends the call.
func (s *Service) Leave(ctx context.Context, id, user string) error {
	return s.leave(ctx, id, user, "")
}

// leave frees the user's seat; the last one out ends the call (the only way a
// call ends). Retries when someone else joined/left between our read and our
// write. msgID (when set) marks the triggering message processed in the same write.
func (s *Service) leave(ctx context.Context, id, user, msgID string) error {
	for range 3 {
		c, err := s.St.Get(ctx, id)
		switch {
		case err != nil:
			return err
		case c == nil || c.Status != domain.StatusActive:
			return domain.ErrNotActive
		case !c.Has(user):
			return domain.ErrNotIn
		}
		now := s.NowFn().UnixMilli()
		last := c.Taken == 1
		left, err := message(domain.DestTopic, "ParticipantLeft", id, map[string]string{"callId": id, "userId": user})
		if err != nil {
			return err
		}
		out := []domain.Outgoing{left}
		if last {
			ended, err := message(domain.DestTopic, "CallEnded", id, map[string]any{
				"callId": id, "relayId": c.RelayID, "durationMs": now - c.StartedAt, "reason": "everyone left",
			})
			if err != nil {
				return err
			}
			out = append(out, ended)
		}
		err = s.St.Leave(ctx, msgID, c, user, last, now, out...)
		if errors.Is(err, domain.ErrStateChanged) {
			continue // counter moved under us: read again
		}
		if err != nil {
			return err
		}
		s.Log.Info("participant left", "correlationId", id, "userId", user, "callEnded", last)
		c.Participants = slices.DeleteFunc(c.Participants, func(p string) bool { return p == user })
		s.broadcast(c, user, "participant_left")
		if last {
			s.Log.Info("call ended", "correlationId", id, "reason", "everyone left")
		}
		return nil
	}
	return domain.ErrStateChanged
}

// Invite adds a user to an active call's invite list and notifies them. Only a
// participant can invite. Inviting twice just notifies again.
func (s *Service) Invite(ctx context.Context, id, from, to string) error {
	c, err := s.St.Get(ctx, id)
	switch {
	case err != nil:
		return err
	case c == nil || c.Status != domain.StatusActive:
		return domain.ErrNotActive
	case !c.Has(from):
		return domain.ErrNotIn
	}
	if !slices.Contains(c.Invited, to) {
		if err := s.St.AddInvite(ctx, id, to); err != nil {
			return err
		}
		if !c.Has(to) {
			s.makeGroup(ctx, c) // someone new is coming: no longer 1:1
		}
	}
	s.Hub.Notify(to, "incoming_call", map[string]any{"callId": id, "from": from})
	s.Log.Info("participant invited", "correlationId", id, "userId", from, "inviteeId", to)
	return nil
}

// Offer sends a participant's WebRTC offer to the relay pod that owns the call
// and returns its answer. The browser retries ErrRelayNotReady (allocation or
// failover in progress).
func (s *Service) Offer(ctx context.Context, id, user, sdp string) (string, error) {
	c, err := s.St.Get(ctx, id)
	switch {
	case err != nil:
		return "", err
	case c == nil || c.Status != domain.StatusActive:
		return "", domain.ErrNotActive
	case !c.Has(user):
		return "", domain.ErrNotIn
	case c.RelaySignal == "":
		return "", domain.ErrRelayNotReady
	}
	return s.Media.Offer(ctx, c.RelaySignal, id, user, sdp)
}

// SetState stores mute/deafen so the others see the indicators, then tells
// every participant (SSE participant_state).
func (s *Service) SetState(ctx context.Context, id, user string, muted, deafened bool) error {
	if err := s.St.SetState(ctx, id, user, muted, deafened); err != nil {
		return err
	}
	c, err := s.St.Get(ctx, id)
	if err != nil || c == nil {
		return err
	}
	for _, u := range c.Participants {
		s.Hub.Notify(u, "participant_state", map[string]any{"callId": id, "userId": user, "muted": muted, "deafened": deafened})
	}
	return nil
}

// ReplayInvites resends invitations the user missed while not connected:
// active calls that invited them and that they have not joined yet.
func (s *Service) ReplayInvites(ctx context.Context, user string) {
	calls, err := s.St.ActiveInvites(ctx, user)
	if err != nil {
		s.Log.Warn("invite replay failed", "userId", user, "err", err)
		return
	}
	for i := range calls {
		if !calls[i].Has(user) {
			s.Hub.Notify(user, "incoming_call", map[string]any{"callId": calls[i].ID, "from": calls[i].CallerID})
		}
	}
}

// broadcast tells everyone still in the call that user joined or left.
func (s *Service) broadcast(c *domain.Call, user, event string) {
	for _, u := range c.Participants {
		if u != user {
			s.Hub.Notify(u, event, map[string]any{"callId": c.ID, "userId": user, "participants": c.Participants})
		}
	}
}

// SweepTimeouts compensates calls that waited longer than RELAY_TIMEOUT_MS.
func (s *Service) SweepTimeouts(ctx context.Context) {
	for ctx.Err() == nil {
		time.Sleep(time.Second)
		s.sweepOnce(ctx)
	}
}

func (s *Service) sweepOnce(ctx context.Context) {
	calls, err := s.St.Pending(ctx)
	if err != nil {
		s.Log.Warn("timeout sweep failed, retrying", "err", err)
		return
	}
	deadline := s.NowFn().Add(-s.Cfg.RelayTimeout).UnixMilli()
	for i := range calls {
		if calls[i].CreatedAt < deadline {
			if err := s.compensate(ctx, &calls[i], "relay timeout", ""); err != nil && err != domain.ErrStateChanged {
				s.Log.Warn("timeout compensation failed", "correlationId", calls[i].ID, "err", err)
			}
		}
	}
}
