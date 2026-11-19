package domain

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// The papers a driver and a truck need on the road: the driver's licence,
// and the truck's insurance and annual inspection (ቦሎ). Each is stored as
// the last day it is valid. The office is reminded PapersNotice before, by
// an alert, and a driver whose licence has run out can't be given a trip.

// PapersNotice is how long before papers run out the office is reminded.
const PapersNotice = 30 * 24 * time.Hour

// Paper states.
const (
	PaperNone   = ""       // no date recorded
	PaperValid  = "valid"  // more than PapersNotice to go
	PaperDue    = "due"    // runs out within PapersNotice
	PaperLapsed = "lapsed" // the last valid day has passed
)

// PaperState is where papers valid to the end of day stand on today.
func PaperState(day pgtype.Date, today time.Time) string {
	if !day.Valid {
		return PaperNone
	}
	last := time.Date(day.Time.Year(), day.Time.Month(), day.Time.Day(), 0, 0, 0, 0, time.UTC)
	now := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	switch {
	case now.After(last):
		return PaperLapsed
	case !last.After(now.Add(PapersNotice)):
		return PaperDue
	}
	return PaperValid
}
