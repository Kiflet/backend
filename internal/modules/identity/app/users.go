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

// Users is identity.Users on Postgres.
type Users struct{ q *store.Queries }

var _ identity.Users = Users{}

// NewUsers is the people, read through conn (the pool) when a call has no
// connection of its own.
func NewUsers(conn database.Conn) Users { return Users{q: store.New(conn)} }

func found(u identity.User, err error) (identity.User, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return u, identity.ErrNoUser
	}
	return u, err
}

func (Users) Get(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID) (identity.User, error) {
	return found(store.New(conn).GetUserByID(ctx, store.GetUserByIDParams{ID: id, TenantID: tenantID}))
}

func (Users) ByPhone(ctx context.Context, conn database.Conn, phone string) (identity.User, error) {
	return found(store.New(conn).GetUserByPhone(ctx, &phone))
}

func (Users) ByEmail(ctx context.Context, conn database.Conn, email string) (identity.User, error) {
	return found(store.New(conn).GetUserByEmail(ctx, email))
}

func (Users) Staff(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) ([]identity.User, error) {
	return store.New(conn).ListUsersByTenant(ctx, store.ListUsersByTenantParams{TenantID: tenantID, Limit: 500})
}

func (Users) CountStaff(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) (int64, error) {
	return store.New(conn).CountStaffByTenant(ctx, tenantID)
}

func (Users) CountDrivers(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) (int64, error) {
	return store.New(conn).CountActiveDriversByTenant(ctx, tenantID)
}

func (u Users) Names(ctx context.Context, ids []pgtype.UUID) (map[pgtype.UUID]string, error) {
	rows, err := u.q.UserNamesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[pgtype.UUID]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.FullName
	}
	return out, nil
}

func (Users) Create(ctx context.Context, conn database.Conn, n identity.NewUser) (identity.User, error) {
	return store.New(conn).CreateUser(ctx, store.CreateUserParams(n))
}

func (Users) UpdateProfile(ctx context.Context, conn database.Conn, p identity.Profile) (identity.User, error) {
	return found(store.New(conn).UpdateUserProfile(ctx, store.UpdateUserProfileParams(p)))
}

func (Users) SetRole(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID, role string) error {
	return store.New(conn).SetUserRole(ctx, store.SetUserRoleParams{ID: id, TenantID: tenantID, Role: role})
}

func (Users) SetActive(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID, active bool) error {
	return store.New(conn).SetUserActive(ctx, store.SetUserActiveParams{ID: id, TenantID: tenantID, IsActive: active})
}

func (Users) FailedLogin(ctx context.Context, conn database.Conn, id pgtype.UUID) (int32, error) {
	u, err := store.New(conn).IncrementFailedLoginAttempts(ctx, id)
	return u.FailedLoginAttempts, err
}

func (Users) Lock(ctx context.Context, conn database.Conn, id pgtype.UUID, until time.Time) error {
	return store.New(conn).LockUserAccount(ctx, store.LockUserAccountParams{ID: id, LockedUntil: pgtype.Timestamptz{Time: until, Valid: true}})
}

func (Users) LoggedIn(ctx context.Context, conn database.Conn, id pgtype.UUID) error {
	return store.New(conn).RecordSuccessfulLogin(ctx, id)
}

func (Users) SetPassword(ctx context.Context, conn database.Conn, id pgtype.UUID, hash string, mustChange bool) error {
	return store.New(conn).UpdatePassword(ctx, store.UpdatePasswordParams{ID: id, PasswordHash: &hash, PasswordResetRequired: mustChange})
}

func (Users) SetPIN(ctx context.Context, conn database.Conn, id pgtype.UUID, hash string, mustChange bool) error {
	return store.New(conn).UpdatePIN(ctx, store.UpdatePINParams{ID: id, PinHash: &hash, PasswordResetRequired: mustChange})
}

func (Users) ResetPassword(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID, hash string) error {
	return store.New(conn).ResetUserPassword(ctx, store.ResetUserPasswordParams{ID: id, TenantID: tenantID, PasswordHash: &hash})
}

func (Users) VerifyEmail(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID, email string) (bool, error) {
	n, err := store.New(conn).SetEmailVerified(ctx, store.SetEmailVerifiedParams{ID: id, TenantID: tenantID, Email: &email})
	return n > 0, err
}
