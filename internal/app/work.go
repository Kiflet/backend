package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/kiflet/backend/internal/service"
)

// workListener wakes this worker's senders when a process without them
// queues a text, an email or a webhook (service.WorkChannel). Missed
// wake-ups cost only time: each sender also looks every few seconds.
type workListener struct {
	pool *pgxpool.Pool
	svc  *service.Services
	log  zerolog.Logger
}

func (w *workListener) Run(ctx context.Context) {
	delay := time.Second
	for ctx.Err() == nil {
		err := w.listen(ctx)
		if ctx.Err() != nil {
			return
		}
		w.log.Warn().Err(err).Dur("retry_in", delay).Msg("work wake-ups: listener lost")
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func (w *workListener) listen(ctx context.Context) error {
	pooled, err := w.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	conn := pooled.Hijack()
	defer conn.Close(context.Background()) //nolint:errcheck // closing
	if _, err := conn.Exec(ctx, "LISTEN "+service.WorkChannel); err != nil {
		return err
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		w.svc.Wake(n.Payload)
	}
}
