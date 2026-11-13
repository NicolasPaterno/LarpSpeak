package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
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
	nodeAlive  = "ALIVE"
	nodeDead   = "DEAD"
	leaderKey  = "leader" // the lease row, stored in relay-nodes
)

type node struct {
	ID     string `dynamodbav:"pk"`
	Beat   int64  `dynamodbav:"beat"` // heartbeat counter: +1 per ping
	Status string `dynamodbav:"status"`
	Addr   string `dynamodbav:"addr"` // host:port of the pod's HTTP API (health check, adopt)
	Host   string `dynamodbav:"host"` // where participants send UDP audio
	Port   int    `dynamodbav:"port"`
}

// lease is the leader row. Term is the generation clock: +1 per new leader.
type lease struct {
	Holder string `dynamodbav:"holder"`
	Term   int64  `dynamodbav:"term"`
	Beat   int64  `dynamodbav:"beat"`
}

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

func (r *relay) failover(ctx context.Context) {
	r.unhealthyAt.Store(time.Now().UnixNano())
	r.reloadOwn(ctx)
	go func() {
		for beat := int64(1); ctx.Err() == nil; beat++ {
			if !r.pingsPaused.Load() {
				start := time.Now()
				r.checkLink(start, r.st.ping(ctx, r, beat))
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
func (r *relay) elect(ctx context.Context) {
	var seen pingSeen
	var seenTerm int64
	for ctx.Err() == nil {
		time.Sleep(pingEvery)
		if r.pingsPaused.Load() { // fault injection: no link to DynamoDB at all
			continue
		}
		start := time.Now()
		if t := r.term.Load(); t > 0 {
			// A failed or slow renewal steps down: better a healthy leader than a slow one.
			if err := r.st.renew(ctx, r.id, t); !r.checkLink(start, err) {
				r.term.Store(0)
				r.log.Warn("leadership lost", "term", t, "err", err)
			}
			continue
		}
		l, err := r.st.lease(ctx)
		if !r.checkLink(start, err) {
			continue
		}
		switch {
		case l == nil: // nobody ever led
		case l.Holder == r.id: // our own lease from before a restart or a step-down: take it back now
		case l.Beat != seen.beat || l.Term != seenTerm:
			seen, seenTerm = pingSeen{l.Beat, time.Now()}, l.Term
			continue
		case time.Since(seen.at) <= deadAfter, time.Since(time.Unix(0, r.unhealthyAt.Load())) < deadAfter:
			continue // leader still renewing, or our own link was just unhealthy
		}
		term, err := r.st.claim(ctx, r.id, l)
		if err != nil {
			continue // another pod won the race
		}
		if l != nil && l.Holder != r.id {
			r.inherited.Store(&suspect{l.Holder, seen.at})
		}
		r.term.Store(term)
		r.log.Warn("elected leader", "term", term)
	}
}

func (r *relay) watchNodes(ctx context.Context, seen map[string]pingSeen, term int64, hint *suspect) {
	// After an outage every pod looks silent: give live pods time to ping first.
	if time.Since(time.Unix(0, r.unhealthyAt.Load())) < deadAfter {
		return
	}
	start := time.Now()
	nodes, err := r.st.nodes(ctx)
	if !r.checkLink(start, err) {
		return
	}
	var dead []string
	var alive []node
	for _, n := range nodes {
		s, ok := seen[n.ID]
		if !ok && hint != nil && hint.id == n.ID { // the old leader: silent since its lease stopped
			s, ok = pingSeen{n.Beat, hint.since}, true
			seen[n.ID] = s
		}
		if !ok || s.beat != n.Beat {
			seen[n.ID] = pingSeen{n.Beat, time.Now()} // counter moved since last look: alive
			if n.Status == nodeAlive {
				alive = append(alive, n)
			}
			continue
		}
		switch {
		case n.Status == nodeAlive && (n.ID == r.id || time.Since(s.at) <= deadAfter):
			alive = append(alive, n)
		case n.Status == nodeAlive:
			if answersHealth(n.Addr, n.ID) { // second opinion: pings stopped but the pod is serving
				r.log.Warn("relay pod missed pings but answers health checks: no failover", "pod", n.ID)
				seen[n.ID] = pingSeen{n.Beat, time.Now()} // ask again after another deadAfter
				alive = append(alive, n)
				continue
			}
			if r.st.markDead(ctx, n, r.id, term) == nil {
				r.log.Warn("relay pod dead: no ping, no health answer", "pod", n.ID, "term", term,
					"silentMs", time.Since(s.at).Milliseconds())
				dead = append(dead, n.ID)
			}
		case n.Status == nodeDead:
			dead = append(dead, n.ID)
		}
	}
	if len(dead) > 0 {
		r.redistribute(ctx, dead, alive, term)
	}
}

var probe = &http.Client{Timeout: 500 * time.Millisecond}

// answersHealth calls the suspect pod directly. A dead or frozen pod does not
// answer. The reply must name the suspect: a replacement pod can reuse a dead
// pod's IP address, and its answer must not keep the dead pod "alive".
func answersHealth(addr, id string) bool {
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

// checkLink records an unhealthy link when a DynamoDB call failed OR was slow.
// A frozen database does not fail requests, it delays them: pings then land
// late with old timestamps and every pod looks dead. Waiting deadAfter of
// healthy operation after any hiccup prevents those false failovers.
func (r *relay) checkLink(start time.Time, err error) bool {
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
func (r *relay) redistribute(ctx context.Context, dead []string, alive []node, term int64) {
	sessions, err := r.st.allocated(ctx)
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
		moved, err := newEnvelope("RelayMoved", old.CallID, map[string]any{
			"callId": old.CallID, "relayId": old.RelayID, "host": to.Host, "port": to.Port,
		})
		if err != nil {
			continue
		}
		out, err := r.st.outbox(r.cfg.RelayResults, moved)
		if err != nil {
			continue
		}
		if err := r.st.commit(ctx, r.st.fence(r.id, term), r.st.takeover(old, to), out); err != nil {
			left[old.Owner] = true // no longer leader, or retry next round
			continue
		}
		load[to.ID]++
		r.log.Warn("call moved from dead relay pod", "correlationId", old.CallID, "from", old.Owner, "to", to.ID,
			"load", load[to.ID], "term", term)
		if to.ID == r.id {
			old.Host, old.Port, old.Owner = to.Host, to.Port, to.ID
			r.track(old)
		} else {
			go notifyAdopt(to.Addr, old.CallID)
		}
	}
	for _, d := range dead {
		if !left[d] {
			r.st.forgetNode(ctx, d)
		}
	}
}

// notifyAdopt tells the new owner to start forwarding the call right away. If
// it is lost, the new owner still picks the call up on its first audio packet.
func notifyAdopt(addr, callID string) {
	resp, err := probe.Post("http://"+addr+"/adopt?callId="+url.QueryEscape(callID), "", nil)
	if err == nil {
		resp.Body.Close()
	}
}

// pickUp starts forwarding a call that the leader moved to this pod. Lookups are
// limited to one per call per second: unknown calls in UDP packets cost a read.
func (r *relay) pickUp(ctx context.Context, callID string) {
	r.mu.Lock()
	if r.live[callID] != nil || time.Since(r.lookups[callID]) < time.Second {
		r.mu.Unlock()
		return
	}
	if len(r.lookups) > 10000 { // ponytail: crude bound on junk call ids; an LRU if it ever matters
		clear(r.lookups)
	}
	r.lookups[callID] = time.Now()
	r.mu.Unlock()
	ss, err := r.st.getSession(ctx, callID)
	if err != nil || ss == nil || ss.State != stateAllocated || ss.Owner != r.id {
		return
	}
	r.track(*ss)
	r.log.Info("call picked up after failover", "correlationId", callID)
}

// reloadOwn takes back this pod's calls after a restart: same address, nothing to announce.
func (r *relay) reloadOwn(ctx context.Context) {
	sessions, err := r.st.allocated(ctx)
	if err != nil {
		return
	}
	for _, s := range sessions {
		if s.Owner == r.id {
			r.track(s)
		}
	}
}

// --- relay-nodes, leader lease and session ownership in DynamoDB ---

func (s *store) ping(ctx context.Context, r *relay, beat int64) error {
	_, err := s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.cfg.NodesTable,
		Item: map[string]types.AttributeValue{"pk": str(r.id), "beat": num(beat), "status": str(nodeAlive),
			"addr": str(r.probeAddr), "host": str(r.host), "port": num(int64(r.cfg.UDPPort))},
	})
	return err
}

func (s *store) nodes(ctx context.Context) ([]node, error) {
	out, err := s.db.Scan(ctx, &dynamodb.ScanInput{TableName: &s.cfg.NodesTable, ConsistentRead: aws.Bool(true)})
	if err != nil {
		return nil, err
	}
	var ns []node
	for _, item := range out.Items {
		var n node
		if err := attributevalue.UnmarshalMap(item, &n); err != nil {
			return nil, err
		}
		if n.ID != leaderKey {
			ns = append(ns, n)
		}
	}
	return ns, nil
}

func (s *store) lease(ctx context.Context) (*lease, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.cfg.NodesTable, Key: key(leaderKey), ConsistentRead: aws.Bool(true)})
	if err != nil || out.Item == nil {
		return nil, err
	}
	var l lease
	return &l, attributevalue.UnmarshalMap(out.Item, &l)
}

// claim takes the lease. It succeeds for exactly one pod: the lease must still
// be the one we saw stop (same term and beat), or not exist at all.
func (s *store) claim(ctx context.Context, id string, old *lease) (int64, error) {
	next := lease{Holder: id, Term: 1, Beat: 1}
	cond, vals := "attribute_not_exists(pk)", map[string]types.AttributeValue(nil)
	if old != nil {
		next = lease{Holder: id, Term: old.Term + 1, Beat: old.Beat + 1}
		cond, vals = "term = :t AND beat = :b", map[string]types.AttributeValue{":t": num(old.Term), ":b": num(old.Beat)}
	}
	item := map[string]types.AttributeValue{"pk": str(leaderKey), "holder": str(id), "term": num(next.Term), "beat": num(next.Beat)}
	_, err := s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.cfg.NodesTable, Item: item, ConditionExpression: &cond, ExpressionAttributeValues: vals,
	})
	return next.Term, err
}

// renew keeps the lease alive; it fails once another pod took it (newer term).
func (s *store) renew(ctx context.Context, id string, term int64) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.cfg.NodesTable,
		Key:                       key(leaderKey),
		UpdateExpression:          aws.String("SET beat = beat + :one"),
		ConditionExpression:       aws.String("holder = :me AND term = :t"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":one": num(1), ":me": str(id), ":t": num(term)},
	})
	return err
}

// fence is the fencing token check: the transaction fails unless we still lead in this term.
func (s *store) fence(id string, term int64) types.TransactWriteItem {
	return types.TransactWriteItem{ConditionCheck: &types.ConditionCheck{
		TableName:                 &s.cfg.NodesTable,
		Key:                       key(leaderKey),
		ConditionExpression:       aws.String("holder = :me AND term = :t"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":me": str(id), ":t": num(term)},
	}}
}

// markDead succeeds once, and only for the current leader: the counter must
// still be the one we saw and our term must still be current.
func (s *store) markDead(ctx context.Context, n node, leader string, term int64) error {
	return s.commit(ctx, s.fence(leader, term), types.TransactWriteItem{Update: &types.Update{
		TableName:                 &s.cfg.NodesTable,
		Key:                       key(n.ID),
		UpdateExpression:          aws.String("SET #st = :dead"),
		ConditionExpression:       aws.String("#st = :alive AND (beat = :seen OR attribute_not_exists(beat))"),
		ExpressionAttributeNames:  map[string]string{"#st": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":dead": str(nodeDead), ":alive": str(nodeAlive), ":seen": num(n.Beat)},
	}})
}

// forgetNode removes a DEAD pod once its calls are moved (a revived pod re-pings as ALIVE).
func (s *store) forgetNode(ctx context.Context, id string) {
	_, _ = s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:                 &s.cfg.NodesTable,
		Key:                       key(id),
		ConditionExpression:       aws.String("#st = :dead"),
		ExpressionAttributeNames:  map[string]string{"#st": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":dead": str(nodeDead)},
	})
}

// allocated lists every allocated session. A scan is fine at demo scale.
func (s *store) allocated(ctx context.Context) ([]session, error) {
	out, err := s.db.Scan(ctx, &dynamodb.ScanInput{
		TableName:                 &s.cfg.SessionsTable,
		ConsistentRead:            aws.Bool(true),
		FilterExpression:          aws.String("#s = :alloc"),
		ExpressionAttributeNames:  map[string]string{"#s": "state"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":alloc": str(stateAllocated)},
	})
	if err != nil {
		return nil, err
	}
	var ss []session
	return ss, attributevalue.UnmarshalListOfMaps(out.Items, &ss)
}

// takeover moves a session from its dead owner to the chosen pod; only one move can win.
func (s *store) takeover(old session, to node) types.TransactWriteItem {
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                &s.cfg.SessionsTable,
		Key:                      key(old.CallID),
		UpdateExpression:         aws.String("SET #o = :to, #h = :host, #p = :port"),
		ConditionExpression:      aws.String("#s = :alloc AND #o = :dead"),
		ExpressionAttributeNames: map[string]string{"#s": "state", "#o": "owner", "#h": "host", "#p": "port"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":to": str(to.ID), ":host": str(to.Host), ":port": num(int64(to.Port)),
			":alloc": str(stateAllocated), ":dead": str(old.Owner),
		},
	}}
}
