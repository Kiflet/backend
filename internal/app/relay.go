package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/kiflet/backend/internal/db"
	"github.com/kiflet/backend/pkg/broker"
	"github.com/kiflet/backend/pkg/metrics"
)

// relayLock is the advisory lock that makes one relay the leader, so
// events leave in the order they were written.
const relayLock = 0x6b69666c6574 // "kiflet"

const (
	relayBatch = 500
	// relayPoll is how often the relay looks without being woken: for
	// events whose retry delay has passed, and the lag metrics.
	relayPoll = 5 * time.Second
)

// relay publishes event_outbox to RabbitMQ (docs/architecture.md, section
// 6): woken by NOTIFY when a transaction writes events, it takes the
// oldest unsent with FOR UPDATE SKIP LOCKED, publishes them with confirms,
// and marks them sent in the same transaction. An event is published at
// least once: a relay that stops between the confirm and the commit
// publishes it again on its next start, and consumers ignore repeats.
type relay struct {
	pool   *pgxpool.Pool
	broker *broker.Broker
	log    zerolog.Logger
}

// Run relays until ctx ends. Every instance runs one; the one holding the
// lock works, and the others wait to take over.
func (r *relay) Run(ctx context.Context) {
	delay := time.Second
	for ctx.Err() == nil {
		err := r.lead(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			r.log.Warn().Err(err).Dur("retry_in", delay).Msg("outbox relay stopped")
			delay = min(delay*2, 30*time.Second)
		} else {
			delay = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// lead takes the lock and relays while it holds it. It returns nil when
// another relay holds the lock.
func (r *relay) lead(ctx context.Context) error {
	pooled, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// The connection holds the lock and the LISTEN: it isn't handed back
	// to the pool with either, and closing it lets go of both.
	conn := pooled.Hijack()
	defer conn.Close(context.Background()) //nolint:errcheck // closing
	var leader bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", int64(relayLock)).Scan(&leader); err != nil {
		return err
	}
	if !leader {
		// Another instance relays; look again in a while.
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
		return nil
	}
	if _, err := conn.Exec(ctx, "LISTEN event_outbox"); err != nil {
		return err
	}
	r.log.Info().Msg("outbox relay leading")
	for {
		if err := r.drain(ctx); err != nil {
			return err
		}
		r.lag(ctx)
		wait, cancel := context.WithTimeout(ctx, relayPoll)
		_, err := conn.WaitForNotification(wait)
		cancel()
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil && !errors.Is(err, context.DeadlineExceeded):
			return err
		}
	}
}

// drain publishes batches until none is full.
func (r *relay) drain(ctx context.Context) error {
	for {
		n, err := r.batch(ctx)
		if err != nil || n < relayBatch {
			return err
		}
	}
}

func (r *relay) batch(ctx context.Context) (int, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	q := db.New(tx)
	rows, err := q.ClaimOutboxEvents(ctx, relayBatch)
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	msgs := make([]broker.Message, 0, len(rows))
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, e := range rows {
		env := broker.Envelope{
			ID: e.ID.String(), Type: e.Type, Version: int(e.Version), OccurredAt: e.CreatedAt.Time.UTC(), Payload: e.Payload,
		}
		if e.TenantID.Valid {
			env.TenantID = e.TenantID.String()
		}
		if e.TraceID != nil {
			env.TraceID = *e.TraceID
		}
		msgs = append(msgs, broker.Message{Exchange: e.Exchange, RoutingKey: e.RoutingKey, Envelope: env})
		ids = append(ids, e.ID)
	}
	pub, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = r.broker.Publish(pub, msgs...)
	cancel()
	if err != nil {
		why := err.Error()
		// Tried again after a pause that grows with the attempts, up to
		// a minute.
		delay := min(float64(int(1)<<min(rows[0].Attempts, 6)), 60)
		if ferr := q.MarkOutboxFailed(ctx, db.MarkOutboxFailedParams{LastError: &why, DelaySeconds: delay, Ids: ids}); ferr != nil {
			return 0, ferr
		}
		if cerr := tx.Commit(ctx); cerr != nil {
			return 0, cerr
		}
		return 0, fmt.Errorf("publish events: %w", err)
	}
	if err := q.MarkOutboxSent(ctx, ids); err != nil {
		return 0, err
	}
	return len(rows), tx.Commit(ctx)
}

// lag sets the outbox's metrics.
func (r *relay) lag(ctx context.Context) {
	l, err := db.New(r.pool).OutboxLag(ctx)
	if err != nil {
		return
	}
	metrics.EventOutboxWaiting.Set(float64(l.Waiting))
	metrics.EventOutboxOldest.Set(l.OldestSeconds)
}
