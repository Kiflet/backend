package service

import (
	"context"
	"github.com/kiflet/backend/internal/modules/shipments"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/db"
	"github.com/kiflet/backend/internal/model"
	"github.com/kiflet/backend/internal/platform/geo"
	"github.com/kiflet/backend/pkg/metrics"
)

// TrackingService records driver positions and serves the live map
// (FR-025–028).
type TrackingService struct{ *core }

// Driver connectivity on the map: live within 15 minutes, stale within four
// hours (the offline-alert threshold), offline beyond.
const (
	liveWindow  = 15 * time.Minute
	staleWindow = 4 * time.Hour
)

// DriverPin is a driver's last known position on the dispatcher map.
type DriverPin struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Phone    string     `json:"phone"`
	Lat      float64    `json:"lat"`
	Lng      float64    `json:"lng"`
	SpeedKPH float64    `json:"speed_kph"`
	Battery  *int32     `json:"battery"`
	At       time.Time  `json:"at"`
	LastSync *time.Time `json:"last_sync"`
	Paused   bool       `json:"paused"`
	// PowerSaving: the phone records a position every 15 minutes.
	PowerSaving bool   `json:"power_saving"`
	State       string `json:"state"`
	// Region is the region of the nearest known town, for the map filter.
	Region string `json:"region"`
}

// ShipmentPin is an active shipment on the map: its destination, and the
// line from where it was collected.
type ShipmentPin struct {
	ID             string     `json:"id"`
	TrackingNumber string     `json:"tracking_number"`
	Status         string     `json:"status"`
	StatusLabel    string     `json:"status_label"`
	Priority       string     `json:"priority"`
	Customer       string     `json:"customer"`
	Origin         string     `json:"origin"`
	OriginLat      *float64   `json:"origin_lat"`
	OriginLng      *float64   `json:"origin_lng"`
	Destination    string     `json:"destination"`
	Lat            *float64   `json:"lat"`
	Lng            *float64   `json:"lng"`
	ETA            *time.Time `json:"eta"`
	DriverID       string     `json:"driver_id"`
	DriverName     string     `json:"driver_name"`
	// Regions are the regions of the origin and the destination, for the
	// map filter.
	Regions []string `json:"regions"`
}

// MapData is everything the live map draws.
type MapData struct {
	Drivers   []DriverPin   `json:"drivers"`
	Shipments []ShipmentPin `json:"shipments"`
}

// ConnectionState classifies how recently a device was heard from.
func ConnectionState(last time.Time, now time.Time) string {
	switch age := now.Sub(last); {
	case last.IsZero():
		return "never"
	case age <= liveWindow:
		return "live"
	case age <= staleWindow:
		return "stale"
	default:
		return "offline"
	}
}

// Map returns driver positions from the last week and active shipments.
func (s *TrackingService) Map(ctx context.Context, tenantID pgtype.UUID) (*MapData, error) {
	locs, err := s.q.GetLatestDriverLocations(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	m := &MapData{Drivers: make([]DriverPin, 0, len(locs)), Shipments: []ShipmentPin{}}
	for _, l := range locs {
		last := l.RecordedAt.Time
		if l.LastSyncAt.Valid && l.LastSyncAt.Time.After(last) {
			last = l.LastSyncAt.Time
		}
		m.Drivers = append(m.Drivers, DriverPin{
			ID: l.DriverID.String(), Name: l.DriverName, Phone: deref(l.DriverPhone),
			Lat: l.Latitude, Lng: l.Longitude, SpeedKPH: numericOr(l.SpeedKph), Battery: l.BatteryLevel,
			At: l.RecordedAt.Time, LastSync: timePtr(l.LastSyncAt), Paused: deref(l.TrackingPaused), PowerSaving: l.PowerSaving,
			State: ConnectionState(last, now), Region: regionAt(l.Latitude, l.Longitude),
		})
	}
	ships, err := s.q.ListActiveShipmentsForMap(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, sh := range ships {
		pin := ShipmentPin{
			ID: sh.ID.String(), TrackingNumber: sh.TrackingNumber, Status: sh.Status, StatusLabel: model.StatusLabel(sh.Status),
			Priority: sh.Priority, Customer: sh.CustomerName,
			Origin: deref(sh.OriginCity), OriginLat: sh.OriginLat, OriginLng: sh.OriginLng,
			Destination: deref(sh.DestinationCity), Lat: sh.DestinationLat, Lng: sh.DestinationLng,
			ETA: timePtr(sh.EstimatedDelivery), DriverID: uuidString(sh.DriverID), DriverName: deref(sh.DriverName),
			Regions: []string{},
		}
		for _, end := range [][2]*float64{{sh.OriginLat, sh.OriginLng}, {sh.DestinationLat, sh.DestinationLng}} {
			if end[0] == nil || end[1] == nil {
				continue
			}
			if r := regionAt(*end[0], *end[1]); r != "" && !slices.Contains(pin.Regions, r) {
				pin.Regions = append(pin.Regions, r)
			}
		}
		m.Shipments = append(m.Shipments, pin)
	}
	return m, nil
}

// regionAt names the region of the known town nearest a point, or nothing
// when no town is within 150 km: a point that far out is in no region the
// map filter offers.
func regionAt(lat, lng float64) string {
	p, d := geo.Nearest(lat, lng)
	if d > 150000 {
		return ""
	}
	return p.Region
}

// Ping is one GPS fix from a driver's device.
type Ping struct {
	ClientID   string      `json:"client_id"`
	ShipmentID pgtype.UUID `json:"shipment_id"`
	Lat        float64     `json:"lat"`
	Lng        float64     `json:"lng"`
	Accuracy   *float64    `json:"accuracy,omitempty"`
	Speed      *float64    `json:"speed,omitempty"`
	Heading    *float64    `json:"heading,omitempty"`
	Battery    *int        `json:"battery,omitempty"`
	RecordedAt time.Time   `json:"recorded_at"`
}

// PositionBatch is a driver's checked GPS fixes from one sync, as the
// tracking workers take them from RabbitMQ (tracking.positions).
type PositionBatch struct {
	TenantID   pgtype.UUID `json:"tenant_id"`
	DriverID   pgtype.UUID `json:"driver_id"`
	DriverName string      `json:"driver_name"`
	DeviceID   string      `json:"device_id"`
	Pings      []Ping      `json:"pings"`
}

// trackingBatch is how many fixes go to the database in one statement.
const trackingBatch = 1000

// record takes a batch of fixes from a driver, discarding ones that cannot
// be real. With RabbitMQ the rest go to the tracking workers, and are
// counted as stored once RabbitMQ holds them; without it, or when it can't
// take them, they are stored at once. allowed is the set of shipments the
// driver may attribute positions to.
func (s *TrackingService) record(ctx context.Context, a Actor, deviceID string, pings []Ping, allowed map[pgtype.UUID]bool) (stored int, rejected []string) {
	now := s.now()
	valid := make([]Ping, 0, len(pings))
	for _, p := range pings {
		switch {
		case !allowed[p.ShipmentID],
			math.IsNaN(p.Lat) || math.IsNaN(p.Lng) || !geo.InServiceArea(p.Lat, p.Lng),
			p.Accuracy != nil && *p.Accuracy > 5000,
			p.RecordedAt.After(now.Add(time.Minute)),
			p.RecordedAt.Before(now.Add(-30 * 24 * time.Hour)):
			rejected = append(rejected, p.ClientID)
			continue
		}
		if p.Battery != nil && (*p.Battery < 0 || *p.Battery > 100) {
			p.Battery = nil
		}
		if p.Speed != nil && (*p.Speed < 0 || *p.Speed >= 200) {
			p.Speed = nil
		}
		if p.Heading != nil && (*p.Heading < 0 || *p.Heading >= 360) {
			p.Heading = nil
		}
		valid = append(valid, p)
	}
	if len(valid) == 0 {
		return 0, rejected
	}
	b := PositionBatch{TenantID: a.TenantID, DriverID: a.UserID, DriverName: a.Name, DeviceID: deviceID, Pings: valid}
	if s.positions != nil {
		err := s.positions.Enqueue(ctx, b)
		if err == nil {
			metrics.PositionsQueued.WithLabelValues("queue").Add(float64(len(valid)))
			return len(valid), rejected
		}
		// Stored here instead: slower, but nothing waits on RabbitMQ.
		s.log.Warn().Err(err).Int("fixes", len(valid)).Msg("queue gps fixes; storing them directly")
	}
	metrics.PositionsQueued.WithLabelValues("direct").Add(float64(len(valid)))
	stored, _ = s.store(ctx, b)
	return stored, rejected
}

// StorePositions writes a batch the tracking workers took from RabbitMQ.
// It fails if any fix could not be written, for the batch to be tried
// again: fixes stored already are skipped then, by their client IDs.
func (s *TrackingService) StorePositions(ctx context.Context, b PositionBatch) error {
	_, err := s.store(ctx, b)
	return err
}

// store writes fixes in statements of trackingBatch, keeps the newest as
// the driver's position, and pushes it to the map. A statement that fails
// stores none of its fixes; the others still go in.
func (s *TrackingService) store(ctx context.Context, b PositionBatch) (stored int, err error) {
	// or is a number for the batch insert, which reads -1 as "none".
	or := func(v *float64) float64 {
		if v == nil {
			return -1
		}
		return *v
	}
	var newest *Ping
	for chunk := range slices.Chunk(b.Pings, trackingBatch) {
		params := db.CreateTrackingEventsParams{TenantID: b.TenantID, DriverID: b.DriverID, DeviceID: toText(b.DeviceID)}
		for _, p := range chunk {
			battery := int32(-1)
			if p.Battery != nil {
				battery = int32(*p.Battery)
			}
			params.ShipmentIds = append(params.ShipmentIds, p.ShipmentID)
			params.Longitudes = append(params.Longitudes, p.Lng)
			params.Latitudes = append(params.Latitudes, p.Lat)
			params.Accuracies = append(params.Accuracies, or(p.Accuracy))
			params.Speeds = append(params.Speeds, or(p.Speed))
			params.Headings = append(params.Headings, or(p.Heading))
			params.Batteries = append(params.Batteries, battery)
			params.Recorded = append(params.Recorded, toTimestamp(p.RecordedAt))
			params.ClientIds = append(params.ClientIds, p.ClientID)
		}
		// The phone (or RabbitMQ) keeps what it wasn't told was stored,
		// and sends it again.
		if e := s.q.CreateTrackingEvents(ctx, params); e != nil {
			s.log.Error().Err(e).Int("fixes", len(chunk)).Msg("store gps fixes")
			err = e
			continue
		}
		stored += len(chunk)
		for i := range chunk {
			if newest == nil || chunk[i].RecordedAt.After(newest.RecordedAt) {
				newest = &chunk[i]
			}
		}
	}
	if newest != nil {
		pos := db.SetDriverPositionParams{
			DriverID: b.DriverID, TenantID: b.TenantID, Longitude: newest.Lng, Latitude: newest.Lat,
			RecordedAt: toTimestamp(newest.RecordedAt),
		}
		if newest.Speed != nil {
			pos.SpeedKph = toNumeric(*newest.Speed)
		}
		if newest.Battery != nil {
			pos.BatteryLevel = ptr(int32(*newest.Battery))
		}
		if e := s.q.SetDriverPosition(ctx, pos); e != nil {
			s.log.Error().Err(e).Msg("keep the driver's newest position")
			err = e
		}
		s.publish(b.TenantID, "location", map[string]any{
			"driver_id": b.DriverID.String(), "name": b.DriverName, "lat": newest.Lat, "lng": newest.Lng,
			"at": newest.RecordedAt, "speed_kph": newest.Speed, "battery": newest.Battery,
		})
	}
	return stored, err
}

// Progress is how far along its route a load is, in percent, for the
// progress bars: the share of the straight line from origin to destination
// that the driver's last position has covered. A load that has arrived is
// at 100. It is nil when it can't be told: the load isn't under way, an end
// has no coordinates, or the driver has sent no position.
func Progress(sh shipments.Shipment, pos *DriverPin) *int {
	full := 100
	switch sh.Status {
	case model.StatusArrived, model.StatusDelivered:
		return &full
	case model.StatusInTransit, model.StatusDelayed, model.StatusIssue:
	default:
		return nil
	}
	if pos == nil || sh.OriginLat == nil || sh.OriginLng == nil || sh.DestinationLat == nil || sh.DestinationLng == nil {
		return nil
	}
	whole := geo.DistanceMeters(*sh.OriginLat, *sh.OriginLng, *sh.DestinationLat, *sh.DestinationLng)
	if whole < 1000 {
		return nil
	}
	left := geo.DistanceMeters(pos.Lat, pos.Lng, *sh.DestinationLat, *sh.DestinationLng)
	// Short of arriving, a load is never shown as done, nor as not started.
	p := int(math.Round(100 * (1 - left/whole)))
	p = max(1, min(p, 99))
	return &p
}
