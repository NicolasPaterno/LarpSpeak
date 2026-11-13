package main

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Relay pod failover:
//  1. every pod pings (writes its own row in relay-nodes) every second;
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
	ID       string `dynamodbav:"pk"`
	LastPing int64  `dynamodbav:"lastPing"` // unix ms
	Status   string `dynamodbav:"status"`
}

func (r *relay) failover(ctx context.Context) {
	r.unhealthyAt.Store(time.Now().UnixNano())
	r.adoptSessions(ctx, []string{r.id}) // restarted pod: take our own calls back
	go func() {
		for ctx.Err() == nil {
			start := time.Now()
			r.checkLink(start, r.st.ping(ctx, r.id, start.UnixMilli()))
			time.Sleep(pingEvery)
		}
	}()
	for ctx.Err() == nil {
		time.Sleep(watchEvery)
		r.watchNodes(ctx)
	}
}

func (r *relay) watchNodes(ctx context.Context) {
	// After an outage every pod looks silent: give live pods time to ping first.
	if time.Since(time.Unix(0, r.unhealthyAt.Load())) < deadAfter {
		return
	}
	start := time.Now()
	nodes, err := r.st.nodes(ctx)
	if !r.checkLink(start, err) {
		return
	}
	stale := time.Now().Add(-deadAfter).UnixMilli()
	var dead []string
	for _, n := range nodes {
		switch {
		case n.ID == r.id:
		case n.Status == nodeAlive && n.LastPing < stale:
			if r.st.markDead(ctx, n) == nil {
				r.log.Warn("relay pod dead: no ping", "pod", n.ID, "silentMs", time.Now().UnixMilli()-n.LastPing)
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

func (s *store) ping(ctx context.Context, id string, now int64) error {
	_, err := s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.cfg.NodesTable,
		Item:      map[string]types.AttributeValue{"pk": str(id), "lastPing": num(now), "status": str(nodeAlive)},
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

// markDead succeeds for exactly one coordinator: the ping must still be the stale one we saw.
func (s *store) markDead(ctx context.Context, n node) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.cfg.NodesTable,
		Key:                       key(n.ID),
		UpdateExpression:          aws.String("SET #st = :dead"),
		ConditionExpression:       aws.String("#st = :alive AND lastPing = :seen"),
		ExpressionAttributeNames:  map[string]string{"#st": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":dead": str(nodeDead), ":alive": str(nodeAlive), ":seen": num(n.LastPing)},
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
