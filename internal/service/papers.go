package service

import (
	"context"
	"fmt"
	fleetdomain "github.com/kiflet/backend/internal/modules/fleet/domain"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/db"
	"github.com/kiflet/backend/internal/model"
)

// The papers and their states are the fleet module's (fleet/domain).
const PapersNotice = fleetdomain.PapersNotice

// Alert types for papers.
const (
	AlertLicenceDue    = "licence_due"
	AlertInsuranceDue  = "insurance_due"
	AlertInspectionDue = "inspection_due"
)

// Paper states.
const (
	PaperNone   = fleetdomain.PaperNone
	PaperValid  = fleetdomain.PaperValid
	PaperDue    = fleetdomain.PaperDue
	PaperLapsed = fleetdomain.PaperLapsed
)

// PaperState is where papers valid to the end of day stand on today.
var PaperState = fleetdomain.PaperState

// Today is the date in the company's time zone.
func (c *core) Today() time.Time {
	return c.now().In(c.cfg.Location)
}

// FormatDay writes a date as "2 Dec 2026".
func FormatDay(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2 Jan 2006")
}

// parseDay reads a date from a form (2026-12-02). Empty is no date.
var parseDay = fleetdomain.ParseDay

func sameDay(a, b pgtype.Date) bool {
	return a.Valid == b.Valid && (!a.Valid || a.Time.Equal(b.Time))
}

// papersMessage is what the alert about papers says.
func papersMessage(kind, name string, due pgtype.Date, lapsed bool) string {
	day := FormatDay(due)
	switch kind {
	case "licence":
		if lapsed {
			return fmt.Sprintf("%s's driving licence ran out on %s. They can't be given trips until the renewed one is recorded.", name, day)
		}
		return fmt.Sprintf("%s's driving licence runs out on %s. After that they can't be given trips.", name, day)
	case "insurance":
		if lapsed {
			return fmt.Sprintf("The insurance on %s ran out on %s.", name, day)
		}
		return fmt.Sprintf("The insurance on %s runs out on %s.", name, day)
	}
	if lapsed {
		return fmt.Sprintf("The annual inspection (ቦሎ) of %s was due on %s.", name, day)
	}
	return fmt.Sprintf("The annual inspection (ቦሎ) of %s is due on %s.", name, day)
}

var papersAlert = map[string]string{"licence": AlertLicenceDue, "insurance": AlertInsuranceDue, "inspection": AlertInspectionDue}

// CheckPapers reminds each company of papers that run out within
// PapersNotice, and again, critically, once they have. Each reminder is
// raised once per date: renewing the papers resolves it.
func (j *Jobs) CheckPapers(ctx context.Context) error {
	today := j.Today()
	rows, err := j.fleet.PapersDue(ctx, today.Add(PapersNotice))
	if err != nil {
		return err
	}
	for _, r := range rows {
		lapsed := PaperState(r.DueOn, today) == PaperLapsed
		episode, severity := r.DueOn.Time.Format("2006-01-02"), SeverityWarning
		if lapsed {
			episode, severity = episode+"/lapsed", SeverityCritical
		}
		a := newAlert{TenantID: r.TenantID, Type: papersAlert[r.Kind], Severity: severity,
			Message: papersMessage(r.Kind, r.Name, r.DueOn, lapsed), Episode: episode}
		if r.Kind == "licence" {
			a.DriverID = r.SubjectID
		} else {
			a.VehicleID = r.SubjectID
		}
		err := j.tx(ctx, func(q *db.Queries, after *afterCommit) error {
			// The reminder that it was coming gives way to the one that it
			// has come.
			if err := q.AcknowledgePapersAlerts(ctx, db.AcknowledgePapersAlertsParams{
				TenantID: r.TenantID, AlertType: a.Type, DriverID: a.DriverID, VehicleID: a.VehicleID, KeepEpisode: &episode,
			}); err != nil {
				return err
			}
			j.raise(ctx, q, after, a)
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// papersRenewed resolves the alerts about papers whose date has changed.
func (c *core) papersRenewed(ctx context.Context, q *db.Queries, tenantID pgtype.UUID, alertType string, driverID, vehicleID pgtype.UUID) error {
	return q.AcknowledgePapersAlerts(ctx, db.AcknowledgePapersAlertsParams{
		TenantID: tenantID, AlertType: alertType, DriverID: driverID, VehicleID: vehicleID,
	})
}

// licenceLapsed is why a driver can't be given a trip, or nil.
func (c *core) licenceLapsed(d db.User) error {
	if PaperState(d.LicenceExpiresOn, c.Today()) != PaperLapsed {
		return nil
	}
	return invalid("driver_id", fmt.Sprintf("%s's driving licence ran out on %s. Record the renewed licence on their page before giving them a trip.",
		d.FullName, FormatDay(d.LicenceExpiresOn)))
}

// RemindLicence texts a driver whose licence runs out soon, or has, to
// renew it.
func (s *UserService) RemindLicence(ctx context.Context, p *Principal, id pgtype.UUID, ci ClientInfo) error {
	if !p.CanManageFleet() {
		return ErrUnauthorized
	}
	return s.tx(ctx, func(q *db.Queries, after *afterCommit) error {
		d, err := s.Get(ctx, p.TenantID, id)
		if err != nil {
			return err
		}
		if d.Role != model.RoleDriver || !d.IsActive {
			return notFound("Driver not found.")
		}
		state := PaperState(d.LicenceExpiresOn, s.Today())
		if state != PaperDue && state != PaperLapsed {
			return conflict(d.FullName + "'s licence isn't due for renewal.")
		}
		if d.Phone == nil {
			return conflict(d.FullName + " has no phone number to text.")
		}
		tenant, err := s.tenant(ctx, q, p.TenantID)
		if err != nil {
			return err
		}
		verb := "runs out on"
		if state == PaperLapsed {
			verb = "ran out on"
		}
		s.enqueueSMS(ctx, q, after, outboundSMS{
			TenantID: p.TenantID, Phone: *d.Phone, Purpose: PurposeDriverMessage,
			Body: fmt.Sprintf("%s: your driving licence %s %s. Please renew it and show the office the new one.",
				tenant.Name, verb, FormatDay(d.LicenceExpiresOn)),
		})
		s.audit(ctx, q, p.Actor(SourceDashboard, ci.IP, ci.UserAgent), "licence_reminder_sent", "user", id, nil,
			map[string]any{"expires": FormatDay(d.LicenceExpiresOn)})
		return nil
	})
}
