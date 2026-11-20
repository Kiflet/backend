//go:build integration

package integration

import (
	"context"
	"testing"
)

// TestTrackingPartitions (FR-019): positions are kept one table a day. A day
// with no partition yet (a phone sending what it kept offline) waits in the
// default partition until the hourly job gives it one; retention drops whole
// days past 90, and positions older than every partition with them.
func TestTrackingPartitions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	office := e.company("Partitioned Freight")
	driverID, _, _ := office.addDriver("Abebe Kebede")
	id, _ := office.book("Addis Ababa", "Dire Dawa", driverID)
	ping := func(ago string) {
		exec(t, `INSERT INTO tracking_events (tenant_id, shipment_id, driver_id, coordinates, event_type, recorded_at)
			SELECT tenant_id, id, driver_id, ST_SetSRID(ST_MakePoint(38.74, 9.0), 4326), 'gps_ping', NOW() - $2::interval
			FROM shipments WHERE id = $1`, id, ago)
	}
	where := func(ago string) string {
		return scalar[string](t, `SELECT tableoid::regclass::text FROM tracking_events
			WHERE shipment_id = $1 AND recorded_at::date = (NOW() - $2::interval)::date LIMIT 1`, id, ago)
	}

	// Today's and the week ahead's partitions exist from the migration.
	ping("1 minute")
	if p := where("1 minute"); p == "tracking_events_default" {
		t.Errorf("a position from a minute ago went to %s", p)
	}

	// Forty days ago has no partition: it waits in the default one.
	ping("40 days")
	ping("100 days")
	if p := where("40 days"); p != "tracking_events_default" {
		t.Fatalf("a position from 40 days ago went to %s, want the default partition", p)
	}
	if err := e.svc.Jobs.TrackingPartitions(ctx); err != nil {
		t.Fatal(err)
	}
	if p := where("40 days"); p == "tracking_events_default" {
		t.Error("the job left a recent day in the default partition")
	}
	if p := where("100 days"); p != "tracking_events_default" {
		t.Errorf("a day past retention got a partition, %s", p)
	}

	// The same position again, as from a phone that lost the answer, is
	// stored once.
	for range 2 {
		exec(t, `INSERT INTO tracking_events (tenant_id, shipment_id, driver_id, coordinates, event_type, recorded_at, device_id, client_id)
			SELECT tenant_id, id, driver_id, ST_SetSRID(ST_MakePoint(38.74, 9.0), 4326), 'gps_ping', '2026-11-19 08:00:00+03', 'phone-1', 'fix-1'
			FROM shipments WHERE id = $1
			ON CONFLICT (device_id, client_id, recorded_at) WHERE client_id IS NOT NULL AND device_id IS NOT NULL DO NOTHING`, id)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM tracking_events WHERE client_id = 'fix-1' AND shipment_id = $1`, id); n != 1 {
		t.Errorf("a repeated position stored %d times", n)
	}

	// Retention: 100 days ago goes, 40 days ago and today stay.
	if err := e.svc.Jobs.Retention(ctx); err != nil {
		t.Fatal(err)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM tracking_events WHERE shipment_id = $1`, id); n != 3 {
		t.Errorf("%d positions after retention, want 3 (today's two and 40 days ago)", n)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM tracking_events WHERE shipment_id = $1 AND recorded_at < NOW() - INTERVAL '90 days'`, id); n != 0 {
		t.Error("retention kept a position older than 90 days")
	}
}
