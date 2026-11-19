// Package domain is the tenancy module's rules, without I/O.
package domain

import (
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/kiflet/backend/internal/platform/apperror"
	"github.com/kiflet/backend/internal/platform/geo"
	"github.com/kiflet/backend/internal/platform/phone"
	"github.com/kiflet/backend/internal/platform/text"
)

// Settings is the company settings form: its profile and when its
// customers and drivers are texted.
type Settings struct {
	Name string
	// TIN is the taxpayer identification number printed on invoices.
	TIN                string
	ContactPhone       string
	ContactEmail       string
	RegionCity         string
	MaxActivePerDriver int
	SMSOnCreated       bool
	SMSOnDispatched    bool
	SMSOnArrived       bool
	SMSOnDelivered     bool
	SMSOnCancelled     bool
	SMSOnDelayed       bool
	SMSDriverOnAssign  bool
	// AlertOnDelivery raises an alert for every delivery.
	AlertOnDelivery bool
	// CustomerLanguage is the language customers are written to in. Empty
	// keeps the company's.
	CustomerLanguage string
}

// Clean checks a settings form and returns it as stored: spaces collapsed,
// the phone number in its local form, the town's own name and the TIN's
// digits. language is the company's current language, kept when the form
// has none.
func (in Settings) Clean(language string) (Settings, error) {
	out := in
	out.Name = strings.Join(strings.Fields(in.Name), " ")
	if n := utf8.RuneCountInString(out.Name); n < 2 || n > 120 {
		return out, apperror.Invalid("name", "Enter the company name.")
	}
	out.ContactPhone = ""
	if strings.TrimSpace(in.ContactPhone) != "" {
		ph, err := phone.Normalize(in.ContactPhone)
		if err != nil {
			return out, apperror.Invalid("contact_phone", err.Error())
		}
		out.ContactPhone = ph
	}
	out.ContactEmail = strings.TrimSpace(in.ContactEmail)
	if out.ContactEmail != "" {
		if _, err := mail.ParseAddress(out.ContactEmail); err != nil {
			return out, apperror.Invalid("contact_email", "Enter a valid email address.")
		}
	}
	city, ok := geo.Lookup(in.RegionCity)
	if !ok {
		return out, apperror.Invalid("region_city", "Pick your base town from the list.")
	}
	out.RegionCity = city.Name
	if in.MaxActivePerDriver < 1 || in.MaxActivePerDriver > 50 {
		return out, apperror.Invalid("max_active_per_driver", "Choose between 1 and 50.")
	}
	out.TIN = strings.Join(strings.Fields(in.TIN), "")
	if out.TIN != "" && (len(out.TIN) != 10 || strings.Trim(out.TIN, "0123456789") != "") {
		return out, apperror.Invalid("tin", "A TIN is 10 digits.")
	}
	if out.CustomerLanguage == "" {
		out.CustomerLanguage = language
	}
	if !text.Valid(out.CustomerLanguage) {
		return out, apperror.Invalid("customer_language", "Choose English or Amharic.")
	}
	return out, nil
}
