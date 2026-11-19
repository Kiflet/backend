package broker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// liveTTL is how long a live update waits for a dashboard's instance to
// take it: a page that missed one catches up at its next fetch.
const liveTTL = 10 * time.Second

// PublishLive sends a live update for a company's dashboards to every
// instance. It is best-effort: no confirm is waited for.
func (b *Broker) PublishLive(ctx context.Context, tenantID string, body []byte) error {
	ch, err := b.channel()
	if err != nil {
		return err
	}
	// On a confirm channel every publish is confirmed; this one's confirm
	// is not waited for.
	_, err = ch.PublishWithDeferredConfirmWithContext(ctx, Live, "live.tenant."+tenantID, false, false, amqp.Publishing{
		ContentType: "application/json", Body: body, Expiration: fmt.Sprint(liveTTL.Milliseconds()),
	})
	return err
}

// SubscribeLive hands every live update to fn until ctx ends, through a
// queue of this instance's own that goes when the instance does. Updates
// sent while it is reconnecting are missed.
func (b *Broker) SubscribeLive(ctx context.Context, fn func(tenantID string, body []byte)) {
	delay := time.Second
	for ctx.Err() == nil {
		err := b.subscribeLive(ctx, fn)
		if ctx.Err() != nil {
			return
		}
		b.log.Warn().Err(err).Dur("retry_in", delay).Msg("rabbitmq: live updates lost")
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func (b *Broker) subscribeLive(ctx context.Context, fn func(tenantID string, body []byte)) error {
	conn, err := amqp.DialConfig(b.url, amqp.Config{Heartbeat: 10 * time.Second, Properties: amqp.Table{"connection_name": "kiflet live"}})
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
	q, err := ch.QueueDeclare("", false, true, true, false, amqp.Table{
		"x-message-ttl": liveTTL.Milliseconds(),
		// A slow instance drops the oldest rather than piling them up.
		"x-max-length": 10_000,
	})
	if err != nil {
		return err
	}
	if err := ch.QueueBind(q.Name, "live.tenant.*", Live, false, nil); err != nil {
		return err
	}
	deliveries, err := ch.ConsumeWithContext(ctx, q.Name, "", true, true, false, false, nil)
	if err != nil {
		return err
	}
	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-closed:
			return fmt.Errorf("connection closed: %v", e)
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("deliveries closed")
			}
			if tenant, found := strings.CutPrefix(d.RoutingKey, "live.tenant."); found {
				fn(tenant, d.Body)
			}
		}
	}
}
