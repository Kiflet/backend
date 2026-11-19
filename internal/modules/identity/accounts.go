package identity

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/identity/store"
	"github.com/kiflet/backend/internal/platform/apperror"
	"github.com/kiflet/backend/internal/platform/database"
)

// Codes are the one-time codes sent by SMS: a driver's PIN reset, a staff
// password reset, a customer's lookup and the phone check at sign-up. The
// purpose names which; a code works only for its own.
type Codes interface {
	// Issue makes a code for a phone and purpose and returns it to be
	// sent. conn must be a transaction: requests for one number and
	// purpose wait for each other in it, so requests sent together can't
	// all pass the hourly limit. allow, if given, runs once that limit has
	// passed and may refuse the code (the platform's daily SMS budget).
	Issue(ctx context.Context, conn database.Conn, phone, purpose string, allow func(context.Context) error) (code string, err error)
	// Verify checks a code without using it up, and returns its ID for
	// Consume. The try counts whatever happens next, outside any
	// transaction of the caller's.
	Verify(ctx context.Context, phone, purpose, code string) (pgtype.UUID, error)
	// Consume uses up a code Verify accepted. In the caller's
	// transaction, the code is spent only if what it unlocks is saved
	// too; a code used meanwhile by another request is ErrCodeInvalid.
	Consume(ctx context.Context, conn database.Conn, id pgtype.UUID) error
	// DeleteExpired removes codes a day after they expired.
	DeleteExpired(ctx context.Context) error
}

// CodeTTL is how long a code works: a text that couldn't be sent within it
// isn't sent late.
const CodeTTL = 10 * time.Minute

// The errors of a code. Their Key is the customer text (internal/platform/text).
var (
	ErrCodeInvalid  = &apperror.Error{Kind: apperror.KindInvalid, Field: "code", Message: "That code is wrong or has expired. Request a new one.", Key: "error.code_invalid"}
	ErrCodeTooMany  = &apperror.Error{Kind: apperror.KindLimit, Message: "Too many codes requested for this number. Try again in an hour.", Key: "error.code_too_many"}
	ErrCodeAttempts = &apperror.Error{Kind: apperror.KindInvalid, Field: "code", Message: "Too many wrong attempts. Request a new code.", Key: "error.code_attempts"}
)

// Kinds of email link.
const (
	LinkPasswordReset = "password_reset"
	LinkEmailVerify   = "email_verify"
)

// ErrLinkExpired is a link that has expired, has been used or no longer
// counts.
var ErrLinkExpired = &apperror.Error{Kind: apperror.KindNotFound, Message: "This link has expired or has already been used."}

// Link is an email link that still works, and whose it is.
type Link struct {
	ID       pgtype.UUID
	TenantID pgtype.UUID
	UserID   pgtype.UUID
	// Email is the address the link was sent to.
	Email    string
	FullName string
	Role     string
}

// Links are the links sent by email. Only a hash of each token is kept.
type Links interface {
	// Create stores a link for a person's address and returns its token.
	Create(ctx context.Context, conn database.Conn, tenantID, userID pgtype.UUID, purpose, email string, expires time.Time) (token string, err error)
	// Sent counts the links of a purpose sent to a person since a time.
	Sent(ctx context.Context, tenantID, userID pgtype.UUID, purpose string, since time.Time) (int64, error)
	// Find is the link a token stands for, locked in conn's transaction if
	// it has one. A link that has expired, has been used, or whose person
	// is a driver, inactive or now has another address is ErrLinkExpired.
	// Whether the company is active is the tenancy module's to say.
	Find(ctx context.Context, conn database.Conn, token, purpose string) (Link, error)
	// ExpireAll stops a person's other links of a purpose working.
	ExpireAll(ctx context.Context, conn database.Conn, tenantID, userID pgtype.UUID, purpose string) error
	// DeleteOld removes links made before a time.
	DeleteOld(ctx context.Context, before time.Time) error
}

// PendingSignup is a sign-up waiting for its code.
type PendingSignup = store.PendingSignup

// NewSignup is a sign-up to hold until its code comes back.
type NewSignup struct {
	Company      string
	City         string
	FullName     string
	Email        string
	Phone        string
	PasswordHash string
	Plan         string
	BillingCycle string
	Expires      time.Time
}

// ErrSignupExpired is a sign-up that has expired or is complete.
var ErrSignupExpired = &apperror.Error{Kind: apperror.KindNotFound, Message: "This sign-up has expired or is already complete. If you finished it, sign in; otherwise fill in the form again."}

// Signups are sign-ups waiting for their SMS code.
type Signups interface {
	// Hold keeps a sign-up and returns the token that names it to the
	// browser that started it.
	Hold(ctx context.Context, s NewSignup) (token string, err error)
	// Find is the sign-up a token names, locked in conn's transaction if
	// it has one; one gone or expired is ErrSignupExpired.
	Find(ctx context.Context, conn database.Conn, token string) (PendingSignup, error)
	// Done removes a sign-up once its company exists.
	Done(ctx context.Context, conn database.Conn, id pgtype.UUID) error
	// DeleteExpired removes sign-ups expired before a time.
	DeleteExpired(ctx context.Context, before time.Time) error
}
