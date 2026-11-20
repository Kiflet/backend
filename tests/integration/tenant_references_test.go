//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// singleReferences are the references between companies' tables that stay
// on the ID alone, because the platform's operators, who belong to no
// customer company, make them (migrations/20261120000300).
var singleReferences = map[string]bool{
	"audit_logs.user_id":         true, // what an operator did to a company
	"invoices.created_by":        true,
	"payments.recorded_by":       true,
	"support_messages.author_id": true, // an operator's answer
	"sync_metadata.driver_id":    true, // no tenant_id: one row per driver, its only reference
}

// TestReferencesCarryTheTenant (audit D2): every reference from a table
// that belongs to a company to another such table includes tenant_id, so
// the database refuses a row pointing at another company's. A new table or
// reference is held to it too.
func TestReferencesCarryTheTenant(t *testing.T) {
	rows, err := pool.Query(context.Background(), `
		SELECT c.conrelid::regclass::text || '.' || a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
		JOIN pg_class k ON k.oid = c.conrelid
		WHERE c.contype = 'f' AND array_length(c.conkey, 1) = 1
		  AND c.confrelid <> 'tenants'::regclass
		  AND NOT k.relispartition
		  AND EXISTS (SELECT 1 FROM pg_attribute r WHERE r.attrelid = c.confrelid AND r.attname = 'tenant_id' AND NOT r.attisdropped)
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			t.Fatal(err)
		}
		if !singleReferences[ref] {
			t.Errorf("%s refers to a company's table by the ID alone: make it (tenant_id, %s), or say in singleReferences why not", ref, ref)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// crossCompany runs a write that names another company's row, and expects
// the database to refuse it.
func crossCompany(t *testing.T, what, sql string, args ...any) {
	t.Helper()
	_, err := pool.Exec(context.Background(), sql, args...)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23503" {
		t.Errorf("%s: %v, want a foreign key violation", what, err)
	}
}

// TestAnotherCompanysRowsAreRefused: a load can't be given another
// company's driver or truck, nor a proof, charge or alert another company's
// load.
func TestAnotherCompanysRowsAreRefused(t *testing.T) {
	e := newEnv(t)
	ours := e.company("Own Freight")
	theirs := e.company("Other Freight")
	ourDriver, _, _ := ours.addDriver("Abebe Bikila")
	theirDriver, _, _ := theirs.addDriver("Derartu Tulu")
	load, _ := ours.book("Addis Ababa", "Adama", ourDriver)
	theirLoad, _ := theirs.book("Addis Ababa", "Hawassa", theirDriver)
	tenant := scalar[string](t, `SELECT tenant_id::text FROM shipments WHERE id = $1`, load)

	crossCompany(t, "a load given another company's driver",
		`UPDATE shipments SET driver_id = $2 WHERE id = $1`, load, theirDriver)
	theirTruck := scalar[string](t, `INSERT INTO vehicles (tenant_id, plate_number) SELECT tenant_id, '3-B' || floor(random() * 1e5)::int FROM users WHERE id = $1 RETURNING id::text`, theirDriver)
	crossCompany(t, "a load given another company's truck",
		`UPDATE shipments SET vehicle_id = $2 WHERE id = $1`, load, theirTruck)
	crossCompany(t, "a charge on another company's load",
		`INSERT INTO shipment_charges (tenant_id, shipment_id, description, amount_santim) VALUES ($1, $2, 'Freight', 10000)`, tenant, theirLoad)
	crossCompany(t, "an alert about another company's driver",
		`INSERT INTO alerts (tenant_id, driver_id, alert_type, message) VALUES ($1, $2, 'driver_offline', 'Offline')`, tenant, theirDriver)

	// The company's own rows are taken as before.
	exec(t, `UPDATE shipments SET driver_id = $2 WHERE id = $1`, load, ourDriver)
}
