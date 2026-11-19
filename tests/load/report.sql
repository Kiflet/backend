-- How long the load test's positions waited before the tracking worker
-- wrote them (make test-load runs this afterwards). For a live phone's
-- position, sent as it was taken, that is its time in tracking.ingest and
-- on the way there; the target is under two minutes for every one.
-- Positions sent after a reconnect were taken hours before, so they are
-- left out.
SELECT count(*) AS live_positions,
       percentile_disc(0.5) WITHIN GROUP (ORDER BY synced_at - recorded_at) AS median_wait,
       percentile_disc(0.99) WITHIN GROUP (ORDER BY synced_at - recorded_at) AS p99_wait,
       max(synced_at - recorded_at) AS longest_wait,
       CASE WHEN max(synced_at - recorded_at) < INTERVAL '2 minutes' THEN 'pass' ELSE 'FAIL' END AS target
FROM tracking_events
WHERE device_id LIKE 'load-%' AND client_id LIKE 'live-%'
  AND recorded_at > NOW() - INTERVAL '1 day';

SELECT count(*) AS reconnect_positions
FROM tracking_events
WHERE device_id LIKE 'load-%' AND client_id LIKE 'back-%'
  AND synced_at > NOW() - INTERVAL '1 day';
