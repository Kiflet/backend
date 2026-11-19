package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/db"
	"github.com/kiflet/backend/internal/modules/shipments"
	shipmentsdomain "github.com/kiflet/backend/internal/modules/shipments/domain"
)

// A shipment's documents and charges are the shipments module's
// (shipments.Files). What stays here is storing the files, and who may.

// DocumentKinds are what a document can be, in the order the form offers
// them.
var (
	DocumentKinds     = shipmentsdomain.DocumentKinds
	DocumentKindLabel = shipmentsdomain.DocumentKindLabel
)

// MaxDocumentBytes is the largest document taken.
const MaxDocumentBytes = shipmentsdomain.MaxDocumentBytes

// Documents lists a shipment's documents, newest first.
func (s *ShipmentService) Documents(ctx context.Context, tenantID, shipmentID pgtype.UUID) ([]shipments.Document, error) {
	return s.shipmentFiles.Documents(ctx, s.q.Conn(), tenantID, shipmentID)
}

// AddDocument stores a file with a shipment.
func (s *ShipmentService) AddDocument(ctx context.Context, a Actor, shipmentID pgtype.UUID, kind, name string, body []byte) (shipments.Document, error) {
	var doc shipments.Document
	file, err := shipmentsdomain.CheckDocument(kind, name, body)
	if err != nil {
		return doc, err
	}
	if s.media == nil || s.media.store == nil {
		return doc, errors.New("file storage is not configured")
	}

	sh, err := s.Get(ctx, a.TenantID, shipmentID)
	if err != nil {
		return doc, err
	}
	if sh.AnonymisedAt.Valid {
		return doc, invalid("file", "This shipment's details were removed at the end of the keeping period, so it takes no new documents.")
	}
	// Checked before the file is stored, so that a full shipment costs no
	// upload; AddDocument checks again.
	if err := s.shipmentFiles.HasRoomForDocument(ctx, s.q.Conn(), a.TenantID, shipmentID); err != nil {
		return doc, err
	}

	key := fmt.Sprintf("documents/%s/%s/%s%s", uuidString(a.TenantID), uuidString(shipmentID), randomToken(16), file.Ext)
	if _, err := s.media.store.Upload(ctx, key, bytes.NewReader(body), file.ContentType); err != nil {
		return doc, fmt.Errorf("store document: %w", err)
	}
	err = s.tx(ctx, func(q *db.Queries, _ *afterCommit) error {
		tenant, err := s.tenant(ctx, q, a.TenantID)
		if err != nil {
			return err
		}
		if err := s.requireWritable(ctx, q, tenant); err != nil {
			return err
		}
		doc, err = s.shipmentFiles.AddDocument(ctx, q.Conn(), shipments.NewDocument{
			TenantID: a.TenantID, ShipmentID: shipmentID, File: file, StorageKey: key,
			SizeBytes: int32(len(body)), UploadedBy: a.UserID, UploadedByName: a.Name,
		})
		if err != nil {
			return err
		}
		s.audit(ctx, q, a, "document_added", "shipment", shipmentID, nil, map[string]any{"document": file.Name, "kind": kind, "bytes": len(body)})
		return nil
	})
	if err != nil {
		_ = s.media.store.Delete(context.WithoutCancel(ctx), key)
		return shipments.Document{}, err
	}
	return doc, nil
}

// OpenDocument streams one of a shipment's documents.
func (s *ShipmentService) OpenDocument(ctx context.Context, tenantID, shipmentID, id pgtype.UUID) (shipments.Document, io.ReadCloser, error) {
	doc, err := s.shipmentFiles.Document(ctx, s.q.Conn(), tenantID, shipmentID, id)
	if err != nil {
		return doc, nil, err
	}
	rc, _, err := s.media.Open(ctx, doc.StorageKey)
	return doc, rc, err
}

// DeleteDocument removes a document and its file.
func (s *ShipmentService) DeleteDocument(ctx context.Context, a Actor, shipmentID, id pgtype.UUID) error {
	return s.tx(ctx, func(q *db.Queries, after *afterCommit) error {
		key, err := s.shipmentFiles.RemoveDocument(ctx, q.Conn(), a.TenantID, shipmentID, id)
		if err != nil {
			return err
		}
		s.audit(ctx, q, a, "document_removed", "shipment", shipmentID, map[string]any{"document": uuidString(id)}, nil)
		after.do(func() { s.media.Delete(context.Background(), key) })
		return nil
	})
}

// Charges lists a shipment's charges in the order they were added, and
// their total in birr.
func (s *ShipmentService) Charges(ctx context.Context, tenantID, shipmentID pgtype.UUID) ([]shipments.Charge, float64, error) {
	rows, err := s.shipmentFiles.Charges(ctx, s.q.Conn(), tenantID, shipmentID)
	total := 0.0
	for _, c := range rows {
		total += numericOr(c.Amount)
	}
	return rows, math.Round(total*100) / 100, err
}

// AddCharge adds a line to what the customer is charged for a load. A
// negative amount is a discount.
func (s *ShipmentService) AddCharge(ctx context.Context, a Actor, shipmentID pgtype.UUID, description string, amount float64) error {
	description, amount, err := shipmentsdomain.CheckCharge(description, amount)
	if err != nil {
		return err
	}
	if _, err := s.Get(ctx, a.TenantID, shipmentID); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries, _ *afterCommit) error {
		tenant, err := s.tenant(ctx, q, a.TenantID)
		if err != nil {
			return err
		}
		if err := s.requireWritable(ctx, q, tenant); err != nil {
			return err
		}
		if err := s.shipmentFiles.AddCharge(ctx, q.Conn(), a.TenantID, shipmentID, a.UserID, description, amount); err != nil {
			return err
		}
		s.audit(ctx, q, a, "charge_added", "shipment", shipmentID, nil, map[string]any{"description": description, "amount": amount})
		return nil
	})
}

// DeleteCharge removes a line from a shipment's charges.
func (s *ShipmentService) DeleteCharge(ctx context.Context, a Actor, shipmentID, id pgtype.UUID) error {
	return s.tx(ctx, func(q *db.Queries, _ *afterCommit) error {
		if err := s.shipmentFiles.RemoveCharge(ctx, q.Conn(), a.TenantID, shipmentID, id); err != nil {
			return err
		}
		s.audit(ctx, q, a, "charge_removed", "shipment", shipmentID, map[string]any{"charge": uuidString(id)}, nil)
		return nil
	})
}
