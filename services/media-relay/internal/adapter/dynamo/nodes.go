package dynamo

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/larpspeak/media-relay/internal/domain"
)

// --- relay-nodes, leader lease and session ownership in DynamoDB ---

const leaderKey = "leader" // the lease row, stored in relay-nodes

func (s *Store) Ping(ctx context.Context, n domain.Node) error {
	_, err := s.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.Cfg.NodesTable,
		Item: map[string]types.AttributeValue{"pk": str(n.ID), "beat": num(n.Beat), "status": str(n.Status),
			"addr": str(n.Addr), "host": str(n.Host), "port": num(int64(n.Port))},
	})
	return err
}

func (s *Store) Nodes(ctx context.Context) ([]domain.Node, error) {
	out, err := s.DB.Scan(ctx, &dynamodb.ScanInput{TableName: &s.Cfg.NodesTable, ConsistentRead: aws.Bool(true)})
	if err != nil {
		return nil, err
	}
	var ns []domain.Node
	for _, item := range out.Items {
		var n domain.Node
		if err := attributevalue.UnmarshalMap(item, &n); err != nil {
			return nil, err
		}
		if n.ID != leaderKey {
			ns = append(ns, n)
		}
	}
	return ns, nil
}

func (s *Store) Lease(ctx context.Context) (*domain.Lease, error) {
	out, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.Cfg.NodesTable, Key: key(leaderKey), ConsistentRead: aws.Bool(true)})
	if err != nil || out.Item == nil {
		return nil, err
	}
	var l domain.Lease
	return &l, attributevalue.UnmarshalMap(out.Item, &l)
}

// Claim takes the lease. It succeeds for exactly one pod: the lease must still
// be the one we saw stop (same term and beat), or not exist at all.
func (s *Store) Claim(ctx context.Context, id string, old *domain.Lease) (int64, error) {
	next := domain.Lease{Holder: id, Term: 1, Beat: 1}
	cond, vals := "attribute_not_exists(pk)", map[string]types.AttributeValue(nil)
	if old != nil {
		next = domain.Lease{Holder: id, Term: old.Term + 1, Beat: old.Beat + 1}
		cond, vals = "term = :t AND beat = :b", map[string]types.AttributeValue{":t": num(old.Term), ":b": num(old.Beat)}
	}
	item := map[string]types.AttributeValue{"pk": str(leaderKey), "holder": str(id), "term": num(next.Term), "beat": num(next.Beat)}
	_, err := s.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.Cfg.NodesTable, Item: item, ConditionExpression: &cond, ExpressionAttributeValues: vals,
	})
	return next.Term, err
}

// Renew keeps the lease alive; it fails once another pod took it (newer term).
func (s *Store) Renew(ctx context.Context, id string, term int64) error {
	_, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.Cfg.NodesTable,
		Key:                       key(leaderKey),
		UpdateExpression:          aws.String("SET beat = beat + :one"),
		ConditionExpression:       aws.String("holder = :me AND term = :t"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":one": num(1), ":me": str(id), ":t": num(term)},
	})
	return err
}

// fence is the fencing token check: the transaction fails unless we still lead in this term.
func (s *Store) fence(id string, term int64) types.TransactWriteItem {
	return types.TransactWriteItem{ConditionCheck: &types.ConditionCheck{
		TableName:                 &s.Cfg.NodesTable,
		Key:                       key(leaderKey),
		ConditionExpression:       aws.String("holder = :me AND term = :t"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":me": str(id), ":t": num(term)},
	}}
}

// MarkDead succeeds once, and only for the current leader: the counter must
// still be the one we saw and our term must still be current.
func (s *Store) MarkDead(ctx context.Context, n domain.Node, leader string, term int64) error {
	return s.commit(ctx, s.fence(leader, term), types.TransactWriteItem{Update: &types.Update{
		TableName:                 &s.Cfg.NodesTable,
		Key:                       key(n.ID),
		UpdateExpression:          aws.String("SET #st = :dead"),
		ConditionExpression:       aws.String("#st = :alive AND (beat = :seen OR attribute_not_exists(beat))"),
		ExpressionAttributeNames:  map[string]string{"#st": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":dead": str(domain.NodeDead), ":alive": str(domain.NodeAlive), ":seen": num(n.Beat)},
	}})
}

// ForgetNode removes a DEAD pod once its calls are moved (a revived pod re-pings as ALIVE).
func (s *Store) ForgetNode(ctx context.Context, id string) {
	_, _ = s.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:                 &s.Cfg.NodesTable,
		Key:                       key(id),
		ConditionExpression:       aws.String("#st = :dead"),
		ExpressionAttributeNames:  map[string]string{"#st": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":dead": str(domain.NodeDead)},
	})
}

// Move hands a session from its dead owner to pod `to`: [fence, takeover, RelayMoved].
// Only the current leader, and only one move, can win.
func (s *Store) Move(ctx context.Context, leader string, term int64, old domain.Session, to domain.Node, out domain.Outgoing) error {
	takeover := types.TransactWriteItem{Update: &types.Update{
		TableName:                &s.Cfg.SessionsTable,
		Key:                      key(old.CallID),
		UpdateExpression:         aws.String("SET #o = :to, #h = :host, #p = :port"),
		ConditionExpression:      aws.String("#s = :alloc AND #o = :dead"),
		ExpressionAttributeNames: map[string]string{"#s": "state", "#o": "owner", "#h": "host", "#p": "port"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":to": str(to.ID), ":host": str(to.Host), ":port": num(int64(to.Port)),
			":alloc": str(domain.StateAllocated), ":dead": str(old.Owner),
		},
	}}
	return s.commit(ctx, s.fence(leader, term), takeover, s.outbox(out))
}
