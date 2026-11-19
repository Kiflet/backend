//go:build integration

package integration

// Kiflet as separate processes (docs/architecture.md, section 3): a web
// process queues the work, a worker process does it.

import (
	"net/http"
	"testing"
	"time"

	"github.com/kiflet/backend/internal/platform/config"
)

func withRoles(roles string) func(*setup) {
	return func(s *setup) {
		r, err := config.ParseRoles(roles)
		if err != nil {
			panic(err)
		}
		s.cfg.Roles = r
	}
}

// TestSeparateProcesses: a shipment booked on a web process is texted to
// its customer by a worker process, woken at once rather than at its next
// look; the worker serves only its health and figures.
func TestSeparateProcesses(t *testing.T) {
	// The company is made on a process with every role, stopped before the
	// others start, so that only the worker sends.
	base := newEnv(t)
	office := base.company("Split Freight")
	base.stop()

	worker := newEnv(t, withRoles("worker"))
	web := newEnv(t, withRoles("web"))

	for path, want := range map[string]int{"/healthz": http.StatusOK, "/readyz": http.StatusOK, "/": http.StatusNotFound, "/login": http.StatusNotFound} {
		res, err := http.Get(worker.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("the worker's %s: %d, want %d", path, res.StatusCode, want)
		}
	}

	customer := phoneNumber()
	start := time.Now()
	_, tracking := office.on(web).bookFor("Addis Ababa", "Adama", "", customer)
	worker.received("sms", customer, tracking)
	// The sender looks every 5 s on its own; woken, it sends at once.
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("the worker sent the text %s after the booking: it wasn't woken", took.Round(time.Millisecond))
	}
	for _, it := range web.inbox.Items() {
		if it.Kind == "sms" && it.To == customer {
			t.Error("the web process sent a text itself")
		}
	}
}
