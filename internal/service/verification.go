package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/kiflet/backend/internal/modules/identity"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/db"
	"github.com/kiflet/backend/pkg/metrics"
)

// One-time codes sent by SMS: driver PIN reset, staff password reset,
// customer lookups, and the phone check at sign-up.
const (
	codePurposePINReset    = "pin_reset"
	codePurposeStaffReset  = "staff_reset"
	codePurposeTrackLookup = "track_lookup"
	codePurposeSignup      = "signup"
)

var (
	errCodeInvalid = identity.ErrCodeInvalid
	errCodeBudget  = &Error{Kind: KindLimit, Message: "We can't send codes right now. Please try again in a few hours.", Key: "error.code_budget"}
)

func randomDigits(n int) string {
	b := make([]byte, n)
	for i := range b {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			panic(fmt.Sprintf("crypto/rand: %v", err))
		}
		b[i] = byte('0' + d.Int64())
	}
	return string(b)
}

// issueCode creates a code and queues it by SMS; text formats the message
// around the code. With a nil text the code is only recorded, not sent:
// callers use that for a number with no account behind it, so that it
// counts towards the hourly limit like any other and the answer to the
// request is the same whoever holds the number.
func (c *core) issueCode(ctx context.Context, tenantID pgtype.UUID, phone, purpose string, text func(code string) string) error {
	return c.tx(ctx, func(q *db.Queries, after *afterCommit) error {
		// The daily budget is checked for codes recorded but not sent too,
		// so that the answer stays the same for every number.
		code, err := c.codes.Issue(ctx, q.Conn(), phone, purpose, func(ctx context.Context) error {
			return c.withinCodeBudget(ctx, q)
		})
		if err != nil {
			return err
		}
		if text != nil {
			c.enqueueSMS(ctx, q, after, outboundSMS{
				TenantID: tenantID, Phone: phone, Body: text(code), Purpose: PurposeOTP, Sensitive: true,
			})
		}
		return nil
	})
}

// withinCodeBudget refuses a code once the platform has texted its daily
// budget of them (Settings.CodesPerDay). Codes go to any number someone
// types in, so without a ceiling a script could run up the SMS bill. The
// share used is exported for the monitoring alert.
func (c *core) withinCodeBudget(ctx context.Context, q *db.Queries) error {
	budget := int64(c.cfg.CodesPerDay)
	if budget <= 0 {
		return nil
	}
	sent, err := q.CountCodesTextedSince(ctx, toTimestamp(c.now().Add(-24*time.Hour)))
	if err != nil {
		return err
	}
	if sent >= budget {
		metrics.VerificationBudgetUsed.Set(1)
		c.log.Error().Int64("sent", sent).Int64("budget", budget).Msg("daily budget of verification codes reached: codes refused")
		return errCodeBudget
	}
	metrics.VerificationBudgetUsed.Set(float64(sent+1) / float64(budget))
	if (sent+1)*5 == budget*4 {
		c.log.Warn().Int64("sent", sent+1).Int64("budget", budget).Msg("80% of the daily budget of verification codes used")
	}
	return nil
}

// verifyCode checks a code without using it up, and returns its ID for
// consumeCode (identity.Codes.Verify).
func (c *core) verifyCode(ctx context.Context, phone, purpose, code string) (pgtype.UUID, error) {
	return c.codes.Verify(ctx, phone, purpose, code)
}

// consumeCode uses up a code verifyCode accepted, in q's transaction.
func (c *core) consumeCode(ctx context.Context, q *db.Queries, id pgtype.UUID) error {
	return c.codes.Consume(ctx, q.Conn(), id)
}

// checkCode verifies and consumes a code.
func (c *core) checkCode(ctx context.Context, phone, purpose, code string) error {
	id, err := c.verifyCode(ctx, phone, purpose, code)
	if err != nil {
		return err
	}
	return c.consumeCode(ctx, c.q, id)
}

// Sign returns an HMAC of value under the deployment secret, for short-lived
// cookies such as a verified lookup phone.
func (c *core) Sign(value string) string {
	m := hmac.New(sha256.New, c.cfg.Secret)
	m.Write([]byte(value))
	return hex.EncodeToString(m.Sum(nil))
}
