package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/db"
	"github.com/kiflet/backend/internal/modules/audit"
	"github.com/kiflet/backend/internal/platform/database"
)

// AuditService is the dashboard's way into the audit module
// (internal/modules/audit): a company's own log, and every company's for
// the platform admin.
type AuditService struct{ *core }

// audit records an action within the caller's transaction (q's). A failure
// is logged rather than returned: losing an audit row must not undo the
// action.
func (c *core) audit(ctx context.Context, q *db.Queries, a Actor, action, entityType string, entityID pgtype.UUID, oldValue, newValue any) {
	c.auditLog.Write(ctx, q.Conn(), audit.Entry{
		TenantID: a.TenantID, UserID: a.UserID, Action: action, EntityType: entityType, EntityID: entityID,
		OldValue: oldValue, NewValue: newValue, IP: a.IP, UserAgent: a.UserAgent,
	})
}

// List returns a page of a tenant's audit log, newest first.
func (s *AuditService) List(ctx context.Context, tenantID pgtype.UUID, page, perPage int) ([]audit.Record, error) {
	return s.auditLog.List(ctx, tenantID, page, perPage)
}

// Platform returns a page of every company's audit log for the platform
// admin's investigations, newest first (AC-042.7).
func (s *AuditService) Platform(ctx context.Context, p *Principal, f audit.Filter, page, perPage int) ([]audit.Record, error) {
	if !p.IsPlatformAdmin() {
		return nil, ErrUnauthorized
	}
	return s.auditLog.Search(ctx, f, page, perPage)
}

// pageBounds converts a 1-based page into LIMIT/OFFSET, bounding the page size.
func pageBounds(page, perPage int) (limit, offset int32) {
	return database.Page(page, perPage)
}
