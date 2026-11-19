package domain

import (
	"fmt"
	"math"
	"net/http"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kiflet/backend/internal/platform/apperror"
)

// A shipment's documents and charges: the files the office keeps with a
// load and what the customer is charged for it.

// DocumentKinds are what a document can be, in the order the form offers
// them.
var DocumentKinds = []string{"waybill", "invoice", "customs", "delivery_note", "photo", "other"}

var documentKindLabels = map[string]string{
	"waybill": "Waybill", "invoice": "Invoice", "customs": "Customs papers",
	"delivery_note": "Delivery note", "photo": "Photo", "other": "Other",
}

// DocumentKindLabel names a kind of document for people.
func DocumentKindLabel(kind string) string {
	if l, ok := documentKindLabels[kind]; ok {
		return l
	}
	return "Document"
}

// Limits on a shipment's files and lines.
const (
	MaxDocumentBytes      = 10 << 20
	MaxDocumentsPerLoad   = 30
	MaxChargesPerShipment = 50
)

// documentTypes are the files accepted, by the type their first bytes
// show, and the extension they are stored with. What a browser claims is
// not trusted.
var documentTypes = map[string]string{
	"application/pdf": ".pdf",
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
}

// File is a document checked: its kind, the name it is kept under, and
// the type and extension its content shows.
type File struct {
	Kind        string
	Name        string
	ContentType string
	Ext         string
}

// CheckDocument checks a file uploaded as a kind of document and names it.
func CheckDocument(kind, name string, body []byte) (File, error) {
	f := File{Kind: kind}
	if _, ok := documentKindLabels[kind]; !ok {
		return f, apperror.Invalid("kind", "Choose what the document is.")
	}
	if len(body) == 0 {
		return f, apperror.Invalid("file", "Choose a file to upload.")
	}
	if len(body) > MaxDocumentBytes {
		return f, apperror.Invalid("file", "The file is larger than 10 MB. Scan it at a lower resolution, or split it.")
	}
	ct := http.DetectContentType(body)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ext, ok := documentTypes[ct]
	if !ok {
		return f, apperror.Invalid("file", "Upload a PDF, or a photo as JPEG or PNG.")
	}
	f.ContentType, f.Ext = ct, ext
	f.Name = DocumentName(name, ext)
	if f.Name == "" {
		f.Name = DocumentKindLabel(kind) + ext
	}
	return f, nil
}

// ErrTooManyDocuments is a shipment that holds all the documents it may.
var ErrTooManyDocuments = apperror.Invalid("file", fmt.Sprintf("A shipment holds at most %d documents. Remove one to add another.", MaxDocumentsPerLoad))

// DocumentName keeps a file's own name, tidied: no folders, no control
// characters, at most 120 letters, ending in the extension its content
// shows. A name with nothing left is empty.
func DocumentName(name, ext string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(strings.TrimSuffix(name, path.Ext(name)))
	if name == "." || name == "/" || name == "" {
		return ""
	}
	for utf8.RuneCountInString(name)+len(ext) > 120 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name + ext
}

// CheckCharge checks a line of what the customer is charged, and returns
// it tidied, the amount to the cent. A negative amount is a discount.
func CheckCharge(description string, amount float64) (string, float64, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return "", 0, apperror.Invalid("description", "Say what the charge is for.")
	}
	if utf8.RuneCountInString(description) > 120 {
		return "", 0, apperror.Invalid("description", "Describe the charge in 120 letters or fewer.")
	}
	amount = math.Round(amount*100) / 100
	if amount == 0 || math.IsNaN(amount) || math.Abs(amount) >= 1e12 {
		return "", 0, apperror.Invalid("amount", "Enter an amount in birr; a discount is negative.")
	}
	return description, amount, nil
}

// ErrTooManyCharges is a shipment that holds all the charges it may.
var ErrTooManyCharges = apperror.Invalid("description", fmt.Sprintf("A shipment holds at most %d charges.", MaxChargesPerShipment))
