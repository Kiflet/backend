package domain

import (
	"testing"

	"github.com/kiflet/backend/internal/platform/apperror"
)

func TestSettingsClean(t *testing.T) {
	ok := Settings{
		Name: "  Abay   Freight ", TIN: "00 1234 5678", ContactPhone: "+251 911 234 567",
		ContactEmail: " office@abay.et ", RegionCity: "adama", MaxActivePerDriver: 3,
	}
	got, err := ok.Clean("am")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Abay Freight" || got.TIN != "0012345678" || got.ContactPhone != "0911234567" ||
		got.ContactEmail != "office@abay.et" || got.RegionCity != "Adama" || got.CustomerLanguage != "am" {
		t.Errorf("cleaned: %+v", got)
	}

	for field, change := range map[string]func(*Settings){
		"name":                  func(s *Settings) { s.Name = "A" },
		"contact_phone":         func(s *Settings) { s.ContactPhone = "12345" },
		"contact_email":         func(s *Settings) { s.ContactEmail = "not an address" },
		"region_city":           func(s *Settings) { s.RegionCity = "Atlantis" },
		"max_active_per_driver": func(s *Settings) { s.MaxActivePerDriver = 51 },
		"tin":                   func(s *Settings) { s.TIN = "12345" },
		"customer_language":     func(s *Settings) { s.CustomerLanguage = "fr" },
	} {
		in := ok
		change(&in)
		_, err := in.Clean("en")
		if e, isErr := apperror.As(err); !isErr || e.Field != field {
			t.Errorf("%s wrong: got %v", field, err)
		}
	}
}
