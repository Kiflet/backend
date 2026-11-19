package app

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/identity"
	"github.com/kiflet/backend/internal/modules/identity/domain"
	"github.com/kiflet/backend/internal/modules/identity/store"
	"github.com/kiflet/backend/internal/platform/database"
)

// Links is identity.Links on Postgres.
type Links struct{ q *store.Queries }

var _ identity.Links = Links{}

// NewLinks is the email links, read through conn (the pool).
func NewLinks(conn database.Conn) Links { return Links{q: store.New(conn)} }

func (Links) Create(ctx context.Context, conn database.Conn, tenantID, userID pgtype.UUID, purpose, email string, expires time.Time) (string, error) {
	token := randomToken(32)
	err := store.New(conn).CreateEmailToken(ctx, store.CreateEmailTokenParams{
		TenantID: tenantID, UserID: userID, Purpose: purpose, Email: email,
		TokenHash: HashToken(token), ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	})
	return token, err
}

func (l Links) Sent(ctx context.Context, tenantID, userID pgtype.UUID, purpose string, since time.Time) (int64, error) {
	return l.q.CountEmailTokensSince(ctx, store.CountEmailTokensSinceParams{
		TenantID: tenantID, UserID: userID, Purpose: purpose, CreatedAt: pgtype.Timestamptz{Time: since, Valid: true},
	})
}

func (Links) Find(ctx context.Context, conn database.Conn, token, purpose string) (identity.Link, error) {
	t, err := store.New(conn).GetEmailToken(ctx, store.GetEmailTokenParams{TokenHash: HashToken(token), Purpose: purpose})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Link{}, identity.ErrLinkExpired
	}
	if err != nil {
		return identity.Link{}, err
	}
	// A link counts only while it goes to the person's current address,
	// and drivers have none to reset by.
	if !t.IsActive || t.Role == domain.RoleDriver || t.CurrentEmail == nil || *t.CurrentEmail != t.Email {
		return identity.Link{}, identity.ErrLinkExpired
	}
	return identity.Link{ID: t.ID, TenantID: t.TenantID, UserID: t.UserID, Email: t.Email, FullName: t.FullName, Role: t.Role}, nil
}

func (Links) ExpireAll(ctx context.Context, conn database.Conn, tenantID, userID pgtype.UUID, purpose string) error {
	return store.New(conn).ExpireEmailTokens(ctx, store.ExpireEmailTokensParams{TenantID: tenantID, UserID: userID, Purpose: purpose})
}

func (l Links) DeleteOld(ctx context.Context, before time.Time) error {
	return l.q.DeleteOldEmailTokens(ctx, pgtype.Timestamptz{Time: before, Valid: true})
}
