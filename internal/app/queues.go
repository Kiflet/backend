package app

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/rs/zerolog"

	"github.com/kiflet/backend/internal/service"
	"github.com/kiflet/backend/pkg/broker"
)

// trackingPrefetch is how many syncs' fixes a tracking worker takes at a
// time.
const trackingPrefetch = 50

// brokerPositions hands GPS fixes to the tracking workers through
// tracking.ingest. The phone is told they are stored once RabbitMQ has
// confirmed it holds them.
type brokerPositions struct{ b *broker.Broker }

func (p brokerPositions) Enqueue(ctx context.Context, b service.PositionBatch) error {
	env, err := broker.NewEnvelope("tracking.positions", b.TenantID.String(), b)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return p.b.Publish(ctx, broker.Message{Exchange: broker.Commands, RoutingKey: "tracking.positions", Envelope: env})
}

// storePositions is the tracking worker: it writes each batch, and a batch
// that fails is tried again, the fixes already written being skipped by
// their client IDs.
func storePositions(t *service.TrackingService) broker.Handler {
	return func(ctx context.Context, batch []broker.Delivery) error {
		var errs []error
		for _, d := range batch {
			var b service.PositionBatch
			if err := json.Unmarshal(d.Envelope.Payload, &b); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := t.StorePositions(ctx, b); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
}

// brokerLive carries live updates between instances through RabbitMQ's
// kiflet.live exchange, in place of Postgres's NOTIFY (liveRelay).
type brokerLive struct {
	broker *broker.Broker
	hub    localHub
	log    zerolog.Logger
}

// BroadcastToTenant publishes an update for every instance's dashboards of
// a company; if RabbitMQ can't take it, this instance's own still get it.
func (l *brokerLive) BroadcastToTenant(tenantID string, message []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.broker.PublishLive(ctx, tenantID, message); err != nil {
		l.log.Warn().Err(err).Msg("publish live update")
		l.hub.BroadcastToTenant(tenantID, message)
	}
}

// Run hands the updates every instance publishes to this one's dashboards.
func (l *brokerLive) Run(ctx context.Context) {
	l.broker.SubscribeLive(ctx, l.hub.BroadcastToTenant)
}

// senderQueues are the queues of the senders whose work goes through
// RabbitMQ (service.WorkQueue), by sender.
var senderQueues = map[string]string{
	service.WorkSMS:      "notify.sms",
	service.WorkMail:     "notify.email",
	service.WorkWebhooks: "webhook.deliver",
}

// senderPrefetch is how many messages (of up to workChunk rows) a
// sender's consumer holds at a time: with one consumer per worker, it caps
// what a worker has claimed, and so how hard it presses on the SMS
// gateway.
const senderPrefetch = 5

// workChunk is the most rows one message names: a worker claims a
// message's rows as it comes to it, so its lease starts then.
const workChunk = 20

// brokerWork is service.WorkQueue on RabbitMQ.
type brokerWork struct {
	b     *broker.Broker
	kinds map[string]bool
}

// workMessage is what notify.sms, notify.email and webhook.deliver carry.
type workMessage struct {
	IDs []pgtype.UUID `json:"ids"`
}

func (w brokerWork) Has(kind string) bool { return w.kinds[kind] }

func (w brokerWork) Publish(ctx context.Context, kind string, ids []pgtype.UUID) error {
	queue := senderQueues[kind]
	var msgs []broker.Message
	for chunk := range slices.Chunk(ids, workChunk) {
		env, err := broker.NewEnvelope(queue, "", workMessage{IDs: chunk})
		if err != nil {
			return err
		}
		msgs = append(msgs, broker.Message{Exchange: broker.Commands, RoutingKey: queue, Envelope: env})
	}
	return w.b.Publish(ctx, msgs...)
}

func (w brokerWork) Ready(ctx context.Context, kind string) (int, error) {
	return w.b.Waiting(ctx, senderQueues[kind])
}

// deliverWork is a sender's consumer: each message's rows are claimed and
// sent in turn.
func deliverWork(deliver func(context.Context, []pgtype.UUID) error) broker.Handler {
	return func(ctx context.Context, batch []broker.Delivery) error {
		for _, d := range batch {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var m workMessage
			if err := json.Unmarshal(d.Envelope.Payload, &m); err != nil {
				return err
			}
			if err := deliver(ctx, m.IDs); err != nil {
				return err
			}
		}
		return nil
	}
}
