// Package app assembles Kiflet from its settings: the providers, the business
// services, the web server and the background workers. The server command
// and the integration tests both build it here, so the tests run the wiring
// that production runs.
package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	auditapp "github.com/kiflet/backend/internal/modules/audit/app"
	identityapp "github.com/kiflet/backend/internal/modules/identity/app"
	tenancyapp "github.com/kiflet/backend/internal/modules/tenancy/app"
	"github.com/redis/go-redis/v9"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/kiflet/backend/internal/platform/config"
	"github.com/kiflet/backend/internal/service"
	"github.com/kiflet/backend/internal/web"
	"github.com/kiflet/backend/pkg/broker"
	"github.com/kiflet/backend/pkg/console"
	"github.com/kiflet/backend/pkg/mail"
	"github.com/kiflet/backend/pkg/payments"
	"github.com/kiflet/backend/pkg/sms"
	"github.com/kiflet/backend/pkg/storage"
	"github.com/kiflet/backend/pkg/websocket"
)

// Options change how the app is put together. The zero value is
// production's.
type Options struct {
	// Console is where the stand-in providers print what they would have
	// sent. Nil prints to standard output.
	Console *console.Console
	// NoJobs leaves the periodic jobs out of Run. The integration tests
	// share one database and run a job only when a test calls for it.
	NoJobs bool
}

// App is Kiflet, assembled: the roles its settings name (config.Roles).
type App struct {
	Services *service.Services
	web      *web.Server
	roles    config.Roles
	// workers are the background loops Run runs, besides the services'.
	workers []func(context.Context)
	broker  *broker.Broker
	redis   redis.UniversalClient
}

// New builds the app. It refuses stand-in providers in production unless
// the settings allow them.
func New(cfg *config.Config, pool *pgxpool.Pool, log zerolog.Logger, opts Options) (*App, error) {
	if mocked := cfg.MockedProviders(); cfg.IsProduction() && len(mocked) > 0 && !cfg.AllowMockProviders {
		return nil, fmt.Errorf("%s would be stand-ins in production, sending nothing and taking no money: configure real providers, "+
			"or set ALLOW_MOCK_PROVIDERS=true for a demo site", strings.Join(mocked, ", "))
	}
	if cfg.IsProduction() && cfg.SandboxURL != "" && !cfg.AllowMockProviders {
		return nil, errors.New("SANDBOX_URL is set, so the providers are the sandbox, which sends no texts and takes no money: " +
			"unset it in production, or set ALLOW_MOCK_PROVIDERS=true for a demo site")
	}
	out := opts.Console
	if out == nil {
		out = console.New(os.Stdout)
	}
	providers, err := paymentProviders(cfg)
	if err != nil {
		return nil, fmt.Errorf("payments: %w", err)
	}
	store, err := storage.New(&storage.Config{
		Type: cfg.StorageType, LocalPath: cfg.StoragePath,
		S3Bucket: cfg.S3Bucket, S3Region: cfg.S3Region, S3Endpoint: cfg.S3Endpoint,
		S3AccessKey: cfg.S3AccessKey, S3SecretKey: cfg.S3SecretKey, S3UseSSL: cfg.S3UseSSL,
		S3ForcePathStyle: cfg.S3ForcePathStyle, S3PublicEndpoint: cfg.S3PublicEndpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	// Opt-outs and pacing live in the service's outbox.
	provider, err := sms.NewProvider(sms.Config{
		Provider:           sms.ProviderType(cfg.SMSProvider),
		EthioTelecomAPIURL: cfg.EthioTelecomAPIURL, EthioTelecomAPIKey: cfg.EthioTelecomAPIKey, EthioTelecomSenderID: cfg.EthioTelecomSenderID,
		Console: out,
	}, smsLog{log.With().Str("component", "sms").Logger()})
	if err != nil {
		return nil, fmt.Errorf("sms: %w", err)
	}
	mailer, err := mail.New(mail.Config{
		Provider: cfg.MailProvider, Console: out,
		SMTP: mail.SMTP{Addr: cfg.SMTPAddr, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword, From: cfg.MailFrom},
	})
	if err != nil {
		return nil, fmt.Errorf("email: %w", err)
	}

	hub := websocket.NewHub(log.With().Str("component", "live").Logger())
	go hub.Run()
	roles := cfg.Roles
	a := &App{roles: roles}
	if cfg.RabbitMQURL != "" {
		a.broker, err = broker.Dial(context.Background(), cfg.RabbitMQURL, log.With().Str("component", "broker").Logger())
		if err != nil {
			return nil, err
		}
		if roles.Has("relay") {
			a.workers = append(a.workers, (&relay{pool: pool, broker: a.broker, log: log.With().Str("component", "relay").Logger()}).Run)
		}
	}
	// Updates reach the hub through RabbitMQ or Postgres, so that every
	// instance's dashboards get them. Every role publishes them; the web
	// processes, where dashboards connect, listen.
	liveLog := log.With().Str("component", "live").Logger()
	var live service.Publisher
	if cfg.WorkerLive == "queue" {
		bl := &brokerLive{broker: a.broker, hub: hub, log: liveLog}
		live = bl
		if roles.Has("web") {
			a.workers = append(a.workers, bl.Run)
		}
	} else {
		pl := &liveRelay{pool: pool, hub: hub, log: liveLog}
		live = pl
		if roles.Has("web") {
			a.workers = append(a.workers, pl.Run)
		}
	}
	var positions service.PositionQueue
	if cfg.WorkerTracking == "queue" {
		positions = brokerPositions{a.broker}
	}
	senders := map[string]bool{
		service.WorkSMS: cfg.WorkerSMS == "queue", service.WorkMail: cfg.WorkerMail == "queue",
		service.WorkWebhooks: cfg.WorkerWebhooks == "queue",
	}
	var work service.WorkQueue
	if senders[service.WorkSMS] || senders[service.WorkMail] || senders[service.WorkWebhooks] {
		work = brokerWork{b: a.broker, kinds: senders}
	}
	svc := service.New(service.Deps{
		Audit:    auditapp.New(pool, log),
		Tenancy:  tenancyapp.New(),
		Sessions: identityapp.NewSessions(pool, nil, cfg.SupportEmail),
		Codes:    identityapp.NewCodes(pool, cfg.Secret, nil),
		Links:    identityapp.NewLinks(pool),
		Signups:  identityapp.NewSignups(pool),
		Users:    identityapp.NewUsers(pool),
		Pool:     pool, Log: log, Publisher: live, Positions: positions, Outbox: a.broker != nil,
		RemoteWorkers: !roles.Has("worker"), Work: work,
		Storage: store, SMS: smsSender{provider}, Mail: mailSender{mailer},
		Payments: providers,
		Settings: service.Settings{
			AppName: cfg.AppName, BaseURL: cfg.PublicURL, Location: cfg.Timezone,
			Secret: cfg.Secret, SMSNumber: cfg.SMSNumber,
			SupportEmail: cfg.SupportEmail, BankDetails: cfg.BankDetails,
			RetainShipments: time.Duration(cfg.RetainShipmentDays) * 24 * time.Hour,
			CodesPerDay:     cfg.CodesPerDay,
			PrivateWebhooks: cfg.PrivateWebhooks,
		},
	})
	var rdb redis.UniversalClient
	if cfg.RedisURL != "" {
		if rdb, err = connectRedis(cfg.RedisURL, log); err != nil {
			return nil, err
		}
		a.redis = rdb
	}
	srv, err := web.New(cfg, svc, pool, hub, rdb, log)
	if err != nil {
		return nil, err
	}
	a.Services, a.web = svc, srv
	if roles.Has("worker") {
		a.workers = append(a.workers, svc.SMS.Run, svc.Mail.Run, svc.Webhooks.Run,
			(&workListener{pool: pool, svc: svc, log: log.With().Str("component", "work").Logger()}).Run)
		if positions != nil {
			a.workers = append(a.workers, func(ctx context.Context) {
				a.broker.Consume(ctx, "tracking.ingest", trackingPrefetch, storePositions(svc.Tracking))
			})
		}
		deliver := map[string]func(context.Context, []pgtype.UUID) error{
			service.WorkSMS: svc.SMS.DeliverQueued, service.WorkMail: svc.Mail.DeliverQueued,
			service.WorkWebhooks: svc.Webhooks.DeliverQueued,
		}
		for kind, on := range senders {
			if on {
				a.workers = append(a.workers, func(ctx context.Context) {
					a.broker.Consume(ctx, senderQueues[kind], senderPrefetch, deliverWork(deliver[kind]))
				})
			}
		}
	}
	if cfg.WorkerJobs == "queue" && roles.Has("worker") {
		a.workers = append(a.workers, func(ctx context.Context) {
			a.broker.Consume(ctx, "jobs.scheduled", jobsPrefetch, runJobs(svc.Jobs))
		})
	}
	if roles.Has("scheduler") && !opts.NoJobs && cfg.WorkerJobs == "queue" {
		a.workers = append(a.workers, func(ctx context.Context) {
			svc.Jobs.Schedule(ctx, brokerJobs{a.broker}.Publish)
		})
	} else if roles.Has("scheduler") && !opts.NoJobs {
		a.workers = append(a.workers, svc.Jobs.Run)
	}
	log.Info().Str("roles", roles.String()).Msg("roles")
	return a, nil
}

// Handler serves the dashboard, the public pages, the driver app and the
// APIs; in a process without the web role, only /healthz, /readyz and
// /metrics.
func (a *App) Handler() http.Handler {
	if !a.roles.Has("web") {
		return a.web.OpsHandler()
	}
	return a.web.Handler()
}

// Run does the background work of the process's roles until ctx ends:
// passing live updates to its dashboards (web); sending texts, email and
// webhooks, and writing GPS fixes (worker); the periodic jobs (scheduler);
// publishing the event outbox (relay). It returns once each has finished
// what it had in hand. Texts and email taken but not started are handed
// back, for the next start to send at once.
func (a *App) Run(ctx context.Context) {
	workers := a.workers
	var wg sync.WaitGroup
	for _, run := range workers {
		wg.Go(func() { run(ctx) })
	}
	wg.Wait()
}

// Close lets go of what New connected to, once Run has returned.
func (a *App) Close() {
	if a.broker != nil {
		a.broker.Close()
	}
	if a.redis != nil {
		_ = a.redis.Close()
	}
}

// connectRedis makes the Redis client. What the app keeps there can be
// counted again or fetched from Postgres, so Redis is answered quickly or
// not at all: short timeouts and no retries, and an app that starts while
// Redis is down still starts.
func connectRedis(url string, log zerolog.Logger) (redis.UniversalClient, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL: %w", err)
	}
	opts.ClientName = "kiflet"
	opts.DialTimeout = time.Second
	opts.ReadTimeout = 200 * time.Millisecond
	opts.WriteTimeout = 200 * time.Millisecond
	opts.MaxRetries = -1
	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Warn().Err(err).Msg("redis not answering; request limits are counted per instance until it does")
	}
	return rdb, nil
}

// paymentProviders are the mobile-money methods offered. The stand-ins are
// a telebirr whose checkout is a page on this server, and the real CBE Birr
// adapter, which only answers calls, called by a page playing CBE Birr.
func paymentProviders(cfg *config.Config) ([]payments.Provider, error) {
	cbe := payments.CBEBirrConfig{
		MerchantID: cfg.CBEBirr.MerchantID, Username: cfg.CBEBirr.CallbackUsername, Password: cfg.CBEBirr.CallbackPassword,
	}
	switch cfg.PaymentProvider {
	case "none":
		return nil, nil
	case "mock":
		checkout := func(reference string) string { return cfg.PublicURL + "/dev/payments/" + reference }
		cbe.StandIn = true
		standIn, err := payments.NewCBEBirr(cbe)
		if err != nil {
			return nil, fmt.Errorf("cbebirr: %w", err)
		}
		return []payments.Provider{payments.NewMock("telebirr", "telebirr", cfg.Secret, checkout), standIn}, nil
	}
	var out []payments.Provider
	for _, code := range cfg.PaymentProviders() {
		var p payments.Provider
		var err error
		switch code {
		case "telebirr":
			p, err = telebirr(cfg.Telebirr)
		case "cbebirr":
			p, err = payments.NewCBEBirr(cbe)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", code, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func telebirr(t config.TelebirrSettings) (*payments.Telebirr, error) {
	key, err := os.ReadFile(t.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("TELEBIRR_PRIVATE_KEY_FILE: %w", err)
	}
	pub, err := os.ReadFile(t.PublicKeyFile)
	if err != nil {
		return nil, fmt.Errorf("TELEBIRR_PUBLIC_KEY_FILE: %w", err)
	}
	return payments.NewTelebirr(payments.TelebirrConfig{
		APIURL: t.APIURL, CheckoutURL: t.CheckoutURL, FabricAppID: t.FabricAppID, AppSecret: t.AppSecret,
		MerchantAppID: t.MerchantAppID, MerchantCode: t.MerchantCode, PrivateKey: key, PublicKey: pub,
	})
}

// smsSender adapts the provider package to the service's outbox.
type smsSender struct{ p sms.SMSProvider }

func (s smsSender) Name() string { return s.p.Name() }
func (s smsSender) Send(ctx context.Context, to, body string) (string, error) {
	id, err := s.p.Send(ctx, to, body, sms.MessageTypeNotification)
	if errors.Is(err, sms.ErrRejected) {
		err = fmt.Errorf("%w: %w", service.ErrSMSRejected, err)
	}
	return id, err
}

// mailSender adapts the mail package to the service's outbox.
type mailSender struct{ s mail.Sender }

func (m mailSender) Name() string { return m.s.Name() }
func (m mailSender) Send(ctx context.Context, to, subject, body string) error {
	return m.s.Send(ctx, mail.Message{To: to, Subject: subject, Text: body})
}

// smsLog routes the provider's logging into ours.
type smsLog struct{ l zerolog.Logger }

func (s smsLog) Info(msg string, kv ...interface{})  { s.l.Info().Fields(kv).Msg(msg) }
func (s smsLog) Error(msg string, kv ...interface{}) { s.l.Error().Fields(kv).Msg(msg) }
func (s smsLog) Warn(msg string, kv ...interface{})  { s.l.Warn().Fields(kv).Msg(msg) }
func (s smsLog) Debug(msg string, kv ...interface{}) { s.l.Debug().Fields(kv).Msg(msg) }
