// Package httpapi is the call API: 202 + polling for call setup, join/leave/invite.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
	"uuid"

	"github.com/larpspeak/call-signaling/internal/app"
	"github.com/larpspeak/call-signaling/internal/domain"
)

type Handler struct{ Svc *app.Service }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "call-signaling"})
	})
	mux.HandleFunc("POST /calls", h.handleCreate)
	mux.HandleFunc("GET /calls/{id}", h.handleGet)
	mux.HandleFunc("POST /calls/{id}/join", h.handleJoin)
	mux.HandleFunc("POST /calls/{id}/leave", h.handleLeave)
	mux.HandleFunc("POST /calls/{id}/invite", h.handleInvite)
}

type createCallRequest struct {
	CallerID        string   `json:"callerId"`
	CalleeIDs       []string `json:"calleeIds"`       // invited users, notified when the call is live
	MaxParticipants int      `json:"maxParticipants"` // seats in the call; default 2 for 1:1, MAX_PARTICIPANTS otherwise
}

// handleCreate is the R3 async entry point: validate, run saga step 1, answer 202.
// The creator is the first participant; invited users join with POST /calls/{id}/join.
func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	sg := h.Svc
	var req createCallRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil || req.CallerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "callerId is required"})
		return
	}
	c := domain.Call{
		ID: uuid.New().String(), CallerID: req.CallerID, Invited: req.CalleeIDs, Status: domain.StatusPending,
		CreatedAt: sg.NowFn().UnixMilli(), Participants: []string{req.CallerID}, Taken: 1,
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

type userRequest struct {
	UserID string `json:"userId"`
}

func decodeUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req userRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil || req.UserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "userId is required"})
		return "", false
	}
	return req.UserID, true
}

// handleJoin: POST /calls/{id}/join {userId} -> 200 with the relay address, 409 if full/inactive.
func (h *Handler) handleJoin(w http.ResponseWriter, r *http.Request) {
	user, ok := decodeUser(w, r)
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
		})
	}
}

// handleLeave: POST /calls/{id}/leave {userId} -> 202; the last one out ends the call.
func (h *Handler) handleLeave(w http.ResponseWriter, r *http.Request) {
	user, ok := decodeUser(w, r)
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

// handleInvite: POST /calls/{id}/invite {userId, inviteeId} -> 202; a participant
// invites someone into an active call, who gets an incoming_call notification.
func (h *Handler) handleInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID    string `json:"userId"`
		InviteeID string `json:"inviteeId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil || req.UserID == "" || req.InviteeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "userId and inviteeId are required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	err := h.Svc.Invite(ctx, r.PathValue("id"), req.UserID, req.InviteeID)
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
	c, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	switch {
	case err != nil:
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "try again"})
	case c == nil:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "call not found"})
	default:
		if c.Participants == nil {
			c.Participants = []string{} // DynamoDB drops empty sets; clients expect a list
		}
		writeJSON(w, http.StatusOK, c)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
