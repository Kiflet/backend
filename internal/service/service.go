// Package service holds the business rules: what may happen to a shipment,
// who may do it, and what follows from it (history, alerts, SMS, live
// updates). Handlers translate HTTP into calls here and render the result.
package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/kiflet/backend/internal/modules/audit"
	"github.com/kiflet/backend/internal/modules/fleet"
	"github.com/kiflet/backend/internal/modules/identity"
	"github.com/kiflet/backend/internal/modules/tenancy"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/kiflet/backend/internal/db"
	"github.com/kiflet/backend/pkg/payments"
	"github.com/kiflet/backend/pkg/storage"
)

// Settings are the deployment-wide values the business layer needs.
type Settings struct {
	// AppName is the product name used in SMS and page titles.
	AppName string
	// BaseURL is the public origin, without a trailing slash, used to build
	// links sent by SMS.
	BaseURL string
	// Location is the operating time zone for "today", reports and SMS times.
	Location *time.Location
	// Secret keys the HMAC that protects one-time codes and lookup cookies.
	Secret []byte
	// SMSNumber is the number or short code drivers text status updates to.
	SMSNumber string
	// SupportEmail is where companies write to the platform operator.
	SupportEmail string
	// BankDetails tells companies where to send a bank transfer; empty when
	// transfers aren't accepted.
	BankDetails string
	// RetainShipments is how long a finished shipment keeps its customer
	// and delivery details (FR-041). Zero means a year.
	RetainShipments time.Duration
	// CodesPerDay is how many verification codes the platform texts in 24
	// hours at most; zero means no limit.
	CodesPerDay int
	// PrivateWebhooks lets webhooks go to private and loopback addresses
	// and over plain HTTP, for development and tests. Off, they reach only
	// the public internet over HTTPS.
	PrivateWebhooks bool
}

// Publisher delivers live updates to dashboards. The WebSocket hub
// implements it.
type Publisher interface {
	BroadcastToTenant(tenantID string, message []byte)
}

// PositionQueue hands drivers' GPS fixes to the tracking workers through
// RabbitMQ (docs/architecture.md, section 6), so a burst of phones coming
// back into coverage waits in a queue instead of on the database.
type PositionQueue interface {
	Enqueue(ctx context.Context, b PositionBatch) error
}

// Deps is everything the services share.
type Deps struct {
	Pool *pgxpool.Pool
	// Audit, Tenancy and the identity module's parts are modules
	// (internal/modules).
	Audit     audit.Log
	Tenancy   tenancy.Companies
	Sessions  identity.Sessions
	Codes     identity.Codes
	Links     identity.Links
	Signups   identity.Signups
	Users     identity.Users
	Fleet     fleet.Fleet
	Log       zerolog.Logger
	Publisher Publisher
	// Positions, when set, takes GPS fixes for the tracking workers; nil
	// stores them at once.
	Positions PositionQueue
	// Outbox writes events to event_outbox for the relay to publish. Off
	// without RabbitMQ: nothing would read them.
	Outbox bool
	// Work, when set, carries the senders' work through RabbitMQ.
	Work WorkQueue
	// RemoteWorkers says the senders of texts, email and webhooks run in
	// other processes: work queued here wakes them through Postgres
	// (WorkChannel) instead of in this process.
	RemoteWorkers bool
	Storage       storage.Storage
	SMS           SMSSender
	Mail          MailSender
	// Payments are the mobile-money providers, in the order offered.
	Payments []payments.Provider
	Settings Settings
	// Now is the clock, replaceable in tests.
	Now func() time.Time
}

// Services is the business layer.
type Services struct {
	Auth      *AuthService
	Shipments *ShipmentService
	Users     *UserService
	Vehicles  *VehicleService
	Tenants   *TenantService
	Alerts    *AlertService
	SMS       *SMSService
	Mail      *MailService
	Reports   *ReportService
	Tracking  *TrackingService
	Sync      *SyncService
	Media     *MediaService
	Jobs      *Jobs
	Audit     *AuditService
	Billing   *BillingService
	Data      *DataService
	Support   *SupportService
	Health    *HealthService
	API       *APIService
	Webhooks  *WebhookService
}

// New wires the services together.
func New(d Deps) *Services {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Settings.Location == nil {
		d.Settings.Location = time.UTC
	}
	if d.Settings.RetainShipments <= 0 {
		d.Settings.RetainShipments = 365 * 24 * time.Hour
	}
	if d.Publisher == nil {
		d.Publisher = nopPublisher{}
	}
	c := &core{pool: d.Pool, q: db.New(d.Pool), log: d.Log, pub: d.Publisher, now: d.Now, cfg: d.Settings, auditLog: d.Audit, tenancy: d.Tenancy, sessions: d.Sessions, codes: d.Codes, links: d.Links, signups: d.Signups, users: d.Users, fleet: d.Fleet,
		positions: d.Positions, outbox: d.Outbox, remoteWorkers: d.RemoteWorkers, work: d.Work}

	s := &Services{}
	s.Audit = &AuditService{core: c}
	s.SMS = newSMSService(c, d.SMS)
	c.sms = s.SMS
	s.Mail = newMailService(c, d.Mail)
	c.mail = s.Mail
	s.Webhooks = newWebhookService(c)
	c.webhooks = s.Webhooks
	s.Alerts = &AlertService{core: c}
	c.alerts = s.Alerts
	s.Media = &MediaService{core: c, store: d.Storage, pod: podStorage(d.Storage)}
	s.Shipments = &ShipmentService{core: c, media: s.Media}
	s.SMS.shipments = s.Shipments
	s.Auth = &AuthService{core: c}
	s.Users = &UserService{core: c}
	s.Vehicles = &VehicleService{core: c}
	s.Billing = &BillingService{core: c, providers: d.Payments}
	s.Tenants = &TenantService{core: c, auth: s.Auth, billing: s.Billing}
	s.Reports = &ReportService{core: c}
	s.Tracking = &TrackingService{core: c}
	s.Sync = &SyncService{core: c, shipments: s.Shipments, media: s.Media}
	s.Data = &DataService{core: c, media: s.Media}
	s.Support = &SupportService{core: c}
	s.Health = &HealthService{core: c}
	s.API = &APIService{core: c}
	s.Jobs = &Jobs{core: c, billing: s.Billing, data: s.Data}
	return s
}

// core is the state every service embeds.
type core struct {
	pool     *pgxpool.Pool
	q        *db.Queries
	log      zerolog.Logger
	pub      Publisher
	now      func() time.Time
	cfg      Settings
	sms      *SMSService
	mail     *MailService
	alerts   *AlertService
	webhooks *WebhookService
	auditLog audit.Log
	tenancy  tenancy.Companies
	sessions identity.Sessions
	codes    identity.Codes
	links    identity.Links
	signups  identity.Signups
	users    identity.Users
	fleet    fleet.Fleet
	// positions and outbox: see Deps.
	positions     PositionQueue
	outbox        bool
	remoteWorkers bool
	work          WorkQueue
}

// WorkChannel is the Postgres channel that wakes the senders in worker
// processes; the payload names which: sms, mail or webhooks.
const WorkChannel = "kiflet_work"

// wakeWorkers tells the worker processes there is work for a sender, when
// they are elsewhere. Missed, the sender finds it at its next look, within
// seconds.
func (c *core) wakeWorkers(kind string) {
	if !c.remoteWorkers {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.pool.Exec(ctx, "SELECT pg_notify($1, $2)", WorkChannel, kind); err != nil {
		c.log.Warn().Err(err).Str("sender", kind).Msg("wake the workers")
	}
}

// Wake wakes this process's sender of a kind (sms, mail or webhooks), as
// a worker does when told through WorkChannel.
func (s *Services) Wake(kind string) {
	switch kind {
	case "sms":
		s.SMS.poke()
	case "mail":
		s.Mail.poke()
	case "webhooks":
		s.Webhooks.poke()
	}
}

// tx runs fn in a transaction, committing if it returns nil. Work that must
// happen only once the data is durable — live updates, waking the SMS and
// email senders — is queued with after and runs after commit.
func (c *core) tx(ctx context.Context, fn func(q *db.Queries, after *afterCommit) error) error {
	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	after := &afterCommit{}
	if err := fn(c.q.WithTx(tx), after); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	for _, f := range after.fns {
		f()
	}
	return nil
}

type afterCommit struct{ fns []func() }

func (a *afterCommit) do(f func()) { a.fns = append(a.fns, f) }

// publish sends a live update to every dashboard of a tenant.
func (c *core) publish(tenantID pgtype.UUID, kind string, data any) {
	msg, err := json.Marshal(map[string]any{"type": kind, "data": data})
	if err != nil {
		c.log.Error().Err(err).Str("type", kind).Msg("marshal live update")
		return
	}
	c.pub.BroadcastToTenant(tenantID.String(), msg)
}

type nopPublisher struct{}

func (nopPublisher) BroadcastToTenant(string, []byte) {}

// Principal, Actor and where an action came from are the identity
// module's (internal/modules/identity).
type (
	Principal = identity.Principal
	Actor     = identity.Actor
)

const (
	SourceDashboard = identity.SourceDashboard
	SourceApp       = identity.SourceApp
	SourceSMS       = identity.SourceSMS
	SourceSystem    = identity.SourceSystem
	SourceCustomer  = identity.SourceCustomer
	SourceAPI       = identity.SourceAPI
)

// SystemActor is the platform acting on its own, such as a scheduled check.
var SystemActor = identity.SystemActor

// randomToken returns n random bytes, URL-safe base64 encoded.
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// isNoRows reports whether a query found nothing.
// A person who isn't there counts too (identity.ErrNoUser), while the
// code here still checks for no rows after looking one up.
func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows) || errors.Is(err, identity.ErrNoUser)
}

// isUniqueViolation reports whether err is a unique-constraint failure, and
// on which constraint.
func isUniqueViolation(err error) (string, bool) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return pg.ConstraintName, true
	}
	return "", false
}

// mustJSON marshals a value whose type cannot fail to marshal.
func mustJSON(v any) []byte {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// emit writes an event to event_outbox in the transaction of the change it
// tells of; the relay publishes it to RabbitMQ once it commits
// (docs/architecture.md, section 6). Without RabbitMQ it writes nothing.
func (c *core) emit(ctx context.Context, q *db.Queries, tenantID pgtype.UUID, typ string, payload any) {
	if !c.outbox {
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		c.log.Error().Err(err).Str("type", typ).Msg("marshal event")
		return
	}
	id := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	err = q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{
		ID: id, TenantID: tenantID, Type: typ, Version: 1, Exchange: "kiflet.events",
		RoutingKey: typ, Payload: raw, CreatedAt: toTimestamp(c.now()),
	})
	if err != nil {
		// The transaction is spoilt, so the change fails with it: no
		// change goes in without its event.
		c.log.Error().Err(err).Str("type", typ).Msg("write event to the outbox")
	}
}

// tenant loads a company through the tenancy module, on q's connection
// (its transaction, if it has one), as the rest of internal/service knows
// it until it moves into modules.
func (c *core) tenant(ctx context.Context, q *db.Queries, id pgtype.UUID) (db.Tenant, error) {
	t, err := c.tenancy.Get(ctx, q.Conn(), id)
	return db.Tenant(t), err
}

// user, userByPhone, userByEmail, staff, createUser and updateProfile reach
// the identity module's people on q's connection (its transaction, if it
// has one), as the rest of internal/service knows them until it moves into
// modules.
func (c *core) user(ctx context.Context, q *db.Queries, tenantID, id pgtype.UUID) (db.User, error) {
	u, err := c.users.Get(ctx, q.Conn(), tenantID, id)
	return db.User(u), err
}

func (c *core) userByPhone(ctx context.Context, q *db.Queries, phone *string) (db.User, error) {
	if phone == nil {
		return db.User{}, identity.ErrNoUser
	}
	u, err := c.users.ByPhone(ctx, q.Conn(), *phone)
	return db.User(u), err
}

func (c *core) userByEmail(ctx context.Context, q *db.Queries, email string) (db.User, error) {
	u, err := c.users.ByEmail(ctx, q.Conn(), email)
	return db.User(u), err
}

func (c *core) staff(ctx context.Context, q *db.Queries, tenantID pgtype.UUID) ([]db.User, error) {
	us, err := c.users.Staff(ctx, q.Conn(), tenantID)
	out := make([]db.User, len(us))
	for i, u := range us {
		out[i] = db.User(u)
	}
	return out, err
}

func (c *core) createUser(ctx context.Context, q *db.Queries, n identity.NewUser) (db.User, error) {
	u, err := c.users.Create(ctx, q.Conn(), n)
	return db.User(u), err
}

func (c *core) updateProfile(ctx context.Context, q *db.Queries, p identity.Profile) (db.User, error) {
	u, err := c.users.UpdateProfile(ctx, q.Conn(), p)
	return db.User(u), err
}

// vehicle and activeVehicles reach the fleet module's vehicles on q's
// connection, as the rest of internal/service knows them until it moves.
func (c *core) vehicle(ctx context.Context, q *db.Queries, tenantID, id pgtype.UUID) (db.Vehicle, error) {
	v, err := c.fleet.Vehicle(ctx, q.Conn(), tenantID, id)
	return db.Vehicle(v), err
}

func (c *core) activeVehicles(ctx context.Context, q *db.Queries, tenantID pgtype.UUID) ([]db.Vehicle, error) {
	vs, err := c.fleet.ActiveVehicles(ctx, q.Conn(), tenantID)
	out := make([]db.Vehicle, len(vs))
	for i, v := range vs {
		out[i] = db.Vehicle(v)
	}
	return out, err
}
