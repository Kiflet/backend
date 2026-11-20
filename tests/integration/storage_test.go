//go:build integration

package integration

// The S3 storage adapter against Garage, the object store of staging and
// production (docs/architecture.md, section 5), set up as the deploy's
// `kiflet setup-storage` job sets it up, from the same config file.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/kiflet/backend/internal/platform/geo"
	"github.com/kiflet/backend/internal/service"
	"github.com/kiflet/backend/pkg/storage"
	"github.com/kiflet/backend/pkg/storage/garage"
)

// garageImage is the version deployments/ runs.
const garageImage = "dxflrs/garage:v2.5.0"

// objectStore is a running S3 server with one bucket.
type objectStore struct {
	endpoint, bucket, access, secret string
	admin                            *garage.Client
	plan                             garage.Plan
}

// sharedObjectStore starts one S3 server for the tests that need one;
// TestMain stops it.
var sharedObjectStore = sync.OnceValues(startObjectStore)

// stopObjectStore stops the shared S3 server, if one was started.
var stopObjectStore = func() {}

func objectStoreFor(t *testing.T) objectStore {
	t.Helper()
	s, err := sharedObjectStore()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func startObjectStore() (objectStore, error) {
	ctx := context.Background()
	s := objectStore{bucket: "kiflet", access: "GK" + randomHex(12), secret: randomHex(32)}
	conf, err := os.ReadFile("../../deployments/garage/garage.toml")
	if err != nil {
		return s, err
	}
	token := randomHex(16)
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        garageImage,
			Env:          map[string]string{"GARAGE_RPC_SECRET": randomHex(32), "GARAGE_ADMIN_TOKEN": token},
			ExposedPorts: []string{"3900/tcp", "3903/tcp"},
			Files: []testcontainers.ContainerFile{{
				Reader: bytes.NewReader(conf), ContainerFilePath: "/etc/garage.toml", FileMode: 0o644,
			}},
			// /health answers, 503 until the layout is made.
			WaitingFor: wait.ForHTTP("/health").WithPort("3903/tcp").WithStatusCodeMatcher(func(int) bool { return true }).
				WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		return s, err
	}
	stopObjectStore = func() { _ = c.Terminate(ctx, testcontainers.StopTimeout(time.Second)) }
	adminURL, err := c.PortEndpoint(ctx, "3903/tcp", "http")
	if err != nil {
		return s, err
	}
	s.endpoint, err = c.PortEndpoint(ctx, "3900/tcp", "http")
	if err != nil {
		return s, err
	}
	s.admin = &garage.Client{URL: adminURL, Token: token}
	s.plan = garage.Plan{
		Nodes: 1,
		Keys:  []garage.Key{{ID: s.access, Secret: s.secret, Name: "app"}},
		Buckets: []garage.Bucket{{
			Name: s.bucket, Grants: []garage.Grant{{KeyID: s.access, Read: true, Write: true, Owner: true}},
			CORSOrigins: []string{"https://app.example.et"}, AbortUploadsDays: 1,
		}},
	}
	return s, s.admin.Apply(ctx, s.plan, 2*time.Minute)
}

// TestGarageSetup (operations): the set-up job runs at every deploy, so a
// second run over a cluster it set up changes nothing and fails on nothing.
func TestGarageSetup(t *testing.T) {
	s := objectStoreFor(t)
	if err := s.admin.Apply(context.Background(), s.plan, time.Minute); err != nil {
		t.Fatalf("a second run: %v", err)
	}
	bad := s.plan
	bad.Keys = []garage.Key{{ID: "staging-access-key", Secret: "short"}}
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "GK") {
		t.Errorf("a key Garage won't take: %v, want it refused with the format", err)
	}
}

// TestS3Storage (operations): a file is stored in the bucket under its
// key, read back whole, and gone once deleted.
func TestS3Storage(t *testing.T) {
	ctx := context.Background()
	s := objectStoreFor(t)
	cfg := storage.S3Config{Bucket: s.bucket, Region: "garage", Endpoint: s.endpoint, AccessKey: s.access, Secret: s.secret, ForcePathStyle: true}
	store, err := storage.NewS3Storage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("signature "), 10_000)
	key, err := store.Upload(ctx, "/tenant/pod/signature.png", bytes.NewReader(body), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if key != "tenant/pod/signature.png" {
		t.Errorf("stored as %q", key)
	}
	r, err := store.Download(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	r.Close()
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("downloaded %d bytes (%v), want the %d uploaded", len(got), err, len(body))
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Download(ctx, key); !errors.Is(err, storage.ErrFileNotFound) {
		t.Errorf("after deleting: %v, want not found", err)
	}

	wrong := cfg
	wrong.Secret = randomHex(32)
	other, err := storage.NewS3Storage(wrong)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Upload(ctx, "tenant/x.png", strings.NewReader("x"), "image/png"); err == nil {
		t.Error("an upload signed with the wrong secret was accepted")
	}
	region := cfg
	region.Region = "us-east-1"
	if other, err = storage.NewS3Storage(region); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Upload(ctx, "tenant/x.png", strings.NewReader("x"), "image/png"); err == nil {
		t.Error("an upload signed for another region than Garage's was accepted")
	}
}

// TestPresignedURLs (FR-021): a phone puts a photo straight into the bucket
// with a presigned URL, and a browser reads it with another; a page on the
// app's origin may read the answer (CORS), others may not.
func TestPresignedURLs(t *testing.T) {
	ctx := context.Background()
	s := objectStoreFor(t)
	store, err := storage.NewS3Storage(storage.S3Config{
		Bucket: s.bucket, Region: "garage", Endpoint: s.endpoint, AccessKey: s.access, Secret: s.secret, ForcePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	put, err := store.PutURL(ctx, "tenant/pod/photo.jpg", "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(put, s.endpoint+"/"+s.bucket+"/") {
		t.Errorf("presigned URL %s isn't path-style at the endpoint", put)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, put, strings.NewReader("a photo"))
	req.Header.Set("Content-Type", "image/jpeg")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT to the presigned URL: %s", resp.Status)
	}
	get, err := store.GetURL(ctx, "tenant/pod/photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.Get(get) //nolint:gosec,noctx // the test's own URL
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "a photo" {
		t.Errorf("GET from the presigned URL: %s %q", resp.Status, body)
	}

	// What a browser on another origin may read is decided by the rules on
	// the response itself. (Garage answers every preflight with "*"; the
	// URL's signature is what keeps a file private.)
	allowed := func(origin string) string {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, get, nil)
		req.Header.Set("Origin", origin)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("Access-Control-Allow-Origin")
	}
	if got := allowed("https://app.example.et"); got != "https://app.example.et" {
		t.Errorf("the app's origin: Access-Control-Allow-Origin %q", got)
	}
	if got := allowed("https://elsewhere.example"); got != "" {
		t.Errorf("another origin was allowed: %q", got)
	}

	// The public endpoint is the one URLs are signed for.
	public, err := storage.NewS3Storage(storage.S3Config{
		Bucket: s.bucket, Region: "garage", Endpoint: s.endpoint, AccessKey: s.access, Secret: s.secret, ForcePathStyle: true,
		PublicEndpoint: "https://files.example.et",
	})
	if err != nil {
		t.Fatal(err)
	}
	if u, _ := public.GetURL(ctx, "tenant/pod/photo.jpg"); !strings.HasPrefix(u, "https://files.example.et/kiflet/tenant/pod/photo.jpg?") {
		t.Errorf("signed for %s, want the public endpoint", u)
	}
}

// TestDeliveriesStoredInS3 (FR-020, FR-021): the app keeps a delivery's
// signature and photos in the bucket and serves them to the office through
// itself, so the bucket needn't be public.
func TestDeliveriesStoredInS3(t *testing.T) {
	s := objectStoreFor(t)
	e := newEnv(t, func(c *setup) {
		c.cfg.StorageType, c.cfg.S3Endpoint, c.cfg.S3Bucket, c.cfg.S3Region = "s3", s.endpoint, s.bucket, "garage"
		c.cfg.S3AccessKey, c.cfg.S3SecretKey, c.cfg.S3ForcePathStyle = s.access, s.secret, true
	})
	office := e.company("Object Store Freight")
	driverID, phone, pin := office.addDriver("Kebede Alemu")
	shipmentID, _ := office.book("Addis Ababa", "Adama", driverID)
	d := e.signIn(phone, pin)
	for _, st := range []string{"in_transit", "arrived"} {
		if r := d.status(clientID(), shipmentID, st); !r.OK {
			t.Fatalf("%s rejected: %+v", st, r)
		}
	}
	dest, _ := geo.Lookup("Adama")
	photo := photoJPEG(t)
	if r := d.pod(service.SyncPOD{ShipmentID: shipmentID, RecipientName: "Tigist Haile", Lat: &dest.Lat, Lng: &dest.Lng,
		Photos: []string{photo}}); !r.OK {
		t.Fatalf("proof of delivery: %+v", r)
	}
	if files := storedFiles(t, e.files); len(files) != 0 {
		t.Errorf("files on the app's disk: %v", files)
	}
	if p := office.get("/app/shipments/" + shipmentID + "/media/0"); p.Status != http.StatusOK || !strings.HasPrefix(p.Body, "\x89PNG") {
		t.Errorf("signature from the bucket: status %d", p.Status)
	}
	if p := office.get("/app/shipments/" + shipmentID + "/media/1"); p.Status != http.StatusOK || !strings.HasPrefix(p.Body, "\xff\xd8") {
		t.Errorf("photo from the bucket: status %d", p.Status)
	}
}
