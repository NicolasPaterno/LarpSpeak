package app

import (
	"context"
	"time"

	"github.com/larpspeak/media-relay/internal/domain"
)

// Relay pod failover (Leader Election pattern, implemented with a Lease and a
// Generation Clock):
//  1. every pod pings every second: it increments a heartbeat counter in its own
//     row of relay-nodes (a logical clock: no wall-clock time is ever compared);
//  2. one pod is the leader: it holds the "leader" lease row and renews it every
//     second. If the lease stops changing for 3s, any other pod takes it over
//     with a conditional write, and the leader term goes up by one;
//  3. only the leader watches the pods. A pod silent for 3s that also fails a
//     direct health check is marked DEAD;
//  4. the leader moves the calls of DEAD pods to the least loaded live pods
//     (least connections), and RelayMoved makes call-signaling push "reconnect
//     to the new relay" to every participant.
//
// Every write the leader makes also checks the leader term (fencing token): a
// leader that froze and lost the lease can no longer change anything.
// A pod that restarts reloads its own calls, so it resumes at the same address.
const (
	pingEvery  = time.Second
	deadAfter  = 3 * time.Second
	watchEvery = 500 * time.Millisecond
)

// pingSeen is when this pod last saw a counter change, measured on OUR clock,
// so clock differences between pods never make a pod look dead.
type pingSeen struct {
	beat int64
	at   time.Time
}

// suspect is the previous leader, handed to the new leader: its lease already
// stopped for 3s, so the new leader does not need to watch it from zero.
type suspect struct {
	id    string
	since time.Time
}

// Failover runs this pod's heartbeat, the leader election and, while leading,
// the pod watch.
func (r *Relay) Failover(ctx context.Context) {
	r.unhealthyAt.Store(time.Now().UnixNano())
	r.reloadOwn(ctx)
	go func() {
		for beat := int64(1); ctx.Err() == nil; beat++ {
			if !r.PingsPaused.Load() {
				start := time.Now()
				r.checkLink(start, r.Nodes.Ping(ctx, domain.Node{ID: r.ID, Beat: beat, Status: domain.NodeAlive,
					Addr: r.ProbeAddr, Host: r.Host, Port: r.Cfg.UDPPort}))
			}
			time.Sleep(pingEvery)
		}
	}()
	go r.elect(ctx)

	var seen map[string]pingSeen
	var term int64
	for ctx.Err() == nil {
		time.Sleep(watchEvery)
		t := r.term.Load()
		if t == 0 { // follower: the leader does the watching
			term = 0
			continue
		}
		var hint *suspect
		if t != term { // just elected: start a fresh view of the pods
			term, seen, hint = t, map[string]pingSeen{}, r.inherited.Swap(nil)
		}
		r.watchNodes(ctx, seen, term, hint)
	}
}

// elect runs the leader lease in its own goroutine, so a busy audio or watch
// loop never delays the renewal.
func (r *Relay) elect(ctx context.Context) {
	var seen pingSeen
	var seenTerm int64
	for ctx.Err() == nil {
		time.Sleep(pingEvery)
		if r.PingsPaused.Load() { // fault injection: no link to DynamoDB at all
			continue
		}
		start := time.Now()
		if t := r.term.Load(); t > 0 {
			// A failed or slow renewal steps down: better a healthy leader than a slow one.
			if err := r.Nodes.Renew(ctx, r.ID, t); !r.checkLink(start, err) {
				r.term.Store(0)
				r.Log.Warn("leadership lost", "term", t, "err", err)
			}
			continue
		}
		l, err := r.Nodes.Lease(ctx)
		if !r.checkLink(start, err) {
			continue
		}
		switch {
		case l == nil: // nobody ever led
		case l.Holder == r.ID: // our own lease from before a restart or a step-down: take it back now
		case l.Beat != seen.beat || l.Term != seenTerm:
			seen, seenTerm = pingSeen{l.Beat, time.Now()}, l.Term
			continue
		case time.Since(seen.at) <= deadAfter, time.Since(time.Unix(0, r.unhealthyAt.Load())) < deadAfter:
			continue // leader still renewing, or our own link was just unhealthy
		}
		term, err := r.Nodes.Claim(ctx, r.ID, l)
		if err != nil {
			continue // another pod won the race
		}
		if l != nil && l.Holder != r.ID {
			r.inherited.Store(&suspect{l.Holder, seen.at})
		}
		r.term.Store(term)
		r.Log.Warn("elected leader", "term", term)
	}
}

func (r *Relay) watchNodes(ctx context.Context, seen map[string]pingSeen, term int64, hint *suspect) {
	// After an outage every pod looks silent: give live pods time to ping first.
	if time.Since(time.Unix(0, r.unhealthyAt.Load())) < deadAfter {
		return
	}
	start := time.Now()
	nodes, err := r.Nodes.Nodes(ctx)
	if !r.checkLink(start, err) {
		return
	}
	var dead []string
	var alive []domain.Node
	for _, n := range nodes {
		s, ok := seen[n.ID]
		if !ok && hint != nil && hint.id == n.ID { // the old leader: silent since its lease stopped
			s, ok = pingSeen{n.Beat, hint.since}, true
			seen[n.ID] = s
		}
		if !ok || s.beat != n.Beat {
			seen[n.ID] = pingSeen{n.Beat, time.Now()} // counter moved since last look: alive
			if n.Status == domain.NodeAlive {
				alive = append(alive, n)
			}
			continue
		}
		switch {
		case n.Status == domain.NodeAlive && (n.ID == r.ID || time.Since(s.at) <= deadAfter):
			alive = append(alive, n)
		case n.Status == domain.NodeAlive:
			if r.Prober.AnswersHealth(n.Addr, n.ID) { // second opinion: pings stopped but the pod is serving
				r.Log.Warn("relay pod missed pings but answers health checks: no failover", "pod", n.ID)
				seen[n.ID] = pingSeen{n.Beat, time.Now()} // ask again after another deadAfter
				alive = append(alive, n)
				continue
			}
			if r.Nodes.MarkDead(ctx, n, r.ID, term) == nil {
				r.Log.Warn("relay pod dead: no ping, no health answer", "pod", n.ID, "term", term,
					"silentMs", time.Since(s.at).Milliseconds())
				dead = append(dead, n.ID)
			}
		case n.Status == domain.NodeDead:
			dead = append(dead, n.ID)
		}
	}
	if len(dead) > 0 {
		r.redistribute(ctx, dead, alive, term)
	}
}

// checkLink records an unhealthy link when a DynamoDB call failed OR was slow.
// A frozen database does not fail requests, it delays them: pings then land
// late with old timestamps and every pod looks dead. Waiting deadAfter of
// healthy operation after any hiccup prevents those false failovers.
func (r *Relay) checkLink(start time.Time, err error) bool {
	if err != nil || time.Since(start) > pingEvery {
		r.unhealthyAt.Store(time.Now().UnixNano())
		return false
	}
	return true
}

// redistribute moves the calls of dead pods to the live pod with the fewest
// calls (least connections). Each move checks the old owner and the leader
// term, so a call moves once and only by the current leader. Dead pods with
// no calls left are removed from relay-nodes.
func (r *Relay) redistribute(ctx context.Context, dead []string, alive []domain.Node, term int64) {
	sessions, err := r.Sessions.Allocated(ctx)
	if err != nil || len(alive) == 0 {
		return
	}
	load, isDead := map[string]int{}, map[string]bool{}
	for _, d := range dead {
		isDead[d] = true
	}
	for _, s := range sessions {
		load[s.Owner]++
	}
	left := map[string]bool{}
	for _, old := range sessions {
		if !isDead[old.Owner] {
			continue
		}
		to := alive[0]
		for _, n := range alive[1:] {
			if load[n.ID] < load[to.ID] {
				to = n
			}
		}
		out, err := message(r.Cfg.RelayResults, "RelayMoved", old.CallID, map[string]any{
			"callId": old.CallID, "relayId": old.RelayID, "host": to.Host, "port": to.Port, "signalAddr": to.Addr,
		})
		if err != nil {
			continue
		}
		if err := r.Nodes.Move(ctx, r.ID, term, old, to, out); err != nil {
			left[old.Owner] = true // no longer leader, or retry next round
			continue
		}
		load[to.ID]++
		r.Log.Warn("call moved from dead relay pod", "correlationId", old.CallID, "from", old.Owner, "to", to.ID,
			"load", load[to.ID], "term", term)
		if to.ID == r.ID {
			old.Host, old.Port, old.Owner = to.Host, to.Port, to.ID
			r.track(old)
		} else {
			go r.Prober.NotifyAdopt(to.Addr, old.CallID)
		}
	}
	for _, d := range dead {
		if !left[d] {
			r.Nodes.ForgetNode(ctx, d)
		}
	}
}

// PickUp starts forwarding a call that the leader moved to this pod. Lookups are
// limited to one per call per second: unknown calls in UDP packets cost a read.
func (r *Relay) PickUp(ctx context.Context, callID string) {
	r.mu.Lock()
	if r.live[callID] != nil || time.Since(r.lookups[callID]) < time.Second {
		r.mu.Unlock()
		return
	}
	if r.lookups == nil || len(r.lookups) > 10000 { // ponytail: crude bound on junk call ids; an LRU if it ever matters
		r.lookups = map[string]time.Time{}
	}
	r.lookups[callID] = time.Now()
	r.mu.Unlock()
	ss, err := r.Sessions.Get(ctx, callID)
	if err != nil || ss == nil || ss.State != domain.StateAllocated || ss.Owner != r.ID {
		return
	}
	r.track(*ss)
	r.Log.Info("call picked up after failover", "correlationId", callID)
}

// reloadOwn takes back this pod's calls after a restart: same address, nothing to announce.
func (r *Relay) reloadOwn(ctx context.Context) {
	sessions, err := r.Sessions.Allocated(ctx)
	if err != nil {
		return
	}
	for _, s := range sessions {
		if s.Owner == r.ID {
			r.track(s)
		}
	}
}
