# Roadmap

What to do next, in order. Rewritten on 2026-11-19 from the audit in
`docs/audit-2026-11-19.md`, at `dcd7225`. Tick the boxes as work lands,
and delete a section once it is done: this file should only ever hold what
is still ahead.

1. [Backend first: the migration](#backend-first-the-migration): the
   phases of `docs/migration.md`, data and flows before anything is built
   on them.
2. [Finish the rename](#finish-the-rename): the checks and registrations
   that make the name final.
3. [Toward the first paying customer](#toward-the-first-paying-customer).

The first is the engineering work. The other two are mostly outside the
repository and run beside it.

## Where things stand

- One Go application, being reorganised into modules: 49,700 lines of Go
  (13,100 of them still in `internal/service`), 37 tables from 13
  migrations, 327 queries; 210 unit tests, 127 integration tests against
  Postgres, RabbitMQ, Redis, Garage and the provider sandbox, and 9
  end-to-end tests on staging. The integration tests cover 76% of the
  code. Audit, tenancy, identity and fleet have moved to
  `internal/modules`, and shipments in part; the events bus, the queues,
  Redis and Garage are in place.
- It still renders the dashboard (53 templates) and the driver PWA (ES
  modules, no build step). Both are **frozen since 2026-11-19**: fixes,
  and what the pilot needs. Kiflet/web and Kiflet/mobile replace them;
  both have their plans and no code yet.
- CI runs four jobs: vet, golangci-lint, unit tests, gofmt, `sqlc diff`,
  govulncheck, shellcheck, `node --check`, a secret scan of the whole
  history and the former-name check; the integration tests, with and
  without RabbitMQ; and the end-to-end tests against staging. Dependabot
  proposes updates weekly.
- The repository moved to the [Kiflet](https://github.com/Kiflet)
  organization on 2026-11-19, beside `Kiflet/web` and `Kiflet/mobile`. The
  move lost `main`'s protection (audit G1). Since 2026-11-19, `main` in all
  three takes changes only through pull requests, for admins too; here the
  four CI jobs must pass. Private vulnerability reporting is on in all
  three, as `SECURITY.md` says.
- The repository is public and all rights are reserved (`LICENSE`). The
  first release is `v0.9.0`, of 2026-11-15. History was rewritten that
  day: every commit has the right author name and GitHub's no-reply
  address, and no co-author lines.
- `docs/audit-2026-11.md` is fixed except W1 (Amharic for the office,
  now Kiflet/web's) and O6 (provider onboarding).
  `docs/audit-2026-11-19.md` found what phases 0 to 2 below fix.
- The product and the repository were renamed Kiflet on 2026-11-15. The
  checks and registrations that make the name final are still open: see
  [Finish the rename](#finish-the-rename).

## Backend first: the migration

`docs/migration.md` has each phase's steps, what done looks like and how
to roll back; the audit IDs are `docs/audit-2026-11-19.md`'s. Phases 0 to
2 are this repository's alone. Phase 3 runs a step ahead of Kiflet/mobile's
milestones, and phase 4 a wave ahead of Kiflet/web's.

### Phase 0: groundwork

- [x] The Kiflet organization and its three repositories (2026-11-19).
- [x] Branch protection and private vulnerability reporting on all three
      (2026-11-19, G1).
- [ ] In the organization's settings, for its owner: two-factor
      authentication for members, a second owner, and the old
      `Natnael-Alemayehu/kiflet` archived with a pointer here.
- [ ] Accounts: Expo, the Play Console as an organization (its D-U-N-S
      number can take weeks, so it starts first), Firebase, GitHub
      Packages.
- [x] Images to `ghcr.io/kiflet/backend` and `ghcr.io/kiflet/sandbox` on
      every merge, and a compose file that starts a given tag with the
      demo data, for the clients' CI (G2).
- [ ] gosec (P2); `TestGatewayFaults` made reliable (T5); coverage of the
      unit and integration suites reported per package, against a floor
      that only rises; each test names its requirement, and a generated
      table lists the requirements with none (T4).

### Phase 1: the data and the flow

- [x] The driver sync's change feed, with an opaque cursor and full
      downloads (D1).
- [x] The tenant in every reference between companies' tables (D2).
- [x] The per-driver load limit counted under a lock (D3).
- [ ] Charges and cargo values in santim (D4).
- [x] Every query without `tenant_id` says why, checked in CI (D5).
- [ ] `pod_media`, and signatures stored as files (D6).
- [ ] A `version` on every edited record, and `idempotency_keys` (D7).
- [ ] One catalogue of the words for codes, for the office, customers and
      drivers, in English and Amharic (D8).
- [ ] Live updates sent only to the roles that may see them (P1).
- [ ] Events carry what their subscribers read (D9).

### Phase 2: one API and its contract

- [ ] `api/openapi/v1.yaml`, each operation tagged `app`, `public` or
      `driver`; `api/asyncapi/live.yaml` and `webhooks.yaml` (A1).
- [ ] Go types and server interface generated from it; `@kiflet/api`
      published; lint, regeneration and oasdiff in CI; every response in
      the API tests validated against it.
- [ ] The conventions in `internal/http`: errors, labels, actions, lists,
      idempotency keys, versions, request IDs.
- [ ] Who is calling, and the cross-origin check on every request the
      cookie authenticates (A2).
- [ ] The providers' callbacks moved to `/api/callbacks/*`.
- [ ] The public API's 8 operations, and the driver's, in the
      specification.

### Phase 3: the driver API, with Kiflet/mobile

- [ ] M2: the sync's new response and its stable refusal codes in the
      specification; `413` for a request too large.
- [ ] M3: the driver's words for every code (`labels`) and the moves in
      order in config, with an `ETag`.
- [ ] M4: the GPS, photo, sync and SMS settings and the map style in
      config.
- [ ] M5: presigned uploads; deliveries sent by key, with each photo's
      time and place; push tokens and Expo pushes.
- [ ] M6: the version gate; each device's health on the driver's page,
      with an alert when recording stops on the road; diagnostics.
- [ ] M7: the two-step sign-in; positions attributed to every load the
      driver carries; mock locations stored and shown.

The modules these touch move out of `internal/service` with them:
driversync, proof, tracking, messaging, notify and the rest of identity.

### Phase 4: the web's API, with Kiflet/web

- [ ] Wave 1: tracking and confirmation.
- [ ] W3 and wave 2: sessions, sign-up, resets, `me`, the live socket at
      `/ws`.
- [ ] Wave 3: account, help, audit log, reports.
- [ ] Wave 4: drivers, vehicles, team, broadcasts.
- [ ] Wave 5: shipments.
- [ ] Wave 6: the overview and the fleet map.
- [ ] Wave 7: alerts, settings, data export, API keys, webhooks, SMS log,
      billing.
- [ ] Wave 8: the operator's pages.
- [ ] Wave 9: landing and plans.

A Go page goes a week after Kiflet/web serves it, and its tests with it
only once its flows are tested through the API (T1).

### Phase 5: production readiness

- [ ] Off-site backups, before the pilot (below).
- [ ] Garage on three nodes, replication factor 3 (G3).
- [ ] Map tiles at `tiles.kiflet.et`, before Kiflet/web's wave 5.
- [ ] Row-level security; Redis caches; PgBouncer and a read replica.
- [ ] The OpenTelemetry collector.
- [ ] The load test at 20,000 drivers, and the failure drills.

### Phase 6: removing the old

- [ ] The PWA, once no driver has used it for 30 days.
- [ ] `/app/live`, `/app/api/*` and what is left of the pages.
- [ ] `internal/web` becomes `internal/http`; `internal/service`,
      `internal/model` and `internal/db` are gone.
- [ ] The documents.

## Finish the rename

The product was renamed Kiflet on 2026-11-15: the code, the documents and
the repository. `CHANGELOG.md` lists what moved. The name was taken "for
now", so this section is what makes it final, or replaces it. It is work
outside the repository.

**Kiflet (ቅፍለት)** is a caravan: loaded animals and their drivers
travelling together, the way goods crossed the country before trucks did.
It is what a trucking company runs today. The Latin spelling has "fleet"
nearly hidden in it, and at six letters it fits an SMS sender ID, which
allows eleven.

Checked on 2026-11-14:

- A web search found no product, company or app named Kiflet, in Ethiopia
  or elsewhere.
- whois has no record of `kiflet.com`, `.io` or `.co`, and `kiflet.et`,
  `.com.et`, `.app` and `.africa` have no DNS entry.
- No GitHub account or organization is named `kiflet`.

Still to check, before anything is registered under the name:

- [ ] Say it to a few people in the trade. Does ቅፍለት mean to them what it
      should, and is it easy to say and to type? "Kiflet" writes ቅ with a
      K, as most people would; "Qiflet" is the stricter spelling.
- [ ] Search the trademark register (EIPA) and the trade name register.
- [x] Search the Play Store and the App Store. Searched on 2026-11-16:
      the App Store has no app of the name in its Ethiopian, British or
      American catalogue, and a Play Store search returns only kitchen
      games, which it offers for a word it doesn't know.

Once those pass:

- [ ] Take the names: the domains (`kiflet.et` and `kiflet.com` at least;
      `docs/migration.md`, phase 0, lists the hosts). The GitHub
      organization [Kiflet](https://github.com/Kiflet) was created on
      2026-11-19, and this repository moved there as `Kiflet/backend`.
- [ ] Ask Ethio telecom for the sender ID `Kiflet`, give telebirr and CBE
      Birr the merchant name, and set up `support@` and `no-reply@` on the
      domain.

Left over from the old name:

- [ ] Any other clone: clone again, since history was rewritten on
      2026-11-15. Its Docker volumes and images still carry the old
      project's name; remove them.

If Kiflet fails a check, the next choices:

| Name | Meaning | What the checks found |
|---|---|---|
| Sirara (ሲራራ) | The long-distance caravan trade, as in ሲራራ ነጋዴ | No product by the name. `sirara.et` has no DNS entry; `sirara.com` and the GitHub name are taken. |
| Dersual (ደርሷል) | "It has arrived": what a proof of delivery says | No product by the name, and none of six domains tried has a DNS entry. The GitHub name is taken. Harder to spell in Latin letters. |
| Rikikib (ርክክብ) | Handover: the moment the product records | The same as Dersual, and more awkward in Latin letters. |

Ruled out, so nobody tries them again: Kella (the Ministry of Transport and
Logistics has a Kella app for truck terminals), Finot (FINOT Technologies is
a software company in Addis Ababa), Sinq (Sinq Technologies sells
supply-chain software), Adera (every domain is taken, `adera.et` included),
Mahtem (`mahtem.et` is in use), Feleg (a given name, and its `.com` and
`.app` are taken), and Chinet, Tolo, Garri and Woye (freight apps).

Renaming again costs little while nobody is live. The first rename was
these commands and no edits by hand, 604 lines in 126 files:

```bash
git grep -lzi kiflet -- . ':!internal/db' ':!CHANGELOG.md' ':!ROADMAP.md' |
  xargs -0 sed -i 's/Kiflet/Newname/g; s/kiflet/newname/g; s/KIFLET/NEWNAME/g'
git mv deployments/grafana/dashboards/kiflet.json \
       deployments/grafana/dashboards/newname.json
git mv deployments/grafana/provisioning/dashboards/kiflet.yml \
       deployments/grafana/provisioning/dashboards/newname.yml
make sqlc
```

Then: a migration that defines `append_only()` again with the new
setting's name, so that a database made under the old name can still purge
(the first rename's was `20261115000000_purge_setting`, now in git history); `gofmt -w .` if the new name isn't
six letters long; a `CHANGELOG.md` entry; and every test, staging included.
The three things that hold state are the reason to settle this before the
first customer: the purge setting in the database, the volumes Compose
names after the project, and the database on each driver's phone. Today
the organization and its three repositories carry the name too, and so
will `@kiflet/api` and the app's store listing once they exist.

## Toward the first paying customer

In order. The first three can run side by side, and none of them waits
for the migration.

1. **Start provider onboarding as soon as the name is settled** (audit O6).
   telebirr, CBE Birr and Ethio telecom each need a contract, and the wait
   is theirs, not ours. Apply under the new name: the sender ID and the
   merchant names are slow to change afterwards. Until the providers
   answer, production takes bank transfers only. `README.md` lists what to
   settle with each.
2. **The driver app on cheap Android phones.** Kiflet/mobile's field test
   (M4: recording with the screen off, on the phones drivers use) and its
   closed test (M6: the pilot build) are where the app is proven. The PWA
   is frozen, but an afternoon with it on a real phone, delivering a load
   with the camera on weak signal with the phone locked in between, is
   still worth it for the drivers who use it until then: fix only what
   would stop a pilot.
3. **A native speaker reads the Amharic.** The texts customers get and
   their tracking and confirmation pages are in Amharic, drafted without
   one; after phase 1, `internal/platform/text` holds every word the
   office, customers and drivers read, in both languages, so one reading
   covers them all, with Kiflet/mobile's glossary of driver words beside
   it. Include the tracking page's words added with its redesign
   (`track.tracked_with` to `track.call`), and ask whether customers
   expect dates in the Ethiopian calendar, which they are not in. Until
   then no company should choose Amharic in Settings. Amharic on the
   dashboard (audit W1) comes after, in Kiflet/web, from the same
   catalogue.
4. **A pilot with one trucking company**, paying by bank transfer, with
   Kiflet/mobile's pilot build (M6) and today's dashboard, frozen but
   working, its driver page showing each phone's health. A company ready
   sooner starts on the PWA and moves to the app at M6. Before it starts,
   production's backups go off the machine: choose the bucket's provider,
   make two age keys held by two people, fill in `backup.env`, and run the
   drill in `docs/deploy.md` once. Decide whether to turn on
   `WAL_ARCHIVE` and add `ship-wal.sh` to cron. Everything after this point
   should be reordered by what the pilot shows.
5. **Built ahead of need:** the public API and webhooks (FR-046, FR-047),
   two app instances (O4, `APP_REPLICAS`) and restoring to the minute
   (`WAL_ARCHIVE`, the rest of O1). Turn the last two on for the pilot if
   it can't afford a restart's gap or a lost day. The public API's error
   codes change in phase 2; tell a company before it integrates.

## Decided, and when to revisit

- **Backend first** (2026-11-19). The data and the flows come before the
  clients, which are built against the backend's contract and adjusted to
  it. Phase 1 fixes the data model while no company's data is in it.
  Revisit the order of the steps by what the pilot shows, not the
  principle.
- **One versioned API** (2026-11-19, replacing the three APIs of
  2026-11-18: `/api/web`, `/api/driver` and `/api/v1`). Every client uses
  `/api/v1`, each operation tagged `app`, `public` or `driver`, with one
  specification (`docs/architecture.md`, section 11). Revisit if one
  audience's needs pull an operation two ways for long; the answer is then
  a second operation, not a second API.
- **The driver sync is a change feed per driver** (2026-11-19), with an
  opaque cursor and full downloads, because the phone holds the driver's
  work in SQLite and the timestamp cursor could miss changes (audit D1).
  Revisit if the feed outgrows a month's entries.
- **The Go pages and the PWA are frozen** (2026-11-19): fixes, and what the
  pilot needs. New features are operations of the API, shown by Kiflet/web
  and Kiflet/mobile. Revisit if a client slips a month behind the pilot.
- **No CSRF tokens; numbered pages** (2026-11-19). Every request the
  session cookie authenticates passes Go's cross-origin check, as both the
  backend's and the web's plans now say. Lists are numbered pages with a
  total, and a cursor only for endless feeds.
- **`v1.0.0` with the first paying customer** (2026-11-15). Until then
  releases are `0.x`; `v0.9.0` is the first.
- **The PWA is ES modules with no build step** (2026-11-16): the files in
  `static/driver` are what the phone runs, and `index.html` preloads every
  module. It holds until the PWA goes (phase 6).
- **The name Kiflet, for now** (2026-11-15). Revisit if one of the checks
  in [Finish the rename](#finish-the-rename) fails. That section has the
  fallbacks and what a second rename takes.
- **The Kiflet organization** (2026-11-19, replacing "No GitHub
  organization yet" of 2026-11-15). Three repositories: `Kiflet/backend`
  (this one, moved with its history and tags from
  `Natnael-Alemayehu/kiflet`; the module path is `github.com/kiflet/backend`),
  `Kiflet/web` and `Kiflet/mobile`, each `main` protected. Still to do in
  GitHub's settings: two-factor authentication for members, a second
  owner, and archiving the old repository with a pointer here. If the name
  Kiflet fails its checks, the organization is renamed with the product.
- **Three codebases, migrated in phases** (2026-11-18, replacing "One
  codebase" of 2026-11-14; the plan revised on 2026-11-19). This
  repository becomes the backend and infrastructure (a Go modular
  monolith with RabbitMQ and Redis, run as separate processes) and serves
  no pages in the end; the web moves to Next.js in `Kiflet/web` and the
  driver app to React Native with Expo and SQLite in `Kiflet/mobile`,
  sized for 20,000 drivers on VMs with Docker Compose.
  `docs/architecture.md` is the target and `docs/migration.md` the plan,
  with no big bang. About 96 routes render pages, so the dashboard is
  rebuilt page by page against the API, a wave at a time.

## Considered and not planned

- **Moving `pkg/` under `internal/`.** Nothing outside this module imports
  it, but the move touches every file for no change a reader would feel.
- **Rewriting old commit messages** beyond the co-author lines and audit
  references the rewrite of 2026-11-15 removed. The early
  history is rough, and it is still the history. That includes the
  product's first name: old commits keep it.
