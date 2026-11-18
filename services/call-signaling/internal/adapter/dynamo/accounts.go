package dynamo

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/larpspeak/call-signaling/internal/domain"
)

// Accounts implements app.AccountStore. signaling-refresh holds two kinds of
// item: "<sha256 of token>" per rotation step and "family#<id>" per login session.

func familyKey(f string) map[string]types.AttributeValue { return key("family#" + f) }

func (s *Store) CreateAccount(ctx context.Context, a domain.Account) error {
	item, err := attributevalue.MarshalMap(a)
	if err != nil {
		return err
	}
	_, err = s.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.Cfg.AccountsTable, Item: item, ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	var ccf *types.ConditionalCheckFailedException
	if errors.As(err, &ccf) { // same R2 technique as seats: concurrent registers, exactly one wins
		return domain.ErrTaken
	}
	return err
}

func (s *Store) Account(ctx context.Context, user string) (*domain.Account, error) {
	out, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.Cfg.AccountsTable, Key: key(user), ConsistentRead: aws.Bool(true)})
	if err != nil || out.Item == nil {
		return nil, err
	}
	var a domain.Account
	return &a, attributevalue.UnmarshalMap(out.Item, &a)
}

func (s *Store) StartSession(ctx context.Context, t domain.RefreshToken) error {
	tok, err := s.putRefresh(t)
	if err != nil {
		return err
	}
	fam := familyKey(t.Family)
	fam["status"], fam["userId"], fam["expiresAt"] = str(domain.RefreshActive), str(t.UserID), num(t.ExpiresAt)
	_, err = s.commit(ctx, "", tok, types.TransactWriteItem{Put: &types.Put{TableName: &s.Cfg.RefreshTable, Item: fam}})
	return err
}

func (s *Store) RefreshToken(ctx context.Context, hash string) (*domain.RefreshToken, error) {
	out, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.Cfg.RefreshTable, Key: key(hash), ConsistentRead: aws.Bool(true)})
	if err != nil || out.Item == nil {
		return nil, err
	}
	var t domain.RefreshToken
	return &t, attributevalue.UnmarshalMap(out.Item, &t)
}

// Rotate: [old ACTIVE -> USED, put next, family still ACTIVE] in one transaction.
// Two tabs racing with the same token: exactly one wins, the other gets ErrTokenInvalid.
func (s *Store) Rotate(ctx context.Context, old, next domain.RefreshToken, nowMs int64) error {
	put, err := s.putRefresh(next)
	if err != nil {
		return err
	}
	failed, err := s.commit(ctx, "",
		types.TransactWriteItem{Update: &types.Update{
			TableName:                 &s.Cfg.RefreshTable,
			Key:                       key(old.Hash),
			UpdateExpression:          aws.String("SET #s = :used, usedAt = :now"),
			ConditionExpression:       aws.String("#s = :active AND expiresAt > :nowS"),
			ExpressionAttributeNames:  map[string]string{"#s": "status"},
			ExpressionAttributeValues: map[string]types.AttributeValue{":used": str(domain.RefreshUsed), ":active": str(domain.RefreshActive), ":now": num(nowMs), ":nowS": num(nowMs / 1000)},
		}},
		put,
		types.TransactWriteItem{ConditionCheck: &types.ConditionCheck{
			TableName:                 &s.Cfg.RefreshTable,
			Key:                       familyKey(old.Family),
			ConditionExpression:       aws.String("#s = :active"),
			ExpressionAttributeNames:  map[string]string{"#s": "status"},
			ExpressionAttributeValues: map[string]types.AttributeValue{":active": str(domain.RefreshActive)},
		}},
	)
	if failed(0) || failed(1) || failed(2) {
		return domain.ErrTokenInvalid
	}
	return err
}

func (s *Store) RevokeFamily(ctx context.Context, family string) error {
	_, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.Cfg.RefreshTable,
		Key:                       familyKey(family),
		UpdateExpression:          aws.String("SET #s = :r"),
		ExpressionAttributeNames:  map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":r": str(domain.RefreshRevoked)},
	})
	return err
}

func (s *Store) putRefresh(t domain.RefreshToken) (types.TransactWriteItem, error) {
	item, err := attributevalue.MarshalMap(t)
	return types.TransactWriteItem{Put: &types.Put{
		TableName: &s.Cfg.RefreshTable, Item: item, ConditionExpression: aws.String("attribute_not_exists(pk)"),
	}}, err
}
