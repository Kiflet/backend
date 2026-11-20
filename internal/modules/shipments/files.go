package shipments

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/shipments/domain"
	"github.com/kiflet/backend/internal/modules/shipments/store"
	"github.com/kiflet/backend/internal/platform/apperror"
	"github.com/kiflet/backend/internal/platform/database"
)

// A document kept with a shipment, and a line of what its customer is
// charged.
type (
	Document = store.ShipmentDocument
	Charge   = store.ShipmentCharge
)

// NewDocument is a document whose file is stored (under StorageKey), to
// record with its shipment.
type NewDocument struct {
	TenantID   pgtype.UUID
	ShipmentID pgtype.UUID
	File       domain.File
	StorageKey string
	SizeBytes  int32
	// UploadedBy and UploadedByName are who added it.
	UploadedBy     pgtype.UUID
	UploadedByName string
}

// The errors of a shipment's documents and charges.
var (
	ErrNoDocument = apperror.NotFound("Document not found.")
	ErrNoCharge   = apperror.NotFound("Charge not found.")
)

// Files are a shipment's documents and charges. The files themselves are
// kept by the caller, in object storage; this keeps their record.
type Files interface {
	// Documents are a shipment's documents, newest first.
	Documents(ctx context.Context, conn database.Conn, tenantID, shipmentID pgtype.UUID) ([]Document, error)
	// Document is one of them; one not there is ErrNoDocument.
	Document(ctx context.Context, conn database.Conn, tenantID, shipmentID, id pgtype.UUID) (Document, error)
	// HasRoomForDocument is domain.ErrTooManyDocuments when a shipment
	// holds all it may: checked before a file is stored, and again by
	// AddDocument.
	HasRoomForDocument(ctx context.Context, conn database.Conn, tenantID, shipmentID pgtype.UUID) error
	AddDocument(ctx context.Context, conn database.Conn, d NewDocument) (Document, error)
	// RemoveDocument forgets a document and returns where its file is
	// stored, for the caller to delete; one not there is ErrNoDocument.
	RemoveDocument(ctx context.Context, conn database.Conn, tenantID, shipmentID, id pgtype.UUID) (storageKey string, err error)

	// Charges are a shipment's charges, in the order they were added.
	Charges(ctx context.Context, conn database.Conn, tenantID, shipmentID pgtype.UUID) ([]Charge, error)
	// AddCharge checks a charge (domain.CheckCharge) and adds it, up to
	// domain.MaxChargesPerShipment.
	AddCharge(ctx context.Context, conn database.Conn, tenantID, shipmentID, by pgtype.UUID, description string, santim int64) error
	// RemoveCharge removes one; one not there is ErrNoCharge.
	RemoveCharge(ctx context.Context, conn database.Conn, tenantID, shipmentID, id pgtype.UUID) error
}
