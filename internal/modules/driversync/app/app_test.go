package app

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/driversync"
	"github.com/kiflet/backend/internal/modules/identity"
	"github.com/kiflet/backend/internal/modules/shipments"
)

func id(b byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{b}, Valid: true} }

func TestForChange(t *testing.T) {
	load, abebe, selam, office, event := id(1), id(2), id(3), id(4), id(5)
	changed := func(status string, driver pgtype.UUID, actor pgtype.UUID, c shipments.Change) shipments.Changed {
		return shipments.Changed{
			EventID:  event,
			Shipment: shipments.Shipment{ID: load, Status: status, DriverID: driver},
			Actor:    identity.Actor{UserID: actor},
			Change:   c,
		}
	}
	tests := []struct {
		name string
		m    shipments.Changed
		want []driversync.Entry
	}{
		{"reassigned: the new driver gets it, the old one lets go",
			changed("assigned", selam, office, shipments.Change{Type: "assigned", DriverID: selam, PreviousDriverID: abebe}),
			[]driversync.Entry{
				{DriverID: selam, Kind: driversync.KindShipment, RefID: load, Op: driversync.OpUpsert},
				{DriverID: abebe, Kind: driversync.KindShipment, RefID: load, Op: driversync.OpRemove, Reason: driversync.Reassigned},
			}},
		{"unassigned",
			changed("pending", pgtype.UUID{}, office, shipments.Change{Type: "unassigned", PreviousDriverID: abebe}),
			[]driversync.Entry{{DriverID: abebe, Kind: driversync.KindShipment, RefID: load, Op: driversync.OpRemove, Reason: driversync.Unassigned}}},
		{"cancelled and taken away",
			changed("cancelled", pgtype.UUID{}, office, shipments.Change{Type: "unassigned", PreviousDriverID: abebe}),
			[]driversync.Entry{{DriverID: abebe, Kind: driversync.KindShipment, RefID: load, Op: driversync.OpRemove, Reason: driversync.Cancelled}}},
		{"cancelled, still the driver's: the load and the office's word",
			changed("cancelled", abebe, office, shipments.Change{Type: "status_changed", DriverID: abebe, VisibleToDriver: true}),
			[]driversync.Entry{
				{DriverID: abebe, Kind: driversync.KindShipment, RefID: load, Op: driversync.OpUpsert},
				{DriverID: abebe, Kind: driversync.KindMessage, RefID: event, Op: driversync.OpUpsert},
			}},
		{"a note to the driver leaves the load as it was",
			changed("in_transit", abebe, office, shipments.Change{Type: "note", DriverID: abebe, VisibleToDriver: true}),
			[]driversync.Entry{{DriverID: abebe, Kind: driversync.KindMessage, RefID: event, Op: driversync.OpUpsert}}},
		{"the driver's own move comes back as the load, not as a message",
			changed("in_transit", abebe, abebe, shipments.Change{Type: "status_changed", DriverID: abebe, VisibleToDriver: true}),
			[]driversync.Entry{{DriverID: abebe, Kind: driversync.KindShipment, RefID: load, Op: driversync.OpUpsert}}},
		{"a load no one drives",
			changed("pending", pgtype.UUID{}, office, shipments.Change{Type: "edited"}),
			nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ForChange(tt.m)
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("entry %d: %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestHonours(t *testing.T) {
	h := driversync.Head{Seq: 10, PrunedThrough: 4}
	for seq, want := range map[int64]bool{3: false, 4: true, 10: true, 11: false} {
		if h.Honours(seq) != want {
			t.Errorf("Honours(%d) = %v", seq, !want)
		}
	}
}
