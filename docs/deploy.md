# Deploying Kiflet

One machine with Docker runs it: the app, PostGIS, and Caddy, which gets a
TLS certificate for `DOMAIN` and serves HTTP/2 and HTTP/3.

```bash
cp .env.example .env    # set DOMAIN, DB_PASSWORD, SMS_* and SMTP_* settings, BILLING_BANK_DETAILS, SUPPORT_*
docker compose -f deployments/docker-compose.prod.yml --env-file .env up -d --build --wait
```

In production the app refuses to start without an `https://` address (the
compose file sets `PUBLIC_URL` from `DOMAIN`), with a stand-in provider or
`SANDBOX_URL` unless `ALLOW_MOCK_PROVIDERS=true`, or with a provider whose
settings or key files are missing, and says which. The database password is
passed to the app on its own, not in a URL, so it may contain any character.
Provider keys, such as telebirr's key files, go in `deployments/secrets/`,
which the app sees at `/run/secrets` (see the README there). Signatures and
photos go to the `app-data` volume, or to Garage beside the app (below,
"Files in Garage"), or to another S3-compatible store with
`STORAGE_TYPE=s3`.

Each container has a read-only root filesystem where it can, no Linux
capabilities beyond what it needs and no privilege escalation, and its logs
rotate. When the app is stopped or replaced, it stops taking requests and
gives those in progress, and the text or email being sent, 30 seconds to
finish. Messages it hadn't started are left for the next start to send at
once.

`APP_REPLICAS=2` runs two instances of the app, so that one carries on
while the other restarts or is busy; staging runs two. Caddy finds them by
name and sends each client address to the same one, so the rate limits,
which each instance keeps in memory, still hold per address. An API key's
allowance is per instance, so a key used from several addresses may get up
to that many times 300 requests a minute. Live updates pass between the
instances through Postgres (`LISTEN`/`NOTIFY`), so a dashboard sees every
change whichever instance made it. Instances starting together take turns
at the migrations, and the background jobs take a Postgres advisory lock,
so they never run twice. Each instance holds up to 20 database
connections. `docker compose up -d` still replaces every instance at once,
so a deploy has the same short gap as with one.

Add `--profile monitoring` for Prometheus and Grafana: set `METRICS_TOKEN`, and
put the same value in `deployments/prometheus/metrics_token`. Grafana listens
on `127.0.0.1:3000` (reach it over an SSH tunnel) and comes with a **Kiflet**
dashboard: requests, errors and response times by route, driver sync, texts
and emails sent and waiting, background jobs, and memory. `/metrics` labels
requests by route pattern (`/app/shipments/{id}`), never by company or ID.

The maps' satellite view uses Esri World Imagery, which needs an ArcGIS
licence for commercial use. Point `MAP_SATELLITE_URL` at your own imagery
provider, or set it to `off`.

Point your SMS provider's webhooks at `https://DOMAIN/api/v1/sms/inbound`
(texts from phones) and `https://DOMAIN/api/v1/sms/status` (delivery
reports), with `SMS_INBOUND_SECRET` in an `X-Webhook-Secret` header or a
`secret` parameter.

Health checks: `/healthz` (process up) and `/readyz` (database reachable).

## Backups

```bash
scripts/backup.sh
scripts/fetch-backup.sh [<time>]
scripts/restore.sh <kiflet_….dump[.age]> [<kiflet_…_files.tar.gz[.age]>]
```

`backup.sh` writes three files to `/var/backups/kiflet` (`BACKUP_DIR`),
readable by their owner only:

- the database (`pg_dump`);
- the app's volume: photos and signatures kept on disk, and the app secret
  that sessions and codes depend on;
- the machine's settings: `.env`, `backup.env` and `deployments/secrets/`.

It deletes those older than two weeks (`KEEP_DAYS`). Run it nightly:

```
MAILTO=you@example.et
15 2 * * *  cd /srv/kiflet && { scripts/backup.sh >>/var/log/kiflet-backup.log 2>&1 || tail -n 20 /var/log/kiflet-backup.log; }
```

It exits with an error when any step fails, the copy off the machine
included. Cron emails `MAILTO` only what a job prints, so the line above
prints the end of the log when it fails. With `STORAGE_TYPE=s3` the
app's bucket is backed up too, every object of it, as a fourth archive
(`kiflet_<time>_objects.tar.gz`): Garage keeps no versions, so this is
the only copy of a file deleted by mistake.

### Off the machine, encrypted

A backup on the machine it protects is lost with it. With `backup.env`,
each backup is encrypted with [age](https://age-encryption.org) as it is
written and copied to an S3-compatible bucket:

1. Install age on the server: `apt install age`.
2. On your own computer, not the server, make a key pair with
   `age-keygen -o kiflet-backup.key`. It prints the public key (`age1…`).
   Keep the private key where a lost server can't take it: in a password
   manager, and on a USB stick in a safe. Make a second pair for a second
   person, so that one lost key loses nothing.
3. Make a private bucket with a provider other than the server's (Backblaze
   B2, Cloudflare R2, Wasabi and AWS all work), with versioning or object
   lock if it offers them, and keys that can list, read and write it.
4. Copy `backup.env.example` to `backup.env`, beside `.env`, and fill in the
   public keys, the bucket and its keys. It is kept out of `.env` so the app
   never sees them.

Nothing leaves the machine unencrypted: with a bucket and no key,
`backup.sh` stops before it starts. The server holds only the public keys,
so neither the bucket's provider nor anyone with the bucket's keys can read
a backup. Without a private key nobody can, you included: keep the keys as
carefully as the data. The bucket keeps backups 90 days
(`BACKUP_S3_KEEP_DAYS`; 0 leaves it to the bucket's own rules). The copying
is rclone in a container (`deployments/docker-compose.backup.yml`), so the
server needs only Docker and age.

### Restoring

`restore.sh` asks for the stack's name, then stops the app, replaces the
database (and the files, if given) with the backup's, and starts it again.
An encrypted backup needs `AGE_IDENTITY`, the private key's file. Each file
is decrypted first, so the wrong key or a damaged copy changes nothing.

On a new machine, the old one gone:

1. Install Docker and age, clone the repository at the release that was
   running (or a newer one: the app brings an older database up to date),
   and copy a private key onto the machine.
2. Write `backup.env` with the bucket's settings, and fetch the newest
   backup with `scripts/fetch-backup.sh`. `scripts/fetch-backup.sh
   20261104_021500` fetches an older one. It doesn't need `.env`, which is
   in the backup.
3. Unpack the settings into the checkout:
   `age -d -i kiflet-backup.key /var/backups/kiflet/kiflet_<time>_settings.tar.gz.age | tar -xzf -`
4. Start the stack with the `docker compose` command at the top of this
   page, then restore:
   `AGE_IDENTITY=kiflet-backup.key scripts/restore.sh` with the `.dump.age`
   and `_files.tar.gz.age` files that `fetch-backup.sh` printed.
5. Point the domain at the new machine, and delete the private key from it.

### To the minute

A nightly backup loses the day's work since. With `WAL_ARCHIVE=on` in
`.env`, Postgres keeps every change as it is made (its WAL), so the database
can be restored to any moment: just before a mistake, or to the last few
minutes before the machine was lost.

1. Set `WAL_ARCHIVE=on` and run `docker compose up -d` (the database
   restarts). Postgres writes each closed segment of WAL, compressed, to the
   `wal-archive` volume, at least every five minutes while there are
   changes (`WAL_ARCHIVE_TIMEOUT`, in seconds).
2. `backup.sh` now also writes `kiflet_<time>_base.tar.gz`: the database's
   files as they are, which the WAL brings forward.
3. Ship the WAL every five minutes:

   ```
   */5 * * * *  cd /srv/kiflet && { scripts/ship-wal.sh >>/var/log/kiflet-wal.log 2>&1 || tail -n 20 /var/log/kiflet-wal.log; }
   ```

   It encrypts each file to the same keys, keeps it in
   `/var/backups/kiflet/wal`, copies it to the bucket under `wal/`, and
   only then removes it from the volume. WAL is kept two weeks, here
   (`KEEP_DAYS`) and in the bucket (`BACKUP_WAL_KEEP_DAYS`): that is how
   far back a restore to the minute reaches. Before that, the nightly
   dumps remain.

If shipping stops, the WAL piles up in the volume and fills the disk.
Grafana's disk alert measures the app's volume, which is on the same disk
as the others in Docker's usual set-up, and cron emails `MAILTO` when
`ship-wal.sh` fails.

To restore, pick the newest base backup from before the moment, and a time
with its offset:

```bash
AGE_IDENTITY=kiflet-backup.key scripts/restore-pitr.sh \
  /var/backups/kiflet/kiflet_20261117_021500_base.tar.gz.age '2026-11-17 14:04:00+03'
```

It decrypts what it needs first, asks for the stack's name, stops the app,
replaces the database with the base backup, and replays the WAL (from
`/var/backups/kiflet/wal` and whatever the volume holds not yet shipped) up
to that moment. Without a time it replays all there is. The app's files
aren't touched. Afterwards the database is on a new timeline: run
`backup.sh` at once, so the next restore has a base. On a new machine,
`fetch-backup.sh <time>` brings back the base backup and the bucket's WAL
along with the rest.

### The drill

A backup nobody has restored may not restore. Once a quarter, and whenever
`backup.env` changes, follow the steps above on a spare machine, with a
private key from where it is kept, and check that the newest data is there.
`make test-e2e` runs the same round trip on staging for every change: an
encrypted backup copied to a bucket, the copies on the machine lost, and
the backup fetched and restored, and a restore to a moment between two
sign-ups from a base backup and WAL fetched from the bucket.

## Files in Garage

[Garage](https://garagehq.deuxfleurs.fr/) is the store for signatures,
photos and documents (`docs/architecture.md`, section 5). It runs beside
the app as one node, with the `garage` profile:

1. In `.env`, the block under "Files" in `.env.example`:
   `COMPOSE_PROFILES=garage`, two secrets for Garage, and the app's `S3_*`
   settings with a key in Garage's format (`GK` and 24 hexadecimal
   characters; a secret of 64).
2. `docker compose … up -d --wait`. Garage starts, then `garage-setup`
   (`kiflet setup-storage`) makes its layout, imports the app's key, makes
   the bucket, allows `PUBLIC_URL` to use presigned URLs, and has
   unfinished uploads cleaned up after a day. It runs at every deploy and
   changes only what is missing; the app starts once it has finished.

Garage keeps one copy of each file on this machine, and no older versions:
the backups (above) hold the bucket, and take it off the machine. Its
admin API, on port 3903, is reachable only from the compose network; with
the monitoring profile, Prometheus reads its figures there with
`METRICS_TOKEN`.

A machine that kept its files in the `app-data` volume moves them across
before the app is switched, with the app stopped so nothing is written
meanwhile: stop the app, start `garage` and `garage-setup` with the new
`.env`, copy the volume's `uploads` folder into the bucket with rclone
(`rclone copy --checksum`, from a container on the stack's network with
the volume mounted read-only and the app's keys), then start everything.
The keys are the same in both, so the files are found where they were.
The files stay in the volume, which is the way back (`STORAGE_TYPE=local`
again); remove them once Garage has run for a month.

## RabbitMQ

[RabbitMQ](https://www.rabbitmq.com/) (`docs/architecture.md`, section 6)
runs beside the app with the `rabbitmq` profile. With it:

- GPS fixes from phones wait in the `tracking.ingest` queue for the
  tracking workers instead of being written while the phone waits. A
  phone is told its fixes are stored once RabbitMQ holds them on disk.
- Live updates cross instances on RabbitMQ instead of Postgres's
  `NOTIFY`.
- Every event in a shipment's history is written to `event_outbox` with
  the change, and published to the `kiflet.events` exchange once it
  commits, for the consumers to come.
- The timed jobs are named on `jobs.scheduled` by the leading scheduler
  and run by the workers, each job on whichever worker takes it, so a long
  retention run doesn't hold up the alert checks. A job not taken before
  its next turn is dropped rather than run twice in a row.
- Texts, email and webhooks reach the workers through `notify.sms`,
  `notify.email` and `webhook.deliver`. Each is still written to its
  table first, which keeps its record, its lease and its retry schedule:
  a message names the rows to send, and a worker claims them by ID, so a
  message seen twice sends nothing twice. Each worker's sweeper publishes
  what is due whenever a queue is empty: retries whose time has come, and
  anything whose message was lost. A worker holds at most five messages of
  twenty rows, which caps how hard the workers press on the SMS gateway.

To turn it on, in `.env`:

1. `COMPOSE_PROFILES=rabbitmq` (`garage,rabbitmq` with Garage).
2. `RABBITMQ_PASSWORD`, letters and digits only (`openssl rand -hex 24`),
   and `RABBITMQ_URL=amqp://kiflet:<the password>@rabbitmq:5672/`.
3. `docker compose … up -d --wait`.

Each part can be switched back to the old way on its own, with
`WORKER_TRACKING`, `WORKER_LIVE`, `WORKER_SMS`, `WORKER_MAIL`,
`WORKER_WEBHOOKS` or `WORKER_JOBS` set to `inprocess`. Without
`RABBITMQ_URL` the app works as it did, and if RabbitMQ can't take a
phone's fixes, the app writes them itself, so a phone never waits on it.

A message whose handler keeps failing is tried again after a delay, and
after its last attempt it is parked in its queue's `.dead` queue (for
example, `tracking.ingest.dead`) and logged. With the monitoring profile,
`kiflet_broker_consumed_total{result="parked"}` counts them, and
`kiflet_event_outbox_oldest_seconds` shows how far the outbox lags, and
RabbitMQ's own figures (queue depths, consumers) are scraped from it.

RabbitMQ keeps its queues in the `rabbitmq-data` volume. The backups
leave it out: what is in it is either in the database already or on its
way there.

## Separate processes

By default each instance of the app does everything. The same program can
run only some of it, `kiflet serve --roles=…` (or `ROLES`):

- `web`: the pages, the APIs, and live updates to the dashboards open on it
- `worker`: texts, email and webhooks, and GPS fixes from RabbitMQ
- `scheduler`: the timed jobs. Without RabbitMQ one scheduler runs them
  all at a time; with it, one scheduler (holding a lock) names each job
  on `jobs.scheduled` as it falls due and the workers run them
  (`WORKER_JOBS`)
- `relay`: publishing the event outbox to RabbitMQ (one leads)

To give the background work processes of its own, so that a burst of
texts or positions doesn't slow the pages and the pages can be scaled on
their own, add `workers` to `COMPOSE_PROFILES` and set `APP_ROLES=web`
(and `WORKER_REPLICAS`, 1 by default). The `worker` service runs
`--roles=worker,scheduler,relay`. Set both or neither: web processes alone
queue texts that nothing sends. Work a web process queues wakes the
workers through Postgres at once; missed, they find it within seconds.
A process without the web role answers only `/healthz`, `/readyz` and
`/metrics`. Staging runs this way.

## Redis

Redis (`docs/architecture.md`, section 7) runs beside the app with the
`redis` profile. For now it holds the request limits (sign-in, codes,
lookups, each API key), counted for all the instances together rather
than by each one. In `.env`:

1. Add `redis` to `COMPOSE_PROFILES` (`garage,rabbitmq,redis`).
2. `REDIS_PASSWORD`, letters and digits only (`openssl rand -hex 24`), and
   `REDIS_URL=redis://:<the password>@redis:6379/0`.
3. `docker compose … up -d --wait`.

Nothing in it needs keeping: lost, the limits start counting again. If
Redis doesn't answer within 100 ms, each instance counts on its own until
it does, which `kiflet_limits_local` shows. Redis logs a warning at start
about the host's memory overcommit setting; its file is only a
convenience here, so the warning can be left.

## Upgrading PostgreSQL

The compose file runs PostgreSQL 17 (PostGIS 3.6), with its data in the
`postgres17-data` volume. A machine that ran PostgreSQL 15 has its data in
`postgres-data`, which 17 can't open. It moves across as a dump: the app is
down for as long as the restore takes (minutes before the first customer).

1. While the old version still runs, before pulling the new compose file,
   take a backup: `scripts/backup.sh`. Its `.dump` is all that's needed.
2. Pull the new version and start only the database, which comes up empty
   on its new volume: `docker compose -f deployments/docker-compose.prod.yml
   --env-file .env up -d postgres`.
3. Restore the dump into it: `scripts/restore.sh
   /var/backups/kiflet/kiflet_<time>.dump` (with `AGE_IDENTITY` if it is
   encrypted). The files archive isn't needed: the app's volume is
   unchanged.
4. Start everything: `docker compose … up -d --wait`. The app applies the
   newer migrations on start.
5. With `WAL_ARCHIVE=on`, take a new base backup at once
   (`scripts/backup.sh`): base backups and WAL from PostgreSQL 15 can't
   bring a 17 database forward. Keep the old ones until their 90 days are
   out, for a restore onto a 15 server if one is ever needed.

The old volume stays until it is removed by hand (`docker volume rm
kiflet_postgres-data`), which is the way back: the old compose file and
image open it as it was at step 1. Remove it once the new version has run
for a week.

## Staging

Production's compose file and images, with `deployments/staging/compose.yml`
standing in for the outside world: the provider sandbox for Ethio telecom,
telebirr and CBE Birr (the app runs its real adapters, and the sandbox calls
back over HTTPS through Caddy), Mailpit for SMTP, and Caddy's own
certificate authority. Garage is production's, with a second bucket that
stands in for the backups' provider. Prometheus and Grafana are on.

```bash
make staging-up     # build, start, and copy Caddy's root certificate to tmp/staging-ca.crt
make test-e2e       # the end-to-end tests, against staging
make staging-down   # remove staging and its data
```

| | |
|---|---|
| The app | https://kiflet.localhost:8443 |
| The sandbox: phones, wallets and faults | https://sandbox.kiflet.localhost:8443 |
| Email sent (Mailpit) | https://mail.kiflet.localhost:8443 |
| Grafana, as `admin` / `staging-grafana-password` | https://grafana.kiflet.localhost:8443 |

It listens on 127.0.0.1 only, and `*.localhost` names reach this machine
without DNS. Trust `tmp/staging-ca.crt` in the browser, or give it to curl
with `--cacert`. `make staging-logs` follows the logs.

The end-to-end tests (`tests/e2e/`) reach staging as browsers and the
providers do. A company signs up with the code from the sandbox's phone and
the link from Mailpit. A driver works a load in the driver app, with its
photo stored in S3 and the dashboard told over a WebSocket. Invoices are
paid with telebirr and CBE Birr. The tests also check the HTTPS edge (the
redirect, the headers, and that a client can't claim another address) and
the figures in Prometheus and Grafana, and last, take a backup and restore
it.

One test runs the driver app itself in headless Chrome, the size of a
phone: signing in and choosing a PIN, starting a trip, then, with the
signal gone, a delivery form that survives the app being loaded again and
a delivery that is sent once the signal is back. It finds Chrome or
Chromium on the `PATH`, or the program named in `E2E_CHROME`, and is
skipped without one, except in CI.

## Connecting the real providers

The adapters follow what each provider publishes, and the sandbox plays the
same contract. What to settle when the credentials arrive:

| Provider | Contract | Settle at onboarding |
|---|---|---|
| telebirr | H5 C2B web checkout as published: token, preOrder, checkout, notify, queryOrder | the signature (RSA-PSS with SHA-256 is assumed, as in the maintained SDKs), the `order_status` values queryOrder returns (one table in `pkg/payments/telebirr.go`), and whether notifications are retried |
| CBE Birr | none published: pay by reference, with query, validation and confirmation callbacks behind Basic authentication, as in `pkg/payments/cbebirr.go` | the field names, result codes and login, or adapt that file to CBE's |
| Ethio telecom | none published: token, send, delivery reports and inbound texts, as in `pkg/sms/ethiotelecom.go` | the whole contract |

Then:

1. Set the providers' settings (see `.env.example`): telebirr's API and
   checkout URLs, app and merchant IDs, app secret and the two key files;
   CBE Birr's merchant ID and callback login; Ethio telecom's API URL, key
   and sender ID. Unset `SANDBOX_URL`.
2. Give the providers the app's addresses: telebirr is sent
   `https://DOMAIN/api/v1/payments/telebirr/notify` with each order; CBE
   Birr calls `https://DOMAIN/api/v1/payments/cbebirr/query`, `/validation`
   and `/confirmation`; Ethio telecom posts to the SMS webhooks above.
3. Where a provider's contract differs from ours, change the adapter and the
   sandbox's side (`internal/sandbox/sms.go`, `telebirr.go`, `cbebirr.go`)
   together, and run `make test-integration`: its sandbox tests run the real
   adapters against the sandbox.
4. Pay a small invoice with each provider, and send a text to a driver's
   phone, before opening sign-ups.
