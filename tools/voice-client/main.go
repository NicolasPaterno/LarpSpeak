// voice-client: talk in a Larpspeak call from the terminal.
//
// Needs sox (mic + speakers): brew install sox | apt install sox.
// Use headphones: there is no echo cancellation.
//
//	voice-client -user ana -create -max 5 -invite bob,carl   # create a call
//	voice-client -user bob -join <callId>                    # join a call
//	voice-client -user carl -listen                          # join when invited
//
// In a call, type a command and press Enter:
//
//	m          mute / unmute the microphone      (or: kill -USR1 <pid>)
//	d          deafen / undeafen (also mutes)    (or: kill -USR2 <pid>)
//	i <user>   invite someone into this call
//	q          leave the call
//
// Testing alone on one computer: run a second client with -nomic in another
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
	"sync"
	"sync/atomic"
	"syscall"
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
	noMic := flag.Bool("nomic", false, "never open the microphone, only listen (one-computer testing)")
	mute := flag.Bool("mute", false, "start muted")
	deafen := flag.Bool("deafen", false, "start deafened: hear nothing and send silence")
	loss := flag.Float64("loss", 0, "drop this fraction of received packets on purpose (e.g. 0.1)")
	password := flag.String("password", envOr("LARPSPEAK_PASSWORD", "larpspeak-demo"), "account password (registered on first use)")
	flag.Parse()
	if *user == "" || (!*create && *join == "" && !*listen) {
		flag.Usage()
		os.Exit(2)
	}
	log.SetFlags(log.Ltime)
	signal.Ignore(syscall.SIGUSR1, syscall.SIGUSR2) // mute/deafen toggles before joining must not kill us
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	api := &client{base: strings.TrimRight(*server, "/"), user: *user, password: *password}
	if _, err := api.token(ctx); err != nil {
		log.Fatalf("login: %v", err)
	}
	invites := make(chan string, 1)
	gone := make(chan string, 4)    // call ids we were removed from (ended, failed, moved)
	moved := make(chan callInfo, 4) // relay failover: new address for a call
	go api.notifications(ctx, invites, gone, moved)

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
	log.Printf("in call %s via relay %s:%d - talk! (m=mute d=deafen i <user>=invite q=leave)", info.CallID, info.RelayHost, info.RelayPort)
	callCtx, hangUp := context.WithCancel(ctx)
	var st state
	st.muted.Store(*mute || *noMic)
	st.deafened.Store(*deafen)
	go controls(callCtx, &st, api, info.CallID, *noMic, hangUp)
	var removed atomic.Bool
	go func() {
		for id := range gone {
			if id == info.CallID {
				removed.Store(true)
				hangUp()
				return
			}
		}
	}()
	relayAddr := func(i callInfo) (*net.UDPAddr, error) {
		if *relay != "" {
			i.RelayHost = *relay
		}
		return net.ResolveUDPAddr("udp", net.JoinHostPort(i.RelayHost, strconv.Itoa(i.RelayPort)))
	}
	var target atomic.Pointer[net.UDPAddr]
	addr, err := relayAddr(info)
	if err != nil {
		log.Fatal(err)
	}
	target.Store(addr)
	go func() {
		for m := range moved {
			if m.CallID != info.CallID {
				continue
			}
			if a, err := relayAddr(m); err == nil {
				target.Store(a)
				log.Printf("relay failed over, audio now goes to %s", a)
			}
		}
	}()
	err = talk(callCtx, info, &target, *user, *noMic, *loss, &st)
	hangUp()
	if !removed.Load() {
		if lerr := api.leave(info.CallID); lerr != nil {
			log.Printf("leave: %v", lerr)
		}
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
	log.Print("left the call")
}

// talk runs full duplex audio: mic -> UDP and UDP -> jitter buffers -> mixer -> speakers.
// The relay address can change mid-call (failover), so the socket is not
// connected to one address: every packet goes to whatever target holds now.
func talk(ctx context.Context, info callInfo, target *atomic.Pointer[net.UDPAddr], user string, noMic bool, loss float64, st *state) error {
	conn, err := net.ListenUDP("udp", nil)
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
	if !noMic {
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
	go func() { errs <- send(conn, target, mic, info.CallID, user, st) }()
	go func() { errs <- receive(conn, m, info.CallID, loss) }()
	go func() { errs <- playout(ctx, m, speaker, st) }()
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
// Muted clients keep sending silent packets: the relay counts packets, not
// voice, to tell a muted participant from a crashed one.
func send(conn *net.UDPConn, target *atomic.Pointer[net.UDPAddr], mic io.Reader, callID, user string, st *state) error {
	cur, prev := make([]byte, frameBytes), make([]byte, frameBytes)
	for seq := uint32(1); ; seq++ {
		if _, err := io.ReadFull(mic, cur); err != nil {
			return fmt.Errorf("mic: %w", err)
		}
		if st.muted.Load() || st.deafened.Load() {
			clear(cur)
		}
		pkt := fmt.Appendf(nil, "%s %s %d\n", callID, user, seq)
		pkt = append(append(pkt, cur...), prev...)
		if _, err := conn.WriteToUDP(pkt, target.Load()); err != nil {
			log.Printf("send: %v", err) // keep going: UDP errors are transient
		}
		cur, prev = prev, cur
	}
}

// silence stands in for the microphone with -nomic: 20 ms of zeros every 20 ms.
type silence struct{}

func (silence) Read(p []byte) (int, error) {
	time.Sleep(frameDuration)
	clear(p)
	return len(p), nil
}

func receive(conn *net.UDPConn, m *mixer, callID string, loss float64) error {
	buf := make([]byte, 64*1024)
	for {
		n, _, err := conn.ReadFromUDP(buf)
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

func playout(ctx context.Context, m *mixer, speaker io.Writer, st *state) error {
	t := time.NewTicker(frameDuration)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-t.C:
			out := m.tick(now) // keep draining the jitter buffers while deafened
			if st.deafened.Load() {
				clear(out)
			}
			if _, err := speaker.Write(out); err != nil {
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

// state is what the user toggles mid-call.
type state struct{ muted, deafened atomic.Bool }

// controls reads commands from the terminal and toggle signals from other
// processes (make mute / make deafen send SIGUSR1 / SIGUSR2).
func controls(ctx context.Context, st *state, api *client, callID string, noMic bool, hangUp func()) {
	toggleMute := func() {
		if noMic {
			log.Print("no microphone (-nomic): always muted")
			return
		}
		log.Printf("muted: %v", !st.muted.Load())
		st.muted.Store(!st.muted.Load())
	}
	toggleDeafen := func() {
		log.Printf("deafened: %v", !st.deafened.Load())
		st.deafened.Store(!st.deafened.Load())
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGUSR1, syscall.SIGUSR2)
	go func() {
		defer signal.Stop(sig)
		for {
			select {
			case <-ctx.Done():
				return
			case s := <-sig:
				if s == syscall.SIGUSR1 {
					toggleMute()
				} else {
					toggleDeafen()
				}
			}
		}
	}()
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		switch {
		case len(f) == 0:
		case f[0] == "m":
			toggleMute()
		case f[0] == "d":
			toggleDeafen()
		case f[0] == "i" && len(f) == 2:
			if err := api.invite(callID, f[1]); err != nil {
				log.Printf("invite: %v", err)
			} else {
				log.Printf("invited %s", f[1])
			}
		case f[0] == "q":
			hangUp()
			return
		default:
			log.Print("commands: m (mute) | d (deafen) | i <user> (invite) | q (leave)")
		}
	}
}

// --- call-signaling API ---

type client struct {
	base, user, password string
	mu                   sync.Mutex
	tok                  string
	tokAt                time.Time
}

// token logs in (registering the account on first use) and reuses the access
// token for 4 minutes; the server issues 5-minute tokens.
func (c *client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tok != "" && time.Since(c.tokAt) < 4*time.Minute {
		return c.tok, nil
	}
	creds, _ := json.Marshal(map[string]string{"username": c.user, "password": c.password})
	post := func(path string) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(ctx, "POST", c.base+path, bytes.NewReader(creds))
		return http.DefaultClient.Do(req)
	}
	if resp, err := post("/auth/register"); err == nil {
		resp.Body.Close() // 201 new account, 409 already exists: both fine
	}
	resp, err := post("/auth/login")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct{ AccessToken string }
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&out) != nil || out.AccessToken == "" {
		return "", fmt.Errorf("login as %q failed: HTTP %d (wrong -password?)", c.user, resp.StatusCode)
	}
	c.tok, c.tokAt = out.AccessToken, time.Now()
	return c.tok, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

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
	tok, err := c.token(ctx)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
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
	body := map[string]any{"calleeIds": invited, "maxParticipants": maxP}
	if _, err := c.do(ctx, "POST", "/calls", body, &info); err != nil {
		return info, err
	}
	log.Printf("call %s requested, waiting for a relay...", info.CallID)
	for range 75 { // ~15 s; the server fails the call itself if no relay answers in time
		time.Sleep(200 * time.Millisecond)
		if _, err := c.do(ctx, "GET", "/calls/"+info.CallID, nil, &info); err != nil {
			log.Printf("status check failed, retrying: %v", err)
			continue
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
	_, err := c.do(ctx, "POST", "/calls/"+callID+"/join", nil, &info)
	return info, err
}

func (c *client) invite(callID, user string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := c.do(ctx, "POST", "/calls/"+callID+"/invite", map[string]string{"inviteeId": user}, nil)
	return err
}

func (c *client) leave(callID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := c.do(ctx, "POST", "/calls/"+callID+"/leave", nil, nil)
	return err
}

// notifications prints the user's SSE events and forwards invitations.
func (c *client) notifications(ctx context.Context, invites, gone chan<- string, moved chan<- callInfo) {
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, "GET", c.base+"/notifications", nil)
		tok, err := c.token(ctx)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		req.Header.Set("Authorization", "Bearer "+tok)
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
					CallID    string `json:"callId"`
					From      string `json:"from"`
					UserID    string `json:"userId"`
					Reason    string `json:"reason"`
					RelayHost string `json:"relayHost"`
					RelayPort int    `json:"relayPort"`
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
				case "relay_moved":
					select {
					case moved <- callInfo{CallID: d.CallID, RelayHost: d.RelayHost, RelayPort: d.RelayPort}:
					default:
					}
				case "call_ended", "call_failed", "left_call":
					log.Printf("%s %s: %s", strings.ReplaceAll(event, "_", " "), d.CallID, d.Reason)
					select {
					case gone <- d.CallID:
					default:
					}
				}
			}
		}
		resp.Body.Close()
	}
}
