//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// live opens a dashboard's live updates on the browser's own instance and
// returns the update types it receives, with the shipment each is about.
func (b *browser) live() <-chan string {
	b.e.t.Helper()
	dialer := websocket.Dialer{Jar: b.c.Jar, HandshakeTimeout: 5 * time.Second}
	conn, res, err := dialer.Dial("ws"+strings.TrimPrefix(b.e.srv.URL, "http")+"/app/live", http.Header{"Origin": {b.e.srv.URL}})
	if err != nil {
		status := 0
		if res != nil {
			status = res.StatusCode
		}
		b.e.t.Fatalf("live updates: %v (status %d)", err, status)
	}
	b.e.t.Cleanup(func() { conn.Close() })
	out := make(chan string, 64)
	go func() {
		defer close(out)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m struct {
				Type string `json:"type"`
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if json.Unmarshal(raw, &m) == nil {
				out <- m.Type + " " + m.Data.ID
			}
		}
	}()
	return out
}

// on is the same signed-in browser, sending its requests to another
// instance of the app.
func (b *browser) on(e *env) *browser {
	return &browser{e: e, c: b.c, email: b.email, phone: b.phone}
}

// TestTwoInstances (FR-005, FR-027; audit O4): two instances of the app
// share one database. A change made on one reaches the dashboards open on
// the other, and only that company's.
func TestTwoInstances(t *testing.T) {
	first, second := newEnv(t), newEnv(t)
	office := first.company("Twin Freight")
	other := first.company("Other Freight")

	mine, theirs := office.live(), other.live()
	time.Sleep(200 * time.Millisecond) // the hub registers both

	// Signed in on the first, working on the second: the session is the
	// database's, not an instance's.
	id, _ := office.on(second).book("Addis Ababa", "Adama", "")

	want := "shipment " + id
	deadline := time.After(5 * time.Second)
	for got := ""; got != want; {
		select {
		case m, ok := <-mine:
			if !ok {
				t.Fatal("the live updates closed")
			}
			got = m
		case <-deadline:
			t.Fatalf("the dashboard on the first instance had no %q from the second", want)
		}
	}
	select {
	case m := <-theirs:
		t.Errorf("another company's dashboard was sent %q", m)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestLiveUpdatesByRole (FR-004, FR-027; audit P1): a payment's live update
// reaches the company's admins and not its dispatchers, who still get the
// shipments'.
func TestLiveUpdatesByRole(t *testing.T) {
	e := newEnv(t)
	admin := e.company("Live Roles Freight")
	email := "dispatch" + phoneNumber() + "@example.et"
	p := admin.post("/app/team", url.Values{"full_name": {"Dawit Bekele"}, "email": {email}, "phone": {phoneNumber()}, "role": {"dispatcher"}})
	m := secretRe.FindStringSubmatch(p.Body)
	if m == nil {
		t.Fatalf("add dispatcher: status %d %s", p.Status, errorText(p.Body))
	}
	dispatcher := e.browser()
	dispatcher.mustPost("/login", url.Values{"identifier": {email}, "password": {strings.TrimSpace(m[1])}})
	dispatcher.mustPost("/app/account/password", url.Values{
		"current_password": {strings.TrimSpace(m[1])}, "new_password": {"dispatch-pass-9"}, "confirm_password": {"dispatch-pass-9"},
	})

	adminLive, dispatcherLive := admin.live(), dispatcher.live()
	time.Sleep(200 * time.Millisecond) // the hub registers both

	// A payment declined at the checkout, then a load booked.
	invoiceID := admin.choose("business", "monthly")
	admin.confirmEmail()
	_, checkout := admin.startPayment(invoiceID)
	e.answer(checkout, "decline")
	shipment, _ := admin.book("Addis Ababa", "Adama", "")

	seen := func(live <-chan string, until string) []string {
		t.Helper()
		var got []string
		deadline := time.After(5 * time.Second)
		for {
			select {
			case msg, ok := <-live:
				if !ok {
					t.Fatal("the live updates closed")
				}
				got = append(got, strings.Fields(msg)[0])
				if msg == until {
					return got
				}
			case <-deadline:
				t.Fatalf("no %q among %v", until, got)
			}
		}
	}
	if got := seen(adminLive, "shipment "+shipment); !slices.Contains(got, "payment") {
		t.Errorf("the admin's live updates %v have no payment", got)
	}
	if got := seen(dispatcherLive, "shipment "+shipment); slices.Contains(got, "payment") || slices.Contains(got, "invoice") {
		t.Errorf("the dispatcher was sent billing updates: %v", got)
	}
}
