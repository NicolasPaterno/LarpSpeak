// Package relayhttp forwards WebRTC offers to the media-relay pod that owns a call.
package relayhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/larpspeak/call-signaling/internal/domain"
)

// Client implements app.MediaSignaler. Mint creates the join token media-relay
// checks; only call-signaling can create it (it holds the private key).
type Client struct {
	HTTP *http.Client
	Mint func(userID, callID string) string
}

func (c *Client) Offer(ctx context.Context, addr, callID, userID, sdp string) (string, error) {
	body, _ := json.Marshal(map[string]string{"callId": callID, "userId": userID, "sdp": sdp, "joinToken": c.Mint(userID, callID)})
	req, err := http.NewRequestWithContext(ctx, "POST", "http://"+addr+"/webrtc", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", domain.ErrRelayNotReady, err) // pod down: failover will move the call
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		var out struct{ SDP string }
		return out.SDP, json.NewDecoder(resp.Body).Decode(&out)
	// 404 not the owner (yet: failover), 409 our ParticipantJoined event not applied on the pod yet,
	// 503 public key not loaded yet: all settle in moments, so the browser retries.
	case http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable:
		return "", domain.ErrRelayNotReady
	default:
		return "", fmt.Errorf("relay answered HTTP %d", resp.StatusCode)
	}
}
