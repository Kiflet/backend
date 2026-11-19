// Package audit is the audit module (docs/architecture.md, section 4): the
// append-only record of security-relevant actions, such as sign-ins,
// account and credential changes and settings. What happens to a shipment
// is in its own timeline instead (shipment_events).
//
// Other modules reach it only through Log. It owns audit_logs, which a
// trigger keeps append-only.
package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/platform/database"
)

// Log is what other modules may call.
type Log interface {
	// Write records an action within the caller's transaction (conn). A
	// failure is logged rather than returned: losing an audit row must not
	// undo the action.
	Write(ctx context.Context, conn database.Conn, e Entry)
	// List is a page of a company's log, newest first.
	List(ctx context.Context, tenantID pgtype.UUID, page, perPage int) ([]Record, error)
	// Search is a page of every company's log, newest first, for the
	// platform admin's investigations (AC-042.7). The caller checks that
	// it is the platform admin asking.
	Search(ctx context.Context, f Filter, page, perPage int) ([]Record, error)
	// DeleteTenant removes a deleted company's rows, within the
	// transaction that deletes it, which must allow it (data's purgeTx).
	DeleteTenant(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) error
}

// Entry is an action to record. OldValue and NewValue are marshalled to
// JSON; nil records nothing.
type Entry struct {
	TenantID   pgtype.UUID
	UserID     pgtype.UUID
	Action     string
	EntityType string
	EntityID   pgtype.UUID
	OldValue   any
	NewValue   any
	IP         string
	UserAgent  string
}

// Record is an action recorded.
type Record struct {
	ID         pgtype.UUID
	TenantID   pgtype.UUID
	UserID     pgtype.UUID
	Action     string
	EntityType string
	EntityID   pgtype.UUID
	OldValue   json.RawMessage
	NewValue   json.RawMessage
	IP         string
	UserAgent  string
	At         time.Time
}

// Filter narrows Search. Zero fields match all.
type Filter struct {
	// TenantID matches a company's own rows and the platform's actions on
	// it.
	TenantID     pgtype.UUID
	Action, IP   string
	Since, Until time.Time
}
