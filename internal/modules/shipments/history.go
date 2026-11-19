package shipments

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/identity"
	"github.com/kiflet/backend/internal/modules/shipments/domain"
	"github.com/kiflet/backend/internal/platform/events"
)

// Change is an event in a shipment's history as it happens: what the
// history keeps, and what the modules reacting to it need that the
// history doesn't keep.
type Change struct {
	Type             string // "status_changed": published as shipment.status_changed
	From, To         string // statuses
	DriverID         pgtype.UUID
	PreviousDriverID pgtype.UUID
	Note             string
	Reason           string
	Lat, Lng         *float64
	VisibleToDriver  bool
	Public           bool // shown on the tracking page
	Details          map[string]any
	ClientID         string    // the device's ID for it, so a repeat is not added again
	OccurredAt       time.Time // zero is now

	// Not kept in the history; for the subscribers in this process.
	NewDriver      bool         // assigned: the driver is new to the load
	FirstDeparture bool         // status_changed: the load's first move to in transit
	Recipient      string       // status_changed: who took delivery, from the proof
	By             domain.Actor // status_changed: how it was made
	POD            *PODCheck    // pod_recorded: where the proof was recorded
}

// PODCheck is how the place a proof of delivery was recorded compares with
// the destination.
type PODCheck struct {
	Driver            pgtype.UUID // who recorded it
	HaveFix, Verified bool
	Distance          float64 // metres from the destination
	Reason            string  // the driver's, for being elsewhere
}

// Changed is the event on the bus for each event in a shipment's history
// (shipment.<type>), and what kiflet.events carries of it (version 1).
// Consumers elsewhere fetch more if they need it; customers' details stay
// out of the broker.
type Changed struct {
	ShipmentID     string    `json:"shipment_id"`
	TrackingNumber string    `json:"tracking_number"`
	Status         string    `json:"status"`
	Type           string    `json:"type"`
	FromStatus     string    `json:"from_status,omitempty"`
	ToStatus       string    `json:"to_status,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	DriverID       string    `json:"driver_id,omitempty"`
	ActorID        string    `json:"actor_id,omitempty"`
	Source         string    `json:"source"`
	Lat            *float64  `json:"lat,omitempty"`
	Lng            *float64  `json:"lng,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`

	// For the subscribers in this process: the shipment as it is after the
	// change, who made it, and the change in full.
	Shipment Shipment       `json:"-"`
	Actor    identity.Actor `json:"-"`
	Change   Change         `json:"-"`
}

// History records what happens to shipments and tells the modules that
// react.
type History interface {
	// Record appends a change to a shipment's history and publishes it as
	// shipment.<type>, in tx. False, with nothing published, when the
	// device sent it before (the same client ID). sh is the shipment as it
	// is after the change.
	Record(ctx context.Context, tx events.Tx, a identity.Actor, sh Shipment, c Change) (bool, error)
}
