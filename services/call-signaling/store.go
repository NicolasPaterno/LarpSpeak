package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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
	errRoomFull     = errors.New("call is full")
	errNotActive    = errors.New("call is not active")
	errAlreadyIn    = errors.New("user already in call")
	errNotIn        = errors.New("user not in call")
	errDuplicate    = errors.New("message already processed")
	errStateChanged = errors.New("call state changed concurrently")
)

// call is a voice channel: up to Capacity participants talk at the same time.
// Taken is the seat counter guarded by conditional writes (R2).
type call struct {
	ID           string   `json:"callId" dynamodbav:"pk"`
	Kind         string   `json:"kind" dynamodbav:"kind"`
	CallerID     string   `json:"callerId" dynamodbav:"callerId"`
	Invited      []string `json:"invited,omitempty" dynamodbav:"invited,omitempty"`
	Participants []string `json:"participants" dynamodbav:"participants,stringset,omitempty"`
	Taken        int      `json:"taken" dynamodbav:"taken"`
	Capacity     int      `json:"capacity" dynamodbav:"capacity"`
	Status       string   `json:"status" dynamodbav:"status"`
	Reason       string   `json:"reason,omitempty" dynamodbav:"reason,omitempty"`
	RelayID      string   `json:"relayId,omitempty" dynamodbav:"relayId,omitempty"`
	RelayHost    string   `json:"relayHost,omitempty" dynamodbav:"relayHost,omitempty"`
	RelayPort    int      `json:"relayPort,omitempty" dynamodbav:"relayPort,omitempty"`
	CreatedAt    int64    `json:"createdAt" dynamodbav:"createdAt"` // unix ms
	StartedAt    int64    `json:"startedAt,omitempty" dynamodbav:"startedAt,omitempty"`
	EndedAt      int64    `json:"endedAt,omitempty" dynamodbav:"endedAt,omitempty"`
}

func (c *call) has(user string) bool { return slices.Contains(c.Participants, user) }

type store struct {
	db  *dynamodb.Client
	cfg config
}

func str(s string) types.AttributeValue { return &types.AttributeValueMemberS{Value: s} }
func num(n int64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatInt(n, 10)}
}
func set(s string) types.AttributeValue { return &types.AttributeValueMemberSS{Value: []string{s}} }
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

// activeInvites lists active calls that invited the user. A scan is fine at demo scale.
func (s *store) activeInvites(ctx context.Context, user string) ([]call, error) {
	out, err := s.db.Scan(ctx, &dynamodb.ScanInput{
		TableName:                 &s.cfg.CallsTable,
		FilterExpression:          aws.String("#s = :a AND contains(invited, :u)"),
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":a": str(statusActive), ":u": str(user)},
	})
	if err != nil {
		return nil, err
	}
	var calls []call
	return calls, attributevalue.UnmarshalListOfMaps(out.Items, &calls)
}

// currentCall returns the call the user is in according to signaling-users ("" if none).
func (s *store) currentCall(ctx context.Context, user string) (string, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.cfg.UsersTable, Key: key(user), ConsistentRead: aws.Bool(true)})
	if err != nil || out.Item == nil {
		return "", err
	}
	v, _ := out.Item["callId"].(*types.AttributeValueMemberS)
	if v == nil {
		return "", nil
	}
	return v.Value, nil
}

// forgetUser removes the user's "current call" record if it still points to callID.
// Best effort outside transactions: a stale record is also cleaned on the next join.
func (s *store) forgetUser(ctx context.Context, user, callID string) {
	_, _ = s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &s.cfg.UsersTable, Key: key(user),
		ConditionExpression:       aws.String("callId = :c"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":c": str(callID)},
	})
}

// addInvite appends a user to an active call's invite list.
func (s *store) addInvite(ctx context.Context, id, user string) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                &s.cfg.CallsTable,
		Key:                      key(id),
		UpdateExpression:         aws.String("SET invited = list_append(if_not_exists(invited, :none), :u)"),
		ConditionExpression:      aws.String("#s = :active"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":none": &types.AttributeValueMemberL{}, ":u": &types.AttributeValueMemberL{Value: []types.AttributeValue{str(user)}},
			":active": str(statusActive),
		},
	})
	var ccf *types.ConditionalCheckFailedException
	if errors.As(err, &ccf) {
		return errNotActive
	}
	return err
}

// --- transaction items ---

// claimUser records that the user is now in callID. It fails if the user is
// already recorded in a call, which enforces one call per user.
func (s *store) claimUser(user, callID string) types.TransactWriteItem {
	return types.TransactWriteItem{Put: &types.Put{
		TableName:           &s.cfg.UsersTable,
		Item:                map[string]types.AttributeValue{"pk": str(user), "callId": str(callID)},
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	}}
}

// releaseUser deletes the record if it points to callID (or is already gone).
func (s *store) releaseUser(user, callID string) types.TransactWriteItem {
	return types.TransactWriteItem{Delete: &types.Delete{
		TableName:                 &s.cfg.UsersTable,
		Key:                       key(user),
		ConditionExpression:       aws.String("attribute_not_exists(pk) OR callId = :c"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":c": str(callID)},
	}}
}

// join is the R2 concurrency control: one atomic conditional update takes a seat
// only while the call is active, not full, and the user is not already inside.
// 100 concurrent joins on 10 free seats => exactly 10 succeed.
func (s *store) join(callID, user string) types.TransactWriteItem {
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                aws.String(s.cfg.CallsTable),
		Key:                      key(callID),
		UpdateExpression:         aws.String("SET taken = taken + :one ADD participants :u"),
		ConditionExpression:      aws.String("#s = :active AND taken < #cap AND NOT contains(participants, :uid)"),
		ExpressionAttributeNames: map[string]string{"#s": "status", "#cap": "capacity"}, // both are reserved words
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":one": num(1), ":u": set(user), ":uid": str(user), ":active": str(statusActive),
		},
	}}
}

// leave frees the user's seat. takenNow pins the counter so two "last" leavers
// cannot both think they are last; lastOne also ends the call in the same update.
func (s *store) leave(callID, user string, takenNow int, lastOne bool, now int64) types.TransactWriteItem {
	expr := "SET taken = taken - :one"
	vals := map[string]types.AttributeValue{
		":one": num(1), ":u": set(user), ":uid": str(user), ":active": str(statusActive), ":n": num(int64(takenNow)),
	}
	if lastOne {
		expr += ", #s = :ended, endedAt = :now, reason = :why"
		vals[":ended"], vals[":now"], vals[":why"] = str(statusEnded), num(now), str("everyone left")
	}
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                 aws.String(s.cfg.CallsTable),
		Key:                       key(callID),
		UpdateExpression:          aws.String(expr + " DELETE participants :u"),
		ConditionExpression:       aws.String("#s = :active AND contains(participants, :uid) AND taken = :n"),
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: vals,
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
func (s *store) transition(id, from, to string, fields map[string]types.AttributeValue) types.TransactWriteItem {
	expr := "SET #s = :to"
	vals := map[string]types.AttributeValue{":from": str(from), ":to": str(to)}
	for k, v := range fields {
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
