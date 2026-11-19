// Package broker is Kiflet's connection to RabbitMQ (docs/architecture.md,
// section 6): the exchanges and queues, publishing with confirms, and
// consumers that acknowledge a message only once it has been handled.
//
// Every message is an Envelope. Delivery is at least once: a consumer may
// see a message twice, and each handler is idempotent (a unique key, or
// processed_messages). A handler that fails has its message tried again
// after its queue's delay, through a retry queue; after MaxAttempts it is
// parked in the queue's dead-letter queue for a person to look at.
package broker

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"

	"github.com/kiflet/backend/pkg/metrics"
)

// The exchanges.
const (
	// Events: what happened (shipment.status_changed), to whoever listens.
	Events = "kiflet.events"
	// Commands: work for one kind of worker (tracking.positions).
	Commands = "kiflet.commands"
	// Jobs: the scheduler's timed work.
	Jobs = "kiflet.jobs"
	// Live: updates for open dashboards, best-effort.
	Live = "kiflet.live"
)

// Envelope is every message's shape. Consumers ignore versions they don't
// know.
type Envelope struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Version    int             `json:"version"`
	TenantID   string          `json:"tenant_id,omitempty"`
	OccurredAt time.Time       `json:"occurred_at"`
	TraceID    string          `json:"trace_id,omitempty"`
	Payload    json.RawMessage `json:"payload"`
}

// NewEnvelope wraps a payload with a new ID.
func NewEnvelope(typ, tenantID string, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{ID: NewID(), Type: typ, Version: 1, TenantID: tenantID, OccurredAt: time.Now().UTC(), Payload: raw}, nil
}

// NewID is a random UUID (version 4).
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Message is one envelope on its way to an exchange.
type Message struct {
	Exchange   string
	RoutingKey string
	Envelope   Envelope
}

// Queue is a durable queue with its bindings and its retry policy.
type Queue struct {
	Name     string
	Bindings []Binding
	// RetryAfter is how long a failed message waits before it is tried
	// again; MaxAttempts how many tries it gets before it is parked.
	RetryAfter  time.Duration
	MaxAttempts int
}

// Binding routes an exchange's messages to a queue.
type Binding struct{ Exchange, Key string }

// Topology is the queues of docs/architecture.md section 6 that exist so
// far. A queue is declared by every process, consumer or not, so that a
// message published before its consumer first starts is kept.
var Topology = []Queue{
	{
		Name: "tracking.ingest", Bindings: []Binding{{Commands, "tracking.positions"}},
		RetryAfter: 10 * time.Second, MaxAttempts: 5,
	},
}

// Broker is a connection to RabbitMQ that comes back by itself when it is
// lost.
type Broker struct {
	url string
	log zerolog.Logger

	// dial is held while reconnecting, so that only one does.
	dial   sync.Mutex
	mu     sync.Mutex
	conn   *amqp.Connection
	pub    *amqp.Channel // in confirm mode
	closed bool
}

// Dial connects and declares the exchanges and queues.
func Dial(ctx context.Context, url string, log zerolog.Logger) (*Broker, error) {
	b := &Broker{url: url, log: log}
	deadline := time.Now().Add(60 * time.Second)
	for {
		err := b.connect()
		if err == nil {
			return b, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil, fmt.Errorf("rabbitmq: %w", err)
		}
		log.Warn().Err(err).Msg("rabbitmq not ready, retrying")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (b *Broker) connect() error {
	conn, err := amqp.DialConfig(b.url, amqp.Config{Heartbeat: 10 * time.Second, Properties: amqp.Table{"connection_name": "kiflet"}})
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	if err := declare(ch); err != nil {
		_ = conn.Close()
		return err
	}
	if err := ch.Confirm(false); err != nil {
		_ = conn.Close()
		return err
	}
	b.mu.Lock()
	b.conn, b.pub = conn, ch
	b.mu.Unlock()
	return nil
}

// declare makes the exchanges and queues; declaring what exists with the
// same settings changes nothing.
func declare(ch *amqp.Channel) error {
	for name, kind := range map[string]string{Events: "topic", Commands: "direct", Jobs: "direct", Live: "topic"} {
		if err := ch.ExchangeDeclare(name, kind, true, false, false, false, nil); err != nil {
			return fmt.Errorf("exchange %s: %w", name, err)
		}
	}
	for _, q := range Topology {
		// Failed messages go to the retry queue, which hands them back
		// after the delay.
		if _, err := ch.QueueDeclare(q.Name, true, false, false, false, amqp.Table{
			"x-queue-type":              "quorum",
			"x-dead-letter-exchange":    "",
			"x-dead-letter-routing-key": q.Name + ".retry",
		}); err != nil {
			return fmt.Errorf("queue %s: %w", q.Name, err)
		}
		if _, err := ch.QueueDeclare(q.Name+".retry", true, false, false, false, amqp.Table{
			"x-queue-type":              "quorum",
			"x-message-ttl":             q.RetryAfter.Milliseconds(),
			"x-dead-letter-exchange":    "",
			"x-dead-letter-routing-key": q.Name,
		}); err != nil {
			return fmt.Errorf("queue %s.retry: %w", q.Name, err)
		}
		if _, err := ch.QueueDeclare(q.Name+".dead", true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
			return fmt.Errorf("queue %s.dead: %w", q.Name, err)
		}
		for _, bd := range q.Bindings {
			if err := ch.QueueBind(q.Name, bd.Key, bd.Exchange, false, nil); err != nil {
				return fmt.Errorf("bind %s: %w", q.Name, err)
			}
		}
	}
	return nil
}

// channel is the publishing channel, reconnecting when it was lost.
func (b *Broker) channel() (*amqp.Channel, error) {
	b.mu.Lock()
	closed, ch := b.closed, b.pub
	b.mu.Unlock()
	if closed {
		return nil, errors.New("rabbitmq: closed")
	}
	if ch != nil && !ch.IsClosed() {
		return ch, nil
	}
	b.dial.Lock()
	defer b.dial.Unlock()
	b.mu.Lock()
	ch = b.pub
	b.mu.Unlock()
	if ch != nil && !ch.IsClosed() {
		return ch, nil // another caller reconnected meanwhile
	}
	if err := b.connect(); err != nil {
		return nil, fmt.Errorf("rabbitmq: %w", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pub, nil
}

// Publish sends messages and returns once RabbitMQ has confirmed it holds
// every one, or with an error: then any of them may or may not be held, and
// the caller sends them again (consumers are idempotent).
func (b *Broker) Publish(ctx context.Context, msgs ...Message) error {
	ch, err := b.channel()
	if err != nil {
		return err
	}
	// The channel numbers each message for its confirm, so several callers
	// may publish on it at once; each waits for its own messages' confirms.
	confirms := make([]*amqp.DeferredConfirmation, 0, len(msgs))
	for _, m := range msgs {
		body, err := json.Marshal(m.Envelope)
		if err != nil {
			return err
		}
		dc, err := ch.PublishWithDeferredConfirmWithContext(ctx, m.Exchange, m.RoutingKey, false, false, amqp.Publishing{
			ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: m.Envelope.ID,
			Type: m.Envelope.Type, Timestamp: m.Envelope.OccurredAt, Body: body,
		})
		if err != nil {
			return fmt.Errorf("rabbitmq: publish: %w", err)
		}
		confirms = append(confirms, dc)
	}
	for _, dc := range confirms {
		ok, err := dc.WaitContext(ctx)
		if err != nil {
			return fmt.Errorf("rabbitmq: confirm: %w", err)
		}
		if !ok {
			return errors.New("rabbitmq: a message was refused")
		}
	}
	for _, m := range msgs {
		metrics.BrokerPublished.WithLabelValues(m.Exchange).Inc()
	}
	return nil
}

// Delivery is a message handed to a consumer.
type Delivery struct {
	Envelope Envelope
	// Attempt is 1 the first time, 2 after one failure, and so on.
	Attempt int
}

// Handler handles a batch of messages. Returning nil acknowledges them
// all; an error sends them all to be tried again.
type Handler func(ctx context.Context, batch []Delivery) error

// Consume runs handler on queue until ctx ends, in batches of up to
// prefetch messages (a batch is what arrives within a short wait),
// reconnecting when the connection is lost.
func (b *Broker) Consume(ctx context.Context, queue string, prefetch int, handler Handler) {
	q, ok := lookup(queue)
	if !ok {
		b.log.Error().Str("queue", queue).Msg("rabbitmq: no such queue in the topology")
		return
	}
	delay := time.Second
	for ctx.Err() == nil {
		err := b.consume(ctx, q, prefetch, handler)
		if ctx.Err() != nil {
			return
		}
		b.log.Warn().Err(err).Str("queue", queue).Dur("retry_in", delay).Msg("rabbitmq: consumer lost")
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func lookup(name string) (Queue, bool) {
	for _, q := range Topology {
		if q.Name == name {
			return q, true
		}
	}
	return Queue{}, false
}

func (b *Broker) consume(ctx context.Context, q Queue, prefetch int, handler Handler) error {
	conn, err := amqp.DialConfig(b.url, amqp.Config{Heartbeat: 10 * time.Second, Properties: amqp.Table{"connection_name": "kiflet " + q.Name}})
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck // closing
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	if err := declare(ch); err != nil {
		return err
	}
	if err := ch.Qos(prefetch, 0, false); err != nil {
		return err
	}
	deliveries, err := ch.ConsumeWithContext(ctx, q.Name, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	for {
		var batch []amqp.Delivery
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-closed:
			return fmt.Errorf("connection closed: %v", e)
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("deliveries closed")
			}
			batch = append(batch, d)
		}
		// What else has arrived, up to prefetch, without waiting long.
		wait := time.NewTimer(50 * time.Millisecond)
	more:
		for len(batch) < prefetch {
			select {
			case d, ok := <-deliveries:
				if !ok {
					break more
				}
				batch = append(batch, d)
			case <-wait.C:
				break more
			}
		}
		wait.Stop()
		b.handle(ctx, ch, q, batch, handler)
	}
}

func (b *Broker) handle(ctx context.Context, ch *amqp.Channel, q Queue, batch []amqp.Delivery, handler Handler) {
	items := make([]Delivery, 0, len(batch))
	var good []amqp.Delivery
	for _, d := range batch {
		var env Envelope
		if err := json.Unmarshal(d.Body, &env); err != nil || env.ID == "" {
			// Never going to work: parked at once.
			b.park(ctx, ch, q, d, "not an envelope")
			continue
		}
		items = append(items, Delivery{Envelope: env, Attempt: attempts(d) + 1})
		good = append(good, d)
	}
	if len(good) == 0 {
		return
	}
	err := handler(ctx, items)
	for i, d := range good {
		switch {
		case err == nil:
			_ = d.Ack(false)
			metrics.BrokerConsumed.WithLabelValues(q.Name, "ok").Inc()
		case items[i].Attempt >= q.MaxAttempts:
			b.park(ctx, ch, q, d, err.Error())
		default:
			// To the retry queue, which hands it back after the delay.
			_ = d.Nack(false, false)
			metrics.BrokerConsumed.WithLabelValues(q.Name, "retry").Inc()
		}
	}
	if err != nil {
		b.log.Warn().Err(err).Str("queue", q.Name).Int("messages", len(good)).Msg("rabbitmq: handler failed")
	}
}

// park moves a message to its queue's dead-letter queue and acknowledges
// it, so it waits there for a person instead of going round for ever.
func (b *Broker) park(ctx context.Context, ch *amqp.Channel, q Queue, d amqp.Delivery, why string) {
	headers := amqp.Table{"x-kiflet-error": why}
	for k, v := range d.Headers {
		headers[k] = v
	}
	err := ch.PublishWithContext(ctx, "", q.Name+".dead", false, false, amqp.Publishing{
		ContentType: d.ContentType, DeliveryMode: amqp.Persistent, MessageId: d.MessageId, Type: d.Type,
		Timestamp: d.Timestamp, Headers: headers, Body: d.Body,
	})
	if err != nil {
		// Left unacknowledged: it comes back when the channel closes.
		b.log.Error().Err(err).Str("queue", q.Name).Msg("rabbitmq: park a message")
		return
	}
	_ = d.Ack(false)
	metrics.BrokerConsumed.WithLabelValues(q.Name, "parked").Inc()
	b.log.Error().Str("queue", q.Name).Str("id", d.MessageId).Str("error", why).Msg("rabbitmq: message parked after its last attempt")
}

// attempts is how many times a message has failed: how often it has come
// back from its retry queue.
func attempts(d amqp.Delivery) int {
	deaths, _ := d.Headers["x-death"].([]any)
	n := 0
	for _, x := range deaths {
		t, _ := x.(amqp.Table)
		if reason, _ := t["reason"].(string); reason != "rejected" {
			continue
		}
		switch c := t["count"].(type) {
		case int64:
			n += int(c)
		case int32:
			n += int(c)
		case string:
			v, _ := strconv.Atoi(c)
			n += v
		}
	}
	return n
}

// Ready says whether the publishing connection is open, for /readyz.
func (b *Broker) Ready() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conn != nil && !b.conn.IsClosed()
}

// Close closes the connection.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	if b.conn != nil {
		_ = b.conn.Close()
	}
}
