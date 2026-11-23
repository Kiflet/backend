package service

import (
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/shipments"
	shipmentsdomain "github.com/kiflet/backend/internal/modules/shipments/domain"
	"github.com/kiflet/backend/internal/platform/text"
)

// What the public API sends, and webhooks with it: one shape for a
// shipment and one for an event, so that an integration reads the same
// fields from both. static/openapi.json describes them, and a test in
// internal/web holds the two together.

// CodeJSON is a code with its words for the office, in the reader's
// language (docs/architecture.md, section 11, "Codes and their words"). A
// client acts on the code and shows the label; a code it doesn't know, it
// shows by its label.
type CodeJSON struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// CodeOf is a code of a kind with its office words in lang.
func CodeOf(lang, kind, code string) CodeJSON {
	return CodeJSON{Code: code, Label: text.Label(lang, text.Office, kind, code)}
}

// codeOrNil is CodeOf, or nil for no code.
func codeOrNil(lang, kind, code string) *CodeJSON {
	if code == "" {
		return nil
	}
	c := CodeOf(lang, kind, code)
	return &c
}

// PlaceJSON is an origin or a destination.
type PlaceJSON struct {
	City    string   `json:"city"`
	Address string   `json:"address"`
	Lat     *float64 `json:"lat"`
	Lng     *float64 `json:"lng"`
}

// PartyJSON is a customer or a receiver.
type PartyJSON struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

// CargoJSON is what is carried.
type CargoJSON struct {
	Description string  `json:"description"`
	WeightKg    float64 `json:"weight_kg"`
	// ValueSantim is what the cargo is worth, in santim; 0 when not given.
	ValueSantim int64 `json:"value_santim"`
	// Pieces is how many pieces the load is, 0 when not counted, and Unit
	// what they are ("bales").
	Pieces int    `json:"pieces"`
	Unit   string `json:"unit"`
}

// ShipmentJSON is a shipment as the API and webhooks send it.
type ShipmentJSON struct {
	ID             string    `json:"id"`
	TrackingNumber string    `json:"tracking_number"`
	Status         CodeJSON  `json:"status"`
	StatusReason   *CodeJSON `json:"status_reason"`
	Priority       CodeJSON  `json:"priority"`
	// Actions are the API's operations the shipment allows now.
	Actions []string `json:"actions"`
	// Version is raised by every change; it is the ETag, for If-Match.
	Version           int32      `json:"version"`
	Origin            PlaceJSON  `json:"origin"`
	Destination       PlaceJSON  `json:"destination"`
	Customer          PartyJSON  `json:"customer"`
	Receiver          *PartyJSON `json:"receiver"`
	Cargo             CargoJSON  `json:"cargo"`
	Instructions      string     `json:"instructions"`
	DriverID          *string    `json:"driver_id"`
	VehicleID         *string    `json:"vehicle_id"`
	PlannedPickup     *time.Time `json:"planned_pickup"`
	EstimatedDelivery *time.Time `json:"estimated_delivery"`
	PickedUpAt        *time.Time `json:"picked_up_at"`
	DeliveredAt       *time.Time `json:"delivered_at"`
	ReceiptConfirmed  bool       `json:"receipt_confirmed"`
	TrackingURL       string     `json:"tracking_url"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// EventJSON is one entry of a shipment's history.
type EventJSON struct {
	Type       string    `json:"type"`
	FromStatus *CodeJSON `json:"from_status"`
	ToStatus   *CodeJSON `json:"to_status"`
	Reason     *CodeJSON `json:"reason"`
	Note       string    `json:"note"`
	Source     string    `json:"source"`
	Actor      string    `json:"actor"`
	Lat        *float64  `json:"lat"`
	Lng        *float64  `json:"lng"`
	At         time.Time `json:"at"`
}

// NewEventJSON is an entry of a shipment's history, its codes with their
// words in lang.
func NewEventJSON(lang, typ, from, to, reason string) EventJSON {
	return EventJSON{
		Type: typ, FromStatus: codeOrNil(lang, text.KindStatus, from), ToStatus: codeOrNil(lang, text.KindStatus, to),
		Reason: codeOrNil(lang, text.KindReason, reason),
	}
}

// apiActions are the actions the API has an operation for: a cancelled
// shipment is reopened on the dashboard.
var apiActions = []string{shipmentsdomain.ActionUpdate, shipmentsdomain.ActionAssign, shipmentsdomain.ActionUnassign, shipmentsdomain.ActionCancel}

// AsJSON is a shipment as the API sends it, its codes' words in lang.
// baseURL is the public origin, for the tracking page's address.
func AsJSON(sh shipments.Shipment, baseURL, lang string) ShipmentJSON {
	actions := slices.DeleteFunc(shipmentsdomain.Actions(sh.Status), func(a string) bool { return !slices.Contains(apiActions, a) })
	out := ShipmentJSON{
		ID: sh.ID.String(), TrackingNumber: sh.TrackingNumber,
		Status:       CodeOf(lang, text.KindStatus, sh.Status),
		StatusReason: codeOrNil(lang, text.KindReason, deref(sh.StatusReason)),
		Priority:     CodeOf(lang, text.KindPriority, sh.Priority),
		Actions:      actions, Version: sh.Version,
		Origin:      PlaceJSON{City: deref(sh.OriginCity), Address: sh.OriginAddress, Lat: sh.OriginLat, Lng: sh.OriginLng},
		Destination: PlaceJSON{City: deref(sh.DestinationCity), Address: sh.DestinationAddress, Lat: sh.DestinationLat, Lng: sh.DestinationLng},
		Customer:    PartyJSON{Name: sh.CustomerName, Phone: sh.CustomerPhone},
		Cargo: CargoJSON{Description: deref(sh.CargoDescription), WeightKg: numericOr(sh.CargoWeight), ValueSantim: sh.CargoValueSantim,
			Pieces: int(derefInt(sh.CargoPieces)), Unit: deref(sh.CargoUnit)},
		Instructions: deref(sh.SpecialInstructions), DriverID: uuidJSON(sh.DriverID), VehicleID: uuidJSON(sh.VehicleID),
		PlannedPickup: utcJSON(sh.PlannedPickupAt), EstimatedDelivery: utcJSON(sh.EstimatedDelivery),
		PickedUpAt: utcJSON(sh.PickedUpAt), DeliveredAt: utcJSON(sh.ActualDelivery),
		ReceiptConfirmed: sh.CustomerConfirmedAt.Valid,
		TrackingURL:      baseURL + "/track/" + sh.TrackingNumber,
		CreatedAt:        sh.CreatedAt.Time.UTC(), UpdatedAt: sh.UpdatedAt.Time.UTC(),
	}
	if sh.ConsigneePhone != nil {
		out.Receiver = &PartyJSON{Name: deref(sh.ConsigneeName), Phone: *sh.ConsigneePhone}
	}
	return out
}

func utcJSON(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	u := t.Time.UTC()
	return &u
}

func uuidJSON(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.String()
	return &s
}
