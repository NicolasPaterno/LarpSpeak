// Package messaging moves envelopes between the outbox and SQS.
package messaging

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/larpspeak/media-relay/internal/domain"
)

type Bus struct {
	sqs  *sqs.Client
	log  *slog.Logger
	mu   sync.Mutex
	urls map[string]string
}

func New(q *sqs.Client, log *slog.Logger) *Bus {
	return &Bus{sqs: q, log: log, urls: map[string]string{}}
}

// Row is one pending outbox entry.
type Row struct{ ID, Dest, Body string }

// Outbox is the outbox table, as the relay loop needs it.
type Outbox interface {
	PendingOutbox(ctx context.Context) ([]Row, error)
	DeleteOutbox(ctx context.Context, id string) error
}

// queueURL resolves and caches a queue URL by name; it retries naturally on the next call if the broker is down.
func (b *Bus) queueURL(ctx context.Context, name string) (string, error) {
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

// Consume long-polls a queue forever. A message is deleted only after it was
// handled (or recognised as a duplicate); anything else is redelivered and, after
// maxReceiveCount attempts, moved to the DLQ by SQS (R1, F2, F4).
func (b *Bus) Consume(ctx context.Context, queue string, handle func(context.Context, domain.Envelope) error) {
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
			e, err := domain.ParseEnvelope(aws.ToString(m.Body))
			if err != nil {
				b.log.Error("poison message left for DLQ", "queue", queue, "err", err)
				continue
			}
			err = handle(ctx, e)
			if errors.Is(err, domain.ErrDuplicate) {
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

// RelayOutbox publishes outbox rows and deletes them after a successful send.
// A crash between send and delete republishes the same messageId; consumers dedupe it.
func RelayOutbox(ctx context.Context, ob Outbox, b *Bus) {
	var bo backoff
	for ctx.Err() == nil {
		rows, err := ob.PendingOutbox(ctx)
		if err != nil {
			b.log.Warn("outbox scan failed, retrying", "err", err)
			bo.fail()
			continue
		}
		for _, row := range rows {
			id, dest, body := row.ID, row.Dest, row.Body
			if err := b.publish(ctx, dest, body); err != nil {
				b.log.Warn("outbox publish failed, will retry", "messageId", id, "dest", dest, "err", err)
				bo.fail()
				break
			}
			if err := ob.DeleteOutbox(ctx, id); err != nil {
				b.log.Warn("outbox delete failed", "messageId", id, "err", err)
			}
			b.log.Info("published", "messageId", id, "dest", dest)
			bo.ok()
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (b *Bus) publish(ctx context.Context, queue, body string) error {
	url, err := b.queueURL(ctx, queue)
	if err != nil {
		return err
	}
	_, err = b.sqs.SendMessage(ctx, &sqs.SendMessageInput{QueueUrl: &url, MessageBody: &body})
	return err
}
