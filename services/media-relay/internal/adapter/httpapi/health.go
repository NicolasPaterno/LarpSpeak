// Package httpapi is the pod-to-pod HTTP API: health checks and call adoption.
package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/larpspeak/media-relay/internal/app"
	"github.com/larpspeak/media-relay/internal/domain"
)

// Register serves GET /health and POST /adopt.
// Offerer answers WebRTC offers (adapter/webrtc.SFU).
type Offerer interface {
	Offer(ctx context.Context, callID, userID, sdp string) (string, error)
}

func Register(mux *http.ServeMux, r *app.Relay, replica string, sfu Offerer, key func() ed25519.PublicKey) {
	// POST /webrtc: internal, called by call-signaling with a join token that
	// only call-signaling can create (it holds the private key).
	mux.HandleFunc("POST /webrtc", func(w http.ResponseWriter, req *http.Request) {
		var in struct{ CallID, UserID, SDP, JoinToken string }
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1<<17)).Decode(&in); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		pub := key()
		if pub == nil {
			http.Error(w, "public key not loaded yet", http.StatusServiceUnavailable)
			return
		}
		c, err := domain.VerifyJoin(pub, in.JoinToken, in.CallID, time.Now())
		if err != nil || c.Sub != in.UserID {
			http.Error(w, "invalid join token", http.StatusForbidden)
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
		defer cancel()
		answer, err := sfu.Offer(ctx, in.CallID, in.UserID, in.SDP)
		switch {
		case errors.Is(err, domain.ErrNotOwner):
			http.Error(w, err.Error(), http.StatusNotFound)
		case errors.Is(err, domain.ErrNotIn): // ParticipantJoined not applied here yet (async): retry
			http.Error(w, err.Error(), http.StatusConflict)
		case err != nil:
			r.Log.Warn("webrtc offer failed", "correlationId", in.CallID, "userId", in.UserID, "err", err)
			http.Error(w, "offer failed", http.StatusServiceUnavailable)
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"sdp": answer})
		}
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "media-relay", "replica": replica})
	})
	// The leader calls this after moving a call here, so we forward it before the first packet.
	mux.HandleFunc("POST /adopt", func(w http.ResponseWriter, req *http.Request) {
		go r.PickUp(context.WithoutCancel(req.Context()), req.URL.Query().Get("callId"))
		w.WriteHeader(http.StatusAccepted)
	})
}

var probe = &http.Client{Timeout: 500 * time.Millisecond}

// Prober implements app.Prober over HTTP.
type Prober struct{}

// AnswersHealth calls the suspect pod directly. A dead or frozen pod does not
// answer. The reply must name the suspect: a replacement pod can reuse a dead
// pod's IP address, and its answer must not keep the dead pod "alive".
func (Prober) AnswersHealth(addr, id string) bool {
	if addr == "" {
		return false
	}
	resp, err := probe.Get("http://" + addr + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var h struct {
		Replica string `json:"replica"`
	}
	return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&h) == nil && h.Replica == id
}

// NotifyAdopt tells the new owner to start forwarding the call right away. If
// it is lost, the new owner still picks the call up on its first audio packet.
func (Prober) NotifyAdopt(addr, callID string) {
	resp, err := probe.Post("http://"+addr+"/adopt?callId="+url.QueryEscape(callID), "", nil)
	if err == nil {
		resp.Body.Close()
	}
}
