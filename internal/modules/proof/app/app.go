// Package app is the proof module's implementation of proof.Uploads.
package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // a file's digest, as S3's Content-MD5, not a secret
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/proof"
	"github.com/kiflet/backend/internal/modules/proof/store"
	"github.com/kiflet/backend/internal/platform/apperror"
	"github.com/kiflet/backend/internal/platform/database"
	"github.com/kiflet/backend/pkg/storage"
)

// Uploads is proof.Uploads on Postgres and a store.
type Uploads struct {
	q       *store.Queries
	files   storage.Storage
	secret  []byte
	baseURL string
	now     func() time.Time
}

var _ proof.Uploads = (*Uploads)(nil)

// NewUploads is the module's uploads, reading through conn (the pool),
// storing in files, and signing the API's own upload URLs, at baseURL,
// with secret.
func NewUploads(conn database.Conn, files storage.Storage, secret []byte, baseURL string) *Uploads {
	return &Uploads{q: store.New(conn), files: files, secret: secret, baseURL: strings.TrimSuffix(baseURL, "/"), now: time.Now}
}

var md5Hex = regexp.MustCompile(`^[0-9a-f]{32}$`)

// validate names every field that is wrong.
func validate(files []proof.File) error {
	var p apperror.Problems
	if len(files) == 0 {
		p.Add("files", "Name at least one file.")
	}
	if len(files) > proof.MaxFiles {
		p.Add("files", fmt.Sprintf("Send at most %d files at a time.", proof.MaxFiles))
	}
	for i, f := range files {
		field := fmt.Sprintf("files[%d]", i)
		if f.Kind != proof.KindPhoto && f.Kind != proof.KindSignature {
			p.Add(field+".kind", "A file is a photo or a signature.")
		}
		if f.Size <= 0 || f.Size > proof.MaxSize {
			p.Add(field+".size", fmt.Sprintf("A file is at most %d MB.", proof.MaxSize>>20))
		}
		if !md5Hex.MatchString(f.MD5) {
			p.Add(field+".md5", "The MD5 is 32 lowercase hex digits.")
		}
	}
	return p.Err()
}

func (u *Uploads) Prepare(ctx context.Context, d proof.Driver, files []proof.File) ([]proof.Ticket, error) {
	if err := validate(files); err != nil {
		return nil, err
	}
	expires := u.now().Add(proof.Expires).Truncate(time.Second)
	out := make([]proof.Ticket, 0, len(files))
	for _, f := range files {
		ext := ".jpg"
		if f.Kind == proof.KindSignature {
			ext = ".png"
		}
		// Prefixed by the company, as every key is (docs/architecture.md,
		// section 8).
		key := fmt.Sprintf("%s/uploads/%s/%s%s", uuidString(d.TenantID), uuidString(d.UserID), uuid.NewString(), ext)
		row, err := u.q.PrepareUpload(ctx, store.PrepareUploadParams{
			TenantID: d.TenantID, DriverID: d.UserID, Kind: f.Kind, StorageKey: key,
			ContentType: proof.ContentType(f.Kind), SizeBytes: f.Size, Md5: f.MD5,
		})
		if err != nil {
			return nil, err
		}
		t, err := u.ticket(ctx, row, expires)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// ticket is where to PUT a file: storage itself when it can presign, the
// API's own URL otherwise.
func (u *Uploads) ticket(ctx context.Context, row store.Upload, expires time.Time) (proof.Ticket, error) {
	sum, _ := hex.DecodeString(row.Md5)
	t := proof.Ticket{
		ID: uuidString(row.ID), Key: row.StorageKey, Kind: row.Kind, Method: http.MethodPut, ExpiresAt: expires,
		Headers: map[string]string{"Content-Type": row.ContentType, "Content-MD5": base64.StdEncoding.EncodeToString(sum)},
		Stored:  row.StoredAt.Valid,
	}
	if p, ok := u.files.(storage.Presigner); ok {
		signed, err := p.PutURL(ctx, row.StorageKey, storage.Put{ContentType: row.ContentType, MD5: sum, Expires: expires.Sub(u.now())})
		if err != nil {
			return t, err
		}
		t.URL = signed
		return t, nil
	}
	q := url.Values{"token": {u.token(row.TenantID, row.ID, expires)}}
	t.URL = u.baseURL + "/api/v1/driver/uploads/" + t.ID + "?" + q.Encode()
	return t, nil
}

// token signs an upload to the API's own URL: its company, its expiry and
// a MAC of both and the upload's ID.
func (u *Uploads) token(tenantID, id pgtype.UUID, expires time.Time) string {
	exp := strconv.FormatInt(expires.Unix(), 10)
	return uuidString(tenantID) + "." + exp + "." + u.mac(uuidString(tenantID), uuidString(id), exp)
}

func (u *Uploads) mac(tenant, id, exp string) string {
	m := hmac.New(sha256.New, u.secret)
	m.Write([]byte("upload\x00" + tenant + "\x00" + id + "\x00" + exp))
	return hex.EncodeToString(m.Sum(nil))
}

var (
	errBadToken = &apperror.Error{Kind: apperror.KindForbidden, Message: "This upload link isn't valid or has expired. Ask for a new one."}
	errMismatch = apperror.Invalid("file", "The file isn't the one announced: its size or MD5 differs. Ask for a new link if it changed.")
)

func (u *Uploads) Receive(ctx context.Context, id pgtype.UUID, token, contentType string, body io.Reader) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errBadToken
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || u.now().Unix() > exp || !hmac.Equal([]byte(parts[2]), []byte(u.mac(parts[0], uuidString(id), parts[1]))) {
		return errBadToken
	}
	var tenantID pgtype.UUID
	if err := tenantID.Scan(parts[0]); err != nil {
		return errBadToken
	}
	row, err := u.q.GetUpload(ctx, store.GetUploadParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return errBadToken
	}
	if err != nil {
		return err
	}
	if ct, _, _ := strings.Cut(contentType, ";"); strings.TrimSpace(ct) != row.ContentType {
		return apperror.Invalid("content_type", "Send it as "+row.ContentType+".")
	}

	data, err := io.ReadAll(io.LimitReader(body, row.SizeBytes+1))
	if err != nil {
		return err
	}
	sum := md5.Sum(data) //nolint:gosec // see the import
	if int64(len(data)) != row.SizeBytes || hex.EncodeToString(sum[:]) != row.Md5 {
		return errMismatch
	}
	// What it is, not only what it is called.
	if http.DetectContentType(data) != row.ContentType {
		return apperror.Invalid("file", "The file isn't a "+strings.TrimPrefix(row.ContentType, "image/")+" image.")
	}
	if _, err := u.files.Upload(ctx, row.StorageKey, bytes.NewReader(data), row.ContentType); err != nil {
		return err
	}
	return u.q.MarkUploadStored(ctx, store.MarkUploadStoredParams{TenantID: row.TenantID, ID: row.ID})
}

func (u *Uploads) TenantKeys(ctx context.Context, conn database.Conn, tenantID pgtype.UUID) ([]string, error) {
	return store.New(conn).ListTenantUploadKeys(ctx, tenantID)
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}
