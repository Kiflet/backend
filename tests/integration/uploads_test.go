//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // a file's digest, as storage checks it
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kiflet/backend/internal/modules/proof"
	"github.com/kiflet/backend/internal/service"
	"github.com/kiflet/backend/pkg/storage"
)

// A delivery's files, as the app makes them.
func uploadFiles(t *testing.T) (photo, signature []byte) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for x := range 40 {
		img.Set(x, x%30, color.RGBA{R: 200, A: 255})
	}
	var j, p bytes.Buffer
	if err := jpeg.Encode(&j, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&p, img); err != nil {
		t.Fatal(err)
	}
	return j.Bytes(), p.Bytes()
}

func describe(kind string, b []byte) proof.File {
	sum := md5.Sum(b) //nolint:gosec // see the import
	return proof.File{Kind: kind, Size: int64(len(b)), MD5: hex.EncodeToString(sum[:])}
}

// put sends a file where a ticket says, as the app does: no token, the
// ticket's headers, changed by headers (an empty value leaves one out).
func put(t *testing.T, tk proof.Ticket, body []byte, headers map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), tk.Method, tk.URL, bytes.NewReader(body))
	for k, v := range tk.Headers {
		req.Header.Set(k, v)
	}
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	answer, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(answer)
}

type tickets struct {
	Uploads []proof.Ticket `json:"uploads"`
}

// TestUploadsThroughTheAPI (FR-021): without storage that can presign, a
// delivery's files go to the API's own signed URL, which takes only the
// file named, by its size, MD5 and type; asked again, the app gets the
// same key, marked stored.
func TestUploadsThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	office := e.company("Uploading Freight")
	_, phone, pin := office.addDriver("Abebe Kebede")
	d := e.signIn(phone, pin)
	photo, signature := uploadFiles(t)
	files := []proof.File{describe(proof.KindPhoto, photo), describe(proof.KindSignature, signature)}

	var got tickets
	if st := e.api(http.MethodPost, "/api/v1/driver/uploads", d.token, map[string]any{"files": files}, &got); st != http.StatusOK || len(got.Uploads) != 2 {
		t.Fatalf("prepare: status %d, %+v", st, got)
	}
	ph, sig := got.Uploads[0], got.Uploads[1]
	tenant := scalar[string](t, `SELECT tenant_id::text FROM users WHERE phone = $1`, phone)
	if !strings.HasPrefix(ph.Key, tenant+"/uploads/") || !strings.HasSuffix(ph.Key, ".jpg") || !strings.HasSuffix(sig.Key, ".png") {
		t.Errorf("keys %q, %q: not the company's, or not by kind", ph.Key, sig.Key)
	}
	if !strings.HasPrefix(ph.URL, e.srv.URL+"/api/v1/driver/uploads/"+ph.ID+"?token=") || ph.Method != http.MethodPut ||
		ph.Headers["Content-Type"] != "image/jpeg" || ph.Headers["Content-MD5"] == "" || ph.Stored {
		t.Errorf("photo ticket: %+v", ph)
	}

	// Not the file named: refused, and nothing stored.
	if st, body := put(t, ph, signature, nil); st != http.StatusBadRequest {
		t.Errorf("another file: %d %s", st, body)
	}
	if st, body := put(t, ph, photo, map[string]string{"Content-Type": "image/png"}); st != http.StatusBadRequest {
		t.Errorf("another type: %d %s", st, body)
	}
	forged := ph
	forged.URL = strings.Replace(ph.URL, "token=", "token=0", 1)
	if st, body := put(t, forged, photo, nil); st != http.StatusForbidden {
		t.Errorf("a forged token: %d %s", st, body)
	}
	other := sig
	other.URL = strings.Replace(sig.URL, sig.ID, ph.ID, 1)
	if st, body := put(t, other, signature, nil); st != http.StatusForbidden {
		t.Errorf("one upload's token on another: %d %s", st, body)
	}
	if _, err := os.Stat(filepath.Join(e.files, ph.Key)); err == nil {
		t.Error("a refused upload was stored")
	}

	// The file named: stored at its key.
	for i, body := range [][]byte{photo, signature} {
		if st, answer := put(t, got.Uploads[i], body, nil); st != http.StatusNoContent {
			t.Fatalf("upload %d: %d %s", i, st, answer)
		}
	}
	if b, err := os.ReadFile(filepath.Join(e.files, ph.Key)); err != nil || !bytes.Equal(b, photo) {
		t.Errorf("the stored photo: %v", err)
	}

	// Asked again: the same keys, stored.
	var again tickets
	e.api(http.MethodPost, "/api/v1/driver/uploads", d.token, map[string]any{"files": files}, &again)
	if len(again.Uploads) != 2 || again.Uploads[0].Key != ph.Key || !again.Uploads[0].Stored || again.Uploads[1].Key != sig.Key {
		t.Errorf("asked again: %+v", again.Uploads)
	}

	// Another driver's same photo is theirs.
	_, phone2, pin2 := office.addDriver("Almaz Tesfaye")
	var theirs tickets
	e.api(http.MethodPost, "/api/v1/driver/uploads", e.signIn(phone2, pin2).token, map[string]any{"files": files[:1]}, &theirs)
	if len(theirs.Uploads) != 1 || theirs.Uploads[0].Key == ph.Key || theirs.Uploads[0].Stored {
		t.Errorf("another driver's: %+v", theirs.Uploads)
	}

	// What can't be prepared, each field named; and only by a driver.
	var fail apiFailure
	bad := []proof.File{{Kind: "video", Size: proof.MaxSize + 1, MD5: "abc"}}
	if st := e.api(http.MethodPost, "/api/v1/driver/uploads", d.token, map[string]any{"files": bad}, &fail); st != http.StatusBadRequest ||
		fail.Error.Fields["files[0].kind"] == "" || fail.Error.Fields["files[0].size"] == "" || fail.Error.Fields["files[0].md5"] == "" {
		t.Errorf("bad files: status %d, %+v", st, fail.Error)
	}
	if st := e.api(http.MethodPost, "/api/v1/driver/uploads", "", map[string]any{"files": files}, nil); st != http.StatusUnauthorized {
		t.Errorf("without a token: status %d", st)
	}
}

// TestPresignedUploads (FR-021): with the platform's storage, the app puts
// a delivery's files straight into it, by presigned URLs that take only the
// file named.
func TestPresignedUploads(t *testing.T) {
	s := objectStoreFor(t)
	e := newEnv(t, func(c *setup) {
		c.cfg.StorageType, c.cfg.S3Bucket, c.cfg.S3Region, c.cfg.S3Endpoint = "s3", s.bucket, "garage", s.endpoint
		c.cfg.S3AccessKey, c.cfg.S3SecretKey, c.cfg.S3ForcePathStyle = s.access, s.secret, true
	})
	office := e.company("Presigning Freight")
	_, phone, pin := office.addDriver("Kebede Alemu")
	d := e.signIn(phone, pin)
	photo, signature := uploadFiles(t)

	var got tickets
	if st := e.api(http.MethodPost, "/api/v1/driver/uploads", d.token, map[string]any{"files": []proof.File{describe(proof.KindPhoto, photo)}}, &got); st != http.StatusOK || len(got.Uploads) != 1 {
		t.Fatalf("prepare: status %d, %+v", st, got)
	}
	tk := got.Uploads[0]
	if !strings.HasPrefix(tk.URL, s.endpoint+"/"+s.bucket+"/") {
		t.Errorf("not presigned at storage: %s", tk.URL)
	}
	if st, body := put(t, tk, signature, nil); st == http.StatusOK {
		t.Errorf("storage took another file than the one named: %s", body)
	}
	// The MD5 is part of the signature: leaving it out doesn't get another
	// file in.
	if st, body := put(t, tk, signature, map[string]string{"Content-MD5": ""}); st == http.StatusOK {
		t.Errorf("storage took another file sent without the MD5: %s", body)
	}
	if st, body := put(t, tk, photo, nil); st != http.StatusOK {
		t.Fatalf("PUT: %d %s", st, body)
	}
	store, err := storage.NewS3Storage(storage.S3Config{Bucket: s.bucket, Region: "garage", Endpoint: s.endpoint, AccessKey: s.access, Secret: s.secret, ForcePathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := store.Download(context.Background(), tk.Key)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(b, photo) {
		t.Error("storage holds another file than the photo")
	}
}

// prepare asks for where to put files, failing the test otherwise.
func (d *driverApp) prepare(files ...proof.File) []proof.Ticket {
	d.e.t.Helper()
	var got tickets
	if st := d.e.api(http.MethodPost, "/api/v1/driver/uploads", d.token, map[string]any{"files": files}, &got); st != http.StatusOK || len(got.Uploads) != len(files) {
		d.e.t.Fatalf("prepare: status %d, %+v", st, got)
	}
	return got.Uploads
}

// podResult sends one delivery as it is, files and all.
func (d *driverApp) podResult(p service.SyncPOD) service.SyncResult {
	d.e.t.Helper()
	resp, code := d.sync(service.SyncRequest{PODs: []service.SyncPOD{p}})
	if code != http.StatusOK || len(resp.Results) != 1 {
		d.e.t.Fatalf("sync POD: status %d, results %+v", code, resp.Results)
	}
	return resp.Results[0]
}

// TestDeliveryByUploadedFiles (FR-021, AC-021.3, AC-021.4): a delivery names
// the files the app uploaded first, with when and where each photo was
// taken. Named before they are all in storage, it is retried, not refused;
// once they are, it is kept with them, and the office sees the photos.
func TestDeliveryByUploadedFiles(t *testing.T) {
	e := newEnv(t)
	office := e.company("Keyed Freight")
	driverID, phone, pin := office.addDriver("Selam Tadesse")
	shipmentID, _ := office.book("Addis Ababa", "Adama", driverID)
	d := e.signIn(phone, pin)
	if r := d.status(clientID(), shipmentID, "in_transit"); !r.OK {
		t.Fatalf("depart: %+v", r)
	}
	photo, signature := uploadFiles(t)
	photo2 := append(append([]byte{}, photo...), 0xFF, 0xD9) // another photo, still a JPEG
	tk := d.prepare(describe(proof.KindSignature, signature), describe(proof.KindPhoto, photo), describe(proof.KindPhoto, photo2))
	taken := time.Now().Add(-3 * time.Minute).Truncate(time.Second)
	lat, lng := 8.54, 39.27
	delivery := service.SyncPOD{
		ClientID: clientID(), ShipmentID: shipmentID, RecipientName: "Tigist Bekele", RecordedAt: time.Now(),
		SignatureFile: &service.SyncPODFile{Key: tk[0].Key},
		PhotoFiles: []service.SyncPODFile{
			{Key: tk[1].Key, TakenAt: &taken, Lat: &lat, Lng: &lng},
			{Key: tk[2].Key},
		},
	}

	// One photo not sent yet: retried.
	for i, b := range [][]byte{signature, photo} {
		if st, body := put(t, tk[i], b, nil); st != http.StatusNoContent {
			t.Fatalf("upload %d: %d %s", i, st, body)
		}
	}
	if r := d.podResult(delivery); r.OK || !r.Retry || r.Code != "files_missing" {
		t.Errorf("a delivery before its files: %+v", r)
	}
	if st := scalar[string](t, `SELECT status FROM shipments WHERE id = $1`, shipmentID); st == "delivered" {
		t.Fatal("delivered before its files were stored")
	}

	// Sent: kept, with each file's size, MD5, and the photo's time and place.
	if st, body := put(t, tk[2], photo2, nil); st != http.StatusNoContent {
		t.Fatalf("the last upload: %d %s", st, body)
	}
	if r := d.podResult(delivery); !r.OK {
		t.Fatalf("the delivery: %+v", r)
	}
	if st := scalar[string](t, `SELECT status FROM shipments WHERE id = $1`, shipmentID); st != "delivered" {
		t.Errorf("the shipment is %s", st)
	}
	type media struct {
		Key, MD5 string
		Size     int64
		Taken    *time.Time
		Lat      *float64
	}
	rows := map[int]media{}
	for _, pos := range []int{0, 1, 2} {
		var m media
		err := pool.QueryRow(context.Background(), `SELECT m.storage_key, m.md5, m.size_bytes, m.taken_at, m.latitude FROM pod_media m
			JOIN proof_of_deliveries p ON p.id = m.pod_id WHERE p.shipment_id = $1 AND m.position = $2`, shipmentID, pos).Scan(&m.Key, &m.MD5, &m.Size, &m.Taken, &m.Lat)
		if err != nil {
			t.Fatalf("media %d: %v", pos, err)
		}
		rows[pos] = m
	}
	if rows[0].Key != tk[0].Key || rows[1].Key != tk[1].Key || rows[2].Key != tk[2].Key {
		t.Errorf("the media aren't the uploads: %+v", rows)
	}
	if rows[1].MD5 != describe(proof.KindPhoto, photo).MD5 || rows[1].Size != int64(len(photo)) {
		t.Errorf("the photo's digest: %+v", rows[1])
	}
	if rows[1].Taken == nil || !rows[1].Taken.Equal(taken) || rows[1].Lat == nil || *rows[1].Lat != lat {
		t.Errorf("when and where the photo was taken: %+v", rows[1])
	}
	if rows[2].Taken != nil || rows[2].Lat != nil {
		t.Errorf("the second photo has a time or place it wasn't given: %+v", rows[2])
	}
	if p := office.get("/app/shipments/" + shipmentID + "/media/1"); p.Status != http.StatusOK || p.Body != string(photo) {
		t.Errorf("the office opening the photo: status %d", p.Status)
	}

	// Sent again: the same, and the files stay.
	if r := d.podResult(delivery); !r.OK {
		t.Errorf("the delivery again: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(e.files, tk[1].Key)); err != nil {
		t.Errorf("a replay removed the photo: %v", err)
	}

	// Files that aren't the driver's, or sent both ways: refused.
	other, _ := office.book("Addis Ababa", "Hawassa", driverID)
	_, phone2, pin2 := office.addDriver("Dawit Mekonnen")
	theirs := e.signIn(phone2, pin2).prepare(describe(proof.KindSignature, signature))
	for why, p := range map[string]service.SyncPOD{
		"another driver's signature": {SignatureFile: &service.SyncPODFile{Key: theirs[0].Key}},
		"a photo as the signature":   {SignatureFile: &service.SyncPODFile{Key: tk[1].Key}},
		"files by key and inline":    {SignatureFile: &service.SyncPODFile{Key: tk[0].Key}, Signature: signaturePNG},
		"no signature":               {PhotoFiles: []service.SyncPODFile{{Key: tk[1].Key}}},
	} {
		p.ClientID, p.ShipmentID, p.RecipientName, p.RecordedAt = clientID(), other, "Tigist Bekele", time.Now()
		if r := d.podResult(p); r.OK || r.Retry || r.Code != "invalid" {
			t.Errorf("%s: %+v", why, r)
		}
	}

	// A month on, the uploads no delivery used go, with their files; those
	// the delivery uses stay, and so do recent ones.
	unused := d.prepare(describe(proof.KindPhoto, append(append([]byte{}, photo...), 0xFF, 0xD9, 0xFF, 0xD9)))[0]
	recent := d.prepare(describe(proof.KindPhoto, append(append([]byte{}, photo...), 0xFF, 0xD9, 0xFF, 0xD9, 0xFF, 0xD9)))[0]
	if st, body := put(t, unused, append(append([]byte{}, photo...), 0xFF, 0xD9, 0xFF, 0xD9), nil); st != http.StatusNoContent {
		t.Fatalf("upload the unused photo: %d %s", st, body)
	}
	exec(t, `UPDATE uploads SET created_at = now() - interval '31 days' WHERE storage_key = ANY($1)`, []string{unused.Key, tk[1].Key})
	if err := e.svc.Jobs.Retention(context.Background()); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int64{unused.Key: 0, tk[1].Key: 1, recent.Key: 1} {
		if n := scalar[int64](t, `SELECT COUNT(*) FROM uploads WHERE storage_key = $1`, key); n != want {
			t.Errorf("upload %s: %d rows, want %d", key, n, want)
		}
	}
	if _, err := os.Stat(filepath.Join(e.files, tk[1].Key)); err != nil {
		t.Errorf("retention removed a delivery's photo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.files, unused.Key)); !os.IsNotExist(err) {
		t.Errorf("the unused photo is still stored: %v", err)
	}
}

// TestDeliveryByPresignedFiles (FR-021): a delivery naming files put
// straight into storage is kept once storage has them.
func TestDeliveryByPresignedFiles(t *testing.T) {
	s := objectStoreFor(t)
	e := newEnv(t, func(c *setup) {
		c.cfg.StorageType, c.cfg.S3Bucket, c.cfg.S3Region, c.cfg.S3Endpoint = "s3", s.bucket, "garage", s.endpoint
		c.cfg.S3AccessKey, c.cfg.S3SecretKey, c.cfg.S3ForcePathStyle = s.access, s.secret, true
	})
	office := e.company("Presigned Deliveries")
	driverID, phone, pin := office.addDriver("Hana Girma")
	shipmentID, _ := office.book("Addis Ababa", "Adama", driverID)
	d := e.signIn(phone, pin)
	if r := d.status(clientID(), shipmentID, "in_transit"); !r.OK {
		t.Fatalf("depart: %+v", r)
	}
	_, signature := uploadFiles(t)
	tk := d.prepare(describe(proof.KindSignature, signature))
	delivery := service.SyncPOD{
		ClientID: clientID(), ShipmentID: shipmentID, RecipientName: "Tigist Bekele", RecordedAt: time.Now(),
		SignatureFile: &service.SyncPODFile{Key: tk[0].Key},
	}
	if r := d.podResult(delivery); r.OK || !r.Retry || r.Code != "files_missing" {
		t.Errorf("before the upload: %+v", r)
	}
	if st, body := put(t, tk[0], signature, nil); st != http.StatusOK {
		t.Fatalf("PUT: %d %s", st, body)
	}
	if r := d.podResult(delivery); !r.OK {
		t.Fatalf("after the upload: %+v", r)
	}
	if p := office.get("/app/shipments/" + shipmentID + "/media/0"); p.Status != http.StatusOK || p.Body != string(signature) {
		t.Errorf("the office opening the signature: status %d", p.Status)
	}
}
