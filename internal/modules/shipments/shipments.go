// Package shipments is the shipments module (docs/architecture.md, section
// 4): the shipments and their history. It owns shipments and
// shipment_events (append-only); its rules, the statuses and who may move
// a shipment between them, are in domain.
//
// Other modules reach it only through Shipments, on their connection: a
// write within another module's transaction takes that transaction as
// conn. The argument types are the store's, named here, until booking,
// assigning and status changes move in (docs/migration.md, phases 3 and 4).
package shipments

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/shipments/store"
	"github.com/kiflet/backend/internal/platform/apperror"
	"github.com/kiflet/backend/internal/platform/database"
)

// A shipment, an entry of its history, and what the queries return.
type (
	Shipment      = store.Shipment
	Event         = store.ShipmentEvent
	StatusCount   = store.CountShipmentsByStatusRow
	PublicEvent   = store.ListPublicShipmentEventsRow
	ActivityEntry = store.ListTenantActivityRow
	DriverMessage = store.ListDriverMessagesRow
)

// The arguments of Shipments' methods.
type (
	NewShipment          = store.CreateShipmentParams
	Details              = store.UpdateShipmentDetailsParams
	ETA                  = store.SetShipmentETAParams
	Assignment           = store.SetShipmentAssignmentParams
	DriverChange         = store.SetShipmentDriverParams
	StatusDetail         = store.SetShipmentStatusDetailParams
	StatusUpdate         = store.SetShipmentStatusParams
	ConfirmToken         = store.SetShipmentConfirmTokenParams
	CustomerConfirmation = store.SetCustomerConfirmationParams
	CustomerFeedback     = store.SetCustomerFeedbackParams
	DriverShipments      = store.ListActiveShipmentsByDriverParams
	DriverCount          = store.CountActiveShipmentsByDriverParams
	SyncQuery            = store.ListShipmentsForDriverSyncParams
	DriverRecent         = store.ListRecentShipmentsByDriverParams
	DeliveredSince       = store.CountDeliveredSinceParams
	CreatedSince         = store.CountShipmentsCreatedSinceParams
	Route                = store.RouteTransitHoursParams
	NewEvent             = store.CreateShipmentEventParams
	EventsQuery          = store.ListShipmentEventsParams
	ActivityQuery        = store.ListTenantActivityParams
	DriverMessagesQuery  = store.ListDriverMessagesParams
	UnassignedQuery      = store.ListShipmentsUnassignedFromDriverSinceParams
	AssignedQuery        = store.WasAssignedToParams
	ClientEventQuery     = store.GetShipmentEventByClientIDParams
)

// ErrNoShipment is a shipment that isn't there, or isn't the company's.
var ErrNoShipment = apperror.NotFound("Shipment not found.")

// Shipments is what other modules may call.
type Shipments interface {
	History
	// Get finds one of a company's shipments; one not there is ErrNoShipment.
	Get(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID) (Shipment, error)

	// GetForUpdate is Get, locking the shipment in conn's transaction.
	GetForUpdate(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID) (Shipment, error)

	// ByTrackingNumber finds a shipment by its public number, in any company.
	ByTrackingNumber(ctx context.Context, conn database.Conn, trackingNumber string) (Shipment, error)

	// ByConfirmToken finds the shipment a customer's confirmation link is for.
	ByConfirmToken(ctx context.Context, conn database.Conn, confirmToken *string) (Shipment, error)

	// Create books a shipment.
	Create(ctx context.Context, conn database.Conn, arg NewShipment) (Shipment, error)

	// UpdateDetails changes what was booked.
	UpdateDetails(ctx context.Context, conn database.Conn, arg Details) (Shipment, error)

	// SetETA records when the shipment is expected.
	SetETA(ctx context.Context, conn database.Conn, arg ETA) error

	// SetAssignment gives the shipment a driver and vehicle, or takes them off.
	SetAssignment(ctx context.Context, conn database.Conn, arg Assignment) (Shipment, error)

	// SetDriver changes the driver alone.
	SetDriver(ctx context.Context, conn database.Conn, arg DriverChange) (Shipment, error)

	// SetStatusDetail moves the shipment to a status with its reason and times.
	SetStatusDetail(ctx context.Context, conn database.Conn, arg StatusDetail) (Shipment, error)

	// SetStatus moves the shipment to a status.
	SetStatus(ctx context.Context, conn database.Conn, arg StatusUpdate) (Shipment, error)

	// SetConfirmToken stores the hash of a customer's confirmation link.
	SetConfirmToken(ctx context.Context, conn database.Conn, arg ConfirmToken) error

	// SetCustomerConfirmation records the customer's answer to the link.
	SetCustomerConfirmation(ctx context.Context, conn database.Conn, arg CustomerConfirmation) (Shipment, error)

	// SetCustomerFeedback records the customer's rating and comment.
	SetCustomerFeedback(ctx context.Context, conn database.Conn, arg CustomerFeedback) (Shipment, error)

	// ActiveForDriver are a driver's current shipments.
	ActiveForDriver(ctx context.Context, conn database.Conn, arg DriverShipments) ([]Shipment, error)

	// CountActiveForDriver counts them.
	CountActiveForDriver(ctx context.Context, conn database.Conn, arg DriverCount) (int64, error)

	// ForDriverSync are the shipments a driver's phone should hold.
	ForDriverSync(ctx context.Context, conn database.Conn, arg SyncQuery) ([]Shipment, error)

	// ByIDs are a company's shipments of the IDs given, as they are now.
	ByIDs(ctx context.Context, conn database.Conn, tenantID pgtype.UUID, ids []pgtype.UUID) ([]Shipment, error)

	// RecentForDriver are a driver's latest shipments.
	RecentForDriver(ctx context.Context, conn database.Conn, arg DriverRecent) ([]Shipment, error)

	// CountByStatus counts a company's shipments in each status.
	CountByStatus(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) ([]StatusCount, error)

	// CountDeliveredSince counts deliveries since a time.
	CountDeliveredSince(ctx context.Context, conn database.Conn, arg DeliveredSince) (int64, error)

	// CountCreatedSince counts bookings since a time.
	CountCreatedSince(ctx context.Context, conn database.Conn, arg CreatedSince) (int64, error)

	// RouteTransitHours are how long past trips on a route took, for an ETA.
	RouteTransitHours(ctx context.Context, conn database.Conn, arg Route) ([]float64, error)

	// AddEvent appends to a shipment's history. One a device sent before (same client ID) is not added again: then no rows.
	AddEvent(ctx context.Context, conn database.Conn, arg NewEvent) (Event, error)

	// Events are a shipment's history, oldest first.
	Events(ctx context.Context, conn database.Conn, arg EventsQuery) ([]Event, error)

	// PublicEvents are the history a customer sees.
	PublicEvents(ctx context.Context, conn database.Conn, shipmentID pgtype.UUID) ([]PublicEvent, error)

	// Activity is a company's latest history, newest first.
	Activity(ctx context.Context, conn database.Conn, arg ActivityQuery) ([]ActivityEntry, error)

	// DriverMessages are the notes the office addressed to a driver: those of the IDs given, or with none the newest since a time.
	DriverMessages(ctx context.Context, conn database.Conn, arg DriverMessagesQuery) ([]DriverMessage, error)

	// UnassignedFromDriverSince are the shipments taken from a driver since a time and not handed back.
	UnassignedFromDriverSince(ctx context.Context, conn database.Conn, arg UnassignedQuery) ([]pgtype.UUID, error)

	// WasAssignedTo says whether a driver ever had a shipment.
	WasAssignedTo(ctx context.Context, conn database.Conn, arg AssignedQuery) (bool, error)

	// EventByClientID is the kind of event a device's change was recorded as, if it was.
	EventByClientID(ctx context.Context, conn database.Conn, arg ClientEventQuery) (string, error)
}
