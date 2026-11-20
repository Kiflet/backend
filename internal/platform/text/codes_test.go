package text_test

import (
	"slices"
	"testing"

	"github.com/kiflet/backend/internal/model"
	"github.com/kiflet/backend/internal/platform/text"
)

// A customer is told the reason for a delay or a problem, in their
// language; prose passes through.
func TestReasons(t *testing.T) {
	for _, code := range slices.Concat(model.DelayReasons, model.IssueReasons) {
		if !text.HasLabel(text.Amharic, text.Customer, text.KindReason, code) {
			t.Errorf("reason %s has no Amharic", code)
		}
		if got := text.Reason(text.English, code); got != model.ReasonLabel(code) {
			t.Errorf("Reason(en, %s) = %q, want the office's %q", code, got, model.ReasonLabel(code))
		}
	}
	if got := text.Reason(text.Amharic, "flat tyre near Mojo"); got != "flat tyre near Mojo" {
		t.Errorf("prose should pass through, got %q", got)
	}
}

func TestStatus(t *testing.T) {
	for _, s := range model.AllStatuses {
		for _, l := range text.Languages {
			if got := text.Status(l.Code, s); got == "" || got == s {
				t.Errorf("Status(%s, %s) = %q", l.Code, s, got)
			}
		}
	}
	if got := text.Status(text.English, model.StatusIssue); got != "Delayed" {
		t.Errorf("a customer is told an issue is a delay, got %q", got)
	}
}

// Each audience reads its own words where it has them, and the office's
// where it doesn't; a language without words falls back to English.
func TestLabel(t *testing.T) {
	for _, c := range []struct {
		lang, audience, kind, code, want string
	}{
		{text.English, text.Office, text.KindStatus, "issue", "Issue"},
		{text.English, text.Customer, text.KindStatus, "issue", "Delayed"},
		{text.English, text.Driver, text.KindStatus, "issue", "Issue"},
		{text.Amharic, text.Driver, text.KindStatus, "in_transit", "በጉዞ ላይ"},
		{text.English, text.Driver, text.KindMove, "issue", "Report a problem"},
		{text.English, text.Customer, text.KindReason, "weather", "Weather"},
		{"fr", text.Office, text.KindRole, "admin", "Admin"},
		{text.Amharic, text.Office, text.KindAlert, "not_a_code", "not_a_code"},
	} {
		if got := text.Label(c.lang, c.audience, c.kind, c.code); got != c.want {
			t.Errorf("Label(%s, %s, %s, %s) = %q, want %q", c.lang, c.audience, c.kind, c.code, got, c.want)
		}
	}
	if text.HasLabel(text.Amharic, text.Office, text.KindAlert, "not_a_code") {
		t.Error("HasLabel found words for an unknown code")
	}
}
