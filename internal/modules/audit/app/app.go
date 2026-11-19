// Package app is the audit module's implementation of audit.Log.
package app

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"

	"github.com/kiflet/backend/internal/modules/audit"
	"github.com/kiflet/backend/internal/modules/audit/store"
	"github.com/kiflet/backend/internal/platform/database"
)

// Service is audit.Log on Postgres.
type Service struct {
	q   *store.Queries
	log zerolog.Logger
}

var _ audit.Log = (*Service)(nil)

// New is the audit module, reading through conn (the pool).
func New(conn database.Conn, log zerolog.Logger) *Service {
	return &Service{q: store.New(conn), log: log}
}

func (s *Service) Write(ctx context.Context, conn database.Conn, e audit.Entry) {
	err := store.New(conn).CreateAuditLog(ctx, store.CreateAuditLogParams{
		TenantID: e.TenantID, UserID: e.UserID, Action: e.Action,
		EntityType: e.EntityType, EntityID: e.EntityID,
		OldValue: marshal(e.OldValue), NewValue: marshal(e.NewValue),
		IpAddress: text(e.IP), UserAgent: text(e.UserAgent),
	})
	if err != nil {
		s.log.Error().Err(err).Str("action", e.Action).Msg("write audit log")
	}
}

func (s *Service) List(ctx context.Context, tenantID pgtype.UUID, page, perPage int) ([]audit.Record, error) {
	limit, offset := database.Page(page, perPage)
	rows, err := s.q.ListAuditLogsByTenant(ctx, store.ListAuditLogsByTenantParams{TenantID: tenantID, Limit: limit, Offset: offset})
	return records(rows), err
}

func (s *Service) Search(ctx context.Context, f audit.Filter, page, perPage int) ([]audit.Record, error) {
	limit, offset := database.Page(page, perPage)
	arg := store.SearchAuditLogsParams{TenantID: f.TenantID, Action: text(f.Action), Ip: text(f.IP), MaxRows: limit, Skip: offset}
	if !f.Since.IsZero() {
		arg.Since = pgtype.Timestamptz{Time: f.Since, Valid: true}
	}
	if !f.Until.IsZero() {
		arg.Until = pgtype.Timestamptz{Time: f.Until, Valid: true}
	}
	rows, err := s.q.SearchAuditLogs(ctx, arg)
	return records(rows), err
}

func (s *Service) DeleteTenant(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) error {
	return store.New(conn).DeleteTenantAuditLogs(ctx, tenantID)
}

func records(rows []store.AuditLog) []audit.Record {
	out := make([]audit.Record, len(rows))
	for i, r := range rows {
		out[i] = audit.Record{
			ID: r.ID, TenantID: r.TenantID, UserID: r.UserID, Action: r.Action,
			EntityType: r.EntityType, EntityID: r.EntityID, OldValue: r.OldValue, NewValue: r.NewValue,
			IP: deref(r.IpAddress), UserAgent: deref(r.UserAgent), At: r.CreatedAt.Time,
		}
	}
	return out
}

// marshal is v as JSON, or nothing for nil or what can't be marshalled.
func marshal(v any) []byte {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
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
