package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"

	"github.com/kiflet/backend/internal/db"
	"github.com/kiflet/backend/internal/modules/identity"
	"github.com/kiflet/backend/internal/modules/shipments"
	"github.com/kiflet/backend/internal/modules/tenancy"
	"github.com/kiflet/backend/internal/platform/database"
	"github.com/kiflet/backend/internal/platform/events"
)

// countingCompanies is the tenancy module, counting how often a company is
// read.
type countingCompanies struct {
	tenancy.Companies
	reads int
}

func (c *countingCompanies) Get(_ context.Context, _ database.Conn, id pgtype.UUID) (tenancy.Tenant, error) {
	c.reads++
	return tenancy.Tenant{ID: id, Name: "Abay Freight", SmsOnCreated: true}, nil
}

// recordingHistory is the shipments module, keeping the changes recorded.
type recordingHistory struct {
	shipments.Shipments
	changes []shipments.Change
}

func (h *recordingHistory) Record(_ context.Context, _ events.Tx, _ identity.Actor, _ shipments.Shipment, c shipments.Change) (bool, error) {
	h.changes = append(h.changes, c)
	return true, nil
}

// TestCompanyReadOncePerTransaction (audit D9): a transaction reads its
// company once, and each change it records carries it, so the subscribers
// that run in the transaction don't read it again.
func TestCompanyReadOncePerTransaction(t *testing.T) {
	ctx := context.Background()
	companies, history := &countingCompanies{}, &recordingHistory{}
	s := &ShipmentService{core: &core{tenancy: companies, shipmentData: history, log: zerolog.Nop(), now: time.Now}}
	q := db.New(nil)
	id := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	after := &afterCommit{scope: &events.Scope{}}

	if _, err := s.tenantIn(ctx, q, after, id); err != nil { // as Create and Assign do
		t.Fatal(err)
	}
	sh := shipments.Shipment{ID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, TenantID: id}
	s.event(ctx, q, after, Actor{}, sh, shipments.Change{Type: "created"})
	s.event(ctx, q, after, Actor{}, sh, shipments.Change{Type: "assigned"})
	if companies.reads != 1 {
		t.Errorf("the company was read %d times in one transaction, want 1", companies.reads)
	}
	for _, c := range history.changes {
		if c.Company.ID != id || c.Company.Name != "Abay Freight" {
			t.Errorf("%s doesn't carry the company: %+v", c.Type, c.Company)
		}
	}

	// Another transaction reads it afresh.
	if _, err := s.tenantIn(ctx, q, &afterCommit{scope: &events.Scope{}}, id); err != nil {
		t.Fatal(err)
	}
	if companies.reads != 2 {
		t.Errorf("a new transaction read the company %d times in all, want 2", companies.reads)
	}
}
