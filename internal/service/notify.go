package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/db"
	"github.com/kiflet/backend/internal/model"
	"github.com/kiflet/backend/internal/modules/shipments"
	"github.com/kiflet/backend/internal/platform/events"
	"github.com/kiflet/backend/internal/platform/text"
)

// The texts customers and drivers receive. They are kept short — a single
// 160-character SMS where possible — and always name the company, since the
// sender ID is the platform's rather than theirs.

func (c *core) trackURL(sh shipments.Shipment) string {
	return c.cfg.BaseURL + "/track/" + sh.TrackingNumber
}

func (c *core) confirmURL(token string) string {
	return c.cfg.BaseURL + "/c/" + token
}

func (c *core) formatWhen(t time.Time) string {
	return t.In(c.cfg.Location).Format("Mon 2 Jan 15:04")
}

func place(city *string, address string) string {
	if city != nil && *city != "" {
		return *city
	}
	return address
}

// The customer's texts are written in the language the company chose for
// its customers (tenants.customer_language), from the catalogue in
// internal/platform/text.

func (s *ShipmentService) customerCreatedText(t db.Tenant, sh shipments.Shipment) string {
	return text.T(t.CustomerLanguage, "sms.created",
		t.Name, sh.TrackingNumber, place(sh.OriginCity, sh.OriginAddress),
		place(sh.DestinationCity, sh.DestinationAddress), s.trackURL(sh))
}

// customerStatusText is what a status change says to the receiver and to
// the customer who booked, if the tenant sends a text for it. They are
// told apart only when the shipment names a consignee: the receiver is the
// one the driver hands over to, so the arrival and the link to confirm
// receipt are theirs, and the customer is told the delivery was made.
func (s *ShipmentService) customerStatusText(t db.Tenant, sh shipments.Shipment, status string, firstDeparture bool, reason, recipient string) (toReceiver, toCustomer string) {
	lang := t.CustomerLanguage
	dest := place(sh.DestinationCity, sh.DestinationAddress)
	switch status {
	case model.StatusInTransit:
		if !t.SmsOnDispatched || !firstDeparture {
			return "", ""
		}
		eta := ""
		if sh.EstimatedDelivery.Valid {
			eta = text.T(lang, "sms.eta", text.ShortWhen(lang, sh.EstimatedDelivery.Time.In(s.cfg.Location)))
		}
		body := text.T(lang, "sms.on_way", t.Name, sh.TrackingNumber, dest, eta, s.trackURL(sh))
		return body, body
	case model.StatusDelayed:
		if !t.SmsOnDelayed {
			return "", ""
		}
		body := text.T(lang, "sms.delayed",
			t.Name, sh.TrackingNumber, strings.ToLower(text.Reason(lang, reason)), s.trackURL(sh))
		return body, body
	case model.StatusArrived:
		if !t.SmsOnArrived {
			return "", ""
		}
		return text.T(lang, "sms.arrived", t.Name, sh.TrackingNumber, dest), ""
	case model.StatusDelivered:
		if !t.SmsOnDelivered {
			return "", ""
		}
		who := ""
		if recipient != "" {
			who = text.T(lang, "sms.delivered_to", recipient)
		}
		at := s.now()
		if sh.ActualDelivery.Valid {
			at = sh.ActualDelivery.Time
		}
		link := s.trackURL(sh)
		if sh.ConfirmToken != nil {
			link = s.confirmURL(*sh.ConfirmToken)
		}
		when := text.ShortWhen(lang, at.In(s.cfg.Location))
		return text.T(lang, "sms.delivered", t.Name, sh.TrackingNumber, who, when, link),
			text.T(lang, "sms.delivered_booker", t.Name, sh.TrackingNumber, who, when, s.trackURL(sh))
	}
	return "", ""
}

func (s *ShipmentService) customerCancelledText(t db.Tenant, sh shipments.Shipment) string {
	contact := ""
	if t.ContactPhone != nil && *t.ContactPhone != "" {
		contact = text.T(t.CustomerLanguage, "sms.questions", *t.ContactPhone)
	}
	return text.T(t.CustomerLanguage, "sms.cancelled", t.Name, sh.TrackingNumber, contact)
}

// receiver is who takes delivery of a shipment: its consignee, or the
// customer who booked it when it names none.
func receiver(sh shipments.Shipment) (name, phone string) {
	if sh.ConsigneePhone != nil {
		return deref(sh.ConsigneeName), *sh.ConsigneePhone
	}
	return sh.CustomerName, sh.CustomerPhone
}

// notifyCustomer texts the people a shipment is for: toReceiver goes to
// whoever takes delivery, and toCustomer to the customer who booked, when
// that is someone else. An empty text is not sent.
func (s *ShipmentService) notifyCustomer(ctx context.Context, q *db.Queries, after *afterCommit, t db.Tenant, sh shipments.Shipment, toReceiver, toCustomer string) {
	send := func(phone, body string) {
		if body == "" {
			return
		}
		s.enqueueSMS(ctx, q, after, outboundSMS{
			TenantID: t.ID, ShipmentID: sh.ID, Phone: phone, Body: body, Purpose: PurposeCustomerUpdate,
		})
	}
	_, to := receiver(sh)
	send(to, toReceiver)
	if sh.ConsigneePhone != nil {
		send(sh.CustomerPhone, toCustomer)
	}
}

// notifyFor texts the customer and the driver what an event in a
// shipment's history means for them, in the transaction of the change
// (FR-011, FR-016): it subscribes to shipment.* on the bus. Which texts a
// company sends is in its settings.
func (s *ShipmentService) notifyFor(ctx context.Context, tx events.Tx, ev events.Event) error {
	m, ok := ev.Payload.(shipments.Changed)
	if !ok {
		return fmt.Errorf("%s carries %T, not a shipments.Changed", ev.Type, ev.Payload)
	}
	e, sh := m.Change, m.Shipment
	switch {
	case e.Type == "created", e.Type == "status_changed":
	case e.Type == "assigned" && e.NewDriver:
	case e.Type == "edited" && e.DriverID.Valid && editedFields(e).has("destination"):
	default:
		return nil
	}
	q, after := db.New(tx.Conn), afterOf(tx)
	t, err := s.tenant(ctx, q, sh.TenantID)
	if err != nil {
		return err
	}
	driverText := func(id pgtype.UUID, body, purpose string) {
		if d, err := s.user(ctx, q, sh.TenantID, id); err == nil && d.Phone != nil {
			s.enqueueSMS(ctx, q, after, outboundSMS{
				TenantID: sh.TenantID, ShipmentID: sh.ID, Phone: *d.Phone, Purpose: purpose, Body: body,
			})
		}
	}

	switch e.Type {
	case "created":
		if t.SmsOnCreated {
			body := s.customerCreatedText(t, sh)
			s.notifyCustomer(ctx, q, after, t, sh, body, body)
		}

	case "assigned":
		if t.SmsDriverOnAssign {
			driverText(e.DriverID, s.driverAssignedText(t, sh), PurposeDriverAssigned)
		}
		// A load handed over on the road: the driver who had it stops.
		if e.PreviousDriverID.Valid && model.IsOnRoad(e.From) {
			to, err := s.user(ctx, q, sh.TenantID, e.DriverID)
			if err != nil {
				return err
			}
			driverText(e.PreviousDriverID, fmt.Sprintf("%s: shipment %s has been handed to %s.",
				t.Name, ShortRef(sh.TrackingNumber), to.FullName), PurposeDriverMessage)
		}

	case "edited":
		// A new drop-off matters enough to text (FR-011 AC-011.4): the
		// driver may be on the road with the app closed.
		driverText(e.DriverID, fmt.Sprintf("%s: shipment %s now goes to %s. %s.",
			t.Name, ShortRef(sh.TrackingNumber), place(sh.DestinationCity, sh.DestinationAddress), meet(sh)), PurposeDriverMessage)

	case "status_changed":
		if e.To != model.StatusCancelled {
			toReceiver, toCustomer := s.customerStatusText(t, sh, e.To, e.FirstDeparture, e.Reason, e.Recipient)
			s.notifyCustomer(ctx, q, after, t, sh, toReceiver, toCustomer)
			return nil
		}
		if t.SmsOnCancelled {
			body := s.customerCancelledText(t, sh)
			s.notifyCustomer(ctx, q, after, t, sh, body, body)
		}
		if e.DriverID.Valid && model.IsActiveStatus(e.From) {
			driverText(e.DriverID, fmt.Sprintf("%s: shipment %s (%s) is CANCELLED: %s. Do not deliver it; call the office.",
				t.Name, ShortRef(sh.TrackingNumber), deref(sh.DestinationCity), model.ReasonLabel(e.Reason)), PurposeDriverMessage)
		}
	}
	return nil
}

// fieldList is the fields an "edited" event names.
type fieldList []string

func (f fieldList) has(name string) bool { return contains(f, name) }

func editedFields(e shipments.Change) fieldList {
	f, _ := e.Details["fields"].([]string)
	return f
}

// meet is who the driver is told to meet: "Customer Name 09…", or "Deliver
// to Name 09…" when the receiver isn't the customer.
func meet(sh shipments.Shipment) string {
	name, phone := receiver(sh)
	if sh.ConsigneePhone != nil {
		return "Deliver to " + name + " " + phone
	}
	return "Customer " + name + " " + phone
}

// driverAssignedText tells a driver of a load they have been given.
func (s *ShipmentService) driverAssignedText(t db.Tenant, sh shipments.Shipment) string {
	ref := ShortRef(sh.TrackingNumber)
	cargo := ""
	if sh.CargoDescription != nil {
		cargo = " " + truncate(*sh.CargoDescription, 40) + "."
	}
	priority := ""
	if sh.Priority != model.PriorityNormal {
		priority = strings.ToUpper(sh.Priority) + " "
	}
	pickup := ""
	if sh.PlannedPickupAt.Valid {
		pickup = " Pickup " + s.formatWhen(sh.PlannedPickupAt.Time) + "."
	}
	// Without an inbound number there is nothing to text, only the app.
	report := " Open the driver app when you leave."
	if s.cfg.SMSNumber != "" {
		report = fmt.Sprintf(" When you leave, open the driver app or text ET %s 1 to %s.", ref, s.cfg.SMSNumber)
	}
	return fmt.Sprintf("%s: %snew shipment %s, %s to %s.%s %s.%s%s",
		t.Name, priority, ref, place(sh.OriginCity, sh.OriginAddress), place(sh.DestinationCity, sh.DestinationAddress),
		pickup, meet(sh), cargo, report)
}

// SMSPreview is the text a customer gets when their load sets off, in one
// language, as the settings page shows it.
type SMSPreview struct {
	Lang  string
	Body  string
	Chars int
	Parts int32
}

// SMSPreviews is that text in each language, for a sample load on its way
// to Dire Dawa, expected tomorrow afternoon.
func (s *TenantService) SMSPreviews(t db.Tenant) []SMSPreview {
	now := s.now().In(s.cfg.Location)
	eta := time.Date(now.Year(), now.Month(), now.Day()+1, 14, 0, 0, 0, s.cfg.Location)
	sh := shipments.Shipment{TrackingNumber: "ET-" + now.Format("20060102") + "-7KQ2MZ4RXA"}
	var out []SMSPreview
	for _, l := range text.Languages {
		body := text.T(l.Code, "sms.on_way", t.Name, sh.TrackingNumber, "Dire Dawa", // the town as the load names it, in either language
			text.T(l.Code, "sms.eta", text.ShortWhen(l.Code, eta)), s.trackURL(sh))
		out = append(out, SMSPreview{Lang: l.Code, Body: body, Chars: utf8.RuneCountInString(body), Parts: smsParts(body)})
	}
	return out
}
