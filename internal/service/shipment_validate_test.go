package service

import (
	"testing"
	"time"
)

// A booking's every invalid field is named at once; the first is the
// error's Field, which the dashboard's form shows.
func TestValidateShipmentNamesEveryField(t *testing.T) {
	ok := ShipmentInput{
		OriginCity: "Addis Ababa", OriginAddress: "Kality, gate 3",
		DestinationCity: "Adama", DestinationAddress: "Industrial zone",
		CustomerName: "Abyssinia Traders", CustomerPhone: "0911223344",
		CargoValue: "1250.50",
	}
	now := time.Now()
	if _, err := validateShipment(ok, now); err != nil {
		t.Fatalf("a good booking: %v", err)
	}
	bad := ok
	bad.Priority, bad.DestinationCity, bad.CustomerName, bad.CustomerPhone, bad.CargoValue = "whenever", "Atlantis", "", "12", "-3"
	_, err := validateShipment(bad, now)
	e, isErr := AsError(err)
	if !isErr {
		t.Fatalf("a bad booking: %v", err)
	}
	for _, f := range []string{"priority", "destination_city", "customer_name", "customer_phone", "cargo_value"} {
		if e.Fields[f] == "" {
			t.Errorf("%s isn't named: %v", f, e.Fields)
		}
	}
	if len(e.Fields) != 5 || e.Field != "priority" || e.Message != e.Fields["priority"] {
		t.Errorf("fields %v, first %q: %q", e.Fields, e.Field, e.Message)
	}
}
