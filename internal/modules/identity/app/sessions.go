// Package app is the identity module's implementation.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/identity"
	"github.com/kiflet/backend/internal/modules/identity/domain"
	"github.com/kiflet/backend/internal/modules/identity/store"
	"github.com/kiflet/backend/internal/platform/database"
)

const (
	// Drivers stay signed in while they use the app: a driver days into a
	// trip without signal must not come back to a login screen.
	apiSessionTTL = 30 * 24 * time.Hour
	// Dashboard sessions end after eight idle hours, or last thirty days
	// with "remember me" (NFR security).
	webIdleTimeout = 8 * time.Hour
	webRememberTTL = 30 * 24 * time.Hour

	touchInterval = time.Minute
)

// Sessions is identity.Sessions on Postgres.
type Sessions struct {
	q   *store.Queries
	now func() time.Time
	// supportEmail is where a suspended company's people are told to write.
	supportEmail string
}

var _ identity.Sessions = (*Sessions)(nil)

// NewSessions is the sessions, reading through conn (the pool).
func NewSessions(conn database.Conn, now func() time.Time, supportEmail string) *Sessions {
	if now == nil {
		now = time.Now
	}
	return &Sessions{q: store.New(conn), now: now, supportEmail: supportEmail}
}

func (s *Sessions) Start(ctx context.Context, conn database.Conn, userID, tenantID pgtype.UUID, kind string, remember bool, ci identity.ClientInfo) (*identity.LoginResult, error) {
	token := randomToken(32)
	now := s.now()
	var expires time.Time
	switch {
	case kind == identity.SessionAPI:
		expires = now.Add(apiSessionTTL)
	case remember:
		expires = now.Add(webRememberTTL)
	default:
		expires = now.Add(webIdleTimeout)
	}
	sess, err := store.New(conn).CreateSession(ctx, store.CreateSessionParams{
		UserID:           userID,
		TenantID:         tenantID,
		RefreshTokenHash: HashToken(token),
		DeviceID:         text(truncate(ci.DeviceID, 100)),
		UserAgent:        text(truncate(ci.UserAgent, 300)),
		IpAddress:        text(ci.IP),
		ExpiresAt:        pgtype.Timestamptz{Time: expires, Valid: true},
		Kind:             kind,
		Remember:         remember,
	})
	if err != nil {
		return nil, err
	}
	p, err := s.authenticate(ctx, store.New(conn), token, kind)
	if err != nil {
		return nil, err
	}
	return &identity.LoginResult{Token: token, ExpiresAt: sess.ExpiresAt.Time, Principal: p}, nil
}

func (s *Sessions) Authenticate(ctx context.Context, token, kind string) (*identity.Principal, error) {
	return s.authenticate(ctx, s.q, token, kind)
}

func (s *Sessions) authenticate(ctx context.Context, q *store.Queries, token, kind string) (*identity.Principal, error) {
	if token == "" {
		return nil, identity.ErrSessionRevoked
	}
	row, err := q.GetSessionPrincipal(ctx, store.GetSessionPrincipalParams{RefreshTokenHash: HashToken(token), Kind: kind})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, identity.ErrSessionRevoked
	}
	if err != nil {
		return nil, err
	}
	if !row.UserActive {
		_ = q.RevokeAllUserSessions(ctx, row.UserID)
		return nil, identity.ErrAccountInactive
	}
	if !row.TenantActive && row.Role != domain.RolePlatformAdmin {
		return nil, identity.Suspended(s.supportEmail)
	}

	now := s.now()
	lastSeen := row.LastSeenAt.Time
	if kind == identity.SessionWeb && !row.Remember && row.LastSeenAt.Valid && now.Sub(lastSeen) > webIdleTimeout {
		_ = q.RevokeSession(ctx, row.SessionID)
		return nil, identity.ErrSessionIdle
	}
	if !row.LastSeenAt.Valid || now.Sub(lastSeen) > touchInterval {
		expires := row.ExpiresAt.Time
		switch {
		case kind == identity.SessionAPI:
			expires = now.Add(apiSessionTTL)
		case !row.Remember:
			expires = now.Add(webIdleTimeout)
		}
		_ = q.TouchSession(ctx, store.TouchSessionParams{ID: row.SessionID, ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true}})
	}

	return &identity.Principal{
		SessionID:            row.SessionID,
		UserID:               row.UserID,
		TenantID:             row.TenantID,
		Role:                 row.Role,
		Name:                 row.FullName,
		Phone:                deref(row.Phone),
		Email:                deref(row.Email),
		TenantName:           row.TenantName,
		TenantPlan:           row.TenantPlan,
		TrialEnds:            timePtr(row.TrialEndsAt),
		SubscriptionEnds:     timePtr(row.SubscriptionEndsAt),
		DebtNumber:           row.DebtNumber,
		DebtDue:              timePtr(row.DebtDueAt),
		MustChangeCredential: row.PasswordResetRequired,
		EmailVerified:        row.EmailVerified,
	}, nil
}

func (s *Sessions) Active(ctx context.Context, id pgtype.UUID) (bool, error) {
	return s.q.SessionStillValid(ctx, id)
}

func (s *Sessions) List(ctx context.Context, userID pgtype.UUID) ([]identity.Session, error) {
	rows, err := s.q.ListActiveSessionsByUser(ctx, userID)
	out := make([]identity.Session, len(rows))
	for i, r := range rows {
		out[i] = identity.Session{
			ID: r.ID, UserID: r.UserID, DeviceID: deref(r.DeviceID), UserAgent: deref(r.UserAgent),
			IP: deref(r.IpAddress), Kind: r.Kind, Remember: r.Remember,
			CreatedAt: r.CreatedAt.Time, LastSeen: r.LastSeenAt.Time, ExpiresAt: r.ExpiresAt.Time,
		}
	}
	return out, err
}

func (s *Sessions) End(ctx context.Context, id pgtype.UUID) error {
	return s.q.RevokeSession(ctx, id)
}

func (s *Sessions) EndUsers(ctx context.Context, userID, id pgtype.UUID) error {
	return s.q.RevokeSessionByUser(ctx, store.RevokeSessionByUserParams{ID: id, UserID: userID})
}

func (s *Sessions) EndAll(ctx context.Context, conn database.Conn, userID pgtype.UUID) error {
	return store.New(conn).RevokeAllUserSessions(ctx, userID)
}

func (s *Sessions) EndOthers(ctx context.Context, conn database.Conn, userID, keep pgtype.UUID) error {
	return store.New(conn).RevokeOtherUserSessions(ctx, store.RevokeOtherUserSessionsParams{UserID: userID, ID: keep})
}

func (s *Sessions) DeleteExpired(ctx context.Context) error {
	return s.q.DeleteExpiredSessions(ctx)
}

// HashToken is how a session's token is kept: only its hash is stored.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// randomToken is n random bytes, URL-safe base64 encoded.
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the system's randomness is gone
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// truncate cuts s to at most n characters.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
