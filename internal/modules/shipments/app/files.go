package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/shipments"
	"github.com/kiflet/backend/internal/modules/shipments/domain"
	"github.com/kiflet/backend/internal/modules/shipments/store"
	"github.com/kiflet/backend/internal/platform/database"
)

var _ shipments.Files = Service{}

func (Service) Documents(ctx context.Context, conn database.Conn, tenantID, shipmentID pgtype.UUID) ([]shipments.Document, error) {
	return store.New(conn).ListShipmentDocuments(ctx, store.ListShipmentDocumentsParams{TenantID: tenantID, ShipmentID: shipmentID})
}

func (Service) Document(ctx context.Context, conn database.Conn, tenantID, shipmentID, id pgtype.UUID) (shipments.Document, error) {
	d, err := store.New(conn).GetShipmentDocument(ctx, store.GetShipmentDocumentParams{TenantID: tenantID, ShipmentID: shipmentID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return d, shipments.ErrNoDocument
	}
	return d, err
}

func (Service) HasRoomForDocument(ctx context.Context, conn database.Conn, tenantID, shipmentID pgtype.UUID) error {
	n, err := store.New(conn).CountShipmentDocuments(ctx, store.CountShipmentDocumentsParams{TenantID: tenantID, ShipmentID: shipmentID})
	if err != nil {
		return err
	}
	if n >= domain.MaxDocumentsPerLoad {
		return domain.ErrTooManyDocuments
	}
	return nil
}

func (s Service) AddDocument(ctx context.Context, conn database.Conn, d shipments.NewDocument) (shipments.Document, error) {
	if err := s.HasRoomForDocument(ctx, conn, d.TenantID, d.ShipmentID); err != nil {
		return shipments.Document{}, err
	}
	return store.New(conn).CreateShipmentDocument(ctx, store.CreateShipmentDocumentParams{
		TenantID: d.TenantID, ShipmentID: d.ShipmentID, Kind: d.File.Kind, Name: d.File.Name, StorageKey: d.StorageKey,
		ContentType: d.File.ContentType, SizeBytes: d.SizeBytes, UploadedBy: d.UploadedBy, UploadedByName: d.UploadedByName,
	})
}

func (Service) RemoveDocument(ctx context.Context, conn database.Conn, tenantID, shipmentID, id pgtype.UUID) (string, error) {
	key, err := store.New(conn).DeleteShipmentDocument(ctx, store.DeleteShipmentDocumentParams{TenantID: tenantID, ShipmentID: shipmentID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", shipments.ErrNoDocument
	}
	return key, err
}

func (Service) Charges(ctx context.Context, conn database.Conn, tenantID, shipmentID pgtype.UUID) ([]shipments.Charge, error) {
	return store.New(conn).ListShipmentCharges(ctx, store.ListShipmentChargesParams{TenantID: tenantID, ShipmentID: shipmentID})
}

func (Service) AddCharge(ctx context.Context, conn database.Conn, tenantID, shipmentID, by pgtype.UUID, description string, santim int64) error {
	description, err := domain.CheckCharge(description, santim)
	if err != nil {
		return err
	}
	q := store.New(conn)
	n, err := q.CountShipmentCharges(ctx, store.CountShipmentChargesParams{TenantID: tenantID, ShipmentID: shipmentID})
	if err != nil {
		return err
	}
	if n >= domain.MaxChargesPerShipment {
		return domain.ErrTooManyCharges
	}
	_, err = q.CreateShipmentCharge(ctx, store.CreateShipmentChargeParams{
		TenantID: tenantID, ShipmentID: shipmentID, Description: description, AmountSantim: santim, CreatedBy: by,
	})
	return err
}

func (Service) RemoveCharge(ctx context.Context, conn database.Conn, tenantID, shipmentID, id pgtype.UUID) error {
	n, err := store.New(conn).DeleteShipmentCharge(ctx, store.DeleteShipmentChargeParams{TenantID: tenantID, ShipmentID: shipmentID, ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return shipments.ErrNoCharge
	}
	return nil
}
