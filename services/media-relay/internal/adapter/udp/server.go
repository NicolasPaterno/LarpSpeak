// Package udp moves audio packets; app.Relay decides where they go.
package udp

import (
	"context"
	"log/slog"
	"net"

	"github.com/larpspeak/media-relay/internal/app"
	"github.com/larpspeak/media-relay/internal/domain"
)

// Serve forwards audio. Packet = "<callId> <userId> [anything]\n" + payload;
// the relay only reads callId and userId and forwards the packet untouched.
func Serve(ctx context.Context, conn *net.UDPConn, r *app.Relay, log *slog.Logger) {
	buf := make([]byte, 64*1024)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Error("udp read failed", "err", err)
			return
		}
		callID, userID, ok := domain.ParseHeader(buf[:n])
		if !ok {
			continue
		}
		for _, t := range r.Forward(ctx, callID, userID, from) {
			_, _ = conn.WriteToUDP(buf[:n], t)
		}
	}
}
