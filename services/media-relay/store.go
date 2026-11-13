package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	stateAllocated = "ALLOCATED"
	stateReleased  = "RELEASED"
)

var (
	errDuplicate = errors.New("message already processed")
	errReleased  = errors.New("relay already released")
)

// session is one relay, keyed by callId (one relay per call).
type session struct {
	CallID       string   `dynamodbav:"pk"`
	RelayID      string   `dynamodbav:"relayId"`
	Host         string   `dynamodbav:"host"`
	Port         int      `dynamodbav:"port"`
	Participants []string `dynamodbav:"participants,stringset,omitempty"`
	State        string   `dynamodbav:"state"`
	Reason       string   `dynamodbav:"reason,omitempty"`
	Owner        string   `dynamodbav:"owner"`       // replica forwarding this call's audio
	HeartbeatAt  int64    `dynamodbav:"heartbeatAt"` // unix ms, refreshed by the owner every 2s
}

type store struct {
	db  *dynamodb.Client
	cfg config
}

func str(s string) types.AttributeValue { return &types.AttributeValueMemberS{Value: s} }
func num(n int64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatInt(n, 10)}
}
func key(id string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"pk": str(id)}
}

func (s *store) getSession(ctx context.Context, callID string) (*session, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.cfg.SessionsTable, Key: key(callID), ConsistentRead: aws.Bool(true)})
	if err != nil || out.Item == nil {
		return nil, err
	}
	var ss session
	return &ss, attributevalue.UnmarshalMap(out.Item, &ss)
}

// heartbeat proves this replica still owns the session. It fails if another
// replica took it over or the session was released.
func (s *store) heartbeat(ctx context.Context, callID, owner string, now int64) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.cfg.SessionsTable,
		Key:                       key(callID),
		UpdateExpression:          aws.String("SET heartbeatAt = :now"),
		ConditionExpression:       aws.String("#s = :alloc AND #o = :me"),
		ExpressionAttributeNames:  map[string]string{"#s": "state", "#o": "owner"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":now": num(now), ":alloc": str(stateAllocated), ":me": str(owner)},
	})
	var ccf *types.ConditionalCheckFailedException
	if errors.As(err, &ccf) {
		return errReleased
	}
	return err
}

// orphans lists allocated sessions whose owner stopped heartbeating. A scan is fine at demo scale.
func (s *store) orphans(ctx context.Context, staleBefore int64) ([]session, error) {
	out, err := s.db.Scan(ctx, &dynamodb.ScanInput{
		TableName:                 &s.cfg.SessionsTable,
		ConsistentRead:            aws.Bool(true),
		FilterExpression:          aws.String("#s = :alloc AND heartbeatAt < :t"),
		ExpressionAttributeNames:  map[string]string{"#s": "state"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":alloc": str(stateAllocated), ":t": num(staleBefore)},
	})
	if err != nil {
		return nil, err
	}
	var ss []session
	return ss, attributevalue.UnmarshalListOfMaps(out.Items, &ss)
}

// takeover moves an orphaned session to this replica. The condition on the old
// heartbeat makes sure only one replica wins and a revived owner cannot keep it.
func (s *store) takeover(old session, owner, host string, port int, now int64) types.TransactWriteItem {
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                &s.cfg.SessionsTable,
		Key:                      key(old.CallID),
		UpdateExpression:         aws.String("SET #o = :me, #h = :host, #p = :port, heartbeatAt = :now"),
		ConditionExpression:      aws.String("#s = :alloc AND heartbeatAt = :old"),
		ExpressionAttributeNames: map[string]string{"#s": "state", "#o": "owner", "#h": "host", "#p": "port"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":me": str(owner), ":host": str(host), ":port": num(int64(port)), ":now": num(now),
			":alloc": str(stateAllocated), ":old": num(old.HeartbeatAt),
		},
	}}
}

// markProcessed is the R1 idempotency guard; always item 0 of a transaction.
func (s *store) markProcessed(messageID string) types.TransactWriteItem {
	return types.TransactWriteItem{Put: &types.Put{
		TableName:           &s.cfg.ProcessedTable,
		Item:                key(messageID),
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	}}
}

func (s *store) putSession(ss session) (types.TransactWriteItem, error) {
	item, err := attributevalue.MarshalMap(ss)
	return types.TransactWriteItem{Put: &types.Put{
		TableName: &s.cfg.SessionsTable, Item: item, ConditionExpression: aws.String("attribute_not_exists(pk)"),
	}}, err
}

// release frees a relay once: only an ALLOCATED session can become RELEASED.
func (s *store) release(callID, reason string) types.TransactWriteItem {
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                 &s.cfg.SessionsTable,
		Key:                       key(callID),
		UpdateExpression:          aws.String("SET #s = :rel, reason = :why"),
		ConditionExpression:       aws.String("#s = :alloc"),
		ExpressionAttributeNames:  map[string]string{"#s": "state"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":rel": str(stateReleased), ":alloc": str(stateAllocated), ":why": str(reason)},
	}}
}

// member adds ("ADD") or removes ("DELETE") a user from an allocated session.
func (s *store) member(callID, op, user string) types.TransactWriteItem {
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                &s.cfg.SessionsTable,
		Key:                      key(callID),
		UpdateExpression:         aws.String(op + " participants :u"),
		ConditionExpression:      aws.String("#s = :alloc"),
		ExpressionAttributeNames: map[string]string{"#s": "state"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":u": &types.AttributeValueMemberSS{Value: []string{user}}, ":alloc": str(stateAllocated),
		},
	}}
}

func (s *store) outbox(dest string, e envelope) (types.TransactWriteItem, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return types.TransactWriteItem{}, err
	}
	return types.TransactWriteItem{Put: &types.Put{
		TableName: &s.cfg.OutboxTable,
		Item: map[string]types.AttributeValue{
			"pk": str(e.MessageID), "dest": str(dest), "body": str(string(body)), "createdAt": num(e.OccurredAt.UnixMilli()),
		},
	}}, nil
}

// commit runs one transaction. Item 0 failing => errDuplicate (when dedupe is item 0),
// item 1 failing => errReleased (session state guard).
func (s *store) commit(ctx context.Context, items ...types.TransactWriteItem) error {
	_, err := s.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: items})
	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) {
		for i, r := range tce.CancellationReasons {
			if aws.ToString(r.Code) != "ConditionalCheckFailed" {
				continue
			}
			if i == 0 {
				return errDuplicate
			}
			return errReleased
		}
	}
	return err
}
