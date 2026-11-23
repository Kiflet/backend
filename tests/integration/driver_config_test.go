//go:build integration

package integration

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/kiflet/backend/internal/model"
	"github.com/kiflet/backend/internal/service"
)

// TestDriverConfig (FR-017, FR-039): the driver app's configuration gives
// each status's buttons, main one first, and the driver's words for every
// code in English and Amharic; its ETag lets the app fetch it again only
// when it changed, and each sync names the current one.
func TestDriverConfig(t *testing.T) {
	e := newEnv(t)
	var config struct {
		Transitions map[string][]string                     `json:"transitions"`
		Moves       map[string][]model.Move                 `json:"moves"`
		PODFrom     []string                                `json:"pod_from"`
		Labels      map[string]map[string]map[string]string `json:"labels"`
	}
	st, h := e.apiWithHeaders(http.MethodGet, "/api/v1/driver/config", "", nil, &config)
	tag := h.Get("ETag")
	if st != http.StatusOK || len(tag) < 3 || tag[0] != '"' {
		t.Fatalf("config: status %d, ETag %q", st, tag)
	}

	// The buttons, main one first.
	if got := config.Transitions[model.StatusInTransit]; !slices.Equal(got, []string{"arrived", "delayed", "issue"}) {
		t.Errorf("transitions from in transit: %v", got)
	}
	if got := config.Moves[model.StatusArrived]; len(got) == 0 || got[0] != (model.Move{To: "delivered", Move: "delivered"}) {
		t.Errorf("the main button once arrived should record the delivery: %v", got)
	}
	if got := config.Moves[model.StatusDelayed]; len(got) == 0 || got[0] != (model.Move{To: "in_transit", Move: "resume"}) {
		t.Errorf("the main button when delayed should be back on the road: %v", got)
	}
	if got := config.Moves[model.StatusDelivered]; got == nil || len(got) != 0 {
		t.Errorf("a delivered load has no buttons: %v", got)
	}

	// The words, in both languages, for every code a driver reads.
	for lang, want := range map[string]map[string]string{
		"en": {"move.in_transit": "Start trip", "move.resume": "Back on the road", "status.issue": "Issue", "refusal.reassigned": "This shipment is another driver's now.", "removal.unassigned": "This shipment was taken off you."},
		"am": {"move.in_transit": "ጉዞ ጀምር", "removal.reassigned": "ይህ ጭነት ለሌላ ሹፌር ተሰጥቷል።"},
	} {
		for key, words := range want {
			kind, code, _ := strings.Cut(key, ".")
			if got := config.Labels[lang][kind][code]; got != words {
				t.Errorf("%s %s: %q, want %q", lang, key, got, words)
			}
		}
	}
	for _, kind := range []string{"status", "reason", "priority", "move", "refusal", "removal", "held"} {
		if len(config.Labels["am"][kind]) == 0 || len(config.Labels["am"][kind]) != len(config.Labels["en"][kind]) {
			t.Errorf("%s: %d words in Amharic, %d in English", kind, len(config.Labels["am"][kind]), len(config.Labels["en"][kind]))
		}
	}
	for _, moves := range config.Moves {
		for _, m := range moves {
			if config.Labels["am"]["move"][m.Move] == "" {
				t.Errorf("move %q has no words", m.Move)
			}
		}
	}

	// Fetched again: unchanged.
	if st, h := e.apiSending(http.MethodGet, "/api/v1/driver/config", "", map[string]string{"If-None-Match": tag}, nil, nil); st != http.StatusNotModified || h.Get("ETag") != tag {
		t.Errorf("config with its own ETag: status %d, ETag %q", st, h.Get("ETag"))
	}
	if st, _ := e.apiSending(http.MethodGet, "/api/v1/driver/config", "", map[string]string{"If-None-Match": `"old"`}, nil, nil); st != http.StatusOK {
		t.Errorf("config with another ETag: status %d", st)
	}

	// Each sync names it.
	office := e.company("Configured Freight")
	_, phone, pin := office.addDriver("Hirut Kebede")
	resp, code := e.signIn(phone, pin).sync(service.SyncRequest{})
	if code != http.StatusOK || resp.ConfigETag != tag {
		t.Errorf("sync: status %d, config_etag %q, want %q", code, resp.ConfigETag, tag)
	}
}
