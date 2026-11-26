// Package httpapi is the call API: 202 + polling for call setup, join/leave/invite.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"uuid"

	"github.com/larpspeak/call-signaling/internal/app"
	"github.com/larpspeak/call-signaling/internal/domain"
)

type Handler struct {
	Svc          *app.Service
	Auth         *app.Auth
	SSE          func(w http.ResponseWriter, r *http.Request, userID string) // notification stream of an authenticated user
	CookieSecure bool
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "call-signaling"})
	})
	mux.HandleFunc("POST /calls", h.handleCreate)
	mux.HandleFunc("GET /calls/{id}", h.handleGet)
	mux.HandleFunc("POST /calls/{id}/join", h.handleJoin)
	mux.HandleFunc("POST /calls/{id}/leave", h.handleLeave)
	mux.HandleFunc("POST /calls/{id}/invite", h.handleInvite)
	mux.HandleFunc("POST /calls/{id}/webrtc", h.handleWebRTC)
	mux.HandleFunc("POST /calls/{id}/state", h.handleState)
	h.registerAuth(mux)
}

type createCallRequest struct {
	CalleeIDs       []string `json:"calleeIds"`       // invited users, notified when the call is live
	MaxParticipants int      `json:"maxParticipants"` // seats in the call; default MAX_PARTICIPANTS (direct and group)
}

// handleCreate is the R3 async entry point: validate, run saga step 1, answer 202.
// The creator is the first participant; invited users join with POST /calls/{id}/join.
func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	sg := h.Svc
	caller, ok := h.user(w, r)
	if !ok {
		return
	}
	var req createCallRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	c := domain.Call{
		ID: uuid.New().String(), CallerID: caller, Invited: req.CalleeIDs, Status: domain.StatusPending,
		CreatedAt: sg.NowFn().UnixMilli(), Participants: []string{caller}, Taken: 1,
		Kind: domain.KindGroup, Capacity: req.MaxParticipants,
	}
	if c.Capacity == 0 {
		c.Capacity = sg.Cfg.MaxParticipants
		if len(req.CalleeIDs) == 1 {
			c.Kind = domain.KindDirect // 1:1; becomes a group when a third person joins or is invited
		}
	}
	if c.Capacity < 2 || c.Capacity > sg.Cfg.MaxParticipants {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("maxParticipants must be 2..%d", sg.Cfg.MaxParticipants)})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := sg.Start(ctx, c); err != nil {
		sg.Log.Error("create call failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "try again"})
		return
	}
	sg.Log.Info("call requested", "correlationId", c.ID, "capacity", c.Capacity, "invited", len(c.Invited))
	w.Header().Set("Location", "/calls/"+c.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"callId": c.ID, "status": c.Status, "statusUrl": "/calls/" + c.ID})
}

// handleJoin: POST /calls/{id}/join (user from the token) -> 200 with the relay address, 409 if full/inactive.
func (h *Handler) handleJoin(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	c, err := h.Svc.Join(ctx, r.PathValue("id"), user)
	switch {
	case errors.Is(err, domain.ErrRoomFull), errors.Is(err, domain.ErrNotActive), errors.Is(err, domain.ErrAlreadyIn):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case err != nil:
		h.Svc.Log.Error("join failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "try again"})
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"callId": c.ID, "relayHost": c.RelayHost, "relayPort": c.RelayPort,
			"participants": c.Participants, "taken": c.Taken, "capacity": c.Capacity,
			"muted": orEmpty(c.Muted), "deafened": orEmpty(c.Deafened),
		})
	}
}

// handleLeave: POST /calls/{id}/leave (user from the token) -> 202; the last one out ends the call.
func (h *Handler) handleLeave(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	err := h.Svc.Leave(ctx, r.PathValue("id"), user)
	switch {
	case errors.Is(err, domain.ErrNotActive), errors.Is(err, domain.ErrNotIn), errors.Is(err, domain.ErrStateChanged):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case err != nil:
		h.Svc.Log.Error("leave failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "try again"})
	default:
		writeJSON(w, http.StatusAccepted, map[string]string{"callId": r.PathValue("id"), "userId": user, "status": "left"})
	}
}

// handleInvite: POST /calls/{id}/invite {inviteeId} -> 202; a participant
// invites someone into an active call, who gets an incoming_call notification.
func (h *Handler) handleInvite(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r)
	if !ok {
		return
	}
	var req struct {
		InviteeID string `json:"inviteeId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil || req.InviteeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "inviteeId is required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	err := h.Svc.Invite(ctx, r.PathValue("id"), user, req.InviteeID)
	switch {
	case errors.Is(err, domain.ErrNotActive), errors.Is(err, domain.ErrNotIn):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case err != nil:
		h.Svc.Log.Error("invite failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "try again"})
	default:
		writeJSON(w, http.StatusAccepted, map[string]string{"callId": r.PathValue("id"), "inviteeId": req.InviteeID, "status": "invited"})
	}
}

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.user(w, r); !ok {
		return
	}
	c, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	switch {
	case err != nil:
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "try again"})
	case c == nil:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "call not found"})
	default:
		// DynamoDB drops empty sets; clients expect lists.
		c.Participants, c.Muted, c.Deafened = orEmpty(c.Participants), orEmpty(c.Muted), orEmpty(c.Deafened)
		writeJSON(w, http.StatusOK, c)
	}
}

// handleWebRTC: POST /calls/{id}/webrtc {sdp} -> 200 {sdp}. The offer goes to
// the relay pod that owns the call; 409 "relay not ready" means retry shortly.
func (h *Handler) handleWebRTC(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r)
	if !ok {
		return
	}
	var req struct {
		SDP string `json:"sdp"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<17)).Decode(&req); err != nil || req.SDP == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sdp is required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	answer, err := h.Svc.Offer(ctx, r.PathValue("id"), user, req.SDP)
	switch {
	case errors.Is(err, domain.ErrNotActive), errors.Is(err, domain.ErrNotIn), errors.Is(err, domain.ErrRelayNotReady):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case err != nil:
		h.Svc.Log.Error("webrtc offer failed", "correlationId", r.PathValue("id"), "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "try again"})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"sdp": answer})
	}
}

// handleState: POST /calls/{id}/state {muted, deafened} -> 202; others get participant_state.
func (h *Handler) handleState(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r)
	if !ok {
		return
	}
	var req struct {
		Muted    bool `json:"muted"`
		Deafened bool `json:"deafened"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "muted and deafened are required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	switch err := h.Svc.SetState(ctx, r.PathValue("id"), user, req.Muted, req.Deafened); {
	case errors.Is(err, domain.ErrNotIn):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case err != nil:
		h.Svc.Log.Error("state update failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "try again"})
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{"muted": req.Muted, "deafened": req.Deafened})
	}
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
