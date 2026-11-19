package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/identity"
	"github.com/kiflet/backend/internal/modules/identity/store"
	"github.com/kiflet/backend/internal/platform/database"
)

const (
	codeMaxAttempts  = 5
	codeMaxPerWindow = 3
	codeWindow       = time.Hour
)

// Codes is identity.Codes on Postgres.
type Codes struct {
	q      *store.Queries
	secret []byte
	now    func() time.Time
}

var _ identity.Codes = (*Codes)(nil)

// NewCodes is the codes, read through conn (the pool) and hashed with the
// deployment's secret.
func NewCodes(conn database.Conn, secret []byte, now func() time.Time) *Codes {
	if now == nil {
		now = time.Now
	}
	return &Codes{q: store.New(conn), secret: secret, now: now}
}

// hash keys a code to its phone and purpose with the deployment secret, so
// a leaked table of six-digit codes cannot be reversed by brute force.
func (c *Codes) hash(phone, purpose, code string) string {
	m := hmac.New(sha256.New, c.secret)
	m.Write(fmt.Appendf(nil, "%s|%s|%s", purpose, phone, code))
	return hex.EncodeToString(m.Sum(nil))
}

func (c *Codes) Issue(ctx context.Context, conn database.Conn, phone, purpose string, allow func(context.Context) error) (string, error) {
	q := store.New(conn)
	if err := q.LockVerificationSubject(ctx, purpose+"|"+phone); err != nil {
		return "", err
	}
	recent, err := q.CountVerificationCodesSince(ctx, store.CountVerificationCodesSinceParams{
		Phone: phone, Purpose: purpose, CreatedAt: pgtype.Timestamptz{Time: c.now().Add(-codeWindow), Valid: true},
	})
	if err != nil {
		return "", err
	}
	if recent >= codeMaxPerWindow {
		return "", identity.ErrCodeTooMany
	}
	if allow != nil {
		if err := allow(ctx); err != nil {
			return "", err
		}
	}
	code := digits(6)
	_, err = q.CreateVerificationCode(ctx, store.CreateVerificationCodeParams{
		Phone: phone, Purpose: purpose, CodeHash: c.hash(phone, purpose, code),
		ExpiresAt: pgtype.Timestamptz{Time: c.now().Add(identity.CodeTTL), Valid: true},
	})
	return code, err
}

func (c *Codes) Verify(ctx context.Context, phone, purpose, code string) (pgtype.UUID, error) {
	vc, err := c.q.GetActiveVerificationCode(ctx, store.GetActiveVerificationCodeParams{Phone: phone, Purpose: purpose})
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, identity.ErrCodeInvalid
	}
	if err != nil {
		return pgtype.UUID{}, err
	}
	// Counted first, in one statement that also enforces the limit, so
	// guesses sent together can't get more tries between them.
	if _, err := c.q.CountVerificationAttempt(ctx, store.CountVerificationAttemptParams{ID: vc.ID, MaxAttempts: codeMaxAttempts}); errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, identity.ErrCodeAttempts
	} else if err != nil {
		return pgtype.UUID{}, err
	}
	if !hmac.Equal([]byte(vc.CodeHash), []byte(c.hash(phone, purpose, code))) {
		return pgtype.UUID{}, identity.ErrCodeInvalid
	}
	return vc.ID, nil
}

func (c *Codes) Consume(ctx context.Context, conn database.Conn, id pgtype.UUID) error {
	n, err := store.New(conn).ConsumeVerificationCode(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return identity.ErrCodeInvalid
	}
	return nil
}

func (c *Codes) DeleteExpired(ctx context.Context) error {
	return c.q.DeleteExpiredVerificationCodes(ctx)
}

// digits is n random decimal digits.
func digits(n int) string {
	b := make([]byte, n)
	for i := range b {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			panic(err) // the system's randomness is gone
		}
		b[i] = byte('0' + d.Int64())
	}
	return string(b)
}
