// Package driversync is the driversync module (docs/architecture.md,
// section 4): what keeps a driver's phone right. It owns driver_feed and
// driver_feed_heads, the change feed the sync reads (section 11, "The
// driver sync"); the sync itself, and the devices' tables, move here from
// internal/service with phase 3 of docs/migration.md.
//
// Every change a driver's phone must hear of writes an entry, in the
// transaction of the change: a load that changed or was taken away, a note
// from the office, a message to every driver. The module writes those for
// shipment events itself, subscribed to shipment.* on the bus; messages
// to every driver are added where they are sent (Add), until messaging
// moves too.
package driversync

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/platform/database"
	"github.com/kiflet/backend/internal/platform/events"
)

// What an entry is about.
const (
	KindShipment  = "shipment"  // a load: RefID is the shipment
	KindMessage   = "message"   // a note from the office: RefID is the shipment_events row
	KindBroadcast = "broadcast" // a message to every driver: RefID is the broadcast
)

// What happened to it.
const (
	OpUpsert = "upsert" // new or changed: the phone takes it as it is now
	OpRemove = "remove" // a load taken from the driver: the phone lets go of it
)

// Why a load was taken from a driver.
const (
	Reassigned = "reassigned" // it is another driver's now
	Unassigned = "unassigned" // it is no one's
	Cancelled  = "cancelled"  // the office cancelled it
)

// RemovalReason is why a load in a status, with another driver or none, is
// no longer a driver's.
func RemovalReason(status string, hasDriver bool) string {
	switch {
	case status == "cancelled":
		return Cancelled
	case hasDriver:
		return Reassigned
	default:
		return Unassigned
	}
}

// Retention is how long entries are kept. A cursor older than that has
// missed some and gets a full download.
const Retention = 30 * 24 * time.Hour

// Entry is a change for a driver's phone.
type Entry struct {
	TenantID pgtype.UUID
	DriverID pgtype.UUID
	Kind     string
	RefID    pgtype.UUID
	Op       string
	Reason   string // for OpRemove
}

// Item is an entry as a sync reads it: Seq is its number in the driver's
// sequence.
type Item struct {
	Seq    int64
	Kind   string
	RefID  pgtype.UUID
	Op     string
	Reason string
}

// Head is where a driver's feed stands: the last number given, and the
// last the retention job deleted.
type Head struct {
	Seq           int64
	PrunedThrough int64
}

// Honours says whether a cursor at seq can be carried on from: it is no
// later than the head and nothing after it has been deleted.
func (h Head) Honours(seq int64) bool {
	return seq <= h.Seq && seq >= h.PrunedThrough
}

// Feed is what other modules may call.
type Feed interface {
	// Add writes entries in tx. With a transaction they are written as it
	// ends, each driver's head locked in order of driver ID, so the feed
	// adds no deadlock; without one, at once.
	Add(ctx context.Context, tx events.Tx, entries ...Entry) error
	// Head is where a driver's feed stands; zero for a driver with no
	// entries yet.
	Head(ctx context.Context, conn database.Conn, tenantID, driverID pgtype.UUID) (Head, error)
	// Since are up to max of a driver's entries after a number, in order.
	// Read in the same snapshot as Head and what the entries name.
	Since(ctx context.Context, conn database.Conn, tenantID, driverID pgtype.UUID, after int64, max int) ([]Item, error)
	// Prune deletes every company's entries older than before.
	Prune(ctx context.Context, before time.Time) (int64, error)
}

// Why the server refuses an item a driver's phone sent, the stable codes
// the app chooses its words by (docs/architecture.md, section 11, "Refused
// items"). Phase 3's sync answers with them; the driver's words for each
// are in internal/platform/text.
const (
	RefusedCancelled   = "cancelled"
	RefusedReassigned  = "reassigned"
	RefusedStale       = "stale"
	RefusedHeld        = "held_for_review"
	RefusedNotFound    = "not_found"
	RefusedInvalid     = "invalid"
	RefusedServerError = "server_error"
	// RefusedFilesMissing is a delivery naming files not yet in storage:
	// retried, once the app has sent them.
	RefusedFilesMissing = "files_missing"
)

// Refusals lists them.
var Refusals = []string{RefusedCancelled, RefusedReassigned, RefusedStale, RefusedHeld, RefusedNotFound, RefusedInvalid, RefusedServerError, RefusedFilesMissing}

// Settings are the numbers the driver app works by, sent in its config so
// they can be tuned after the pilot by a deploy rather than a release of
// the app (Kiflet/mobile's docs/architecture.md, sections 5 to 7).
type Settings struct {
	GPS GPS `json:"gps"`
	// Photos are resized to PhotoMaxPx on the long side, as JPEG at
	// PhotoQuality: large enough to read a waybill's stamp.
	PhotoMaxPx   int     `json:"photo_max_px"`
	PhotoQuality float64 `json:"photo_quality"`
	// How often the app syncs while on screen, and in battery saver.
	SyncIntervalS       int `json:"sync_interval_s"`
	SyncIntervalSavingS int `json:"sync_interval_saving_s"`
	// How long without data before the app offers to send a move by SMS.
	SMSFallbackAfterS int `json:"sms_fallback_after_s"`
}

// GPS is how the app records a trip, today's rules (the PWA's gps.js).
type GPS struct {
	// Moving: a position once the truck has moved over MovedM, at most
	// every IntervalS, and at least every MaxIntervalS.
	MovedM       int `json:"moved_m"`
	IntervalS    int `json:"interval_s"`
	MaxIntervalS int `json:"max_interval_s"`
	// Parked: within MovedM for ParkedAfterS. The GPS is off, with a
	// balanced fix every ParkedCheckS; over ParkedLeftM away is moving again.
	ParkedAfterS int `json:"parked_after_s"`
	ParkedCheckS int `json:"parked_check_s"`
	ParkedLeftM  int `json:"parked_left_m"`
	// Fixes vaguer than this are dropped.
	MaxAccuracyM int `json:"max_accuracy_m"`
	// Battery saver, chosen or under SavingBelowBattery (a fraction) and
	// not charging: a position every SavingIntervalS, parked checks every
	// SavingParkedCheckS, fixes up to SavingMaxAccuracyM.
	SavingBelowBattery float64 `json:"saving_below_battery"`
	SavingIntervalS    int     `json:"saving_interval_s"`
	SavingParkedCheckS int     `json:"saving_parked_check_s"`
	SavingMaxAccuracyM int     `json:"saving_max_accuracy_m"`
}

// DefaultSettings are the numbers the app is sent.
var DefaultSettings = Settings{
	GPS: GPS{
		MovedM: 75, IntervalS: 60, MaxIntervalS: 5 * 60,
		ParkedAfterS: 10 * 60, ParkedCheckS: 5 * 60, ParkedLeftM: 150,
		MaxAccuracyM:       500,
		SavingBelowBattery: 0.2, SavingIntervalS: 15 * 60, SavingParkedCheckS: 15 * 60, SavingMaxAccuracyM: 3000,
	},
	PhotoMaxPx: 1280, PhotoQuality: 0.6,
	SyncIntervalS: 60, SyncIntervalSavingS: 5 * 60,
	SMSFallbackAfterS: 15 * 60,
}
