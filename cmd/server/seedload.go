package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	identityapp "github.com/kiflet/backend/internal/modules/identity/app"
	"io"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/kiflet/backend/internal/app"
	"github.com/kiflet/backend/internal/model"
	"github.com/kiflet/backend/internal/platform/config"
	"github.com/kiflet/backend/internal/service"
	"github.com/kiflet/backend/pkg/hash"
)

// loadPIN is every load-test driver's PIN.
const loadPIN = "4826"

// loadPhone is what tests/load/sync.js plays: a driver on the road, signed
// in on a phone.
type loadPhone struct {
	Token      string  `json:"token"`
	ShipmentID string  `json:"shipment_id"`
	Lat        float64 `json:"lat"`
	Lng        float64 `json:"lng"`
}

// The road the load test's trucks are on, Addis Ababa to Adama.
var loadFrom, loadTo = [2]float64{9.0108, 38.7613}, [2]float64{8.5400, 39.2700}

// seedLoad makes a load test's companies, drivers and shipments on the
// road, signs every driver in, and writes their tokens and shipments as
// JSON to --out for k6 (tests/load). It refuses unless --on names the
// host of PUBLIC_URL, so it isn't run against the wrong site by mistake.
func seedLoad(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, log zerolog.Logger, args []string) error {
	fs := flag.NewFlagSet("seed-load", flag.ContinueOnError)
	companies := fs.Int("companies", 60, "companies to make")
	perCompany := fs.Int("drivers", 200, "drivers in each company")
	out := fs.String("out", "-", "where to write the phones, - for standard output")
	on := fs.String("on", "", "the host of PUBLIC_URL, to confirm the site")
	if err := fs.Parse(args); err != nil {
		return err
	}
	u, err := url.Parse(cfg.PublicURL)
	if err != nil || *on == "" || *on != u.Hostname() {
		return fmt.Errorf("seed-load adds %d drivers to this site's data: confirm it with --on=%s", *companies**perCompany, u.Hostname())
	}
	if *companies < 1 || *perCompany < 1 || *companies > 9_000 || *perCompany > 999 {
		return errors.New("seed-load: from 1 to 9000 companies of 1 to 999 drivers")
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE email = 'load-1@kiflet.test')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return errors.New("seed-load: the load test's companies are here already (load-1@kiflet.test)")
	}

	a, err := app.New(cfg, pool, log, app.Options{NoJobs: true})
	if err != nil {
		return err
	}
	defer a.Close()
	pinHash, err := hash.PIN(loadPIN)
	if err != nil {
		return err
	}
	w := io.Writer(os.Stdout)
	if *out != "-" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close() //nolint:errcheck // closed after writing, below
		w = f
	}

	var (
		mu     sync.Mutex
		phones []loadPhone
		errs   []error
		wg     sync.WaitGroup
		slots  = make(chan struct{}, 8)
		start  = time.Now()
	)
	for c := 1; c <= *companies; c++ {
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			got, err := loadCompany(ctx, a.Services, pool, pinHash, c, *perCompany)
			mu.Lock()
			defer mu.Unlock()
			phones = append(phones, got...)
			if err != nil {
				errs = append(errs, fmt.Errorf("company %d: %w", c, err))
			}
			log.Info().Int("company", c).Int("phones", len(phones)).Dur("took", time.Since(start)).Msg("load test company")
		})
		if ctx.Err() != nil {
			break
		}
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if err := json.NewEncoder(w).Encode(phones); err != nil {
		return err
	}
	if f, ok := w.(*os.File); ok && f != os.Stdout {
		return f.Close()
	}
	return nil
}

// loadCompany makes one company on the Enterprise plan, with its drivers
// each on the road and signed in.
func loadCompany(ctx context.Context, svc *service.Services, pool *pgxpool.Pool, pinHash string, c, drivers int) ([]loadPhone, error) {
	ci := service.ClientInfo{IP: "127.0.0.1", UserAgent: "seed-load"}
	res, err := svc.Tenants.CreateCompany(ctx, service.SignupInput{
		Company: fmt.Sprintf("Load Freight %d", c), City: "Addis Ababa", AdminName: "Load Admin",
		Email: fmt.Sprintf("load-%d@kiflet.test", c), Phone: loadPhoneNumber(c, 0), Password: "load-test-password-1",
	}, ci)
	if err != nil {
		return nil, err
	}
	admin := res.Principal
	if _, err := pool.Exec(ctx, `UPDATE tenants SET plan = 'enterprise', max_drivers = $2, sms_quota = 1000000, sms_month_quota = 1000000, trial_ends_at = NULL WHERE id = $1`,
		admin.TenantID, drivers); err != nil {
		return nil, err
	}
	office := admin.Actor(service.SourceDashboard, ci.IP, ci.UserAgent)
	phones := make([]loadPhone, 0, drivers)
	for d := 1; d <= drivers; d++ {
		if err := ctx.Err(); err != nil {
			return phones, err
		}
		number := loadPhoneNumber(c, d)
		acct, err := svc.Users.CreateDriver(ctx, admin, service.PersonInput{FullName: fmt.Sprintf("Load Driver %d-%d", c, d), Phone: number}, ci)
		if err != nil {
			return phones, fmt.Errorf("driver %d: %w", d, err)
		}
		if err := identityapp.NewUsers(pool).SetPIN(ctx, pool, acct.User.ID, pinHash, false); err != nil {
			return phones, err
		}
		// Somewhere along the road, so that the trucks spread out.
		f := float64(d) / float64(drivers+1)
		lat, lng := loadFrom[0]+(loadTo[0]-loadFrom[0])*f, loadFrom[1]+(loadTo[1]-loadFrom[1])*f
		sh, err := svc.Shipments.Create(ctx, office, service.ShipmentInput{
			Priority: model.PriorityNormal, OriginCity: "Addis Ababa", OriginAddress: "Merkato",
			OriginLat: &loadFrom[0], OriginLng: &loadFrom[1],
			DestinationCity: "Adama", DestinationAddress: "Main road",
			DestinationLat: &loadTo[0], DestinationLng: &loadTo[1],
			CustomerName: "Load Customer", CustomerPhone: loadPhoneNumber(c, 0),
			CargoDescription: "Teff", CargoWeightKg: 1000, DriverID: acct.User.ID.String(),
		})
		if err != nil {
			return phones, fmt.Errorf("shipment %d: %w", d, err)
		}
		driver := service.Actor{UserID: acct.User.ID, TenantID: admin.TenantID, Name: acct.User.FullName, Role: model.RoleDriver, Source: service.SourceApp}
		if _, err := svc.Shipments.ChangeStatus(ctx, driver, sh.ID, service.StatusChange{Status: model.StatusInTransit, By: model.ActorDriver}); err != nil {
			return phones, fmt.Errorf("depart %d: %w", d, err)
		}
		login, err := svc.Auth.DriverLogin(ctx, number, loadPIN, ci)
		if err != nil {
			return phones, fmt.Errorf("sign in %d: %w", d, err)
		}
		phones = append(phones, loadPhone{Token: login.Token, ShipmentID: sh.ID.String(), Lat: lat, Lng: lng})
	}
	return phones, nil
}

// loadPhoneNumber is a load-test number: 098, the company, the driver (0 is
// the company's admin). Their texts go wherever the site's SMS provider
// sends them: the sandbox, on staging.
func loadPhoneNumber(company, driver int) string {
	return fmt.Sprintf("098%04d%03d", company, driver)
}
