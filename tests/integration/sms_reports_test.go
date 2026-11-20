//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// TestDeliveryReportBeforeTheTextIsSaved (FR-037): a gateway may report on a
// text before the sender has saved the gateway's message ID. The report is
// kept, and saving the text applies it, rather than the report being lost.
func TestDeliveryReportBeforeTheTextIsSaved(t *testing.T) {
	e := newEnv(t)
	office := e.company("Early Report Freight")
	report := func(id, status string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"message_id": id, "status": status, "done_at": time.Now().Format(time.RFC3339)})
		res, err := http.Post(e.srv.URL+"/api/v1/sms/status", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("delivery report %s %s: status %d", id, status, res.StatusCode)
		}
	}
	kept := func() int64 {
		return scalar[int64](t, `SELECT count(*) FROM sms_early_reports WHERE provider = 'mock' AND provider_message_id = 'mock-early-1'`)
	}

	report("mock-early-1", "delivered")
	if n := kept(); n != 1 {
		t.Fatalf("a report naming no text yet: %d kept, want 1", n)
	}

	// The text is saved after the report looked for it, and before the
	// report looked again...
	exec(t, `INSERT INTO sms_messages (tenant_id, direction, phone, body, purpose, status, provider, provider_message_id, sent_at)
		SELECT id, 'outbound', '+251911000001', 'Hello', 'test', 'sent', 'mock', 'mock-early-1', NOW()
		FROM tenants WHERE name = $1`, "Early Report Freight")
	// ...so the next text the sender saves applies it.
	office.addDriver("Abebe Bikila")
	within(t, 10*time.Second, "the kept report applied", func() bool {
		return scalar[string](t, `SELECT COALESCE(delivery, '') FROM sms_messages WHERE provider_message_id = 'mock-early-1'`) == "delivered"
	})
	if n := kept(); n != 0 {
		t.Errorf("%d reports still kept after being applied", n)
	}

	// A report on a text already saved isn't kept, and delivered stays
	// final.
	report("mock-early-1", "failed")
	if n := kept(); n != 0 {
		t.Errorf("a late failure on a saved text was kept")
	}
	if got := scalar[string](t, `SELECT delivery FROM sms_messages WHERE provider_message_id = 'mock-early-1'`); got != "delivered" {
		t.Errorf("after a late failure the text is %q", got)
	}
}
