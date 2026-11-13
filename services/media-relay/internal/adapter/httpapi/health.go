// Package httpapi is the pod-to-pod HTTP API: health checks and call adoption.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/larpspeak/media-relay/internal/app"
)

// Register serves GET /health and POST /adopt.
func Register(mux *http.ServeMux, r *app.Relay, replica string) {
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
