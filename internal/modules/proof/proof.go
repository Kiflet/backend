// Package proof is the proof-of-delivery module (docs/architecture.md,
// section 4). It owns, so far, uploads: the files a driver's app is about to
// put in storage, a delivery's signature and photos, before the delivery
// that uses them is sent (docs/migration.md, phase 3, M5). The proofs
// themselves are still in internal/service.
//
// A file is known by its content: the app sends each file's kind, size and
// MD5, and gets back a storage key and a URL to PUT it to. With S3 storage
// the URL is presigned and the bytes go straight to storage, which refuses
// a body whose MD5 isn't the one signed; otherwise it is the API's own,
// signed by the server, which checks the same. Asked again for the same
// file, the driver gets the same key, with a fresh URL.
package proof

import (
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/platform/database"
)

// Kinds of files.
const (
	KindSignature = "signature"
	KindPhoto     = "photo"
)

// MaxSize is the largest file taken: a photo is about 150 KB at the app's
// settings (driversync.DefaultSettings), a signature less.
const MaxSize = 5 << 20

// MaxFiles is the most files one request prepares: a signature and the
// photos of a delivery, with room for a second.
const MaxFiles = 10

// Expires is how long an upload's URL works.
const Expires = 15 * time.Minute

// Uploads is what other modules and the API may call.
type Uploads interface {
	// Prepare records the files a driver is about to upload, or finds
	// those recorded already with the same content, and gives each its
	// key and where to PUT it.
	Prepare(ctx context.Context, d Driver, files []File) ([]Ticket, error)
	// Receive stores a file PUT to the API's own URL, once its token, size
	// and MD5 are checked.
	Receive(ctx context.Context, id pgtype.UUID, token string, contentType string, body io.Reader) error
	// TenantKeys are a company's files, read within the transaction that
	// deletes it (conn), to be removed from storage once it commits; its
	// rows go with it.
	TenantKeys(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) ([]string, error)
}

// Driver is who uploads.
type Driver struct {
	TenantID, UserID pgtype.UUID
}

// File is one the app is about to upload. MD5 is its digest, in hex.
type File struct {
	Kind string `json:"kind"`
	Size int64  `json:"size"`
	MD5  string `json:"md5"`
}

// Ticket is where and how to upload a file: a PUT of its bytes to URL,
// with Headers, before ExpiresAt. Stored says it is in storage already,
// and needn't be sent again.
type Ticket struct {
	ID        string            `json:"id"`
	Key       string            `json:"key"`
	Kind      string            `json:"kind"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
	Stored    bool              `json:"stored"`
}

// ContentType is a kind's file type: the app's photos are JPEG, its
// signatures PNG (audit S4).
func ContentType(kind string) string {
	if kind == KindSignature {
		return "image/png"
	}
	return "image/jpeg"
}
