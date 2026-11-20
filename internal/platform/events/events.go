// Package events is how a module tells others of a change without calling
// them (docs/architecture.md, section 4; docs/migration.md, section 5). A
// module publishes an Event in the transaction of the change; the modules
// that react subscribe to its type.
//
// Subscribers are of two kinds. Within runs in the publisher's
// transaction, so what it writes (a webhook to deliver, a text to send, an
// alert) commits with the change or not at all, as it did when those were
// called directly. After runs once the transaction has committed, for what
// may be lost and done again: live updates, waking a worker. Every event
// is also written to event_outbox, when the outbox is on, for the relay to
// publish to RabbitMQ's kiflet.events.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"

	"github.com/kiflet/backend/internal/platform/database"
)

// Event is a change others may react to.
type Event struct {
	// Type is the module and what happened: "shipment.assigned".
	Type     string
	TenantID pgtype.UUID
	// Payload is the event's content. The outbox and the broker carry it as
	// JSON, so only fields with a JSON name go out: a field tagged
	// `json:"-"` is for the subscribers in this process alone.
	Payload any
}

// Tx is the transaction an event is published in.
type Tx struct {
	Conn database.Conn
	// After runs f once the transaction has committed. Nil runs f at once,
	// for a publisher without a transaction.
	After func(f func())
	// Scope is what the transaction's subscribers keep until it ends. Nil
	// for a publisher without a transaction: a subscriber writes at once.
	Scope *Scope
}

// Scope is what one transaction's subscribers share: values they gather
// as its events are published, and work to do with them just before it
// commits. A subscriber that must write once per transaction, or take its
// locks last (the driver feed, which locks each driver's head), gathers
// in a value and writes in BeforeCommit.
type Scope struct {
	mu     sync.Mutex
	values map[any]any
	before []func(context.Context) error
}

// Value is what is kept under key, made by init the first time. init may
// call BeforeCommit.
func (s *Scope) Value(key any, init func() any) any {
	s.mu.Lock()
	v, ok := s.values[key]
	s.mu.Unlock()
	if ok {
		return v
	}
	v = init()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[any]any{}
	}
	s.values[key] = v
	return v
}

// BeforeCommit adds f to run at the end of the transaction, just before it
// commits, after those added before it.
func (s *Scope) BeforeCommit(f func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.before = append(s.before, f)
}

// Finish runs what BeforeCommit added; whoever owns the transaction calls
// it just before committing, and rolls back on an error. Work added while
// it runs runs too.
func (s *Scope) Finish(ctx context.Context) error {
	for i := 0; ; i++ {
		s.mu.Lock()
		if i >= len(s.before) {
			s.mu.Unlock()
			return nil
		}
		f := s.before[i]
		s.mu.Unlock()
		if err := f(ctx); err != nil {
			return err
		}
	}
}

// Handler reacts to an event within the publisher's transaction.
type Handler func(ctx context.Context, tx Tx, e Event) error

// Bus publishes events and runs their subscribers.
type Bus interface {
	// Publish records an event in tx and runs its Within subscribers.
	Publish(ctx context.Context, tx Tx, e Event) error
	// Within subscribes h to events of a type ("shipment.assigned") or a
	// family ("shipment.*"), in the publisher's transaction.
	Within(pattern string, h Handler)
	// After subscribes h to events of a type or family, once the
	// publisher's transaction has committed.
	After(pattern string, h func(ctx context.Context, e Event))
}

// InProcess is a Bus whose subscribers run in this process.
type InProcess struct {
	outbox bool
	now    func() time.Time
	log    zerolog.Logger

	mu     sync.RWMutex
	within []sub[Handler]
	after  []sub[func(context.Context, Event)]
}

type sub[H any] struct {
	pattern string
	h       H
}

var _ Bus = (*InProcess)(nil)

// New is a bus. With outbox, every event is written to event_outbox.
func New(outbox bool, now func() time.Time, log zerolog.Logger) *InProcess {
	if now == nil {
		now = time.Now
	}
	return &InProcess{outbox: outbox, now: now, log: log}
}

func (b *InProcess) Within(pattern string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.within = append(b.within, sub[Handler]{pattern, h})
}

func (b *InProcess) After(pattern string, h func(context.Context, Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.after = append(b.after, sub[func(context.Context, Event)]{pattern, h})
}

func (b *InProcess) Publish(ctx context.Context, tx Tx, e Event) error {
	var errs []error
	if b.outbox {
		if err := b.write(ctx, tx.Conn, e); err != nil {
			// The transaction is spoilt, so the change fails with it: no
			// change goes in without its event.
			errs = append(errs, err)
		}
	}
	b.mu.RLock()
	within := matching(b.within, e.Type)
	after := matching(b.after, e.Type)
	b.mu.RUnlock()
	for _, s := range within {
		if err := s.h(ctx, tx, e); err != nil {
			errs = append(errs, fmt.Errorf("%s, subscriber of %s: %w", e.Type, s.pattern, err))
		}
	}
	if len(after) > 0 {
		run := func() {
			ctx := context.WithoutCancel(ctx)
			for _, s := range after {
				s.h(ctx, e)
			}
		}
		if tx.After != nil {
			tx.After(run)
		} else {
			run()
		}
	}
	return errors.Join(errs...)
}

// Matches says whether an event type is of a pattern: the type itself, or
// its family ("shipment.*").
func Matches(pattern, typ string) bool {
	if family, ok := strings.CutSuffix(pattern, ".*"); ok {
		return strings.HasPrefix(typ, family+".")
	}
	return pattern == typ
}

func matching[H any](subs []sub[H], typ string) []sub[H] {
	var out []sub[H]
	for _, s := range subs {
		if Matches(s.pattern, typ) {
			out = append(out, s)
		}
	}
	return out
}

// write records an event in event_outbox for the relay (version 1, on
// kiflet.events, routed by its type).
func (b *InProcess) write(ctx context.Context, conn database.Conn, e Event) error {
	raw, err := json.Marshal(e.Payload)
	if err != nil {
		return fmt.Errorf("event %s: %w", e.Type, err)
	}
	_, err = conn.Exec(ctx, `INSERT INTO event_outbox (id, tenant_id, type, version, exchange, routing_key, payload, created_at)
		VALUES ($1, $2, $3, 1, 'kiflet.events', $3, $4, $5)`,
		pgtype.UUID{Bytes: uuid.New(), Valid: true}, e.TenantID, e.Type, raw, pgtype.Timestamptz{Time: b.now(), Valid: true})
	if err != nil {
		return fmt.Errorf("write event %s to the outbox: %w", e.Type, err)
	}
	return nil
}
