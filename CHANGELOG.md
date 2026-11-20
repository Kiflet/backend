# Changelog

What changed in each release, newest first.

## Unreleased

### Moved

- **The platform packages** (`docs/migration.md`, phase 1, step 1):
  `internal/config`, `geo`, `phone` and `text` are now under
  `internal/platform/`, the packages without business rules that the
  modules will share. Nothing else changes.
- **The audit module** (phase 1, step 2): `internal/modules/audit` owns
  `audit_logs`, with its own sqlc store, and the rest of the code reaches
  it through its interface. Its rows are still written in the transaction
  of the change they record. The platform's audit page looks up company
  and user names instead of joining their tables. A test checks what
  each module imports, and CI checks that no generated file outlives its
  queries.
- **The tenancy module**: `internal/modules/tenancy` owns `tenants`, the
  companies, with the settings rules in its `domain` and their own unit
  tests. Every read of a company goes through it; a missing company is now
  "not found" rather than an internal error. The errors a person can act
  on moved to `internal/platform/apperror`, shared by the modules.
- **The identity module, its sessions**: `internal/modules/identity` owns
  `sessions`, and holds the roles, the `Principal` a request runs as and
  the `Actor` recorded with a change. Signing in, the session lookup on
  every request, signing out and ending sessions go through it; the old
  names stay as aliases until their callers move. Then its one-time codes,
  email links and waiting sign-ups: `verification_codes`, `email_tokens`
  and `pending_signups` are identity's. Last, `users`: every lookup,
  account change and sign-in record goes through the module, and a person
  who isn't there is now "not found" rather than an internal error. The
  drivers' profile columns of `users` wait for the fleet module.
- **The fleet module**: `internal/modules/fleet` owns `vehicles` and the
  drivers' profile (their vehicle, licence and app settings). The vehicle
  form's rules and the paper states are in its `domain`, with unit tests.
  The vehicles and drivers pages, which gather from several modules, stay
  as read models until reports moves.
- **The shipments module, its tables and rules**: `internal/modules/shipments`
  owns `shipments` and their history (`shipment_events`). The statuses,
  who may move a shipment between them, and the reasons given are in its
  `domain`. Booking, assigning and status changes follow; the lists, the
  map and the overdue checks stay as read models. Then documents and
  charges: `shipment_documents` and `shipment_charges` are the module's,
  with the checks on a file and a charge and the limits per shipment;
  storing the files stays with the service.
- **The events bus** (phase 1, step 4): `internal/platform/events`. A
  change publishes an event in its transaction, and the modules that
  react subscribe to its type, within the transaction (what must commit
  with the change) or after it commits. The bus writes the outbox, and
  webhooks are its first subscriber. Live updates followed: dashboards
  hear of every event in a shipment's history once it has committed,
  notes and the customer's answers included. Then the texts: what
  customers and drivers are sent when a load is booked, given to a driver,
  moves, changes destination or is cancelled is decided by a subscriber
  of the shipment's events, in the same transaction, instead of beside
  each change. A test now covers the cancellation texts. Then alerts:
  those a shipment's events raise or clear (problems and delays
  reported, deliveries, proof held or recorded away from the
  destination, a customer's complaint, a driver's message) come from a
  subscriber too. A new test covers the four that had none. Finally the
  shipments module records each event in a shipment's history and
  publishes it (`shipments.History.Record`, the event `shipments.Changed`),
  so booking and status changes can move into it next without knowing
  who reacts. `internal/app` makes the one bus and hands it to the module
  and the services.
- **To the Kiflet organization.** This repository is now
  [Kiflet/backend](https://github.com/Kiflet/backend), moved with its whole
  history and the `v0.9.0` tag from `Natnael-Alemayehu/kiflet`, beside
  [Kiflet/web](https://github.com/Kiflet/web) and
  [Kiflet/mobile](https://github.com/Kiflet/mobile). The Go module is
  `github.com/kiflet/backend` (it was `github.com/natnael-alemayehu/kiflet`):
  code that imports it changes its imports. `LICENSE` points at this
  repository's issues.

### Fixed

- **`make clients-up` runs again over its data.** Its seed step refused a
  database it had already seeded, so the app never started the second
  time. `server seed --if-empty` leaves loaded data alone, and the local
  stack's compose file uses it.
- **The cross-origin check covers the API** (audit A2). Every `/api/`
  path was exempt from it. Now only requests with an `Authorization`
  header (an API key or a driver's token, which a page on another site
  can't send by itself) and the providers' calls to `/api/callbacks` are,
  so a cookie that reaches `/api/v1` can't be used from another site.
- **Billing's live updates reach only admins** (`docs/migration.md`,
  phase 1, step 10; audit P1). A payment's or an invoice's live update
  went to every open dashboard of the company, dispatchers' included. A
  live message now names the roles that may receive it, and the WebSocket
  hub sends it to no one else, whichever way it crossed instances
  (in-process, Postgres or RabbitMQ).
- **A driver's load limit holds when loads are given at once** (audit D3).
  Two assignments to the same driver at the same moment each counted the
  driver's loads before either was saved, so both could pass the
  company's limit. The assignment now holds the driver's row while it
  counts (`FOR NO KEY UPDATE`, which doesn't hold up rows that refer to
  the driver), and the second waits for the first.
- **Delivery reports that come early are no longer lost.** A gateway may
  report on a text before the sender has saved its message ID; the report
  matched nothing and was dropped, so the text never showed delivered or
  undelivered. It is now kept (`sms_early_reports`, a day) and applied
  when the text is saved. This made `TestGatewayFaults` fail now and then
  on RabbitMQ (audit T5).
- **Texts refused together sign in once.** When Ethio telecom's gateway
  ends its token early, every text in flight was refused and each asked
  for a new token; now the first does and the others use it.
- **Local files can't leave their folder.** The local store (development
  and tests) opens every file through `os.Root`, so no name or link can
  reach outside `uploads/`; its folders are no longer readable by all.
- **A test that failed late in the UTC day.** The platform audit test's
  "tomorrow" is now Addis Ababa's, whose dates the filter reads.
- **Drivers sharing a carrier address.** The driver app's API was limited
  to 600 requests a minute from one address, so a few hundred drivers
  behind one Ethio telecom address would have had their syncs refused. It
  now counts each driver's requests instead (300 a minute, by token), and
  an address's only to bound requests without a good sign-in
  (`DRIVER_API_PER_ADDRESS`, 12,000 a minute).
- **The overview's numbers on a laptop.** At 1280 pixels wide, the five
  numbers at the top of the overview (and of the platform's health page)
  wrapped onto two rows; they now stay on one, and go three, then two, to
  a row on narrower screens.

### Added

- **The code generated from the API's specification, and
  `@kiflet/api`** (`docs/migration.md`, phase 2, step 2; audit A1).
  `make api` generates two things from `api/openapi/v1.yaml`:
  - the Go types and strict server interface, into
    `internal/platform/api` (oapi-codegen, numbers as `float64`), which
    the operations implement as they move to it;
  - `@kiflet/api`'s TypeScript types (openapi-typescript), with a
    client typed by them (openapi-fetch), in `api/typescript`.

  CI runs `make api` and fails on a difference. It also fails on a
  breaking change to a `public` or `driver` operation against the base
  branch (oasdiff, `make api-breaking`); one to an `app` operation needs
  the pull request's `api-break` label.
  `.github/workflows/api-package.yml` publishes the package to GitHub
  Packages from `main`, as `1.0.<run>`, whenever the specification or
  the package changes.
- **The API's specification** (`docs/migration.md`, phase 2, steps 1
  and 3; audit A1). `api/openapi/v1.yaml` describes everything under
  `/api/v1` but the providers' callbacks: the public API's 8 operations
  and its own document, tagged `public`, and the driver app's 7, tagged
  `driver`, as they are today. `api/asyncapi/live.yaml` describes the
  dashboard's live updates, and `webhooks.yaml` what the webhooks send.
  - **The contract test:** the integration tests' server checks every
    answer under `/api/v1` against the specification: its status is
    one the operation lists, and its body has the shape given. It found
    one gap, the 403 a suspended company's key gets when it reads, now
    listed in the specification and in `openapi.json`.
  - **The other checks:** `TestSpecificationMatchesTheAPI` compares the
    routes with the operations; `TestSpecificationHasThePublicAPI` keeps
    the public part equal to the published `openapi.json`; and
    `TestAsyncAPIMatchesTheCode` checks the live updates' kinds and the
    webhooks' events against the code. `make lint` lints the
    specification with Redocly's strict rules.
- **Which tests cover each requirement** (`docs/migration.md`, phase 0,
  step 5; audit T4). Each integration and end-to-end test now names in its
  doc comment the requirement it covers, or says it is about operations,
  and `docs/requirements.md`, generated by `make requirements`, lists the
  tests of each requirement and the requirements with none: only FR-025
  (offline maps), which is not built. `go test ./tests/` checks every test
  names one that exists and that the table is current. A new test,
  `TestDeliveryNotes`, covers FR-023, which no test did.
- **A coverage floor for every package** (`docs/migration.md`, phase 0,
  step 5). `make cover` runs the unit and integration tests, each
  measuring every package, merges what they cover, and checks each
  package against its floor in `tests/coverage-floor.txt`. CI runs it in
  place of the integration tests' report of two packages, and fails below
  a floor. `make coverage-floor` raises the floors to what was measured,
  a point under it for the runs' timing, and never lowers one. Together
  the tests cover 78% of the code's statements.
- **Idempotency keys** (`docs/migration.md`, phase 1, step 8; audit D7).
  `idempotency_keys` remembers a keyed create for 24 hours, by company,
  caller and key, with the request's hash and the first response.
  `IdempotencyService` (`Begin`, `Finish`, `Release`) is what phase 2's
  `Idempotency-Key` middleware will call. A replay gets the first
  response, the same key with another request is refused, a failure on
  our side gives the key back, and a request that died is given up after
  five minutes. Retention prunes the keys.
- **Versions** (`docs/migration.md`, phase 1, step 7; audit D7).
  Shipments, users, vehicles, companies, webhook endpoints and alert rules
  carry a `version`, raised by a trigger on every update that changes what
  a person edits, for phase 2's `If-Match`. Bookkeeping (`updated_at`,
  sign-in counters, the SMS billing month) and credentials don't raise it,
  so a dispatcher's edit won't be refused because a driver signed in
  meanwhile; no query can set it.
- **Every query's company checked** (`docs/migration.md`, phase 1, step 5;
  audit D5). `TestEveryQueryIsScopedByTenant` (`sqlc/`) reads every
  queries file and fails on a query that neither names `tenant_id` nor
  says why on the line after its name (`-- tenant: none, <reason>`). The
  124 global queries (queue workers, retention, sign-in, public look-ups,
  the company's own row) now say why; reading their callers found none
  that should have been scoped.
- **gosec in `make lint`** (audit P2). Its findings were checked one by
  one: none was a vulnerability. The exceptions are in `.golangci.yml`,
  or beside the line, each with its reason.
- **Images for the clients** (audit G2). Every merge to main and every
  release tag builds `ghcr.io/kiflet/backend` and `ghcr.io/kiflet/sandbox`,
  checks them (`scripts/clients-check.sh`: ready, the demo driver has
  loads, texts reach the sandbox) and pushes them. Kiflet/web's and
  Kiflet/mobile's tests start a given tag with the demo company and the
  sandbox from `deployments/clients/compose.yml`. `make clients-up` runs
  that stack from the working tree, for the front ends' development,
  without the registry.
- **The load test** (`docs/deploy.md`, "Load test"). `make load-seed`
  puts `LOAD_PHONES` drivers on the road in staging, signed in, with
  `kiflet seed-load`. `make test-load` then plays their phones with k6: a
  sync a minute from each, and 3,000 coming back with two hours of
  positions. It fails the run if a live sync's 95th percentile is over
  300 ms, and reports how long positions waited for the tracking worker.
- **Timed jobs through RabbitMQ** (`docs/deploy.md`, "RabbitMQ"). With
  `RABBITMQ_URL`, one scheduler, holding a Postgres lock, names each job
  on `jobs.scheduled` as it falls due, and the workers run them, each job
  under a lease of its own (`job_leases`): the alert checks no longer
  wait behind a long retention run, and a scheduler that takes over
  carries on from the record of runs. `WORKER_JOBS=inprocess` switches back.
- **Texts, email and webhooks through RabbitMQ** (`docs/deploy.md`,
  "RabbitMQ"). With `RABBITMQ_URL`, the workers take them from
  `notify.sms`, `notify.email` and `webhook.deliver`. Each is still written
  to its table, which stays the record with its lease and retry schedule;
  a message names the rows due and a worker claims them by ID, and each
  worker's sweeper publishes what is due whenever its queue is empty.
  A worker told to stop finishes what it has in hand and hands the rest
  back. `WORKER_SMS`, `WORKER_MAIL` and `WORKER_WEBHOOKS` switch each back.
  CI runs the integration tests a second time with every worker on
  RabbitMQ.
- **Separate processes** (`docs/deploy.md`, "Separate processes").
  `kiflet serve --roles=web,worker,scheduler,relay` (or `ROLES`) runs only
  some of Kiflet; all of it by default. Production's compose file has a
  `worker` service (profile `workers`, `WORKER_REPLICAS`) for texts,
  email, webhooks, GPS fixes, the timed jobs and the event outbox, with
  `APP_ROLES=web` for the app's instances. Work queued on a web process
  wakes the workers through Postgres. Staging runs this way, and the
  restore scripts stop and start the workers with the app.
- **Redis** (`docs/architecture.md`, section 7; `docs/deploy.md`,
  "Redis"). Production's compose file runs it with
  `COMPOSE_PROFILES=redis`, staging and `make db` always. With
  `REDIS_URL` set, the request limits (sign-in, codes, lookups, each API
  key) are counted for all the instances together, not by each one; if
  Redis doesn't answer within 100 ms, each counts on its own until it
  does (`kiflet_limits_local`).
- **RabbitMQ** (`docs/architecture.md`, section 6; `docs/deploy.md`,
  "RabbitMQ"). Production's compose file runs it with
  `COMPOSE_PROFILES=rabbitmq`, staging and `make db` always, and the app
  uses it when `RABBITMQ_URL` is set. GPS fixes from phones then wait in
  the `tracking.ingest` queue for the tracking workers rather than being
  written while the phone waits, and are written directly if RabbitMQ
  can't take them. Live updates cross instances on it rather than on
  Postgres's `NOTIFY`. Every event in a shipment's history is written to
  the event outbox with its change and published to `kiflet.events` by
  the outbox relay (one instance leads). `WORKER_TRACKING` and
  `WORKER_LIVE` switch each part back. A message that keeps failing is
  parked in its queue's `.dead` queue. `pkg/broker` is the connection:
  confirms, retries through delay queues, and metrics.

- **Garage for files** (`docs/architecture.md`, section 5). Production's
  compose file runs [Garage](https://garagehq.deuxfleurs.fr/) beside the
  app with `COMPOSE_PROFILES=garage`, and `kiflet setup-storage`, run
  before the app at every deploy, makes its layout, imports the app's key,
  makes the bucket and sets its rules through Garage's admin API
  (`pkg/storage/garage`). `.env.example` has the settings and
  `docs/deploy.md`, "Files in Garage", how to move the files a machine
  already has. Staging runs it in place of SeaweedFS, and the integration
  tests start it. `S3_FORCE_PATH_STYLE` and `S3_PUBLIC_ENDPOINT` are new
  settings, and the S3 store signs upload URLs for phase 5's uploads.
- **The bucket in the backups.** With `STORAGE_TYPE=s3`,
  `scripts/backup.sh` also archives every object of the app's bucket
  (`kiflet_<time>_objects.tar.gz`), encrypted and copied off the machine
  like the rest, and `scripts/restore.sh` takes it as a third archive and
  makes the bucket the same as it. Files kept in S3 weren't backed up
  before. Prometheus reads Garage's own figures.

- **The event outbox** (`docs/migration.md`, phase 2). Two tables for the
  move to RabbitMQ: `event_outbox`, where a change will write the events
  others must hear about in its own transaction, for a relay to publish,
  and `processed_messages`, where a consumer records what it has handled
  so a message delivered twice does nothing the second time. Nothing
  writes to them yet. Retention keeps sent events three days and handled
  messages seven.

- **The plan for three codebases.** `docs/architecture.md` (a Go modular
  monolith run as API, realtime, worker, scheduler and relay processes
  over RabbitMQ and Redis; the Next.js web app; the Expo driver app with
  SQLite; deployment on VMs; capacity at 20,000 drivers) and
  `docs/migration.md` (six phases from today's single application).
  Kiflet/web and Kiflet/mobile each have their own architecture and
  migration documents for their phases. `ROADMAP.md`'s "One codebase"
  decision is replaced, and screen-off tracking points at the Expo app.

- **Support by phone and Telegram.** `SUPPORT_PHONE`, `SUPPORT_TELEGRAM`
  (a username) and `SUPPORT_HOURS` add those ways to reach support to the
  Help page, beside the email address. Each is left off when not set.

### Changed

- **The providers' callbacks moved to `/api/callbacks`** (`docs/migration.md`,
  phase 2, step 5): the SMS gateway's `/api/callbacks/sms/inbound` and
  `/sms/status`, and each payment provider's `/api/callbacks/payments/<code>/…`,
  out of `/api/v1`, which is the API and its specification alone. The
  sandbox, telebirr's notify URL, `.env.example`, `SECURITY.md` and
  `docs/deploy.md` follow. No provider had been given the old addresses.
- **A shipment's change carries its company** (`docs/migration.md`,
  phase 1, step 11; audit D9). The texting subscriber read the company
  again inside every transaction that changed a shipment. A transaction
  now reads its company once (`tenantIn`, kept in the transaction's
  scope), and each change it records carries it on the event, so the
  subscribers that run within it look nothing up for it. This completes
  phase 1 but for the release that drops the old columns.
- **One catalogue of words for codes** (`docs/migration.md`, phase 1,
  step 9; audit D8). `internal/platform/text` holds the office's and the
  driver's words for every code, beside the customer's. That covers
  statuses, reasons, roles, alert types, why a delivery was held, invoice
  and payment states, the driver's moves (the PWA's buttons) and the
  sync's refusal codes (`driversync.Refusals`, for phase 3), in English
  and Amharic. `text.Label(lang, audience, kind, code)` reads them, with
  the office's words where an audience has none of its own.
  `StatusLabel`, `ReasonLabel`, `RoleLabel`, `AlertLabel` and the held
  messages now read the catalogue instead of their own maps.
  `TestEveryCodeHasWords` fails on a code without words. The driver's
  Amharic comes from the PWA's `text.js`. The office's Amharic is new and
  goes to the native speaker's review (ROADMAP).
- **Delivery media in `pod_media`** (`docs/migration.md`, phase 1, step 6;
  audit D6). Each stored file of a proof of delivery is a row: the
  signature at position 0, the photos after it, with its key, type, size
  and MD5, and when and where it was taken when the app says (the PWA
  doesn't; Kiflet/mobile's uploads will). Signatures were already files.
  The shipment page, the review of held proofs, the receiver's
  confirmation page, the data export, anonymising and company deletion
  read it; `/media/{n}` keeps its numbering. Existing proofs are filled in
  by the migration, without size or MD5. `signature_url` and `photo_urls`
  are still written for a release, for a rollback, and then dropped.
- **Money in santim** (`docs/migration.md`, phase 1, step 4; audit D4). A
  load's cargo value and its charges are kept in whole santim
  (`cargo_value_santim`, `amount_santim`), as billing is, instead of birr
  with two decimals in floating point on the way. Forms and the import
  take birr as typed, with commas and at most two decimals, and refuse
  more; the pages and exports show birr as before. **The public API's
  cargo `value_etb` (a number of birr) is now `value_santim` (an integer
  of santim)**: no company uses the API yet. The old columns stay, unread,
  for a release, so a rollback finds them.
- **The tenant in every reference** (`docs/migration.md`, phase 1, step 2;
  audit D2). Each of the 39 references between companies' tables is now
  `(tenant_id, x_id) REFERENCES x (tenant_id, id)`, so the database
  refuses a row pointing at another company's, where before only the code
  kept them apart. Deletes do as before; a `SET NULL` clears only its
  column. `broadcast_recipients` gains `tenant_id`. The four references
  the platform's operators make across companies (the audit log's user,
  an invoice's maker, a payment's recorder, a support answer's author)
  stay on the ID. `TestReferencesCarryTheTenant` fails on a new
  reference without the tenant.
- **The driver sync reads a change feed** (`docs/migration.md`, phase 1,
  step 1; audit D1). Every change a driver's phone must hear of writes an
  entry in `driver_feed`, in the transaction of the change, numbered in
  that driver's own sequence: a load that changed or was taken away (with
  the reason: `reassigned`, `unassigned` or `cancelled`), a note from the
  office, a message to every driver. A sync sends what the entries after
  its cursor name, each load once as it is now, instead of every load
  touched in the last week; a change whose transaction committed after a
  sync began is no longer missed. The cursor is an opaque string, and
  the profile is sent only when it changed. A cursor that can't be
  carried on from (missing, another driver's, older than the 30 days the
  feed keeps, or a timestamp from a PWA loaded before this) gets a full
  download (`full`), which the PWA now honours. The new driversync module
  owns the feed; the load test syncs from a cursor, as phones do.
- **The plan, revised after an audit** (`docs/audit-2026-11-19.md`):
  backend first. `docs/migration.md` is rewritten in new phases: the data
  and the flow (a change feed for the driver sync, the tenant in every
  reference, santim, delivery media, versions and idempotency keys, one
  catalogue of words), then one versioned API, `/api/v1`, for every client
  with one OpenAPI specification, then the driver's part of it with
  Kiflet/mobile and the web's with Kiflet/web. `docs/architecture.md`
  (section 11 rewritten: the API, its conventions and the driver sync)
  and `ROADMAP.md` follow. The Go pages and the driver PWA are frozen:
  fixes, and what the pilot needs. `main` is protected again, now in all
  three repositories, and private vulnerability reporting is on.
- **PostgreSQL 17** (PostGIS 3.6), from 15, in development, production,
  CI and the tests. Its data is in a new volume (`postgres17-data`; in
  development `postgres17_data`), so the old one is untouched:
  `docs/deploy.md`, "Upgrading PostgreSQL", moves production's data
  across with a backup and a restore, and in development `make seed`
  fills the new one.
- **GPS positions partitioned by day.** `tracking_events` is now one
  table per UTC day, made a week ahead by an hourly job (on the health
  page as "Days of GPS positions made ahead"). Retention drops a whole
  day at 90 days instead of deleting its rows, which leaves nothing for
  vacuum. The migration moves the existing positions across. A position
  is now told apart by its device, client ID and time together, which a
  phone sending it again repeats, so retries still store nothing twice.
  The index on positions' coordinates is gone: no query used it.

- **A new dark theme.** Night is now a neutral dark grey instead of a teal
  one, in the dashboard, the public pages and the driver app: surfaces
  lighten as they rise, text is off-white, buttons are the light theme's
  ink inverted, and the status colours are toned down so they don't glare
  on a dark screen. Every text colour is at least 4.5:1 against every
  surface (WCAG AA).
- **The road a truck took, on the maps.** Picking a load on the overview's
  map draws its truck's GPS trail so far, then a straight line for the
  rest of the way; it was a straight line from the pickup before. On the
  overview and a shipment's page, a stretch where the phone sent nothing
  for over 20 minutes while the truck moved (the app was closed, or no
  signal) is dashed, with how long, rather than drawn as road driven.
- **Demo trails along the roads.** `make seed` gives every demo trip,
  delivered ones included, a GPS trail along the real road (through Adama
  and Awash to Djibouti, for one), not a straight line, and none at all
  for the delivered ones before.
- **The driver app in the dashboard's look** (the redesign, part 9). The
  same colours (Desk, and Night in dark mode), the Geist font, bundled so
  it works offline, the dark mark in place of the blue one, and the
  dashboard's status badges, buttons, fields and cards, kept at the
  driver app's larger sizes for one hand and bright sun. The app's icon,
  the browser's colour around it and the mini-map's markers follow.
  Drivers see it the next time the app starts after downloading it.
- **The maps in the theme's colours.** Routes, the truck's area, the start
  and a delivery on the dashboard's and tracking page's maps now use the
  palette (and Night's in dark mode) instead of a fixed blue; so does the
  browser tab's icon.
- **The public pages redesigned** (the redesign, part 8).
  - **Tracking**: the carrier first, then the status, the expected time,
    the distance left by road and where the truck was last seen; when
    each stage was reached; the delivery's details, what happens on
    arrival, and a button to call the carrier beside the map.
  - **Sign in**: a choice of phone or email, which only changes the field;
    links for drivers, new companies and customers below.
  - **Find my shipments**: a box for a tracking number under the phone
    form.
  - On a phone, the public pages' header fits on one line.
- **The company's pages redesigned** (the redesign, part 7).
  - **Help**: answers to common questions by topic, with a search that
    narrows them as you type, then your questions to the Kiflet team and
    how to reach support.
  - **Team**: members, those invited who haven't signed in yet (resend or
    cancel), and former staff, with a search; the invite form and what
    each role can do beside them.
  - **Settings**: in sections with a menu beside them, the customer texts
    as switches, and a preview of the text a customer gets, in the
    language chosen, with how many SMS it costs.
  - **Billing**: the plan's price first, this month's numbers, the
    invoices as a full table, and how to pay and what the invoice says
    beside them.
  - **Reports**: the six numbers in one row, and on-time bars by route.

- **Alerts redesigned, with rules** (the redesign, part 6). A company sets
  when it is alerted: problems reported by drivers, running late (by how
  much), phone offline (for how long), a long stop outside a town (new) and
  leaving the route (by how far), each of which can be turned off. An
  alert is resolved with a note of what was done, and the page lists what
  was resolved today and by whom. An administrator chooses who also gets
  alerts by email or SMS, every alert or problems only. The office's
  "customer reported a problem" alerts now offer to text the customer, as
  they were meant to.
- **Drivers and vehicles redesigned, with their papers** (the redesign,
  part 5). A driver has a licence expiry date, and a vehicle its insurance
  and annual inspection (ቦሎ) dates. The office is alerted 30 days before
  each, and critically once it has passed; recording the new date resolves
  it. A driver whose licence has run out can't be given a trip. The drivers
  page has tabs (on a trip, available, off duty), each driver's current
  load and where their phone was last heard from; the vehicles page has the
  fleet at a glance and "papers due in 30 days". A driver can be texted a
  reminder to renew. The dates are in the company's export. Alerts can now
  be about a vehicle.
- **A shipment's documents and charges** (the redesign, part 4). Two
  tabs on a shipment's page. **Documents** keeps files with a load (the
  signed waybill, customs papers, invoices), up to 10 MB each, PDFs and
  photos only, checked by their content; only the company sees them.
  **Charges** lists what the customer is charged, line by line with a
  total, discounts as negative lines. Both are in the company's export.
  The keeping period removes documents with the customer's other details,
  while charges stay for reports.
- **Booking redesigned, and pieces on a load** (the redesign, part 3).
  The booking form is in sections (customer, route and pickup, load,
  driver and priority) with a summary beside it that follows what you
  type: the distance and time on the road (from your own past trips on
  the route when there are enough), when it would arrive, and how full
  the chosen truck will be. A load can say how many pieces it is, such
  as "152 bales": on the shipment's page, the waybill, the list, both
  exports, the CSV import, the API (`cargo.pieces` and `cargo.unit`)
  and the driver app.
- **Dispatch pages redesigned** (the redesign, part 2). The overview
  opens on the date, the figures with icons, the map, what needs
  attention (with buttons to call the driver or customer) and a shipments
  board in tabs: on the road, needing attention, waiting for a driver,
  delivered. Each load shows how far along its route it is. The shipment
  list has counts on its tabs, search and filters in a toolbar, a
  progress column and a menu on each row. A shipment's page has a strip
  of the stages it has passed, the distance left, its speed and where it
  was last seen, with its history and messages in an Activity tab, and
  the driver, customer and load beside it.
- **A new look for the dashboard** (the redesign, part 1). A dark side
  menu with the company at the top and your account at the bottom, a top
  bar with where you are, search (press `/`) and alerts, and a footer on
  every page. Buttons, fields, tables, tabs and status badges follow the
  new design throughout, in the Geist typeface (served from Kiflet itself),
  and dark mode has its own palette. The pages themselves are redesigned
  in the parts that follow.
- **Restoring to the minute** (the rest of audit O1). With
  `WAL_ARCHIVE=on`, Postgres archives every change; `scripts/ship-wal.sh`,
  from cron, encrypts it and copies it to the backup bucket, `backup.sh`
  adds a base backup, and `scripts/restore-pitr.sh` restores the database
  to any moment of the last two weeks. `fetch-backup.sh` brings the base
  and WAL back on a new machine. Staging archives, and the end-to-end
  tests restore to a moment between two sign-ups.
- **The app can run as two instances** (audit O4). `APP_REPLICAS=2`
  starts two behind Caddy, which keeps each client address on one of them
  so that the rate limits hold, and tries the other when one is down. Live
  updates now pass through Postgres, so a dashboard sees a change whichever
  instance made it, and instances starting together take turns at the
  migrations. Staging runs two. With one, nothing changes.
- **Dependencies updated**: the Go modules Dependabot proposed (pgx 5.13,
  chi 5.5, httprate 0.16, goose 3.30 and others), Alpine 3.24, and
  Prometheus 3.16, Grafana 13.3, rclone 1.76, Mailpit 1.33 and
  SeaweedFS 4.53 in the deployments.
- **The live map reads one row a driver** (audit O3). Each driver's
  newest position is kept in its own table as positions arrive, where
  the map used to search a week of them on every load. The migration
  fills it from the last week. A phone's saved positions now go to the
  database a thousand to a statement, not one by one. Nothing looks
  different.
- **The driver app is in modules.** Its 1,686 lines of `app.js` are now 19
  ES modules: the state, the text in both languages, IndexedDB, the
  server, the outbox and sync, settings, GPS, the router, the signature
  box, and one module per screen. They run as written, with no build
  step, and `index.html` preloads them all so start-up doesn't wait on
  one import after another. Nothing changes for drivers.
- **The driver app updates as a whole.** Its service worker used to fetch
  each file afresh whenever there was signal, so an update cut short on a
  weak signal could leave old and new files mixed. Now the server lists
  every file of the app with its SHA-256 in `/driver/build.json`. When the
  app starts, and hourly while it runs, the worker downloads a new version
  beside the old one and keeps it only once every file matches. The new
  version takes over the next time the app starts, or sooner when the
  driver comes back to the app with no form open. The app now starts from
  the cache at once, signal or not, where before it waited up to three
  seconds for the network.

### Added

- **Webhooks** (FR-047). An admin adds an address under **Settings →
  Webhooks** and chooses what it is told: a shipment booked, a status
  changed, a delivery, or a problem reported by the driver or the
  receiver. Each message is the shipment as the API sends it and what
  happened, signed as the Standard Webhooks specification describes, so
  any of its libraries can check it. A receiver that doesn't answer 2xx is
  tried again six times over about a day. Each webhook's page lists the
  last 30 days of messages with their answers, and can send a test, send
  one again, pause, take a new secret or remove the webhook. In
  production a webhook must be HTTPS on a public address, checked again
  as each message connects (`WEBHOOK_PRIVATE_TARGETS` allows others, for
  development).
- **A public API** (FR-046). A company's own systems can book, list,
  change, assign and cancel its shipments, read each one's history and
  where its truck was last seen, and list its drivers, in JSON at
  `/api/v1`. An admin makes keys under **Settings → API keys**, named after
  what uses them; a key shows once, only its SHA-256 is kept, and revoking
  it stops it at once. A key acts for the whole company under the
  dashboard's rules, texts included, and the shipment's history and the
  audit log record what it did under its name. Each key may make 300
  requests a minute. `/api/v1/openapi.json` describes every endpoint and
  field, and a test keeps it true to the code. Every plan has it.
- **Shipments can be booked from a CSV file** (FR-013). **Import CSV** on
  the shipment list takes a spreadsheet saved as CSV, up to 200
  shipments. Every row is checked as the booking form would check it and
  shown with what is wrong, before anything is booked; a file with a row
  that can't be booked books nothing, so it can be corrected and sent
  again whole. The page lists the columns and offers a template, and the
  list's own export can be edited and sent back: columns the import
  doesn't know are ignored. Commas or semicolons both work. Each
  shipment is booked as the form books it, texts included, and the audit
  log records the import. Drivers are assigned afterwards from the list.
- **A shipment can name its receiver** (audit W4). The booking form has
  a second, optional person: who takes delivery at the destination, when
  that isn't the customer who booked. Both get the booked, departed,
  delayed and cancelled texts. What belongs to the handover goes to the
  receiver alone: the arrival text, the driver's Call button and
  suggested names in the driver app, and the link to confirm receipt.
  The customer is told the delivery was made, with the tracking link.
  Either finds the shipment with their phone on `/track/phone`. The
  shipment page, the search, both exports and the waybill (Shipper and
  Consignee) carry the receiver, and the keeping period removes them
  with the customer. A shipment without one works as before: the
  customer receives. Texts double for a shipment with two people.
- **Customers can be written to in Amharic** (audit W1, the customers'
  half). Settings has a language for customers, English or Amharic. A
  company that chooses Amharic sends its customers Amharic texts, and
  their tracking and confirmation pages open in Amharic. Every customer
  page has a switch to the other language, and remembers the choice; the
  pages that belong to no company (`/track`, finding shipments by phone)
  follow the choice, then the browser's language. Dates stay in the
  Gregorian calendar, with the months spelled out. A text in Amharic
  holds 70 characters to a part, so most go as two or three parts of the
  allowance, and Settings says so. The words are in `internal/text`, one
  file to a language. **The Amharic was written without a native
  reviewer and needs one** before a company turns it on. The dashboard,
  the mail and the drivers' texts are still English only.
- **Backups leave the machine, encrypted** (audit O1). With `backup.env`
  (`backup.env.example` is the template), `scripts/backup.sh` encrypts each
  backup with age, to public keys whose private keys are kept off the
  server, and copies it to an S3-compatible bucket, which keeps it 90 days.
  It won't copy a backup that isn't encrypted. `scripts/fetch-backup.sh`
  brings one back from the bucket with neither `.env` nor a running stack,
  and `scripts/restore.sh` restores an encrypted one with `AGE_IDENTITY`,
  decrypting it before anything is replaced. `docs/deploy.md` has the
  set-up, restoring on a new machine, and the drill, which `make test-e2e`
  runs on staging. It now needs age.
- **A backup holds the machine's settings too**: `.env`, `backup.env` and
  `deployments/secrets/`, in a third file, `kiflet_<time>_settings.tar.gz`,
  so a new machine can be set up from the backup alone.

### Fixed

- **A change made during a sync waited for the next one.** What the
  driver recorded while a sync was under way, a status, a message or a
  delivery, stayed on the phone until the next regular sync, a minute
  later or five with battery saver. On a weak signal, where a sync with
  photos can take a minute, that would be often. It now goes as soon as
  the sync under way ends.
- **The signature box could drop the first stroke.** It measured itself
  on the first frame after the delivery form opened, and ignored a stroke
  begun before then. It now measures itself on the first touch if the
  frame hasn't come yet.

## 0.9.0 (2026-11-15)

The first tagged release. Kiflet isn't live yet: `1.0.0` comes with the
first paying customer.

**History was rewritten before the tag.** Every commit now carries the
right author name and a no-reply address, and the co-author lines are
gone. The files are exactly as they were, but every commit has a new hash,
so a clone made earlier has to be made again. The audit quotes the new
hashes.

### Added

- **A licence.** `LICENSE` says all rights are reserved: the code may be
  read, and nothing else, without written permission.
- `SECURITY.md`, on reporting a vulnerability and where to look for one,
  and `CONTRIBUTING.md`, on running, testing and changing the code.
- `docs/deploy.md`: production, monitoring, backups, staging and connecting
  the providers, moved out of the README.
- **The driver app is tested in a browser.** An end-to-end test runs it in
  headless Chrome against staging: signing in, starting a trip, and with
  the signal gone, a delivery form that survives a reload and a delivery
  sent once the signal is back. `make test-e2e` needs Chrome or Chromium
  for it, and skips it without one, except in CI.
- **More checks in CI.** `make lint` runs golangci-lint (errcheck,
  ineffassign, staticcheck and unused), parses every script with Node, and
  scans the whole history for secrets with gitleaks; CI runs it on every
  pull request. The integration job adds to each run's summary how much of
  `internal/service` and `internal/web` its tests cover. Dependabot
  proposes updates each week for the Go modules, the Actions and the images.

### Changed

- **One migration instead of 21.** `migrations/20261115000100_baseline.up.sql`
  holds the whole schema, and `git log -- migrations/` shows the migrations
  that built it. A database they built keeps its data and takes the
  baseline as done, since every statement in it can run again; a new one
  gets the same schema, line for line.
- **Everything moved out of `backend/`** to the repository root, which is
  now where `go.mod`, the `Makefile` and `.env` live and where every command
  runs. The development compose file and the sandbox's settings are in
  `deployments/dev/`; `make db`, `make sandbox` and `make run-sandbox` find
  them.
- The long documents moved to `docs/`: the requirements, the background,
  the manual test guide and the audit, which is now
  `docs/audit-2026-11.md`.

### Removed

- **The Twilio SMS adapter**, with `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`
  and `TWILIO_FROM_NUMBER`. Nothing used it and the provider sandbox never
  covered it. `SMS_PROVIDER` is `mock` or `ethiotelecom`; with `twilio` the
  app now refuses to start.
- Code nothing called: the live feed's per-driver and per-shipment rooms
  and its message types (the dashboard only ever had one feed for each
  company), and a handful of unused helpers.

### Fixed

- **A photo stored on disk is only kept whole.** Local storage ignored the
  error from closing a file it had just written, which is where a full disk
  can show up, and could keep half a photo. It now reports the error and
  removes the file.
- **Staging no longer hangs on start.** The job that makes the stand-in S3
  bucket could wait for ever on one call, holding up the app and CI. Each
  call now has five seconds, and `make staging-up` gives up after ten
  minutes.
- `TestRateLimits` no longer fails on a busy machine. When its requests run
  into a new clock minute, which resets the limiter's count, it runs again
  from the start of the next one.
- **Maps work in every build.** The map library (Leaflet 1.9.4) had never
  been committed: an ignore rule meant for Go's `vendor/` directory also
  matched `static/vendor/`. It was served on the machine the product was
  developed on and returned a 404 everywhere else, including CI, staging
  and any production image. It is in the repository now, a unit test fails
  when a page, script or stylesheet names a static file that isn't
  embedded, and the end-to-end tests request the library from staging.

### Renamed

The product is now called **Kiflet** (ቅፍለት, a caravan). Until this release
it was called Menged, a name another transport app in Ethiopia already uses.

Nothing behaves differently, but the name was in many identifiers, so an
installation made before the rename has things to move:

- **Compose projects** are `kiflet`, `kiflet-dev` and `kiflet-staging`.
  Compose names volumes after the project, so the new project starts with
  empty ones. Move a server's data with `scripts/backup.sh` under the old
  project and `scripts/restore.sh` under the new one; don't start the new
  project on top of it.
- **A deployment's `.env`** keeps what it says. Change `APP_NAME`,
  `MAIL_FROM`, `SUPPORT_EMAIL`, `ETHIO_TELECOM_SENDER_ID` and anything else
  there that carries the old name; `.env.example` shows the new values.
- **Backups** default to `/var/backups/kiflet` and are named
  `kiflet_<time>.dump`. `scripts/backup.sh` only prunes files with that
  prefix, so older backups stay until removed by hand.
- **The database** setting that permits purging the append-only tables is
  `kiflet.purge`. A migration recreates the trigger function for databases
  made before the rename.
- **Metrics** are `kiflet_*`. The Grafana dashboard and alert rules use the
  new names; history recorded under the old ones is not carried over.
- **The driver app** keeps its unsent work in a database whose name
  changed. Sync every phone before updating, or that work is lost.
- **In the browser**, a dispatcher's hidden columns and notification
  setting reset once.
- **The Go module** is `github.com/natnael-alemayehu/kiflet`, and the
  binaries are `kiflet` and `kiflet-sandbox`.
- **Demo logins** are at `kiflet.et`, with the password `kiflet-demo`.
