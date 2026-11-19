-- +goose Up
-- Which timed job is running where, when the jobs go through RabbitMQ
-- (WORKER_JOBS=queue): a worker takes a job's lease before running it, so
-- a job never runs twice at once, while different jobs run side by side.
-- A lease, not a lock held on a connection: a job running needs
-- connections of its own, and a pool whose connections all held locks
-- would leave every job waiting. A worker that dies mid-job leaves its
-- lease to run out. The platform's own table: no tenant.
CREATE TABLE IF NOT EXISTS job_leases (
    name text PRIMARY KEY,
    until timestamptz NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS job_leases;
