package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type bus struct {
	sqs  *sqs.Client
	sns  *sns.Client
	log  *slog.Logger
	mu   sync.Mutex
	urls map[string]string
}

// queueURL resolves and caches a queue URL by name; it retries naturally on the next call if the broker is down.
func (b *bus) queueURL(ctx context.Context, name string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if u, ok := b.urls[name]; ok {
		return u, nil
	}
	out, err := b.sqs.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: &name})
	if err != nil {
		return "", err
	}
	b.urls[name] = *out.QueueUrl
	return *out.QueueUrl, nil
}

// backoff doubles the wait after each consecutive failure, capped at 5s (R6: survive broker outages).
type backoff struct{ d time.Duration }

func (b *backoff) fail() {
	b.d = min(max(b.d*2, 250*time.Millisecond), 5*time.Second)
	time.Sleep(b.d)
}
func (b *backoff) ok() { b.d = 0 }

// consume long-polls a queue forever. A message is deleted only after it was
// handled (or recognised as a duplicate); anything else is redelivered and, after
// maxReceiveCount attempts, moved to the DLQ by SQS (R1, F2, F4).
func (b *bus) consume(ctx context.Context, queue string, handle func(context.Context, envelope) error) {
	var bo backoff
	for ctx.Err() == nil {
		url, err := b.queueURL(ctx, queue)
		if err != nil {
			b.log.Warn("broker unreachable, retrying", "queue", queue, "err", err)
			bo.fail()
			continue
		}
		out, err := b.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: &url, MaxNumberOfMessages: 10, WaitTimeSeconds: 10,
		})
		if err != nil {
			b.log.Warn("receive failed, retrying", "queue", queue, "err", err)
			bo.fail()
			continue
		}
		bo.ok()
		for _, m := range out.Messages {
			e, err := parseEnvelope(aws.ToString(m.Body))
			if err != nil {
				b.log.Error("poison message left for DLQ", "queue", queue, "err", err)
				continue
			}
			err = handle(ctx, e)
			if errors.Is(err, errDuplicate) {
				b.log.Info("duplicate ignored", "messageId", e.MessageID, "correlationId", e.CorrelationID, "type", e.Type)
			} else if err != nil {
				b.log.Warn("handling failed, will be redelivered", "messageId", e.MessageID, "correlationId", e.CorrelationID, "err", err)
				continue
			}
			if _, err := b.sqs.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: &url, ReceiptHandle: m.ReceiptHandle}); err != nil {
				b.log.Warn("delete failed, message will be redelivered and deduplicated", "messageId", e.MessageID, "err", err)
			}
		}
	}
}

// relayOutbox publishes outbox rows and deletes them after a successful send.
// A crash between send and delete republishes the same messageId; consumers dedupe it.
func relayOutbox(ctx context.Context, st *store, b *bus, topicArn string) {
	var bo backoff
	for ctx.Err() == nil {
		out, err := st.db.Scan(ctx, &dynamodb.ScanInput{TableName: &st.cfg.OutboxTable, ConsistentRead: aws.Bool(true)})
		if err != nil {
			b.log.Warn("outbox scan failed, retrying", "err", err)
			bo.fail()
			continue
		}
		for _, item := range out.Items {
			id, dest, body := attr(item, "pk"), attr(item, "dest"), attr(item, "body")
			if err := b.publish(ctx, dest, topicArn, body); err != nil {
				b.log.Warn("outbox publish failed, will retry", "messageId", id, "dest", dest, "err", err)
				bo.fail()
				break
			}
			if _, err := st.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: &st.cfg.OutboxTable, Key: key(id)}); err != nil {
				b.log.Warn("outbox delete failed", "messageId", id, "err", err)
			}
			b.log.Info("published", "messageId", id, "dest", dest)
			bo.ok()
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (b *bus) publish(ctx context.Context, dest, topicArn, body string) error {
	if dest == destTopic {
		_, err := b.sns.Publish(ctx, &sns.PublishInput{TopicArn: &topicArn, Message: &body})
		return err
	}
	url, err := b.queueURL(ctx, dest)
	if err != nil {
		return err
	}
	_, err = b.sqs.SendMessage(ctx, &sqs.SendMessageInput{QueueUrl: &url, MessageBody: &body})
	return err
}
