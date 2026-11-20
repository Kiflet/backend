package domain

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kiflet/backend/internal/platform/apperror"
)

var pdf = []byte("%PDF-1.7\n%âãÏÓ\n1 0 obj\n<<>>\nendobj\n")

func TestCheckDocument(t *testing.T) {
	f, err := CheckDocument("waybill", `C:\scans\Waybill 12.PNG`, pdf)
	if err != nil {
		t.Fatal(err)
	}
	if f.ContentType != "application/pdf" || f.Ext != ".pdf" || f.Name != "Waybill 12.pdf" {
		t.Errorf("checked: %+v", f)
	}
	if f, _ := CheckDocument("customs", "  ", pdf); f.Name != "Customs papers.pdf" {
		t.Errorf("unnamed file named %q", f.Name)
	}
	for name, c := range map[string]struct {
		kind string
		body []byte
		want string
	}{
		"unknown kind": {"receipt", pdf, "kind"},
		"empty":        {"waybill", nil, "file"},
		"too large":    {"waybill", bytes.Repeat([]byte("a"), MaxDocumentBytes+1), "file"},
		"not a pdf":    {"waybill", []byte("<html><body>hi</body></html>"), "file"},
	} {
		_, err := CheckDocument(c.kind, "x", c.body)
		if e, ok := apperror.As(err); !ok || e.Field != c.want {
			t.Errorf("%s: got %v, want an error on %s", name, err, c.want)
		}
	}
}

func TestDocumentName(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd":                "passwd.pdf",
		"report\x00\"final\".docx":        "reportfinal.pdf",
		".":                               "",
		strings.Repeat("ሀ", 200) + ".pdf": strings.Repeat("ሀ", 116) + ".pdf",
	} {
		if got := DocumentName(in, ".pdf"); got != want {
			t.Errorf("DocumentName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckCharge(t *testing.T) {
	d, err := CheckCharge("  Loading fee ", 125046)
	if err != nil || d != "Loading fee" {
		t.Errorf("got %q %v", d, err)
	}
	if _, err := CheckCharge("Discount", -10000); err != nil {
		t.Errorf("a discount: %v", err)
	}
	for name, c := range map[string]struct {
		desc   string
		santim int64
		want   string
	}{
		"no description": {" ", 1000, "description"},
		"too long":       {strings.Repeat("x", 121), 1000, "description"},
		"zero":           {"Fee", 0, "amount"},
		"too much":       {"Fee", MaxCharge, "amount"},
		"too much off":   {"Fee", -MaxCharge, "amount"},
	} {
		_, err := CheckCharge(c.desc, c.santim)
		if e, ok := apperror.As(err); !ok || e.Field != c.want {
			t.Errorf("%s: got %v, want an error on %s", name, err, c.want)
		}
	}
}
