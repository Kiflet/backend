package domain

import (
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/platform/apperror"
)

func TestVehicleFormClean(t *testing.T) {
	ok := VehicleForm{PlateNumber: "  3-a  12345 aa ", VehicleType: "tanker", CapacityKg: 12000, Active: true,
		InsuranceExpires: "2026-12-02"}
	v, err := ok.Clean()
	if err != nil {
		t.Fatal(err)
	}
	if v.Plate != "3-A 12345 AA" || v.Type == nil || *v.Type != "tanker" || !v.CapacityKg.Valid || !v.Active ||
		!v.Insurance.Valid || v.Insurance.Time.Day() != 2 || v.Inspection.Valid {
		t.Errorf("cleaned: %+v", v)
	}

	for _, c := range []struct{ in, out float64 }{{0, 0}, {math.NaN(), 0}} {
		f := ok
		f.CapacityKg = c.in
		if v, err := f.Clean(); err != nil || v.CapacityKg.Valid {
			t.Errorf("capacity %v: %+v, %v; want none", c.in, v.CapacityKg, err)
		}
	}

	for field, change := range map[string]func(*VehicleForm){
		"plate_number":         func(f *VehicleForm) { f.PlateNumber = "  " },
		"vehicle_type":         func(f *VehicleForm) { f.VehicleType = "rocket" },
		"capacity_kg":          func(f *VehicleForm) { f.CapacityKg = 100001 },
		"insurance_expires_on": func(f *VehicleForm) { f.InsuranceExpires = "2.12.2026" },
		"inspection_due_on":    func(f *VehicleForm) { f.InspectionDue = "1999-01-01" },
	} {
		f := ok
		change(&f)
		_, err := f.Clean()
		if e, isErr := apperror.As(err); !isErr || e.Field != field {
			t.Errorf("%s wrong: got %v", field, err)
		}
	}
}

func TestPaperState(t *testing.T) {
	today := time.Date(2026, 11, 19, 15, 0, 0, 0, time.UTC)
	day := func(y int, m time.Month, d int) pgtype.Date {
		return pgtype.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
	}
	for _, c := range []struct {
		day  pgtype.Date
		want string
	}{
		{pgtype.Date{}, PaperNone},
		{day(2026, 11, 18), PaperLapsed},
		{day(2026, 11, 19), PaperDue},
		{day(2026, 12, 19), PaperDue},
		{day(2026, 12, 20), PaperValid},
	} {
		if got := PaperState(c.day, today); got != c.want {
			t.Errorf("PaperState(%v) = %q, want %q", c.day.Time, got, c.want)
		}
	}
}
