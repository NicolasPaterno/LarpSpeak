// voice-client: talk in a Larpspeak call from the terminal.
//
// Needs sox (mic + speakers): brew install sox | apt install sox.
// Use headphones: there is no echo cancellation.
//
//	voice-client -user ana -create -max 5 -invite bob,carl   # create a call
//	voice-client -user bob -join <callId>                    # join a call
//	voice-client -user carl -listen                          # join when invited
//
// Testing alone on one computer: run a second client with -mute in another
// terminal; it only plays what it receives, so you hear yourself with the
// call's real delay. Add -loss 0.1 to hear the packet-loss recovery.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

type callInfo struct {
	CallID    string `json:"callId"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	RelayHost string `json:"relayHost"`
	RelayPort int    `json:"relayPort"`
}

func main() {
	server := flag.String("server", "http://localhost:8081", "call-signaling base URL")
	user := flag.String("user", "", "your user id (required)")
	create := flag.Bool("create", false, "create a new call")
	maxP := flag.Int("max", 0, "max participants when creating (0 = server default)")
	invite := flag.String("invite", "", "comma-separated users to invite when creating")
	join := flag.String("join", "", "call id to join")
	listen := flag.Bool("listen", false, "wait for an invitation and join it")
	relay := flag.String("relay", "", "override relay host (if the server reports an address you cannot reach)")
	mute := flag.Bool("mute", false, "send silence instead of the microphone (one-computer testing)")
	loss := flag.Float64("loss", 0, "drop this fraction of received packets on purpose (e.g. 0.1)")
	flag.Parse()
	if *user == "" || (!*create && *join == "" && !*listen) {
		flag.Usage()
		os.Exit(2)
	}
	log.SetFlags(log.Ltime)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	api := &client{base: strings.TrimRight(*server, "/"), user: *user}
	invites := make(chan string, 1)
	go api.notifications(ctx, invites)

	var info callInfo
	var err error
	switch {
	case *create:
		var invited []string
		if *invite != "" {
			invited = strings.Split(*invite, ",")
		}
		info, err = api.create(ctx, invited, *maxP)
	case *join != "":
		info, err = api.join(ctx, *join)
	default:
		log.Printf("waiting for an invitation as %q (Ctrl+C to quit)...", *user)
		select {
		case id := <-invites:
			info, err = api.join(ctx, id)
		case <-ctx.Done():
			return
		}
	}
	if err != nil {
		log.Fatal(err)
	}
	if *relay != "" {
		info.RelayHost = *relay
	}
	log.Printf("in call %s via relay %s:%d - talk! (Ctrl+C to leave)", info.CallID, info.RelayHost, info.RelayPort)
	err = talk(ctx, info, *user, *mute, *loss)
	if lerr := api.leave(info.CallID); lerr != nil {
		log.Printf("leave: %v", lerr)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
	log.Print("left the call")
}

// talk runs full duplex audio: mic -> UDP and UDP -> jitter buffers -> mixer -> speakers.
func talk(ctx context.Context, info callInfo, user string, mute bool, loss float64) error {
	conn, err := net.Dial("udp", net.JoinHostPort(info.RelayHost, strconv.Itoa(info.RelayPort)))
	if err != nil {
		return err
	}
	defer conn.Close()

	pcm := []string{"-q", "--buffer", strconv.Itoa(frameBytes), "-t", "raw", "-r", strconv.Itoa(sampleRate), "-e", "signed-integer", "-b", "16", "-c", "1"}
	play := exec.CommandContext(ctx, "sox", append(append([]string{}, pcm...), "-", "-d")...)
	speaker, err := play.StdinPipe()
	if err != nil {
		return err
	}
	cmds := []*exec.Cmd{play}
	var mic io.Reader = silence{}
	if !mute {
		rec := exec.CommandContext(ctx, "sox", append(append([]string{"-d"}, pcm...), "-")...)
		if mic, err = rec.StdoutPipe(); err != nil {
			return err
		}
		cmds = append(cmds, rec)
	}
	for _, c := range cmds {
		if err := c.Start(); err != nil {
			return fmt.Errorf("starting sox (is it installed?): %w", err)
		}
	}

	m := newMixer()
	errs := make(chan error, 3)
	go func() { errs <- send(conn, mic, info.CallID, user) }()
	go func() { errs <- receive(conn, m, info.CallID, loss) }()
	go func() { errs <- playout(ctx, m, speaker) }()
	go reportStats(ctx, m)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errs:
		return err
	}
}

// send reads 20 ms of mic audio at a time. Each packet carries the current
// frame plus the previous one, so the receiver can rebuild a single lost packet.
func send(conn net.Conn, mic io.Reader, callID, user string) error {
	cur, prev := make([]byte, frameBytes), make([]byte, frameBytes)
	for seq := uint32(1); ; seq++ {
		if _, err := io.ReadFull(mic, cur); err != nil {
			return fmt.Errorf("mic: %w", err)
		}
		pkt := fmt.Appendf(nil, "%s %s %d\n", callID, user, seq)
		pkt = append(append(pkt, cur...), prev...)
		if _, err := conn.Write(pkt); err != nil {
			log.Printf("send: %v", err) // keep going: UDP errors are transient
		}
		cur, prev = prev, cur
	}
}

// silence stands in for the microphone with -mute: 20 ms of zeros every 20 ms.
type silence struct{}

func (silence) Read(p []byte) (int, error) {
	time.Sleep(frameDuration)
	clear(p)
	return len(p), nil
}

func receive(conn net.Conn, m *mixer, callID string, loss float64) error {
	buf := make([]byte, 64*1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) || strings.Contains(err.Error(), "refused") {
				continue // relay not reachable yet / ICMP noise
			}
			return err
		}
		if rand.Float64() < loss {
			continue // simulated network loss
		}
		head, payload, ok := bytes.Cut(buf[:n], []byte("\n"))
		f := strings.Fields(string(head))
		if !ok || len(f) < 3 || f[0] != callID || len(payload) < frameBytes {
			continue
		}
		seq, err := strconv.ParseUint(f[2], 10, 32)
		if err != nil {
			continue
		}
		var prev []byte
		if len(payload) >= 2*frameBytes {
			prev = payload[frameBytes : 2*frameBytes]
		}
		m.push(f[1], uint32(seq), payload[:frameBytes], prev, time.Now())
	}
}

func playout(ctx context.Context, m *mixer, speaker io.Writer) error {
	t := time.NewTicker(frameDuration)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-t.C:
			if _, err := speaker.Write(m.tick(now)); err != nil {
				return fmt.Errorf("speaker: %w", err)
			}
		}
	}
}

func reportStats(ctx context.Context, m *mixer) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.mu.Lock()
			s, speakers := m.stats, len(m.streams)
			m.mu.Unlock()
			log.Printf("audio: %d speakers | packets %d | late %d | recovered %d | concealed %d | lost %d",
				speakers, s.received, s.late, s.recovered, s.concealed, s.lost)
		}
	}
}

// --- call-signaling API ---

type client struct{ base, user string }

func (c *client) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return resp.StatusCode, json.Unmarshal(data, out)
	}
	return resp.StatusCode, nil
}

// create asks for a call (202) and polls until the relay is ready (R3 from the client side).
func (c *client) create(ctx context.Context, invited []string, maxP int) (callInfo, error) {
	var info callInfo
	body := map[string]any{"callerId": c.user, "calleeIds": invited, "maxParticipants": maxP}
	if _, err := c.do(ctx, "POST", "/calls", body, &info); err != nil {
		return info, err
	}
	log.Printf("call %s requested, waiting for a relay...", info.CallID)
	for range 75 { // ~15 s
		time.Sleep(200 * time.Millisecond)
		if _, err := c.do(ctx, "GET", "/calls/"+info.CallID, nil, &info); err != nil {
			return info, err
		}
		switch info.Status {
		case "ACTIVE":
			log.Printf("share this to let others join: -join %s", info.CallID)
			return info, nil
		case "FAILED", "ENDED":
			return info, fmt.Errorf("call %s: %s", info.Status, info.Reason)
		}
	}
	return info, errors.New("timed out waiting for the call to become active")
}

func (c *client) join(ctx context.Context, callID string) (callInfo, error) {
	var info callInfo
	_, err := c.do(ctx, "POST", "/calls/"+callID+"/join", map[string]string{"userId": c.user}, &info)
	return info, err
}

func (c *client) leave(callID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := c.do(ctx, "POST", "/calls/"+callID+"/leave", map[string]string{"userId": c.user}, nil)
	return err
}

// notifications prints the user's SSE events and forwards invitations.
func (c *client) notifications(ctx context.Context, invites chan<- string) {
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, "GET", c.base+"/users/"+c.user+"/notifications", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		var event string
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				var d struct {
					CallID string `json:"callId"`
					From   string `json:"from"`
					UserID string `json:"userId"`
					Reason string `json:"reason"`
				}
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &d)
				switch event {
				case "incoming_call":
					log.Printf("%s is calling you (call %s)", d.From, d.CallID)
					select {
					case invites <- d.CallID:
					default:
					}
				case "participant_joined", "participant_left":
					log.Printf("%s %s", d.UserID, strings.TrimPrefix(event, "participant_"))
				case "call_ended", "call_failed":
					log.Printf("call %s: %s", strings.TrimPrefix(event, "call_"), d.Reason)
				}
			}
		}
		resp.Body.Close()
	}
}
