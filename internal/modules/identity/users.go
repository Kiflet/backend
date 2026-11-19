package identity

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/identity/store"
	"github.com/kiflet/backend/internal/platform/apperror"
	"github.com/kiflet/backend/internal/platform/database"
)

// User is a person with an account: office staff, a driver or the
// platform's operator. Its driver profile columns (VehicleID,
// LicenceExpiresOn, the app settings) are the fleet module's.
type User = store.User

// ErrNoUser is a person who isn't there, or isn't in that company.
var ErrNoUser = apperror.NotFound("Person not found.")

// NewUser is an account to create. Errors from a phone or email already in
// use are the database's unique violations (users_phone_key,
// users_email_key), for the caller to word.
type NewUser struct {
	TenantID     pgtype.UUID
	Role         string
	FullName     string
	Phone        *string
	Email        *string
	PasswordHash *string
	PinHash      *string
	// PasswordResetRequired makes the person choose their own password or
	// PIN when they first sign in.
	PasswordResetRequired bool
	// VehicleID is a driver's vehicle, until fleet moves.
	VehicleID pgtype.UUID
}

// Profile is what a person's details form changes. A new email address has
// to be confirmed again.
type Profile struct {
	ID       pgtype.UUID
	TenantID pgtype.UUID
	FullName string
	Phone    *string
	Email    *string
}

// Users is what other modules may call of the people. A write within
// another module's transaction takes it as conn; so does a read that must
// see what the transaction wrote.
type Users interface {
	// Get, ByPhone and ByEmail find a person; one not there is ErrNoUser.
	// Get finds them only in their own company.
	Get(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID) (User, error)
	ByPhone(ctx context.Context, conn database.Conn, phone string) (User, error)
	ByEmail(ctx context.Context, conn database.Conn, email string) (User, error)
	// Staff is a company's office accounts, active first, at most 500.
	Staff(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) ([]User, error)
	CountStaff(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) (int64, error)
	// CountDrivers counts a company's active drivers, which its plan
	// limits.
	CountDrivers(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) (int64, error)
	// Names are people's names by ID, from every company: for the
	// platform's operator only, which the caller checks.
	Names(ctx context.Context, ids []pgtype.UUID) (map[pgtype.UUID]string, error)

	Create(ctx context.Context, conn database.Conn, u NewUser) (User, error)
	UpdateProfile(ctx context.Context, conn database.Conn, p Profile) (User, error)
	// SetRole changes a staff member's role; a driver's stays.
	SetRole(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID, role string) error
	SetActive(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID, active bool) error

	// FailedLogin counts a wrong credential and returns how many in a row.
	FailedLogin(ctx context.Context, conn database.Conn, id pgtype.UUID) (int32, error)
	// Lock refuses sign-in until a time, and starts the count again.
	Lock(ctx context.Context, conn database.Conn, id pgtype.UUID, until time.Time) error
	// LoggedIn records a sign-in and clears the count and any lock.
	LoggedIn(ctx context.Context, conn database.Conn, id pgtype.UUID) error
	// SetPassword and SetPIN store a credential's hash, clearing the count
	// and any lock; mustChange asks for another at the next sign-in.
	SetPassword(ctx context.Context, conn database.Conn, id pgtype.UUID, hash string, mustChange bool) error
	SetPIN(ctx context.Context, conn database.Conn, id pgtype.UUID, hash string, mustChange bool) error
	// ResetPassword sets a password from a reset link, which also proves
	// the address.
	ResetPassword(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID, hash string) error
	// VerifyEmail confirms a person's address, if it is still theirs and
	// not yet confirmed, and says whether it did.
	VerifyEmail(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID, email string) (bool, error)
}
