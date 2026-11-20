// Package fleet is the fleet module (docs/architecture.md, section 4): the
// vehicles, their papers and the drivers' profile (their vehicle, licence
// and app settings). It owns vehicles and those columns of users; the rest
// of users is identity's.
//
// Other modules reach it only through Fleet. A write within another
// module's transaction takes that transaction as conn; so does a read that
// must see what the transaction wrote.
package fleet

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/fleet/domain"
	"github.com/kiflet/backend/internal/modules/fleet/store"
	"github.com/kiflet/backend/internal/platform/apperror"
	"github.com/kiflet/backend/internal/platform/database"
)

// Vehicle is a company's vehicle.
type Vehicle = store.Vehicle

// VehicleForm is the add/edit vehicle form; Clean checks it.
type VehicleForm = domain.VehicleForm

// ErrNoVehicle is a vehicle that isn't there, or isn't the company's.
var ErrNoVehicle = apperror.NotFound("Vehicle not found.")

// ErrNoDriver is a driver who isn't there, or isn't the company's.
var ErrNoDriver = apperror.NotFound("Driver not found.")

// PaperDue is a driver's licence (Kind "licence", Subject the driver) or a
// vehicle's insurance or inspection ("insurance", "inspection", Subject the
// vehicle) that runs out on DueOn.
type PaperDue = store.ListPapersDueRow

// AppSettings are the driver app's settings chosen on the phone; nil
// leaves one as it is.
type AppSettings struct {
	Language     *string
	BatterySaver *bool
	Vibrate      *bool
}

// Fleet is what other modules may call.
type Fleet interface {
	// Vehicle finds one of a company's vehicles; one not there is
	// ErrNoVehicle.
	Vehicle(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID) (Vehicle, error)
	// ActiveVehicles are a company's vehicles in service, by plate.
	ActiveVehicles(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) ([]Vehicle, error)
	CountVehicles(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) (int64, error)
	// CreateVehicle registers a checked vehicle. A plate already
	// registered is an apperror on plate_number.
	CreateVehicle(ctx context.Context, conn database.Conn, tenantID pgtype.UUID, v domain.Vehicle) (Vehicle, error)
	// UpdateVehicle changes a vehicle and returns it before and after.
	// Taking it out of service unlinks it from the drivers who had it as
	// their usual truck.
	UpdateVehicle(ctx context.Context, conn database.Conn, tenantID, id pgtype.UUID, v domain.Vehicle) (before, after Vehicle, err error)

	// SetDriverVehicle sets a driver's usual vehicle (none: an invalid ID).
	SetDriverVehicle(ctx context.Context, conn database.Conn, tenantID, driverID, vehicleID pgtype.UUID) error
	// LockDriver holds a driver's row until the transaction (conn) ends: a
	// transaction that counts the driver's loads before giving one takes it
	// first. ErrNoDriver when there is no such driver.
	LockDriver(ctx context.Context, conn database.Conn, tenantID, driverID pgtype.UUID) error
	// SetDriverLicence records the last day a driver's licence is valid.
	SetDriverLicence(ctx context.Context, conn database.Conn, tenantID, driverID pgtype.UUID, expires pgtype.Date) error
	SetDriverAppSettings(ctx context.Context, conn database.Conn, tenantID, driverID pgtype.UUID, s AppSettings) error

	// PapersDue are the papers of every company that run out on or before
	// a day, for the papers check.
	PapersDue(ctx context.Context, until time.Time) ([]PaperDue, error)
}
