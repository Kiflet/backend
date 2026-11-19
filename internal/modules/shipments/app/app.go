// Package app is the shipments module's implementation of
// shipments.Shipments.
package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/shipments"
	"github.com/kiflet/backend/internal/modules/shipments/store"
	"github.com/kiflet/backend/internal/platform/database"
)

// Service is shipments.Shipments on Postgres.
type Service struct{}

var _ shipments.Shipments = Service{}

// New is the shipments module.
func New() Service { return Service{} }

// found makes a shipment that isn't there shipments.ErrNoShipment.
func found(s shipments.Shipment, err error) (shipments.Shipment, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return s, shipments.ErrNoShipment
	}
	return s, err
}

func (Service) Get(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID) (shipments.Shipment, error) {
	return found(store.New(conn).GetShipmentByID(ctx, store.GetShipmentByIDParams{ID: id, TenantID: tenantID}))
}

func (Service) GetForUpdate(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID) (shipments.Shipment, error) {
	return found(store.New(conn).GetShipmentByIDForUpdate(ctx, store.GetShipmentByIDForUpdateParams{ID: id, TenantID: tenantID}))
}

func (Service) ByTrackingNumber(ctx context.Context, conn database.Conn, trackingNumber string) (shipments.Shipment, error) {
	return found(store.New(conn).GetShipmentByTrackingNumber(ctx, trackingNumber))
}

func (Service) ByConfirmToken(ctx context.Context, conn database.Conn, confirmToken *string) (shipments.Shipment, error) {
	return found(store.New(conn).GetShipmentByConfirmToken(ctx, confirmToken))
}

func (Service) Create(ctx context.Context, conn database.Conn, arg shipments.NewShipment) (shipments.Shipment, error) {
	return store.New(conn).CreateShipment(ctx, arg)
}

func (Service) UpdateDetails(ctx context.Context, conn database.Conn, arg shipments.Details) (shipments.Shipment, error) {
	return found(store.New(conn).UpdateShipmentDetails(ctx, arg))
}

func (Service) SetETA(ctx context.Context, conn database.Conn, arg shipments.ETA) error {
	return store.New(conn).SetShipmentETA(ctx, arg)
}

func (Service) SetAssignment(ctx context.Context, conn database.Conn, arg shipments.Assignment) (shipments.Shipment, error) {
	return found(store.New(conn).SetShipmentAssignment(ctx, arg))
}

func (Service) SetDriver(ctx context.Context, conn database.Conn, arg shipments.DriverChange) (shipments.Shipment, error) {
	return found(store.New(conn).SetShipmentDriver(ctx, arg))
}

func (Service) SetStatusDetail(ctx context.Context, conn database.Conn, arg shipments.StatusDetail) (shipments.Shipment, error) {
	return found(store.New(conn).SetShipmentStatusDetail(ctx, arg))
}

func (Service) SetStatus(ctx context.Context, conn database.Conn, arg shipments.StatusUpdate) (shipments.Shipment, error) {
	return found(store.New(conn).SetShipmentStatus(ctx, arg))
}

func (Service) SetConfirmToken(ctx context.Context, conn database.Conn, arg shipments.ConfirmToken) error {
	return store.New(conn).SetShipmentConfirmToken(ctx, arg)
}

func (Service) SetCustomerConfirmation(ctx context.Context, conn database.Conn, arg shipments.CustomerConfirmation) (shipments.Shipment, error) {
	return found(store.New(conn).SetCustomerConfirmation(ctx, arg))
}

func (Service) SetCustomerFeedback(ctx context.Context, conn database.Conn, arg shipments.CustomerFeedback) (shipments.Shipment, error) {
	return found(store.New(conn).SetCustomerFeedback(ctx, arg))
}

func (Service) ActiveForDriver(ctx context.Context, conn database.Conn, arg shipments.DriverShipments) ([]shipments.Shipment, error) {
	return store.New(conn).ListActiveShipmentsByDriver(ctx, arg)
}

func (Service) CountActiveForDriver(ctx context.Context, conn database.Conn, arg shipments.DriverCount) (int64, error) {
	return store.New(conn).CountActiveShipmentsByDriver(ctx, arg)
}

func (Service) ForDriverSync(ctx context.Context, conn database.Conn, arg shipments.SyncQuery) ([]shipments.Shipment, error) {
	return store.New(conn).ListShipmentsForDriverSync(ctx, arg)
}

func (Service) RecentForDriver(ctx context.Context, conn database.Conn, arg shipments.DriverRecent) ([]shipments.Shipment, error) {
	return store.New(conn).ListRecentShipmentsByDriver(ctx, arg)
}

func (Service) CountByStatus(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) ([]shipments.StatusCount, error) {
	return store.New(conn).CountShipmentsByStatus(ctx, tenantID)
}

func (Service) CountDeliveredSince(ctx context.Context, conn database.Conn, arg shipments.DeliveredSince) (int64, error) {
	return store.New(conn).CountDeliveredSince(ctx, arg)
}

func (Service) CountCreatedSince(ctx context.Context, conn database.Conn, arg shipments.CreatedSince) (int64, error) {
	return store.New(conn).CountShipmentsCreatedSince(ctx, arg)
}

func (Service) RouteTransitHours(ctx context.Context, conn database.Conn, arg shipments.Route) ([]float64, error) {
	return store.New(conn).RouteTransitHours(ctx, arg)
}

func (Service) AddEvent(ctx context.Context, conn database.Conn, arg shipments.NewEvent) (shipments.Event, error) {
	return store.New(conn).CreateShipmentEvent(ctx, arg)
}

func (Service) Events(ctx context.Context, conn database.Conn, arg shipments.EventsQuery) ([]shipments.Event, error) {
	return store.New(conn).ListShipmentEvents(ctx, arg)
}

func (Service) PublicEvents(ctx context.Context, conn database.Conn, shipmentID pgtype.UUID) ([]shipments.PublicEvent, error) {
	return store.New(conn).ListPublicShipmentEvents(ctx, shipmentID)
}

func (Service) Activity(ctx context.Context, conn database.Conn, arg shipments.ActivityQuery) ([]shipments.ActivityEntry, error) {
	return store.New(conn).ListTenantActivity(ctx, arg)
}

func (Service) DriverMessagesSince(ctx context.Context, conn database.Conn, arg shipments.DriverMessagesQuery) ([]shipments.DriverMessage, error) {
	return store.New(conn).ListDriverMessagesSince(ctx, arg)
}

func (Service) UnassignedFromDriverSince(ctx context.Context, conn database.Conn, arg shipments.UnassignedQuery) ([]pgtype.UUID, error) {
	return store.New(conn).ListShipmentsUnassignedFromDriverSince(ctx, arg)
}

func (Service) WasAssignedTo(ctx context.Context, conn database.Conn, arg shipments.AssignedQuery) (bool, error) {
	return store.New(conn).WasAssignedTo(ctx, arg)
}

func (Service) EventByClientID(ctx context.Context, conn database.Conn, arg shipments.ClientEventQuery) (string, error) {
	return store.New(conn).GetShipmentEventByClientID(ctx, arg)
}
