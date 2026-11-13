package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Relay pod failover:
//  1. every pod pings every second: it increments a heartbeat counter in its own
//     row of relay-nodes (a logical clock: no wall-clock time is ever compared);
//  2. the surviving pods act as coordinator: a pod silent for 3s is marked DEAD
//     by exactly one of them (conditional write);
//  3. the calls of DEAD pods are adopted by surviving pods, and RelayMoved makes
//     call-signaling push "reconnect to the new relay" to every participant.
//
// A pod that restarts reloads its own calls, so it resumes at the same address.
const (
	pingEvery  = time.Second
	deadAfter  = 3 * time.Second
	watchEvery = 500 * time.Millisecond
	nodeAlive  = "ALIVE"
	nodeDead   = "DEAD"
)

type node struct {
	ID     string `dynamodbav:"pk"`
	Beat   int64  `dynamodbav:"beat"` // heartbeat counter: +1 per ping
	Status string `dynamodbav:"status"`
	Addr   string `dynamodbav:"addr"` // host:port of the pod's /health, for the second opinion
}

// pingSeen is when this coordinator last saw a pod's counter change, measured on
// OUR clock, so clock differences between pods never make a pod look dead.
type pingSeen struct {
	beat int64
	at   time.Time
}

func (r *relay) failover(ctx context.Context) {
	r.unhealthyAt.Store(time.Now().UnixNano())
	r.adoptSessions(ctx, []string{r.id}) // restarted pod: take our own calls back
	go func() {
		for beat := int64(1); ctx.Err() == nil; beat++ {
			if !r.pingsPaused.Load() {
				start := time.Now()
				r.checkLink(start, r.st.ping(ctx, r.id, r.probeAddr, beat))
			}
			time.Sleep(pingEvery)
		}
	}()
	seen := map[string]pingSeen{}
	for ctx.Err() == nil {
		time.Sleep(watchEvery)
		r.watchNodes(ctx, seen)
	}
}

func (r *relay) watchNodes(ctx context.Context, seen map[string]pingSeen) {
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
	for _, n := range nodes {
		if s, ok := seen[n.ID]; !ok || s.beat != n.Beat {
			seen[n.ID] = pingSeen{n.Beat, time.Now()} // counter moved since last look: alive
			continue
		}
		switch {
		case n.ID == r.id:
		case n.Status == nodeAlive && time.Since(seen[n.ID].at) > deadAfter:
			if answersHealth(n.Addr) { // second opinion: pings stopped but the pod is serving
				r.log.Warn("relay pod missed pings but answers health checks: no failover", "pod", n.ID)
				seen[n.ID] = pingSeen{n.Beat, time.Now()} // ask again after another deadAfter
				continue
			}
			if r.st.markDead(ctx, n) == nil {
				r.log.Warn("relay pod dead: no ping, no health answer", "pod", n.ID, "silentMs", time.Since(seen[n.ID].at).Milliseconds())
				dead = append(dead, n.ID)
			}
		case n.Status == nodeDead:
			dead = append(dead, n.ID)
		}
	}
	if len(dead) > 0 {
		r.adoptSessions(ctx, dead)
	}
}

var probe = &http.Client{Timeout: 500 * time.Millisecond}

// answersHealth calls the suspect pod directly. A dead or frozen pod does not answer.
func answersHealth(addr string) bool {
	if addr == "" {
		return false
	}
	resp, err := probe.Get("http://" + addr + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
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

// adoptSessions moves calls owned by the given pods to this pod. The condition
// on the old owner lets exactly one pod win each call. Pods with no calls left
// are removed from relay-nodes.
func (r *relay) adoptSessions(ctx context.Context, owners []string) {
	sessions, err := r.st.ownedBy(ctx, owners)
	if err != nil {
		return
	}
	left := map[string]bool{}
	for _, old := range sessions {
		if old.Owner == r.id { // our own call after a restart: same address, nothing to announce
			r.track(old)
			continue
		}
		moved, err := newEnvelope("RelayMoved", old.CallID, map[string]any{
			"callId": old.CallID, "relayId": old.RelayID, "host": r.host, "port": r.cfg.UDPPort,
		})
		if err != nil {
			continue
		}
		out, err := r.st.outbox(r.cfg.RelayResults, moved)
		if err != nil {
			continue
		}
		if err := r.st.commit(ctx, r.st.takeover(old, r.id, r.host, r.cfg.UDPPort), out); err != nil {
			left[old.Owner] = true // another pod won it, or retry next round
			continue
		}
		deadPod := old.Owner
		old.Host, old.Port, old.Owner = r.host, r.cfg.UDPPort, r.id
		r.track(old)
		r.log.Warn("call adopted from dead relay pod", "correlationId", old.CallID, "from", deadPod, "host", r.host, "port", r.cfg.UDPPort)
	}
	for _, o := range owners {
		if o != r.id && !left[o] {
			r.st.forgetNode(ctx, o)
		}
	}
}

// --- relay-nodes and session ownership in DynamoDB ---

func (s *store) ping(ctx context.Context, id, addr string, beat int64) error {
	_, err := s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.cfg.NodesTable,
		Item:      map[string]types.AttributeValue{"pk": str(id), "beat": num(beat), "status": str(nodeAlive), "addr": str(addr)},
	})
	return err
}

func (s *store) nodes(ctx context.Context) ([]node, error) {
	out, err := s.db.Scan(ctx, &dynamodb.ScanInput{TableName: &s.cfg.NodesTable, ConsistentRead: aws.Bool(true)})
	if err != nil {
		return nil, err
	}
	var ns []node
	return ns, attributevalue.UnmarshalListOfMaps(out.Items, &ns)
}

// markDead succeeds for exactly one coordinator: the counter must still be the one we saw.
func (s *store) markDead(ctx context.Context, n node) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.cfg.NodesTable,
		Key:                       key(n.ID),
		UpdateExpression:          aws.String("SET #st = :dead"),
		ConditionExpression:       aws.String("#st = :alive AND (beat = :seen OR attribute_not_exists(beat))"),
		ExpressionAttributeNames:  map[string]string{"#st": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":dead": str(nodeDead), ":alive": str(nodeAlive), ":seen": num(n.Beat)},
	})
	return err
}

// forgetNode removes a DEAD pod once its calls are adopted (a revived pod re-pings as ALIVE).
func (s *store) forgetNode(ctx context.Context, id string) {
	_, _ = s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:                 &s.cfg.NodesTable,
		Key:                       key(id),
		ConditionExpression:       aws.String("#st = :dead"),
		ExpressionAttributeNames:  map[string]string{"#st": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":dead": str(nodeDead)},
	})
}

// ownedBy lists allocated sessions owned by the given pods. A scan is fine at demo scale.
func (s *store) ownedBy(ctx context.Context, owners []string) ([]session, error) {
	vals := map[string]types.AttributeValue{":alloc": str(stateAllocated)}
	in := ""
	for i, o := range owners {
		k := fmt.Sprintf(":o%d", i)
		vals[k] = str(o)
		if in != "" {
			in += ", "
		}
		in += k
	}
	out, err := s.db.Scan(ctx, &dynamodb.ScanInput{
		TableName:                 &s.cfg.SessionsTable,
		ConsistentRead:            aws.Bool(true),
		FilterExpression:          aws.String("#s = :alloc AND #o IN (" + in + ")"),
		ExpressionAttributeNames:  map[string]string{"#s": "state", "#o": "owner"},
		ExpressionAttributeValues: vals,
	})
	if err != nil {
		return nil, err
	}
	var ss []session
	return ss, attributevalue.UnmarshalListOfMaps(out.Items, &ss)
}

// takeover moves a session from its dead owner to this pod; only one pod can win.
func (s *store) takeover(old session, owner, host string, port int) types.TransactWriteItem {
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                &s.cfg.SessionsTable,
		Key:                      key(old.CallID),
		UpdateExpression:         aws.String("SET #o = :me, #h = :host, #p = :port"),
		ConditionExpression:      aws.String("#s = :alloc AND #o = :dead"),
		ExpressionAttributeNames: map[string]string{"#s": "state", "#o": "owner", "#h": "host", "#p": "port"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":me": str(owner), ":host": str(host), ":port": num(int64(port)),
			":alloc": str(stateAllocated), ":dead": str(old.Owner),
		},
	}}
}
