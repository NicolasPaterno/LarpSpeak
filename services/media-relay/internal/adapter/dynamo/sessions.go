// Package dynamo implements app.SessionStore and app.NodeStore on DynamoDB.
// Each write method is one transaction: item 0 failing means a duplicate
// message (when it is the dedupe marker), any later item means ErrReleased.
package dynamo

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/larpspeak/media-relay/internal/adapter/messaging"
	"github.com/larpspeak/media-relay/internal/config"
	"github.com/larpspeak/media-relay/internal/domain"
)

type Store struct {
	DB  *dynamodb.Client
	Cfg config.Config
}

func str(s string) types.AttributeValue { return &types.AttributeValueMemberS{Value: s} }
func num(n int64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatInt(n, 10)}
}
func key(id string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"pk": str(id)}
}

func (s *Store) Get(ctx context.Context, callID string) (*domain.Session, error) {
	out, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.Cfg.SessionsTable, Key: key(callID), ConsistentRead: aws.Bool(true)})
	if err != nil || out.Item == nil {
		return nil, err
	}
	var ss domain.Session
	return &ss, attributevalue.UnmarshalMap(out.Item, &ss)
}

// Allocated lists every allocated session. A scan is fine at demo scale.
func (s *Store) Allocated(ctx context.Context) ([]domain.Session, error) {
	out, err := s.DB.Scan(ctx, &dynamodb.ScanInput{
		TableName:                 &s.Cfg.SessionsTable,
		ConsistentRead:            aws.Bool(true),
		FilterExpression:          aws.String("#s = :alloc"),
		ExpressionAttributeNames:  map[string]string{"#s": "state"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":alloc": str(domain.StateAllocated)},
	})
	if err != nil {
		return nil, err
	}
	var ss []domain.Session
	return ss, attributevalue.UnmarshalListOfMaps(out.Items, &ss)
}

// Allocate: [dedupe, new session, RelayAllocated]. ErrReleased: a session already exists for the call.
func (s *Store) Allocate(ctx context.Context, msgID string, ss domain.Session, out domain.Outgoing) error {
	item, err := attributevalue.MarshalMap(ss)
	if err != nil {
		return err
	}
	put := types.TransactWriteItem{Put: &types.Put{
		TableName: &s.Cfg.SessionsTable, Item: item, ConditionExpression: aws.String("attribute_not_exists(pk)"),
	}}
	return s.commit(ctx, s.markProcessed(msgID), put, s.outbox(out))
}

// Ack: [dedupe, outbox...].
func (s *Store) Ack(ctx context.Context, msgID string, out ...domain.Outgoing) error {
	items := []types.TransactWriteItem{s.markProcessed(msgID)}
	for _, o := range out {
		items = append(items, s.outbox(o))
	}
	return s.commit(ctx, items...)
}

// Release frees a relay once: only an ALLOCATED session can become RELEASED.
func (s *Store) Release(ctx context.Context, msgID, callID, reason string) error {
	return s.commit(ctx, s.markProcessed(msgID), types.TransactWriteItem{Update: &types.Update{
		TableName:                 &s.Cfg.SessionsTable,
		Key:                       key(callID),
		UpdateExpression:          aws.String("SET #s = :rel, reason = :why"),
		ConditionExpression:       aws.String("#s = :alloc"),
		ExpressionAttributeNames:  map[string]string{"#s": "state"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":rel": str(domain.StateReleased), ":alloc": str(domain.StateAllocated), ":why": str(reason)},
	}})
}

// Member adds ("ADD") or removes ("DELETE") a user from an allocated session.
func (s *Store) Member(ctx context.Context, msgID, callID, op, user string) error {
	return s.commit(ctx, s.markProcessed(msgID), types.TransactWriteItem{Update: &types.Update{
		TableName:                &s.Cfg.SessionsTable,
		Key:                      key(callID),
		UpdateExpression:         aws.String(op + " participants :u"),
		ConditionExpression:      aws.String("#s = :alloc"),
		ExpressionAttributeNames: map[string]string{"#s": "state"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":u": &types.AttributeValueMemberSS{Value: []string{user}}, ":alloc": str(domain.StateAllocated),
		},
	}})
}

// Emit writes one outbox message on its own.
func (s *Store) Emit(ctx context.Context, out domain.Outgoing) error {
	return s.commit(ctx, s.outbox(out))
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

// markProcessed is the R1 idempotency guard; always item 0 of a transaction.
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

// commit runs one transaction. Item 0 failing => ErrDuplicate (when dedupe is item 0),
// item 1 failing => ErrReleased (session state guard).
func (s *Store) commit(ctx context.Context, items ...types.TransactWriteItem) error {
	_, err := s.DB.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: items})
	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) {
		for i, r := range tce.CancellationReasons {
			if aws.ToString(r.Code) != "ConditionalCheckFailed" {
				continue
			}
			if i == 0 {
				return domain.ErrDuplicate
			}
			return domain.ErrReleased
		}
	}
	return err
}
