-- name: AddFeedEntries :exec
-- A driver's new entries, numbered after the driver's head, which this
-- raises: the head's row stays locked until the transaction commits, so
-- the driver's entries become visible in the order of their numbers.
WITH head AS (
    INSERT INTO driver_feed_heads (driver_id, tenant_id, seq)
    VALUES (sqlc.arg(driver_id), sqlc.arg(tenant_id), cardinality(sqlc.arg(kinds)::text[]))
    ON CONFLICT (driver_id) DO UPDATE
    SET seq = driver_feed_heads.seq + EXCLUDED.seq
    WHERE driver_feed_heads.tenant_id = EXCLUDED.tenant_id
    RETURNING seq
)
INSERT INTO driver_feed (driver_id, seq, tenant_id, kind, ref_id, op, reason)
SELECT sqlc.arg(driver_id), head.seq - cardinality(sqlc.arg(kinds)::text[]) + i,
       sqlc.arg(tenant_id), (sqlc.arg(kinds)::text[])[i], (sqlc.arg(ref_ids)::uuid[])[i],
       (sqlc.arg(ops)::text[])[i], NULLIF((sqlc.arg(reasons)::text[])[i], '')
FROM head, generate_series(1, cardinality(sqlc.arg(kinds)::text[])) AS i;

-- name: GetFeedHead :one
-- A driver's last number, and the last the retention job deleted.
SELECT seq, pruned_through FROM driver_feed_heads
WHERE driver_id = sqlc.arg(driver_id) AND tenant_id = sqlc.arg(tenant_id);

-- name: ListFeedEntries :many
-- A driver's entries after a number, in order.
SELECT seq, kind, ref_id, op, COALESCE(reason, '')::text AS reason
FROM driver_feed
WHERE driver_id = sqlc.arg(driver_id) AND tenant_id = sqlc.arg(tenant_id) AND seq > sqlc.arg(after)
ORDER BY seq
LIMIT sqlc.arg(max_entries);

-- name: PruneFeed :execrows
-- tenant: none, retention of every company's feed at once.
-- Entries older than the feed keeps, and on each head the last number
-- deleted, so that a cursor before it gets a full download.
WITH gone AS (
    DELETE FROM driver_feed WHERE created_at < sqlc.arg(before)
    RETURNING driver_id, seq
)
UPDATE driver_feed_heads h
SET pruned_through = g.last
FROM (SELECT driver_id, max(seq) AS last FROM gone GROUP BY driver_id) g
WHERE h.driver_id = g.driver_id AND h.pruned_through < g.last;
