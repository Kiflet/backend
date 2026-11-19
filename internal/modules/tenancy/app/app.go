// Package app is the tenancy module's implementation of
// tenancy.Companies.
package app

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/tenancy"
	"github.com/kiflet/backend/internal/modules/tenancy/store"
	"github.com/kiflet/backend/internal/platform/apperror"
	"github.com/kiflet/backend/internal/platform/database"
)

// Service is tenancy.Companies on Postgres.
type Service struct{}

var _ tenancy.Companies = Service{}

// New is the tenancy module.
func New() Service { return Service{} }

func (Service) Get(ctx context.Context, conn database.Conn, id pgtype.UUID) (tenancy.Tenant, error) {
	t, err := store.New(conn).GetTenantByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, apperror.NotFound("Company not found.")
	}
	return t, err
}

func (Service) Create(ctx context.Context, conn database.Conn, c tenancy.NewCompany) (tenancy.Tenant, error) {
	q := store.New(conn)
	base := slugify(c.Name)
	for attempt := 0; ; attempt++ {
		slug := base
		if attempt > 0 {
			slug = base + "-" + digits(4)
		}
		t, err := q.CreateTenant(ctx, store.CreateTenantParams{
			Name: c.Name, Slug: slug, Plan: c.Plan, MaxDrivers: c.MaxDrivers,
			ContactPhone: text(c.ContactPhone), ContactEmail: text(c.ContactEmail), RegionCity: c.City,
			TrialEndsAt: pgtype.Timestamptz{Time: c.TrialEndsAt, Valid: !c.TrialEndsAt.IsZero()},
		})
		if errors.Is(err, pgx.ErrNoRows) && attempt < 5 { // the slug is taken
			continue
		}
		return t, err
	}
}

func (s Service) UpdateSettings(ctx context.Context, conn database.Conn, id pgtype.UUID, in tenancy.Settings) (before, after tenancy.Tenant, err error) {
	if before, err = s.Get(ctx, conn, id); err != nil {
		return before, after, err
	}
	c, err := in.Clean(before.CustomerLanguage)
	if err != nil {
		return before, after, err
	}
	after, err = store.New(conn).UpdateTenantSettings(ctx, store.UpdateTenantSettingsParams{
		ID: id, Name: c.Name, ContactPhone: text(c.ContactPhone), ContactEmail: text(c.ContactEmail),
		RegionCity: c.RegionCity, MaxActivePerDriver: int32(c.MaxActivePerDriver),
		SmsOnCreated: c.SMSOnCreated, SmsOnDispatched: c.SMSOnDispatched, SmsOnArrived: c.SMSOnArrived,
		SmsOnDelivered: c.SMSOnDelivered, SmsOnCancelled: c.SMSOnCancelled, SmsOnDelayed: c.SMSOnDelayed,
		SmsDriverOnAssign: c.SMSDriverOnAssign, Tin: text(c.TIN), AlertOnDelivery: c.AlertOnDelivery,
		CustomerLanguage: c.CustomerLanguage,
	})
	return before, after, err
}

func (Service) SetActive(ctx context.Context, conn database.Conn, id pgtype.UUID, active bool) error {
	return store.New(conn).SetTenantActive(ctx, store.SetTenantActiveParams{ID: id, IsActive: active})
}

// slugify is a company's name as an address: lower-case ASCII letters and
// digits, with dashes between words, 40 characters at most.
func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	if out == "" {
		out = "company"
	}
	return out
}

// digits is n random decimal digits.
func digits(n int) string {
	var b strings.Builder
	for range n {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			panic(err) // the system's randomness is gone
		}
		b.WriteByte(byte('0' + d.Int64()))
	}
	return b.String()
}

func text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
