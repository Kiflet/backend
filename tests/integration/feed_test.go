//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/kiflet/backend/internal/service"
)

// The driver sync's change feed (docs/architecture.md, section 11, "The
// driver sync"; docs/audit-2026-11-19.md, D1).

// addFeedEntry is the driver feed's write as a test makes it by hand: the
// driver's head raised, its row locked until the transaction ends.
const addFeedEntry = `WITH head AS (
	INSERT INTO driver_feed_heads (driver_id, tenant_id, seq)
	SELECT driver_id, tenant_id, 1 FROM shipments WHERE id = $1
	ON CONFLICT (driver_id) DO UPDATE SET seq = driver_feed_heads.seq + 1
	RETURNING driver_id, tenant_id, seq
)
INSERT INTO driver_feed (driver_id, seq, tenant_id, kind, ref_id, op)
SELECT driver_id, seq, tenant_id, 'shipment', $1, 'upsert' FROM head`

// notesToDriver adds n notes from the office to a load's driver, with
// their feed entries, as if sent one after another.
func notesToDriver(t *testing.T, shipmentID string, n int) {
	t.Helper()
	exec(t, `WITH ev AS (
		INSERT INTO shipment_events (tenant_id, shipment_id, event_type, driver_id, actor_name, note, visible_to_driver, created_at)
		SELECT tenant_id, id, 'note', driver_id, 'Dispatch', 'Note ' || g, true, NOW() - make_interval(secs => $2 - g)
		FROM shipments, generate_series(1, $2) g WHERE id = $1
		RETURNING id, driver_id, tenant_id, created_at
	), head AS (
		INSERT INTO driver_feed_heads (driver_id, tenant_id, seq)
		SELECT driver_id, tenant_id, $2 FROM shipments WHERE id = $1
		ON CONFLICT (driver_id) DO UPDATE SET seq = driver_feed_heads.seq + EXCLUDED.seq
		RETURNING seq
	)
	INSERT INTO driver_feed (driver_id, seq, tenant_id, kind, ref_id, op)
	SELECT ev.driver_id, head.seq - $2 + row_number() OVER (ORDER BY ev.created_at, ev.id), ev.tenant_id, 'message', ev.id, 'upsert'
	FROM ev, head`, shipmentID, n)
}

func shipmentIDs(resp service.SyncResponse) map[string]service.DriverShipment {
	out := map[string]service.DriverShipment{}
	for _, sh := range resp.Shipments {
		out[sh.ID] = sh
	}
	return out
}

// TestFeedDeliversLateCommits: a change whose transaction began before a
// sync and committed after it still reaches the phone, as do the changes
// that waited for it. Today's cursor, the clock less 5 seconds, missed it.
func TestFeedDeliversLateCommits(t *testing.T) {
	e := newEnv(t)
	office := e.company("Late Commit Freight")
	driverID, phone, pin := office.addDriver("Hana Girma")
	first, _ := office.book("Addis Ababa", "Adama", driverID)
	d := e.signIn(phone, pin)
	resp, _ := d.sync(service.SyncRequest{})
	if !resp.Full || len(resp.Shipments) != 1 || resp.Driver == nil {
		t.Fatalf("first sync: full %v, %d loads, driver %v; want a full download of one load", resp.Full, len(resp.Shipments), resp.Driver)
	}
	cursor := resp.Cursor

	// A transaction changes the load, stamped an hour ago, and writes its
	// entry, which locks the driver's head until it commits.
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if _, err := tx.Exec(ctx, `UPDATE shipments SET special_instructions = 'Gate 4', updated_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, addFeedEntry, first); err != nil {
		t.Fatal(err)
	}
	// The office books a second load for the driver meanwhile: its entry
	// waits for the first.
	booked := make(chan string, 1)
	go func() {
		id, _ := office.book("Addis Ababa", "Hawassa", driverID)
		booked <- id
	}()
	time.Sleep(300 * time.Millisecond)
	select {
	case <-booked:
		t.Fatal("the second booking committed before the transaction holding the driver's feed")
	default:
	}

	// A sync now sees neither, and isn't held up.
	mid, code := d.sync(service.SyncRequest{Since: cursor})
	if code != http.StatusOK || mid.Full || len(mid.Shipments) != 0 {
		t.Fatalf("sync during the transaction: status %d, full %v, %d loads; want no change", code, mid.Full, len(mid.Shipments))
	}
	if mid.Driver != nil {
		t.Error("the profile was sent again though it hadn't changed")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	second := <-booked

	last, _ := d.sync(service.SyncRequest{Since: mid.Cursor})
	got := shipmentIDs(last)
	if sh, ok := got[first]; !ok || sh.Instructions != "Gate 4" {
		t.Errorf("the late change didn't arrive: %+v", got)
	}
	if _, ok := got[second]; !ok {
		t.Errorf("the load booked meanwhile didn't arrive: %+v", got)
	}
	if last.Full || last.More {
		t.Errorf("full %v, more %v; want neither", last.Full, last.More)
	}
}

// TestFeedRemovesWithReasons: a load handed to another driver, one taken
// back, and one cancelled and reopened without the driver each leave the
// first driver's phone with the reason, and arrive on the new driver's.
func TestFeedRemovesWithReasons(t *testing.T) {
	e := newEnv(t)
	office := e.company("Reason Logistics")
	abebeID, abebePhone, abebePIN := office.addDriver("Abebe Kebede")
	selamID, selamPhone, selamPIN := office.addDriver("Selam Tesfaye")
	handed, _ := office.book("Addis Ababa", "Adama", abebeID)
	taken, _ := office.book("Addis Ababa", "Dire Dawa", abebeID)
	kept, _ := office.book("Addis Ababa", "Bahir Dar", abebeID)
	abebe := e.signIn(abebePhone, abebePIN)
	selam := e.signIn(selamPhone, selamPIN)
	a, _ := abebe.sync(service.SyncRequest{})
	s, _ := selam.sync(service.SyncRequest{})

	office.mustPost("/app/shipments/"+handed+"/assign", url.Values{"driver_id": {selamID}})
	office.mustPost("/app/shipments/"+taken+"/unassign", nil)
	office.mustPost("/app/shipments/"+kept+"/note", url.Values{"note": {"Call before arriving"}, "to": {"driver"}})

	a, _ = abebe.sync(service.SyncRequest{Since: a.Cursor})
	reasons := map[string]string{}
	for _, r := range a.Removed {
		reasons[r.ID] = r.Reason
	}
	if reasons[handed] != "reassigned" || reasons[taken] != "unassigned" || len(reasons) != 2 {
		t.Errorf("removed %+v, want %s reassigned and %s unassigned", a.Removed, handed, taken)
	}
	if len(a.RemovedShipmentIDs) != 2 {
		t.Errorf("removed_shipment_ids %v, want both", a.RemovedShipmentIDs)
	}
	if len(a.Shipments) != 0 {
		t.Errorf("a note sent the load again: %+v", a.Shipments)
	}
	if len(a.Messages) != 1 || a.Messages[0].Text != "Call before arriving" {
		t.Errorf("messages %+v, want the note", a.Messages)
	}
	s, _ = selam.sync(service.SyncRequest{Since: s.Cursor})
	if _, ok := shipmentIDs(s)[handed]; !ok || len(s.Removed) != 0 {
		t.Errorf("the new driver got %+v, removed %+v; want the load handed over", s.Shipments, s.Removed)
	}

	// The same request again (the answer was lost) brings the same.
	again, _ := selam.sync(service.SyncRequest{Since: s.Cursor})
	replay, _ := selam.sync(service.SyncRequest{Since: s.Cursor})
	if len(again.Shipments) != len(replay.Shipments) || again.Cursor != replay.Cursor {
		t.Errorf("a replayed request: %d loads and cursor %q, then %d and %q", len(again.Shipments), again.Cursor, len(replay.Shipments), replay.Cursor)
	}
}

// TestFeedFullDownloads: a cursor that can't be carried on from (another
// driver's, from before the feed's retention, a PWA's timestamp from
// before the feed) gets a full download, never an error.
func TestFeedFullDownloads(t *testing.T) {
	e := newEnv(t)
	office := e.company("Full Download Freight")
	driverID, phone, pin := office.addDriver("Tigist Alemu")
	otherID, otherPhone, otherPIN := office.addDriver("Dawit Bekele")
	load, _ := office.book("Addis Ababa", "Adama", driverID)
	d := e.signIn(phone, pin)
	other := e.signIn(otherPhone, otherPIN)
	mine, _ := d.sync(service.SyncRequest{})
	theirs, _ := other.sync(service.SyncRequest{})

	full := func(name string, resp service.SyncResponse) {
		t.Helper()
		if !resp.Full || resp.Driver == nil {
			t.Errorf("%s: full %v, driver %v; want a full download", name, resp.Full, resp.Driver)
		}
		if _, ok := shipmentIDs(resp)[load]; !ok {
			t.Errorf("%s: the driver's load is missing", name)
		}
	}
	resp, _ := d.sync(service.SyncRequest{Since: theirs.Cursor})
	full("another driver's cursor", resp)

	// The retention job deleted entries after the cursor.
	office.mustPost("/app/shipments/"+load+"/note", url.Values{"note": {"Gate 4"}, "to": {"driver"}})
	exec(t, `UPDATE driver_feed SET created_at = NOW() - INTERVAL '31 days' WHERE driver_id = $1`, driverID)
	if err := e.svc.Jobs.Retention(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM driver_feed WHERE driver_id = $1`, driverID); n != 0 {
		t.Errorf("%d entries older than the feed keeps are left", n)
	}
	resp, _ = d.sync(service.SyncRequest{Since: mine.Cursor})
	full("a cursor from before the retention", resp)

	// A PWA loaded before the feed sends the time it last synced, and
	// reads the loads taken from it since in removed_shipment_ids.
	before := time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
	office.mustPost("/app/shipments/"+load+"/assign", url.Values{"driver_id": {otherID}})
	resp, code := d.sync(service.SyncRequest{Since: before})
	if code != http.StatusOK || !resp.Full || len(resp.RemovedShipmentIDs) != 1 || resp.RemovedShipmentIDs[0] != load {
		t.Errorf("a timestamp cursor: status %d, full %v, removed %v; want a full download removing the load", code, resp.Full, resp.RemovedShipmentIDs)
	}

	// And a full download gives a cursor that is carried on from.
	resp, _ = d.sync(service.SyncRequest{Since: resp.Cursor})
	if resp.Full || len(resp.Shipments) != 0 {
		t.Errorf("after a full download: full %v, %d loads; want no change", resp.Full, len(resp.Shipments))
	}
}

// TestFeedPageOfExactlyTheLimit: a page of exactly the limit says there
// may be more, and the next sync, with nothing, says there isn't.
func TestFeedPageOfExactlyTheLimit(t *testing.T) {
	e := newEnv(t)
	office := e.company("Exact Page Cargo")
	driverID, phone, pin := office.addDriver("Liya Haile")
	load, _ := office.book("Addis Ababa", "Adama", driverID)
	d := e.signIn(phone, pin)
	resp, _ := d.sync(service.SyncRequest{})
	notesToDriver(t, load, 500)

	resp, _ = d.sync(service.SyncRequest{Since: resp.Cursor})
	if len(resp.Messages) != 500 || !resp.More {
		t.Fatalf("%d messages, more %v; want 500 and more", len(resp.Messages), resp.More)
	}
	resp, _ = d.sync(service.SyncRequest{Since: resp.Cursor})
	if len(resp.Messages) != 0 || resp.More || resp.Full {
		t.Errorf("then %d messages, more %v, full %v; want nothing more", len(resp.Messages), resp.More, resp.Full)
	}
}

// TestFeedBroadcasts: a message to every driver on the road reaches each
// through their feed, once.
func TestFeedBroadcasts(t *testing.T) {
	e := newEnv(t)
	office := e.company("Broadcast Haulage")
	driverID, phone, pin := office.addDriver("Mulu Getachew")
	office.book("Addis Ababa", "Adama", driverID)
	d := e.signIn(phone, pin)
	resp, _ := d.sync(service.SyncRequest{})

	office.mustPost("/app/drivers/message", url.Values{"body": {"Checkpoint delays reported ahead. Drive carefully."}})
	resp, _ = d.sync(service.SyncRequest{Since: resp.Cursor})
	if len(resp.Messages) != 1 || resp.Messages[0].ShipmentID != "" {
		t.Fatalf("messages %+v, want the broadcast", resp.Messages)
	}
	resp, _ = d.sync(service.SyncRequest{Since: resp.Cursor})
	if len(resp.Messages) != 0 {
		t.Errorf("the broadcast came again: %+v", resp.Messages)
	}
}
