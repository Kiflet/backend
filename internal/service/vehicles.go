package service

import (
	"context"
	"github.com/kiflet/backend/internal/modules/fleet"
	fleetdomain "github.com/kiflet/backend/internal/modules/fleet/domain"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/db"
)

// The vehicle types and form are the fleet module's (fleet/domain).
var (
	VehicleTypes     = fleetdomain.VehicleTypes
	VehicleTypeLabel = fleetdomain.VehicleTypeLabel
)

// VehicleService manages the fleet (FR-005).
type VehicleService struct{ *core }

// VehicleInput is the add/edit vehicle form.
type VehicleInput = fleet.VehicleForm

// List returns every vehicle with its drivers and current load.
func (s *VehicleService) List(ctx context.Context, tenantID pgtype.UUID) ([]db.ListVehiclesWithDriversRow, error) {
	return s.q.ListVehiclesWithDrivers(ctx, tenantID)
}

// Active returns vehicles in service, for pickers.
func (s *VehicleService) Active(ctx context.Context, tenantID pgtype.UUID) ([]db.Vehicle, error) {
	return s.activeVehicles(ctx, s.q, tenantID)
}

// Get loads one vehicle.
func (s *VehicleService) Get(ctx context.Context, tenantID, id pgtype.UUID) (db.Vehicle, error) {
	return s.vehicle(ctx, s.q, tenantID, id)
}

// Recent lists a vehicle's latest shipments.
func (s *VehicleService) Recent(ctx context.Context, tenantID, id pgtype.UUID) ([]db.ListRecentShipmentsByVehicleRow, error) {
	return s.q.ListRecentShipmentsByVehicle(ctx, db.ListRecentShipmentsByVehicleParams{VehicleID: id, TenantID: tenantID, Limit: 20})
}

// Create registers a vehicle.
func (s *VehicleService) Create(ctx context.Context, p *Principal, in VehicleInput, ci ClientInfo) (db.Vehicle, error) {
	if !p.CanManageFleet() {
		return db.Vehicle{}, ErrUnauthorized
	}
	vehicle, err := in.Clean()
	if err != nil {
		return db.Vehicle{}, err
	}
	tenant, err := s.tenant(ctx, s.q, p.TenantID)
	if err != nil {
		return db.Vehicle{}, err
	}
	if err := s.requireWritable(ctx, s.q, tenant); err != nil {
		return db.Vehicle{}, err
	}
	created, err := s.fleet.CreateVehicle(ctx, s.q.Conn(), p.TenantID, vehicle)
	if err != nil {
		return db.Vehicle{}, err
	}
	v := db.Vehicle(created)
	s.audit(ctx, s.q, p.Actor(SourceDashboard, ci.IP, ci.UserAgent), "vehicle_created", "vehicle", v.ID, nil, map[string]any{"plate": v.PlateNumber})
	return v, nil
}

// Update edits a vehicle. Taking it out of service unlinks it from drivers
// who had it as their usual truck.
func (s *VehicleService) Update(ctx context.Context, p *Principal, id pgtype.UUID, in VehicleInput, ci ClientInfo) (db.Vehicle, error) {
	if !p.CanManageFleet() {
		return db.Vehicle{}, ErrUnauthorized
	}
	vehicle, err := in.Clean()
	if err != nil {
		return db.Vehicle{}, err
	}
	var out db.Vehicle
	err = s.tx(ctx, func(q *db.Queries, after *afterCommit) error {
		was, now, err := s.fleet.UpdateVehicle(ctx, q.Conn(), p.TenantID, id, vehicle)
		if err != nil {
			return err
		}
		cur := db.Vehicle(was)
		out = db.Vehicle(now)
		for _, c := range []struct {
			alert    string
			was, now pgtype.Date
		}{{AlertInsuranceDue, cur.InsuranceExpiresOn, out.InsuranceExpiresOn}, {AlertInspectionDue, cur.InspectionDueOn, out.InspectionDueOn}} {
			if !sameDay(c.was, c.now) || (cur.IsActive && !in.Active) {
				if err := s.papersRenewed(ctx, q, p.TenantID, c.alert, pgtype.UUID{}, id); err != nil {
					return err
				}
			}
		}
		s.audit(ctx, q, p.Actor(SourceDashboard, ci.IP, ci.UserAgent), "vehicle_updated", "vehicle", id,
			map[string]any{"plate": cur.PlateNumber, "active": cur.IsActive, "insurance": FormatDay(cur.InsuranceExpiresOn), "inspection": FormatDay(cur.InspectionDueOn)},
			map[string]any{"plate": out.PlateNumber, "active": out.IsActive, "insurance": FormatDay(out.InsuranceExpiresOn), "inspection": FormatDay(out.InspectionDueOn)})
		return nil
	})
	return out, err
}
