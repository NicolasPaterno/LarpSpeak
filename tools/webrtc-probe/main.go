// webrtc-probe: a headless browser stand-in for end-to-end tests. It logs in
// against call-signaling, creates or joins a call, sends the same WebRTC offer
// the web client sends (web/src/voice.ts) and streams real Opus RTP (20 ms
// pacing) for a while, then leaves.
//
//	webrtc-probe -user ana -create bob -seconds 10   # prints "callId <id>" first
//	webrtc-probe -user bob -call <id> -seconds 10
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const minSlots = 10 // same offer recipe as web/src/voice.ts

// silence is a valid 20 ms CELT fullband mono Opus packet (TOC 0xF8, config 31, code 0):
// the comfort-noise frame browsers send for a muted track. 960 samples at 48 kHz.
var silence = []byte{0xF8, 0xFF, 0xFE}

var (
	server  = flag.String("server", "http://localhost:8081", "call-signaling base URL")
	user    = flag.String("user", "", "username (registered on first use)")
	pass    = flag.String("password", "larpspeak-demo", "password")
	create  = flag.String("create", "", "create a call inviting these users (comma separated)")
	callID  = flag.String("call", "", "join this call instead of creating one")
	seconds = flag.Float64("seconds", 10, "how long to send audio")
	noLeave = flag.Bool("noleave", false, "do not POST /leave at the end (just drop the connection)")
	token   string
)

func main() {
	flag.Parse()
	log.SetPrefix("probe[" + *user + "] ")
	if *user == "" {
		log.Fatal("-user is required")
	}
	_ = call("POST", "/auth/register", map[string]string{"username": *user, "password": *pass}, nil) // already exists: fine
	var auth struct{ AccessToken string }
	must(call("POST", "/auth/login", map[string]string{"username": *user, "password": *pass}, &auth))
	token = auth.AccessToken

	id := *callID
	if id == "" {
		var invited []string
		if *create != "-" {
			invited = strings.Split(*create, ",")
		}
		var res struct{ CallID string }
		must(call("POST", "/calls", map[string]any{"calleeIds": invited}, &res))
		id = res.CallID
	}
	fmt.Println("callId", id) // scripts read this to start the other probes

	var c struct {
		Status       string
		Participants []string
		Capacity     int
	}
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(300 * time.Millisecond) {
		must(call("GET", "/calls/"+id, nil, &c))
		if c.Status != "PENDING" || time.Now().After(deadline) {
			break
		}
	}
	if c.Status != "ACTIVE" {
		log.Fatalf("call %s is %s", id, c.Status)
	}
	if !slices.Contains(c.Participants, *user) {
		if err := call("POST", "/calls/"+id+"/join", nil, nil); err != nil && !strings.Contains(err.Error(), "already in") {
			log.Fatal(err)
		}
	}

	sent := stream(id, max(c.Capacity, minSlots))
	if !*noLeave {
		must(call("POST", "/calls/"+id+"/leave", nil, nil))
	}
	log.Printf("done: %d Opus packets sent (%.1fs of audio)", sent, float64(sent)*0.02)
}

// stream runs one PeerConnection like the browser does and returns the packets sent.
func stream(id string, slots int) int {
	se := webrtc.SettingEngine{}
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se)) // default media engine: Opus included
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	must(err)
	defer pc.Close()

	mic, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "mic")
	must(err)
	_, err = pc.AddTransceiverFromTrack(mic, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv})
	must(err)
	for range slots - 2 {
		_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
		must(err)
	}
	_, err = pc.CreateDataChannel("meta", nil)
	must(err)
	received := 0
	pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			if _, _, err := t.ReadRTP(); err != nil {
				return
			}
			received++
		}
	})
	connected := make(chan struct{})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("connection %s", s)
		if s == webrtc.PeerConnectionStateConnected {
			close(connected)
		}
	})

	offer, err := pc.CreateOffer(nil)
	must(err)
	gathered := webrtc.GatheringCompletePromise(pc)
	must(pc.SetLocalDescription(offer))
	<-gathered
	var ans struct{ SDP string }
	for wait := time.Second; ; wait = min(wait*2, 8*time.Second) {
		err = call("POST", "/calls/"+id+"/webrtc", map[string]string{"sdp": pc.LocalDescription().SDP}, &ans)
		if err == nil {
			break
		}
		if strings.Contains(err.Error(), "409") && !strings.Contains(err.Error(), "retry") {
			log.Fatal(err)
		}
		log.Printf("offer retry in %s: %v", wait, err)
		time.Sleep(wait)
	}
	must(pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: ans.SDP}))
	select {
	case <-connected:
	case <-time.After(15 * time.Second):
		log.Fatal("WebRTC did not connect in 15s")
	}

	n := int(*seconds * 50)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for i := 0; i < n; i++ {
		<-tick.C
		must(mic.WriteSample(media.Sample{Data: silence, Duration: 20 * time.Millisecond}))
	}
	log.Printf("received %d RTP packets from other participants", received)
	return n
}

func request(method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, *server+path, r)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(req)
}

// call sends a request and decodes a 2xx JSON answer into out; other statuses are errors.
func call(method, path string, body, out any) error {
	res, err := request(method, path, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode/100 != 2 {
		return errors.New(method + " " + path + ": " + res.Status + " " + strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
