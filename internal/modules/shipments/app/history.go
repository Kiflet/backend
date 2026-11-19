package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/identity"
	"github.com/kiflet/backend/internal/modules/shipments"
	"github.com/kiflet/backend/internal/platform/events"
)

var _ shipments.History = Service{}

func (s Service) Record(ctx context.Context, tx events.Tx, a identity.Actor, sh shipments.Shipment, c shipments.Change) (bool, error) {
	if c.OccurredAt.IsZero() {
		c.OccurredAt = time.Now()
	}
	var details []byte
	if len(c.Details) > 0 {
		var err error
		if details, err = json.Marshal(c.Details); err != nil {
			return false, fmt.Errorf("%s details: %w", c.Type, err)
		}
	}
	_, err := s.AddEvent(ctx, tx.Conn, shipments.NewEvent{
		TenantID:         sh.TenantID,
		ShipmentID:       sh.ID,
		EventType:        c.Type,
		FromStatus:       text(c.From),
		ToStatus:         text(c.To),
		DriverID:         c.DriverID,
		PreviousDriverID: c.PreviousDriverID,
		ActorID:          a.UserID,
		ActorName:        a.Name,
		Source:           a.Source,
		Note:             text(c.Note),
		Reason:           text(c.Reason),
		Latitude:         c.Lat,
		Longitude:        c.Lng,
		VisibleToDriver:  c.VisibleToDriver,
		Public:           c.Public,
		Details:          details,
		ClientID:         text(c.ClientID),
		OccurredAt:       pgtype.Timestamptz{Time: c.OccurredAt, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if s.bus == nil {
		return true, nil
	}
	return true, s.bus.Publish(ctx, tx, events.Event{
		Type: "shipment." + c.Type, TenantID: sh.TenantID,
		Payload: shipments.Changed{
			ShipmentID: sh.ID.String(), TrackingNumber: sh.TrackingNumber, Status: sh.Status,
			Type: c.Type, FromStatus: c.From, ToStatus: c.To, Reason: c.Reason,
			DriverID: uuid(c.DriverID), ActorID: uuid(a.UserID), Source: a.Source,
			Lat: c.Lat, Lng: c.Lng, OccurredAt: c.OccurredAt,
			Shipment: sh, Actor: a, Change: c,
		},
	})
}

// text is a column that is null when empty.
func text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func uuid(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return u.String()
}
