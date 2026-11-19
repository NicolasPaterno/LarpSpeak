// Package dynamo implements app.CallStore on DynamoDB. Each write method is one
// TransactWriteItems; item order matters because errors are mapped by index.
package dynamo

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/larpspeak/call-signaling/internal/adapter/messaging"
	"github.com/larpspeak/call-signaling/internal/config"
	"github.com/larpspeak/call-signaling/internal/domain"
)

type Store struct {
	DB  *dynamodb.Client
	Cfg config.Config
}

func str(s string) types.AttributeValue { return &types.AttributeValueMemberS{Value: s} }
func num(n int64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatInt(n, 10)}
}
func set(s string) types.AttributeValue { return &types.AttributeValueMemberSS{Value: []string{s}} }
func key(id string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"pk": str(id)}
}

func (s *Store) Get(ctx context.Context, id string) (*domain.Call, error) {
	out, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.Cfg.CallsTable, Key: key(id), ConsistentRead: aws.Bool(true)})
	if err != nil || out.Item == nil {
		return nil, err
	}
	var c domain.Call
	return &c, attributevalue.UnmarshalMap(out.Item, &c)
}

// Pending lists calls still waiting for a relay. A scan is fine at demo scale.
func (s *Store) Pending(ctx context.Context) ([]domain.Call, error) {
	out, err := s.DB.Scan(ctx, &dynamodb.ScanInput{
		TableName:                 &s.Cfg.CallsTable,
		FilterExpression:          aws.String("#s = :p"),
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":p": str(domain.StatusPending)},
	})
	if err != nil {
		return nil, err
	}
	var calls []domain.Call
	return calls, attributevalue.UnmarshalListOfMaps(out.Items, &calls)
}

// ActiveInvites lists active calls that invited the user. A scan is fine at demo scale.
func (s *Store) ActiveInvites(ctx context.Context, user string) ([]domain.Call, error) {
	out, err := s.DB.Scan(ctx, &dynamodb.ScanInput{
		TableName:                 &s.Cfg.CallsTable,
		FilterExpression:          aws.String("#s = :a AND contains(invited, :u)"),
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":a": str(domain.StatusActive), ":u": str(user)},
	})
	if err != nil {
		return nil, err
	}
	var calls []domain.Call
	return calls, attributevalue.UnmarshalListOfMaps(out.Items, &calls)
}

// CurrentCall returns the call the user is in according to signaling-users ("" if none).
func (s *Store) CurrentCall(ctx context.Context, user string) (string, error) {
	out, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.Cfg.UsersTable, Key: key(user), ConsistentRead: aws.Bool(true)})
	if err != nil || out.Item == nil {
		return "", err
	}
	v, _ := out.Item["callId"].(*types.AttributeValueMemberS)
	if v == nil {
		return "", nil
	}
	return v.Value, nil
}

// ForgetUser removes the user's "current call" record if it still points to callID.
// Best effort outside transactions: a stale record is also cleaned on the next join.
func (s *Store) ForgetUser(ctx context.Context, user, callID string) {
	_, _ = s.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &s.Cfg.UsersTable, Key: key(user),
		ConditionExpression:       aws.String("callId = :c"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":c": str(callID)},
	})
}

// AddInvite appends a user to an active call's invite list.
func (s *Store) AddInvite(ctx context.Context, id, user string) error {
	_, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                &s.Cfg.CallsTable,
		Key:                      key(id),
		UpdateExpression:         aws.String("SET invited = list_append(if_not_exists(invited, :none), :u)"),
		ConditionExpression:      aws.String("#s = :active"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":none": &types.AttributeValueMemberL{}, ":u": &types.AttributeValueMemberL{Value: []types.AttributeValue{str(user)}},
			":active": str(domain.StatusActive),
		},
	})
	var ccf *types.ConditionalCheckFailedException
	if errors.As(err, &ccf) {
		return domain.ErrNotActive
	}
	return err
}

// MakeGroup turns a direct call into a group call. Already a group: nothing to do.
func (s *Store) MakeGroup(ctx context.Context, id string) error {
	_, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.Cfg.CallsTable,
		Key:                       key(id),
		UpdateExpression:          aws.String("SET kind = :group"),
		ConditionExpression:       aws.String("kind = :direct"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":group": str(domain.KindGroup), ":direct": str(domain.KindDirect)},
	})
	var ccf *types.ConditionalCheckFailedException
	if errors.As(err, &ccf) {
		return nil
	}
	return err
}

// --- atomic units of work ---

// Create is saga step 1: [call (creator inside), RelayRequested outbox, claim creator].
func (s *Store) Create(ctx context.Context, c domain.Call, out domain.Outgoing) error {
	put, err := s.putCall(c)
	if err != nil {
		return err
	}
	failed, err := s.commit(ctx, "", put, s.outbox(out), s.claimUser(c.CallerID, c.ID))
	if failed(2) {
		return domain.ErrStateChanged // the creator joined another call at the same moment
	}
	return err
}

// Activate moves a PENDING call to ACTIVE with its relay (c.Relay*, c.StartedAt) and emits CallStarted.
func (s *Store) Activate(ctx context.Context, msgID string, c domain.Call, out domain.Outgoing) error {
	fields := map[string]types.AttributeValue{"relayId": str(c.RelayID), "relayHost": str(c.RelayHost),
		"relayPort": num(int64(c.RelayPort)), "relaySignal": str(c.RelaySignal), "startedAt": num(c.StartedAt)}
	return s.transition(ctx, msgID, c.ID, domain.StatusPending, domain.StatusActive, fields, out)
}

// MoveRelay points an ACTIVE call to the relay pod that adopted it.
func (s *Store) MoveRelay(ctx context.Context, msgID, id, host string, port int, signal string) error {
	fields := map[string]types.AttributeValue{"relayHost": str(host), "relayPort": num(int64(port)), "relaySignal": str(signal)}
	return s.transition(ctx, msgID, id, domain.StatusActive, domain.StatusActive, fields)
}

// Fail is the compensation write: PENDING -> FAILED, seats freed, CallFailed emitted.
func (s *Store) Fail(ctx context.Context, msgID, id, reason string, out domain.Outgoing) error {
	fields := map[string]types.AttributeValue{"reason": str(reason), "taken": num(0)} // seats freed
	return s.transition(ctx, msgID, id, domain.StatusPending, domain.StatusFailed, fields, out)
}

// Join takes a seat (R2), emits ParticipantJoined and claims the user.
func (s *Store) Join(ctx context.Context, id, user string, out domain.Outgoing) error {
	failed, err := s.commit(ctx, "", s.join(id, user), s.outbox(out), s.claimUser(user, id))
	if failed(0) {
		return domain.ErrSeatLost // lost a race for the last seat (or the call just ended)
	}
	if failed(2) {
		return domain.ErrStateChanged // the same user joined another call at the same moment
	}
	return err
}

// Leave frees the user's seat (ending the call when last) and releases the user.
func (s *Store) Leave(ctx context.Context, msgID string, c *domain.Call, user string, last bool, now int64, out ...domain.Outgoing) error {
	items := []types.TransactWriteItem{s.leave(c.ID, user, c.Taken, last, now), s.releaseUser(user, c.ID)}
	for _, o := range out {
		items = append(items, s.outbox(o))
	}
	failed, err := s.commit(ctx, msgID, items...)
	if failed(0) {
		return domain.ErrStateChanged // counter moved under us
	}
	return err
}

// SetState adds/removes the user in the call's muted and deafened sets. Sets
// make it idempotent; it is an HTTP command, not a consumer, so no marker.
func (s *Store) SetState(ctx context.Context, id, user string, muted, deafened bool) error {
	var add, del []string
	for name, on := range map[string]bool{"muted": muted, "deafened": deafened} {
		if on {
			add = append(add, name+" :u")
		} else {
			del = append(del, name+" :u")
		}
	}
	expr := ""
	if len(add) > 0 {
		expr += "ADD " + strings.Join(add, ", ") + " "
	}
	if len(del) > 0 {
		expr += "DELETE " + strings.Join(del, ", ")
	}
	_, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.Cfg.CallsTable,
		Key:                       key(id),
		UpdateExpression:          aws.String(expr),
		ConditionExpression:       aws.String("#s = :active AND contains(participants, :uid)"),
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":u": set(user), ":uid": str(user), ":active": str(domain.StatusActive)},
	})
	var ccf *types.ConditionalCheckFailedException
	if errors.As(err, &ccf) {
		return domain.ErrNotIn
	}
	return err
}

// Ack marks a message processed, optionally emitting messages with it.
func (s *Store) Ack(ctx context.Context, msgID string, out ...domain.Outgoing) error {
	var items []types.TransactWriteItem
	for _, o := range out {
		items = append(items, s.outbox(o))
	}
	_, err := s.commit(ctx, msgID, items...)
	return err
}

// --- outbox rows, read by the messaging relay ---

func (s *Store) PendingOutbox(ctx context.Context) ([]messaging.Row, error) {
	out, err := s.DB.Scan(ctx, &dynamodb.ScanInput{TableName: &s.Cfg.OutboxTable, ConsistentRead: aws.Bool(true)})
	if err != nil {
		return nil, err
	}
	rows := make([]messaging.Row, len(out.Items))
	for i, item := range out.Items {
		rows[i] = messaging.Row{ID: attr(item, "pk"), Dest: attr(item, "dest"), Body: attr(item, "body")}
	}
	return rows, nil
}

func (s *Store) DeleteOutbox(ctx context.Context, id string) error {
	_, err := s.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: &s.Cfg.OutboxTable, Key: key(id)})
	return err
}

func attr(item map[string]types.AttributeValue, k string) string {
	if v, ok := item[k].(*types.AttributeValueMemberS); ok {
		return v.Value
	}
	return ""
}

// --- transaction items ---

// claimUser records that the user is now in callID. It fails if the user is
// already recorded in a call, which enforces one call per user.
func (s *Store) claimUser(user, callID string) types.TransactWriteItem {
	return types.TransactWriteItem{Put: &types.Put{
		TableName:           &s.Cfg.UsersTable,
		Item:                map[string]types.AttributeValue{"pk": str(user), "callId": str(callID)},
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	}}
}

// releaseUser deletes the record if it points to callID (or is already gone).
func (s *Store) releaseUser(user, callID string) types.TransactWriteItem {
	return types.TransactWriteItem{Delete: &types.Delete{
		TableName:                 &s.Cfg.UsersTable,
		Key:                       key(user),
		ConditionExpression:       aws.String("attribute_not_exists(pk) OR callId = :c"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":c": str(callID)},
	}}
}

// join is the R2 concurrency control: one atomic conditional update takes a seat
// only while the call is active, not full, and the user is not already inside.
// 100 concurrent joins on 10 free seats => exactly 10 succeed.
func (s *Store) join(callID, user string) types.TransactWriteItem {
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                aws.String(s.Cfg.CallsTable),
		Key:                      key(callID),
		UpdateExpression:         aws.String("SET taken = taken + :one ADD participants :u"),
		ConditionExpression:      aws.String("#s = :active AND taken < #cap AND NOT contains(participants, :uid)"),
		ExpressionAttributeNames: map[string]string{"#s": "status", "#cap": "capacity"}, // both are reserved words
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":one": num(1), ":u": set(user), ":uid": str(user), ":active": str(domain.StatusActive),
		},
	}}
}

// leave frees the user's seat. takenNow pins the counter so two "last" leavers
// cannot both think they are last; lastOne also ends the call in the same update.
func (s *Store) leave(callID, user string, takenNow int, lastOne bool, now int64) types.TransactWriteItem {
	expr := "SET taken = taken - :one"
	vals := map[string]types.AttributeValue{
		":one": num(1), ":u": set(user), ":uid": str(user), ":active": str(domain.StatusActive), ":n": num(int64(takenNow)),
	}
	if lastOne {
		expr += ", #s = :ended, endedAt = :now, reason = :why"
		vals[":ended"], vals[":now"], vals[":why"] = str(domain.StatusEnded), num(now), str("everyone left")
	}
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                 aws.String(s.Cfg.CallsTable),
		Key:                       key(callID),
		UpdateExpression:          aws.String(expr + " DELETE participants :u, muted :u, deafened :u"), // rejoin starts unmuted
		ConditionExpression:       aws.String("#s = :active AND contains(participants, :uid) AND taken = :n"),
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: vals,
	}}
}

// markProcessed is the R1 idempotency guard. It must be the FIRST item of a
// transaction so a cancellation at index 0 means "duplicate message".
func (s *Store) markProcessed(messageID string) types.TransactWriteItem {
	return types.TransactWriteItem{Put: &types.Put{
		TableName:           &s.Cfg.ProcessedTable,
		Item:                key(messageID),
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	}}
}

// outbox builds the outbox row. The envelope was built by us, so marshalling cannot fail.
func (s *Store) outbox(o domain.Outgoing) types.TransactWriteItem {
	body, _ := json.Marshal(o.Envelope)
	e := o.Envelope
	return types.TransactWriteItem{Put: &types.Put{
		TableName: &s.Cfg.OutboxTable,
		Item: map[string]types.AttributeValue{
			"pk": str(e.MessageID), "dest": str(o.Dest), "body": str(string(body)), "createdAt": num(e.OccurredAt.UnixMilli()),
		},
	}}
}

// transition moves a call from one status to another, failing with
// ErrStateChanged if someone else moved it first.
func (s *Store) transition(ctx context.Context, msgID, id, from, to string, fields map[string]types.AttributeValue, out ...domain.Outgoing) error {
	expr := "SET #s = :to"
	vals := map[string]types.AttributeValue{":from": str(from), ":to": str(to)}
	for k, v := range fields {
		expr += ", " + k + " = :" + k
		vals[":"+k] = v
	}
	items := []types.TransactWriteItem{{Update: &types.Update{
		TableName:                 &s.Cfg.CallsTable,
		Key:                       key(id),
		UpdateExpression:          aws.String(expr),
		ConditionExpression:       aws.String("#s = :from"),
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: vals,
	}}}
	for _, o := range out {
		items = append(items, s.outbox(o))
	}
	failed, err := s.commit(ctx, msgID, items...)
	if failed(0) {
		return domain.ErrStateChanged
	}
	return err
}

func (s *Store) putCall(c domain.Call) (types.TransactWriteItem, error) {
	item, err := attributevalue.MarshalMap(c)
	return types.TransactWriteItem{Put: &types.Put{
		TableName: &s.Cfg.CallsTable, Item: item, ConditionExpression: aws.String("attribute_not_exists(pk)"),
	}}, err
}

// commit runs one DynamoDB transaction. With a msgID, the processed-message
// marker goes first and its failure returns ErrDuplicate. failed(i) reports
// whether items[i] (marker not counted) failed its condition.
func (s *Store) commit(ctx context.Context, msgID string, items ...types.TransactWriteItem) (failed func(int) bool, err error) {
	off := 0
	if msgID != "" {
		items, off = append([]types.TransactWriteItem{s.markProcessed(msgID)}, items...), 1
	}
	_, err = s.DB.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: items})
	var codes []string
	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) {
		for _, r := range tce.CancellationReasons {
			codes = append(codes, aws.ToString(r.Code))
		}
	}
	failed = func(i int) bool { i += off; return i < len(codes) && codes[i] == "ConditionalCheckFailed" }
	if off == 1 && failed(-1) {
		return failed, domain.ErrDuplicate
	}
	return failed, err
}
