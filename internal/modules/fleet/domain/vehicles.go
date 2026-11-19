// Package domain is the fleet module's rules, without I/O.
package domain

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/platform/apperror"
)

// VehicleTypes are the kinds of vehicle a fleet registers.
var VehicleTypes = []struct{ Code, Label string }{
	{"pickup", "Pickup"},
	{"light_truck", "Light truck (up to 7 t)"},
	{"medium_truck", "Medium truck (7–15 t)"},
	{"heavy_truck", "Heavy truck (over 15 t)"},
	{"trailer", "Truck and trailer"},
	{"tanker", "Tanker"},
	{"refrigerated", "Refrigerated"},
	{"van", "Van"},
}

// VehicleTypeLabel is the display name of a vehicle type code.
func VehicleTypeLabel(code string) string {
	for _, t := range VehicleTypes {
		if t.Code == code {
			return t.Label
		}
	}
	return code
}

// VehicleForm is the add/edit vehicle form.
type VehicleForm struct {
	PlateNumber string
	VehicleType string
	CapacityKg  float64
	Active      bool
	// InsuranceExpires and InspectionDue are the last days the insurance
	// and the annual inspection (ቦሎ) are valid, as 2026-12-02; empty when
	// not recorded.
	InsuranceExpires string
	InspectionDue    string
}

// Vehicle is a vehicle form checked, as it is stored.
type Vehicle struct {
	Plate      string
	Type       *string
	CapacityKg pgtype.Numeric
	Active     bool
	Insurance  pgtype.Date
	Inspection pgtype.Date
}

// NormalisePlate is a plate number as stored: upper case, single spaces.
func NormalisePlate(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), " "))
}

// Clean checks a vehicle form.
func (f VehicleForm) Clean() (Vehicle, error) {
	v := Vehicle{Plate: NormalisePlate(f.PlateNumber), Active: f.Active}
	if v.Plate == "" {
		return v, apperror.Invalid("plate_number", "Enter the plate number.")
	}
	if utf8.RuneCountInString(v.Plate) > 20 {
		return v, apperror.Invalid("plate_number", "That plate number is too long.")
	}
	if f.VehicleType != "" {
		found := false
		for _, t := range VehicleTypes {
			found = found || t.Code == f.VehicleType
		}
		if !found {
			return v, apperror.Invalid("vehicle_type", "Choose a vehicle type.")
		}
		vt := f.VehicleType
		v.Type = &vt
	}
	if f.CapacityKg < 0 || f.CapacityKg > 100000 {
		return v, apperror.Invalid("capacity_kg", "Enter a capacity between 0 and 100,000 kg.")
	}
	// None given is no capacity; so is a reading that isn't a number.
	if f.CapacityKg != 0 && !math.IsNaN(f.CapacityKg) {
		_ = v.CapacityKg.Scan(strconv.FormatFloat(f.CapacityKg, 'f', -1, 64))
	}
	var err error
	if v.Insurance, err = ParseDay("insurance_expires_on", "the insurance runs out", f.InsuranceExpires); err != nil {
		return v, err
	}
	v.Inspection, err = ParseDay("inspection_due_on", "the inspection is due", f.InspectionDue)
	return v, err
}

// ParseDay reads a date from a form (2026-12-02). Empty is no date; what
// says which date, for the message.
func ParseDay(field, what, s string) (pgtype.Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.Date{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil || t.Year() < 2000 || t.Year() > 2100 {
		return pgtype.Date{}, apperror.Invalid(field, "Enter the date "+what+" as a day, month and year.")
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}
