// Package webrtc is the browser audio path: a small SFU on pion. Opus RTP from
// each browser is forwarded untouched to the other browsers of the same call.
// app.Relay still decides who may talk (Admit/Touch), so ownership, failover
// and the participant timeout are the same as for the UDP path.
package webrtc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

// Gate is the part of app.Relay the SFU needs.
type Gate interface {
	Admit(ctx context.Context, callID, userID string) error
	Touch(callID, userID string) bool
}

// Recorder receives a copy of every Opus packet a browser sends (see app.Recorder).
// Record must not block: it runs on the audio path.
type Recorder interface {
	Record(callID, userID string, ts uint32, payload []byte)
	Flush(callID, userID string)
}

// SFU keeps the browser peers of the calls this pod relays.
//
// Fan-out without renegotiation: the browser offers one audio m-line per
// other possible participant (capacity-1). The server binds one outgoing
// "slot" track to each m-line and, when another participant talks, assigns
// them a free slot. The DataChannel "meta" tells the browser which slot (mid)
// carries which user.
type SFU struct {
	api  *webrtc.API
	gate Gate
	log  *slog.Logger
	rec  Recorder // nil = calls are not recorded

	mu    sync.Mutex
	calls map[string]map[string]*peer // callId -> userId -> peer
}

type peer struct {
	call, user string
	pc         *webrtc.PeerConnection
	slots      []*slot
	meta       *webrtc.DataChannel
}

type slot struct {
	tr    *webrtc.RTPTransceiver
	track *webrtc.TrackLocalStaticRTP
	owner string // user whose audio this slot carries; "" = free
}

// New builds the pion API: Opus only, every ICE candidate on one UDP port
// (one published port per pod), advertised as host IP (RELAY_HOST).
func New(conn net.PacketConn, host string, gate Gate, log *slog.Logger) (*SFU, error) {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
			SDPFmtpLine: "minptime=10;useinbandfec=1"},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	ir := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, ir); err != nil { // NACK + RTCP reports (browser stats work)
		return nil, err
	}
	se := webrtc.SettingEngine{}
	se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, conn))
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	if host != "" {
		se.SetNAT1To1IPs([]string{host}, webrtc.ICECandidateTypeHost)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(ir), webrtc.WithSettingEngine(se))
	return &SFU{api: api, gate: gate, log: log, calls: map[string]map[string]*peer{}}, nil
}

// SetRecorder turns call recording on. Call it before the first offer arrives.
func (s *SFU) SetRecorder(r Recorder) { s.rec = r }

// Offer answers a browser's SDP offer (ICE already gathered: no trickle). A
// second offer from the same user replaces the old peer, so retries after a
// failover never duplicate audio.
func (s *SFU) Offer(ctx context.Context, callID, userID, sdp string) (string, error) {
	if err := s.gate.Admit(ctx, callID, userID); err != nil {
		return "", err
	}
	pc, err := s.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return "", err
	}
	p := &peer{call: callID, user: userID, pc: pc}
	fail := func(err error) (string, error) { _ = pc.Close(); return "", err }

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "meta" {
			return
		}
		dc.OnOpen(func() {
			s.mu.Lock()
			p.meta = dc
			s.sendMeta(p)
			s.mu.Unlock()
		})
	})
	pc.OnTrack(func(remote *webrtc.TrackRemote, recv *webrtc.RTPReceiver) { s.uplink(p, remote, recv) })
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		if st == webrtc.PeerConnectionStateFailed || st == webrtc.PeerConnectionStateClosed {
			s.drop(p)
		}
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
		return fail(err)
	}
	// One outgoing slot track per audio m-line the browser offered. AddTrack
	// reuses the first offered transceiver that has no sender yet, so exactly
	// n calls bind n slots and the answer gets no extra m-lines.
	n := 0
	for _, tr := range pc.GetTransceivers() {
		if tr.Kind() == webrtc.RTPCodecTypeAudio {
			n++
		}
	}
	for i := range n {
		track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
			"slot"+strconv.Itoa(i), "slot"+strconv.Itoa(i)) // own stream per slot: one <audio> each
		if err != nil {
			return fail(err)
		}
		sender, err := pc.AddTrack(track)
		if err != nil {
			return fail(err)
		}
		go drainRTCP(sender)
		for _, tr := range pc.GetTransceivers() {
			if tr.Sender() == sender {
				p.slots = append(p.slots, &slot{tr: tr, track: track})
			}
		}
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return fail(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		return fail(err)
	}
	select {
	case <-gathered: // one UDP mux port: gathering is instant
	case <-time.After(3 * time.Second):
		return fail(errors.New("ICE gathering timed out"))
	case <-ctx.Done():
		return fail(ctx.Err())
	}

	s.mu.Lock()
	if s.calls[callID] == nil {
		s.calls[callID] = map[string]*peer{}
	}
	old := s.calls[callID][userID]
	s.calls[callID][userID] = p
	s.mu.Unlock()
	if old != nil {
		_ = old.pc.Close() // drop() ignores it: no longer the current peer
		if s.rec != nil {
			s.rec.Flush(callID, userID) // the new peer starts a new RTP timeline: cut the chunk here
		}
	}
	s.log.Info("webrtc peer connected", "correlationId", callID, "userId", userID, "slots", len(p.slots), "replaced", old != nil)
	return pc.LocalDescription().SDP, nil
}

// uplink forwards one browser's audio to every other peer of the call and keeps
// the participant alive. A packet the gate refuses (left, call ended, call moved
// to another pod) closes the peer.
func (s *SFU) uplink(p *peer, remote *webrtc.TrackRemote, recv *webrtc.RTPReceiver) {
	go func() { // RTCP counts as a sign of life too
		for {
			if _, _, err := recv.ReadRTCP(); err != nil {
				return
			}
			s.gate.Touch(p.call, p.user)
		}
	}()
	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			return
		}
		if !s.gate.Touch(p.call, p.user) {
			s.log.Info("webrtc peer refused, closing", "correlationId", p.call, "userId", p.user)
			_ = p.pc.Close()
			return
		}
		var out []*webrtc.TrackLocalStaticRTP
		s.mu.Lock()
		if s.calls[p.call][p.user] != p { // replaced by a newer offer
			s.mu.Unlock()
			return
		}
		for u, o := range s.calls[p.call] {
			if u == p.user {
				continue
			}
			if sl := o.slotFor(p.user); sl != nil {
				out = append(out, sl.track)
			} else if sl := o.freeSlot(); sl != nil {
				sl.owner = p.user
				out = append(out, sl.track)
				s.sendMeta(o)
			}
		}
		s.mu.Unlock()
		if s.rec != nil { // the gate already accepted this participant: record every packet, muted or not
			s.rec.Record(p.call, p.user, pkt.Timestamp, pkt.Payload)
		}
		for _, t := range out {
			_ = t.WriteRTP(pkt)
		}
	}
}

// drop removes a closed peer and frees the slots it occupied in other peers.
func (s *SFU) drop(p *peer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls[p.call][p.user] != p {
		return
	}
	delete(s.calls[p.call], p.user)
	if s.rec != nil {
		s.rec.Flush(p.call, p.user) // keep the last seconds of audio
	}
	for _, o := range s.calls[p.call] {
		if sl := o.slotFor(p.user); sl != nil {
			sl.owner = ""
			s.sendMeta(o)
		}
	}
	if len(s.calls[p.call]) == 0 {
		delete(s.calls, p.call)
	}
	s.log.Info("webrtc peer closed", "correlationId", p.call, "userId", p.user)
}

func (p *peer) slotFor(user string) *slot {
	for _, sl := range p.slots {
		if sl.owner == user {
			return sl
		}
	}
	return nil
}

func (p *peer) freeSlot() *slot { return p.slotFor("") }

// sendMeta pushes {"slots":{"<mid>":"<userId>"}} to the browser. Caller holds s.mu.
func (s *SFU) sendMeta(p *peer) {
	if p.meta == nil {
		return
	}
	slots := map[string]string{}
	for _, sl := range p.slots {
		if sl.owner != "" {
			slots[sl.tr.Mid()] = sl.owner
		}
	}
	b, _ := json.Marshal(map[string]any{"slots": slots})
	_ = p.meta.SendText(string(b))
}

func drainRTCP(sender *webrtc.RTPSender) { // interceptors need RTCP read for NACK
	for {
		if _, _, err := sender.ReadRTCP(); err != nil {
			return
		}
	}
}
