package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// schedulerLockKey makes one scheduler the leader when the timed jobs go
// through RabbitMQ (WORKER_JOBS=queue). The application's other advisory
// locks: jobsLockKey, cmd/server's migrateLockKey (7_311_043) and the
// relay's relayLock. Each must differ, or a leader holding its lock for
// good would keep another process waiting.
const schedulerLockKey = 7_311_044

// JobNames are the timed jobs, in the order they run.
func JobNames() []string {
	names := make([]string, len(jobSchedule))
	for i, s := range jobSchedule {
		names[i] = s.Name
	}
	return names
}

// JobEvery is how often a job in jobSchedule runs.
func JobEvery(name string) (time.Duration, bool) {
	for _, s := range jobSchedule {
		if s.Name == name {
			return s.Every, true
		}
	}
	return 0, false
}

// RunJob runs one job in jobSchedule, as a worker does when the scheduler
// names it (jobs.scheduled). Each job holds a lock of its own while it
// runs, so a job is never run twice at once, while different jobs run on
// different workers. A job that fails is recorded for the health panel and
// runs again at its next turn: only a failure to run it at all is an error.
func (j *Jobs) RunJob(ctx context.Context, name string) error {
	fn, ok := j.jobs()[name]
	if !ok {
		j.log.Warn().Str("job", name).Msg("no such job")
		return nil
	}
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // releases the lock
	var got bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1, hashtext($2))", int32(jobsLockKey), name).Scan(&got); err != nil {
		return err
	}
	if !got {
		return nil // running elsewhere
	}
	if err := j.record(ctx, name, fn); err != nil && ctx.Err() == nil {
		j.log.Error().Err(err).Str("job", name).Msg("job")
	}
	return nil
}

// Schedule publishes the jobs as they fall due until ctx ends, for the
// workers to run (RunJob). Every scheduler runs it; the one holding the
// lock publishes. A job is due when it last started (job_runs) and was
// last published at least its interval ago, so a job still waiting in the
// queue isn't published again, and a scheduler that takes over carries on
// where the last one stopped.
func (j *Jobs) Schedule(ctx context.Context, publish func(ctx context.Context, name string) error) {
	for ctx.Err() == nil {
		err := j.lead(ctx, publish)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			j.log.Warn().Err(err).Msg("scheduler stopped")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

// lead holds the scheduler's lock and publishes what is due each minute.
// It returns nil when another scheduler holds the lock.
func (j *Jobs) lead(ctx context.Context, publish func(ctx context.Context, name string) error) error {
	pooled, err := j.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// The connection holds the lock: closing it lets go.
	conn := pooled.Hijack()
	defer conn.Close(context.Background()) //nolint:errcheck // closing
	var leader bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", int64(schedulerLockKey)).Scan(&leader); err != nil {
		return err
	}
	if !leader {
		return nil
	}
	j.log.Info().Msg("scheduler leading")
	published := map[string]time.Time{}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		// The lock lives as long as the connection: one that has gone
		// means another scheduler may lead.
		if err := conn.Ping(ctx); err != nil {
			return err
		}
		runs, err := j.q.ListJobRuns(ctx)
		if err != nil {
			return err
		}
		started := make(map[string]time.Time, len(runs))
		for _, r := range runs {
			started[r.Name] = r.StartedAt.Time
		}
		now := j.now()
		var errs []error
		for _, s := range jobSchedule {
			// A little early, so that a job started a moment after it was
			// published last minute is due again this minute.
			early := s.Every - 10*time.Second
			if now.Sub(started[s.Name]) < early || now.Sub(published[s.Name]) < early {
				continue
			}
			if err := publish(ctx, s.Name); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", s.Name, err))
				continue
			}
			published[s.Name] = now
		}
		if err := errors.Join(errs...); err != nil && ctx.Err() == nil {
			j.log.Warn().Err(err).Msg("publish jobs")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
