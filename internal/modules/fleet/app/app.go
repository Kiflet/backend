// Package app is the fleet module's implementation of fleet.Fleet.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/fleet"
	"github.com/kiflet/backend/internal/modules/fleet/domain"
	"github.com/kiflet/backend/internal/modules/fleet/store"
	"github.com/kiflet/backend/internal/platform/apperror"
	"github.com/kiflet/backend/internal/platform/database"
)

// Service is fleet.Fleet on Postgres.
type Service struct{ q *store.Queries }

var _ fleet.Fleet = Service{}

// New is the fleet module, read through conn (the pool) when a call has
// no connection of its own.
func New(conn database.Conn) Service { return Service{q: store.New(conn)} }

func (Service) Vehicle(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID) (fleet.Vehicle, error) {
	v, err := store.New(conn).GetVehicleByID(ctx, store.GetVehicleByIDParams{ID: id, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return v, fleet.ErrNoVehicle
	}
	return v, err
}

func (Service) ActiveVehicles(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) ([]fleet.Vehicle, error) {
	return store.New(conn).ListActiveVehiclesByTenant(ctx, tenantID)
}

func (Service) CountVehicles(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) (int64, error) {
	return store.New(conn).CountVehiclesByTenant(ctx, tenantID)
}

func (Service) CreateVehicle(ctx context.Context, conn database.Conn, tenantID pgtype.UUID, v domain.Vehicle) (fleet.Vehicle, error) {
	out, err := store.New(conn).CreateVehicle(ctx, store.CreateVehicleParams{
		TenantID: tenantID, PlateNumber: v.Plate, VehicleType: v.Type, CapacityKg: v.CapacityKg,
		InsuranceExpiresOn: v.Insurance, InspectionDueOn: v.Inspection,
	})
	return out, duplicatePlate(err)
}

func (s Service) UpdateVehicle(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID, v domain.Vehicle) (before, after fleet.Vehicle, err error) {
	if before, err = s.Vehicle(ctx, conn, tenantID, id); err != nil {
		return before, after, err
	}
	q := store.New(conn)
	after, err = q.UpdateVehicle(ctx, store.UpdateVehicleParams{
		ID: id, TenantID: tenantID, PlateNumber: v.Plate, VehicleType: v.Type, CapacityKg: v.CapacityKg, IsActive: v.Active,
		InsuranceExpiresOn: v.Insurance, InspectionDueOn: v.Inspection,
	})
	if err != nil {
		return before, after, duplicatePlate(err)
	}
	if before.IsActive && !after.IsActive {
		err = q.ClearVehicleFromDrivers(ctx, store.ClearVehicleFromDriversParams{VehicleID: id, TenantID: tenantID})
	}
	return before, after, err
}

func (Service) SetDriverVehicle(ctx context.Context, conn database.Conn, tenantID, driverID, vehicleID pgtype.UUID) error {
	return store.New(conn).SetDriverVehicle(ctx, store.SetDriverVehicleParams{ID: driverID, TenantID: tenantID, VehicleID: vehicleID})
}

func (Service) LockDriver(ctx context.Context, conn database.Conn, tenantID, driverID pgtype.UUID) error {
	n, err := store.New(conn).LockDriver(ctx, store.LockDriverParams{ID: driverID, TenantID: tenantID})
	if err == nil && n == 0 {
		return fleet.ErrNoDriver
	}
	return err
}

func (Service) SetDriverLicence(ctx context.Context, conn database.Conn, tenantID, driverID pgtype.UUID, expires pgtype.Date) error {
	return store.New(conn).SetDriverLicence(ctx, store.SetDriverLicenceParams{ID: driverID, TenantID: tenantID, LicenceExpiresOn: expires})
}

func (Service) SetDriverAppSettings(ctx context.Context, conn database.Conn, tenantID, driverID pgtype.UUID, s fleet.AppSettings) error {
	return store.New(conn).SetDriverAppSettings(ctx, store.SetDriverAppSettingsParams{
		Language: s.Language, BatterySaver: s.BatterySaver, Vibrate: s.Vibrate, ID: driverID, TenantID: tenantID,
	})
}

func (s Service) PapersDue(ctx context.Context, until time.Time) ([]fleet.PaperDue, error) {
	day := pgtype.Date{Time: time.Date(until.Year(), until.Month(), until.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
	return s.q.ListPapersDue(ctx, day)
}

// duplicatePlate words a plate already registered.
func duplicatePlate(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return apperror.Invalid("plate_number", "A vehicle with this plate is already registered.")
	}
	return err
}
