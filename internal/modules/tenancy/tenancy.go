// Package tenancy is the tenancy module (docs/architecture.md, section 4):
// the companies, their profile and settings. It owns tenants.
//
// Other modules reach it only through Companies. A write within another
// module's transaction takes that transaction as conn; a read may too, to
// see what the transaction has written.
package tenancy

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/tenancy/domain"
	"github.com/kiflet/backend/internal/modules/tenancy/store"
	"github.com/kiflet/backend/internal/platform/database"
)

// Tenant is a company.
type Tenant = store.Tenant

// Settings is the company settings form (domain.Settings).
type Settings = domain.Settings

// NewCompany is a company signing up, on the plan given.
type NewCompany struct {
	Name         string
	ContactPhone string
	ContactEmail string
	City         string
	Plan         string
	MaxDrivers   int32
	TrialEndsAt  time.Time
}

// Companies is what other modules may call.
type Companies interface {
	// Get loads a company; one that isn't there is an apperror NotFound.
	Get(ctx context.Context, conn database.Conn, id pgtype.UUID) (Tenant, error)
	// Create makes a company, with an address (its slug) of its own.
	Create(ctx context.Context, conn database.Conn, c NewCompany) (Tenant, error)
	// UpdateSettings checks and saves a company's settings, and returns
	// the company before and after.
	UpdateSettings(ctx context.Context, conn database.Conn, id pgtype.UUID, in Settings) (before, after Tenant, err error)
	// SetActive suspends or restores a company.
	SetActive(ctx context.Context, conn database.Conn, id pgtype.UUID, active bool) error
}
