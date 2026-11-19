package app

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/identity"
	"github.com/kiflet/backend/internal/modules/identity/store"
	"github.com/kiflet/backend/internal/platform/database"
)

// Signups is identity.Signups on Postgres.
type Signups struct{ q *store.Queries }

var _ identity.Signups = Signups{}

// NewSignups is the waiting sign-ups, read through conn (the pool).
func NewSignups(conn database.Conn) Signups { return Signups{q: store.New(conn)} }

func (s Signups) Hold(ctx context.Context, n identity.NewSignup) (string, error) {
	token := randomToken(32)
	err := s.q.CreatePendingSignup(ctx, store.CreatePendingSignupParams{
		TokenHash: HashToken(token), Company: n.Company, City: n.City, FullName: n.FullName,
		Email: n.Email, Phone: n.Phone, PasswordHash: n.PasswordHash,
		Plan: n.Plan, BillingCycle: n.BillingCycle, ExpiresAt: pgtype.Timestamptz{Time: n.Expires, Valid: true},
	})
	return token, err
}

func (Signups) Find(ctx context.Context, conn database.Conn, token string) (identity.PendingSignup, error) {
	ps, err := store.New(conn).GetPendingSignup(ctx, HashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return ps, identity.ErrSignupExpired
	}
	return ps, err
}

func (Signups) Done(ctx context.Context, conn database.Conn, id pgtype.UUID) error {
	return store.New(conn).DeletePendingSignup(ctx, id)
}

func (s Signups) DeleteExpired(ctx context.Context, before time.Time) error {
	return s.q.DeleteExpiredPendingSignups(ctx, pgtype.Timestamptz{Time: before, Valid: true})
}
