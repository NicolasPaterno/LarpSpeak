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
	Owner        string   `dynamodbav:"owner"` // relay pod forwarding this call's audio
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
