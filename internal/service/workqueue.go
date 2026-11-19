package service

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// The senders whose work can go through RabbitMQ.
const (
	WorkSMS      = "sms"
	WorkMail     = "mail"
	WorkWebhooks = "webhooks"
)

// WorkQueue hands the texts, email and webhooks queued in Postgres to the
// workers through RabbitMQ (docs/architecture.md, section 6): notify.sms,
// notify.email and webhook.deliver. The rows stay the record, with their
// leases and retry schedules; a message names the rows that are due, and
// a worker claims them by ID. A message for a row already sent, held by
// another worker or not yet due changes nothing, so repeats are harmless.
type WorkQueue interface {
	// Has says whether a sender's work goes through RabbitMQ; the others
	// work in process (WORKER_SMS, WORKER_MAIL, WORKER_WEBHOOKS).
	Has(kind string) bool
	// Publish names rows due to be sent.
	Publish(ctx context.Context, kind string, ids []pgtype.UUID) error
	// Ready is how many messages wait in a sender's queue, not yet taken.
	Ready(ctx context.Context, kind string) (int, error)
}

// queued says whether a sender's work goes through RabbitMQ.
func (c *core) queued(kind string) bool { return c.work != nil && c.work.Has(kind) }

// handOff passes rows written in a transaction to their sender once it
// commits: to RabbitMQ when the sender works from it, or by waking the
// sender. A publish that fails wakes the sweeper instead, which publishes
// what is due. With no transaction (after nil), it hands them off now.
func (c *core) handOff(after func(func()), kind string, ids []pgtype.UUID, kick func()) {
	do := func(f func()) { f() }
	if after != nil {
		do = after
	}
	do(func() {
		if c.queued(kind) && len(ids) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := c.work.Publish(ctx, kind, ids)
			if err == nil {
				return
			}
			c.log.Warn().Err(err).Str("sender", kind).Msg("publish queued work; the sweeper will")
		}
		kick()
	})
}

// sweepBatch is the most rows a sweep publishes at once.
const sweepBatch = 500

// sweep stands in for a sender's loop when its work goes through
// RabbitMQ: whenever the queue has nothing waiting, it publishes the rows
// that are due, which are retries whose time has come and any whose
// message was lost. Woken by kick (a paused webhook resumed, a publish
// that failed), or every interval.
func (c *core) sweep(ctx context.Context, kind string, wake <-chan struct{}, every time.Duration, due func(context.Context, int32) ([]pgtype.UUID, error)) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		if n, err := c.work.Ready(ctx, kind); err != nil {
			if ctx.Err() == nil {
				c.log.Warn().Err(err).Str("sender", kind).Msg("sweep: queue depth")
			}
		} else if n == 0 {
			ids, err := due(ctx, sweepBatch)
			if err == nil && len(ids) > 0 {
				err = c.work.Publish(ctx, kind, ids)
			}
			if err != nil && ctx.Err() == nil {
				c.log.Warn().Err(err).Str("sender", kind).Msg("sweep")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-wake:
		}
	}
}

// DeliverQueued sends the texts a notify.sms message names that are
// still due. Told to stop, it finishes the text in hand and hands back the
// rest, as drain does, and returns ctx's error so the message is left for
// another worker.
func (s *SMSService) DeliverQueued(ctx context.Context, ids []pgtype.UUID) error {
	batch, err := s.q.ClaimSMSByIDs(context.WithoutCancel(ctx), ids)
	if err != nil {
		return err
	}
	for i, m := range batch {
		if ctx.Err() != nil {
			s.release(batch[i:])
			return ctx.Err()
		}
		s.deliver(ctx, m)
	}
	return nil
}

// DeliverQueued sends the email a notify.email message names, stopping as
// SMSService.DeliverQueued does.
func (s *MailService) DeliverQueued(ctx context.Context, ids []pgtype.UUID) error {
	batch, err := s.q.ClaimEmailsByIDs(context.WithoutCancel(ctx), ids)
	if err != nil {
		return err
	}
	for i, m := range batch {
		if ctx.Err() != nil {
			s.release(batch[i:])
			return ctx.Err()
		}
		s.deliver(ctx, m)
	}
	return nil
}

// DeliverQueued sends the webhooks a webhook.deliver message names,
// webhookSenders at a time, stopping as SMSService.DeliverQueued does.
func (s *WebhookService) DeliverQueued(ctx context.Context, ids []pgtype.UUID) error {
	batch, err := s.q.ClaimWebhooksByIDs(context.WithoutCancel(ctx), ids)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, webhookSenders)
	for i, m := range batch {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			wg.Wait()
			s.release(batch[i:])
			return ctx.Err()
		}
		wg.Go(func() {
			defer func() { <-slots }()
			s.deliver(ctx, m)
		})
	}
	wg.Wait()
	return nil
}
