// Package identity is the identity module (docs/architecture.md, section
// 4): who is calling, with what role, through which session. It owns
// users, sessions, verification codes, email links and pending sign-ups;
// this package holds what other modules may use of it, and Sessions.
package identity

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/identity/domain"
	"github.com/kiflet/backend/internal/platform/apperror"
	"github.com/kiflet/backend/internal/platform/database"
)

// Principal is the authenticated caller of a request, with what the
// request needs to know of their company.
type Principal struct {
	SessionID  pgtype.UUID
	UserID     pgtype.UUID
	TenantID   pgtype.UUID
	Role       string
	Name       string
	Phone      string
	Email      string
	TenantName string
	TenantPlan string
	TrialEnds  *time.Time
	// SubscriptionEnds is when the company's plan ends, if its renewal was
	// turned off.
	SubscriptionEnds *time.Time
	// DebtNumber and DebtDue are the company's oldest unpaid renewal or
	// usage invoice, if any: read-only mode follows from them.
	DebtNumber string
	DebtDue    *time.Time
	// MustChangeCredential is set after an admin reset or on first login
	// with an issued credential. The caller may do nothing else until it is
	// changed.
	MustChangeCredential bool
	// EmailVerified reports whether Email has been confirmed by a link.
	EmailVerified bool
}

func (p *Principal) IsDriver() bool         { return p.Role == domain.RoleDriver }
func (p *Principal) CanManageFleet() bool   { return domain.CanManageFleet(p.Role) }
func (p *Principal) CanManageCompany() bool { return domain.CanManageCompany(p.Role) }
func (p *Principal) IsPlatformAdmin() bool  { return p.Role == domain.RolePlatformAdmin }

// Actor builds the record of who did something, for history and audit.
func (p *Principal) Actor(source, ip, userAgent string) Actor {
	return Actor{
		UserID: p.UserID, TenantID: p.TenantID, Name: p.Name, Role: p.Role,
		Source: source, IP: ip, UserAgent: userAgent,
	}
}

// Actor is who performed an action and through what channel.
type Actor struct {
	UserID pgtype.UUID
	// KeyID is the API key that acted, when one did.
	KeyID     pgtype.UUID
	TenantID  pgtype.UUID
	Name      string
	Role      string
	Source    string
	IP        string
	UserAgent string
}

// IsDriver reports whether a driver acted.
func (a Actor) IsDriver() bool { return a.Role == domain.RoleDriver }

// Where an action came from, matching the shipment_events check
// constraint.
const (
	SourceDashboard = "dashboard"
	SourceApp       = "app"
	SourceSMS       = "sms"
	SourceSystem    = "system"
	SourceCustomer  = "customer"
	SourceAPI       = "api"
)

// SystemActor is the platform acting on its own, such as a scheduled
// check.
func SystemActor(tenantID pgtype.UUID) Actor {
	return Actor{TenantID: tenantID, Name: "System", Source: SourceSystem}
}

// ClientInfo is where a request came from.
type ClientInfo struct {
	IP        string
	UserAgent string
	DeviceID  string
}

// LoginResult is a new session.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	Principal *Principal
}

// Session kinds: "api" sessions carry a bearer token (the driver app);
// "web" sessions a cookie (the dashboard).
const (
	SessionAPI = "api"
	SessionWeb = "web"
)

// The errors of signing in and of a session.
var (
	ErrSessionRevoked  = &apperror.Error{Kind: apperror.KindUnauthorized, Message: "Your session has ended. Please sign in again."}
	ErrAccountInactive = &apperror.Error{Kind: apperror.KindForbidden, Message: "This account has been deactivated. Contact your administrator."}
	ErrSessionIdle     = &apperror.Error{Kind: apperror.KindUnauthorized, Message: "You were signed out after 8 hours without activity."}
)

// Suspended is the error for a suspended company's people: they can't
// sign in to ask on the Help page, so it says where to write.
func Suspended(supportEmail string) error {
	return &apperror.Error{Kind: apperror.KindForbidden, Code: "tenant_suspended", Message: "This company account is suspended. Write to " + supportEmail + " to have it restored."}
}

// Session is a signed-in device or browser.
type Session struct {
	ID        pgtype.UUID
	UserID    pgtype.UUID
	DeviceID  string
	UserAgent string
	IP        string
	Kind      string
	Remember  bool
	CreatedAt time.Time
	// LastSeen is zero for a session not used since it began.
	LastSeen  time.Time
	ExpiresAt time.Time
}

// Sessions is what other modules may call of the sessions.
type Sessions interface {
	// Start signs a user in: a new session, of kind SessionAPI or
	// SessionWeb, and the caller it is.
	Start(ctx context.Context, conn database.Conn, userID, tenantID pgtype.UUID, kind string, remember bool, ci ClientInfo) (*LoginResult, error)
	// Authenticate is the caller a token belongs to, keeping the session
	// alive. A session that has ended is ErrSessionRevoked or
	// ErrSessionIdle; a deactivated account ErrAccountInactive (and its
	// sessions end); a suspended company Suspended.
	Authenticate(ctx context.Context, token, kind string) (*Principal, error)
	// Active says whether a session is still good.
	Active(ctx context.Context, id pgtype.UUID) (bool, error)
	// List is a user's active sessions.
	List(ctx context.Context, userID pgtype.UUID) ([]Session, error)
	// End ends a session.
	End(ctx context.Context, id pgtype.UUID) error
	// EndUsers ends a user's session, if it is theirs.
	EndUsers(ctx context.Context, userID, id pgtype.UUID) error
	// EndAll ends every session of a user.
	EndAll(ctx context.Context, conn database.Conn, userID pgtype.UUID) error
	// EndOthers ends every session of a user but one.
	EndOthers(ctx context.Context, conn database.Conn, userID, keep pgtype.UUID) error
	// DeleteExpired removes sessions that ended.
	DeleteExpired(ctx context.Context) error
}
