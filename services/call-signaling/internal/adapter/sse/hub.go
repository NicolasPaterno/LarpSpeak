// Package sse pushes user notifications over Server-Sent Events.
package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Hub pushes notifications ("you're being called", call active/failed) to
// connected users over Server-Sent Events. In-memory: one call-signaling instance.
type Hub struct {
	mu     sync.Mutex
	subs   map[string]map[chan []byte]struct{}
	Replay func(ctx context.Context, userID string) // resends what a newly connected user missed
}

func New() *Hub { return &Hub{subs: map[string]map[chan []byte]struct{}{}} }

func (h *Hub) Notify(userID, event string, data any) {
	msg, _ := json.Marshal(data)
	frame := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, msg))
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[userID] {
		select {
		case ch <- frame:
		default: // slow client: drop rather than block the saga
		}
	}
}

// Serve streams the notifications of an already authenticated user.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, userID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan []byte, 16)
	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = map[chan []byte]struct{}{}
	}
	h.subs[userID][ch] = struct{}{}
	h.mu.Unlock()
	if h.Replay != nil {
		go h.Replay(r.Context(), userID)
	}
	defer func() {
		h.mu.Lock()
		delete(h.subs[userID], ch)
		h.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case frame := <-ch:
			if _, err := w.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
