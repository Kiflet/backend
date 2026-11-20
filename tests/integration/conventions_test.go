//go:build integration

package integration

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// booking is a public API booking for a new customer.
func booking() map[string]any {
	return map[string]any{
		"origin":      map[string]any{"city": "Addis Ababa", "address": "Kality, gate 3"},
		"destination": map[string]any{"city": "Adama", "address": "Industrial zone"},
		"customer":    map[string]any{"name": "Abyssinia Traders", "phone": phoneNumber()},
		"cargo":       map[string]any{"description": "Cement, 200 bags", "weight_kg": 10000},
	}
}

// TestShipmentVersions (FR-046): a shipment's version is its ETag, and a
// change sent with If-Match goes ahead only if no one has changed the
// shipment since; otherwise it is refused 412 with the shipment as it is
// now.
func TestShipmentVersions(t *testing.T) {
	e := newEnv(t)
	key := e.company("Versioned Freight").makeAPIKey("Order system")

	var sh apiShipment
	st, h := e.apiWithHeaders(http.MethodPost, "/api/v1/shipments", key, booking(), &sh)
	if st != http.StatusCreated || h.Get("ETag") != `"1"` {
		t.Fatalf("booked: status %d, ETag %q", st, h.Get("ETag"))
	}
	path := "/api/v1/shipments/" + sh.TrackingNumber
	if st, h = e.apiWithHeaders(http.MethodGet, path, key, nil, &sh); st != http.StatusOK || h.Get("ETag") != `"1"` {
		t.Fatalf("read: status %d, ETag %q", st, h.Get("ETag"))
	}

	// A change from the current copy goes ahead and raises the version.
	change := map[string]any{"instructions": "Call before arriving"}
	if st, h = e.apiSending(http.MethodPatch, path, key, map[string]string{"If-Match": `"1"`}, change, &sh); st != http.StatusOK || h.Get("ETag") != `"2"` {
		t.Fatalf("a change from the current copy: status %d, ETag %q", st, h.Get("ETag"))
	}

	// One from the old copy is refused, with the shipment as it is now.
	var stale struct {
		apiFailure
		Current apiShipment `json:"current"`
	}
	st, h = e.apiSending(http.MethodPatch, path, key, map[string]string{"If-Match": `"1"`}, map[string]any{"instructions": "Overwrite"}, &stale)
	if st != http.StatusPreconditionFailed || stale.Error.Code != "version_mismatch" || h.Get("ETag") != `"2"` || stale.Current.TrackingNumber != sh.TrackingNumber {
		t.Errorf("a change from an old copy: status %d, code %q, ETag %q, current %q", st, stale.Error.Code, h.Get("ETag"), stale.Current.TrackingNumber)
	}
	if got := scalar[string](t, `SELECT special_instructions FROM shipments WHERE tracking_number = $1`, sh.TrackingNumber); got != "Call before arriving" {
		t.Errorf("the refused change was made: %q", got)
	}
	// So is a cancellation, and an If-Match naming none of the versions.
	for _, tag := range []string{`"1"`, `W/"2"`, `2`, `"two"`} {
		stale.Error.Code = ""
		if st, _ := e.apiSending(http.MethodPost, path+"/cancel", key, map[string]string{"If-Match": tag}, map[string]any{"reason": "duplicate"}, &stale); st != http.StatusPreconditionFailed || stale.Error.Code != "version_mismatch" {
			t.Errorf("cancelled with If-Match %s: status %d", tag, st)
		}
	}
	if got := scalar[string](t, `SELECT status FROM shipments WHERE tracking_number = $1`, sh.TrackingNumber); got != "pending" {
		t.Errorf("a refused cancellation cancelled: %s", got)
	}

	// Without If-Match, or with *, any change goes; a list goes when it
	// names the current version.
	for i, tag := range []string{"", "*", `"9", "4"`} {
		headers := map[string]string{}
		if tag != "" {
			headers["If-Match"] = tag
		}
		want := `"` + strconv.Itoa(i+3) + `"`
		if st, h := e.apiSending(http.MethodPatch, path, key, headers, map[string]any{"instructions": "Change " + strconv.Itoa(i)}, &sh); st != http.StatusOK || h.Get("ETag") != want {
			t.Errorf("If-Match %q: status %d, ETag %q, want %s", tag, st, h.Get("ETag"), want)
		}
	}
}

// TestCreateWithIdempotencyKey (FR-046): a booking sent again with its
// Idempotency-Key books once and gets the first answer; the key with
// another booking is refused; one that didn't succeed can be sent again,
// corrected; and each key of a company keeps its own.
func TestCreateWithIdempotencyKey(t *testing.T) {
	e := newEnv(t)
	office := e.company("Retrying Freight")
	key, other := office.makeAPIKey("Order system"), office.makeAPIKey("Billing system")
	body := booking()
	idem := map[string]string{"Idempotency-Key": "order-4471"}

	var first, again apiShipment
	st, h := e.apiSending(http.MethodPost, "/api/v1/shipments", key, idem, body, &first)
	if st != http.StatusCreated || h.Get("Idempotent-Replayed") != "" {
		t.Fatalf("first: status %d", st)
	}
	st, h2 := e.apiSending(http.MethodPost, "/api/v1/shipments", key, idem, body, &again)
	if st != http.StatusCreated || h2.Get("Idempotent-Replayed") != "true" || again.TrackingNumber != first.TrackingNumber ||
		h2.Get("Location") != h.Get("Location") || h2.Get("ETag") != h.Get("ETag") {
		t.Errorf("replay: status %d, replayed %q, %s vs %s, Location %q, ETag %q", st, h2.Get("Idempotent-Replayed"), again.TrackingNumber, first.TrackingNumber, h2.Get("Location"), h2.Get("ETag"))
	}
	booked := func() int64 {
		return scalar[int64](t, `SELECT count(*) FROM shipments WHERE customer_phone = $1`, body["customer"].(map[string]any)["phone"])
	}
	if n := booked(); n != 1 {
		t.Errorf("booked %d times", n)
	}

	// The key with another booking.
	var fail apiFailure
	changed := booking()
	if st, _ := e.apiSending(http.MethodPost, "/api/v1/shipments", key, idem, changed, &fail); st != http.StatusUnprocessableEntity || fail.Error.Code != "idempotency_key_reused" {
		t.Errorf("the key with another booking: status %d %q", st, fail.Error.Code)
	}

	// Another key of the company has keys of its own.
	if st, h := e.apiSending(http.MethodPost, "/api/v1/shipments", other, idem, body, &again); st != http.StatusCreated || h.Get("Idempotent-Replayed") != "" {
		t.Errorf("another API key, the same Idempotency-Key: status %d, replayed %q", st, h.Get("Idempotent-Replayed"))
	}

	// A refused booking gives its key back: corrected, it books.
	retry := map[string]string{"Idempotency-Key": "order-4472"}
	wrong := booking()
	wrong["priority"] = "whenever"
	fail = apiFailure{}
	if st, _ := e.apiSending(http.MethodPost, "/api/v1/shipments", key, retry, wrong, &fail); st != http.StatusBadRequest || fail.Error.Code != "validation_error" {
		t.Errorf("a wrong booking: status %d %q", st, fail.Error.Code)
	}
	delete(wrong, "priority")
	if st, h := e.apiSending(http.MethodPost, "/api/v1/shipments", key, retry, wrong, &again); st != http.StatusCreated || h.Get("Idempotent-Replayed") != "" {
		t.Errorf("corrected, with the same key: status %d", st)
	}

	// A key too long.
	fail = apiFailure{}
	long := map[string]string{"Idempotency-Key": strings.Repeat("k", 256)}
	if st, _ := e.apiSending(http.MethodPost, "/api/v1/shipments", key, long, booking(), &fail); st != http.StatusBadRequest {
		t.Errorf("a key of 256 characters: status %d", st)
	}
}
