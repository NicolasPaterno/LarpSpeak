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
	statusPending = "PENDING"
	statusActive  = "ACTIVE"
	statusFailed  = "FAILED"
	statusEnded   = "ENDED"

	destTopic = "call-events" // outbox destination: SNS topic; anything else is an SQS queue name
)

var (
	errRoomFull     = errors.New("room full")
	errDuplicate    = errors.New("message already processed")
	errStateChanged = errors.New("call state changed concurrently")
)

type call struct {
	ID           string   `json:"callId" dynamodbav:"pk"`
	RoomID       string   `json:"roomId" dynamodbav:"roomId"`
	Kind         string   `json:"kind" dynamodbav:"kind"`
	CallerID     string   `json:"callerId" dynamodbav:"callerId"`
	Participants []string `json:"participants" dynamodbav:"participants"`
	Seats        int      `json:"seats" dynamodbav:"seats"`
	Status       string   `json:"status" dynamodbav:"status"`
	Reason       string   `json:"reason,omitempty" dynamodbav:"reason,omitempty"`
	RelayID      string   `json:"relayId,omitempty" dynamodbav:"relayId,omitempty"`
	RelayHost    string   `json:"relayHost,omitempty" dynamodbav:"relayHost,omitempty"`
	RelayPort    int      `json:"relayPort,omitempty" dynamodbav:"relayPort,omitempty"`
	CreatedAt    int64    `json:"createdAt" dynamodbav:"createdAt"` // unix ms
	StartedAt    int64    `json:"startedAt,omitempty" dynamodbav:"startedAt,omitempty"`
	EndedAt      int64    `json:"endedAt,omitempty" dynamodbav:"endedAt,omitempty"`
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

func (s *store) getCall(ctx context.Context, id string) (*call, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.cfg.CallsTable, Key: key(id), ConsistentRead: aws.Bool(true)})
	if err != nil || out.Item == nil {
		return nil, err
	}
	var c call
	return &c, attributevalue.UnmarshalMap(out.Item, &c)
}

// pendingCalls lists calls still waiting for a relay. A scan is fine at demo scale.
func (s *store) pendingCalls(ctx context.Context) ([]call, error) {
	out, err := s.db.Scan(ctx, &dynamodb.ScanInput{
		TableName:                 &s.cfg.CallsTable,
		FilterExpression:          aws.String("#s = :p"),
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":p": str(statusPending)},
	})
	if err != nil {
		return nil, err
	}
	var calls []call
	return calls, attributevalue.UnmarshalListOfMaps(out.Items, &calls)
}

// --- transaction items ---

// reserveSeats is the R2 concurrency control: the update only succeeds while
// taken + seats <= capacity, atomically, so concurrent requests cannot over-book.
func (s *store) reserveSeats(roomID string, seats int) types.TransactWriteItem {
	return types.TransactWriteItem{Update: &types.Update{
		TableName:           &s.cfg.RoomsTable,
		Key:                 key(roomID),
		UpdateExpression:    aws.String("SET taken = if_not_exists(taken, :zero) + :n"),
		ConditionExpression: aws.String("attribute_not_exists(taken) OR taken <= :max"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":zero": num(0), ":n": num(int64(seats)), ":max": num(int64(s.cfg.RoomCapacity - seats)),
		},
	}}
}

func (s *store) releaseSeats(roomID string, seats int) types.TransactWriteItem {
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                 &s.cfg.RoomsTable,
		Key:                       key(roomID),
		UpdateExpression:          aws.String("SET taken = taken - :n"),
		ConditionExpression:       aws.String("taken >= :n"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":n": num(int64(seats))},
	}}
}

// markProcessed is the R1 idempotency guard. It must be the FIRST item of a
// transaction so a cancellation at index 0 means "duplicate message".
func (s *store) markProcessed(messageID string) types.TransactWriteItem {
	return types.TransactWriteItem{Put: &types.Put{
		TableName:           &s.cfg.ProcessedTable,
		Item:                key(messageID),
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
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

// transition moves a call from one status to another, failing if someone else moved it first.
func (s *store) transition(id, from, to string, set map[string]types.AttributeValue) types.TransactWriteItem {
	expr := "SET #s = :to"
	vals := map[string]types.AttributeValue{":from": str(from), ":to": str(to)}
	for k, v := range set {
		expr += ", " + k + " = :" + k
		vals[":"+k] = v
	}
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                 &s.cfg.CallsTable,
		Key:                       key(id),
		UpdateExpression:          aws.String(expr),
		ConditionExpression:       aws.String("#s = :from"),
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: vals,
	}}
}

func (s *store) putCall(c call) (types.TransactWriteItem, error) {
	item, err := attributevalue.MarshalMap(c)
	return types.TransactWriteItem{Put: &types.Put{
		TableName: &s.cfg.CallsTable, Item: item, ConditionExpression: aws.String("attribute_not_exists(pk)"),
	}}, err
}

// commit runs one DynamoDB transaction and returns the per-item cancellation codes on failure.
func (s *store) commit(ctx context.Context, items ...types.TransactWriteItem) ([]string, error) {
	_, err := s.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: items})
	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) {
		codes := make([]string, len(tce.CancellationReasons))
		for i, r := range tce.CancellationReasons {
			codes[i] = aws.ToString(r.Code)
		}
		return codes, err
	}
	return nil, err
}

// conditionFailed reports whether item i of a cancelled transaction failed its condition.
func conditionFailed(codes []string, i int) bool {
	return i < len(codes) && codes[i] == "ConditionalCheckFailed"
}
