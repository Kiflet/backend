//go:build integration

package integration

import (
	"crypto/md5" //nolint:gosec // the checksum pod_media keeps
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/kiflet/backend/internal/platform/geo"
	"github.com/kiflet/backend/internal/service"
)

// TestDeliveryMediaRecorded (FR-020, FR-021; audit D6): each stored file of
// a delivery is a row of pod_media, the signature at 0 and the photos after
// it, with the size and MD5 of what was stored, and the pages serve them
// from there.
func TestDeliveryMediaRecorded(t *testing.T) {
	e := newEnv(t)
	office := e.company("Media Freight")
	driverID, phone, pin := office.addDriver("Kebede Alemu")
	shipmentID, _ := office.book("Addis Ababa", "Adama", driverID)
	d := e.signIn(phone, pin)
	for _, st := range []string{"in_transit", "arrived"} {
		if r := d.status(clientID(), shipmentID, st); !r.OK {
			t.Fatalf("%s rejected: %+v", st, r)
		}
	}
	dest, _ := geo.Lookup("Adama")
	if r := d.pod(service.SyncPOD{ShipmentID: shipmentID, RecipientName: "Tigist Haile", Lat: &dest.Lat, Lng: &dest.Lng,
		Photos: []string{photoJPEG(t), photoJPEG(t)}}); !r.OK {
		t.Fatalf("proof of delivery: %+v", r)
	}

	rows, err := pool.Query(t.Context(), `
		SELECT m.position, m.kind, m.content_type, m.size_bytes, m.md5
		FROM pod_media m JOIN proof_of_deliveries p ON p.tenant_id = m.tenant_id AND p.id = m.pod_id
		WHERE p.shipment_id = $1 ORDER BY m.position`, shipmentID)
	if err != nil {
		t.Fatal(err)
	}
	type file struct {
		position          int16
		kind, contentType string
		size              int64
		md5               string
	}
	var files []file
	for rows.Next() {
		var f file
		if err := rows.Scan(&f.position, &f.kind, &f.contentType, &f.size, &f.md5); err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []struct{ kind, contentType string }{{"signature", "image/png"}, {"photo", "image/jpeg"}, {"photo", "image/jpeg"}}
	if len(files) != len(want) {
		t.Fatalf("%d files recorded, want %d: %+v", len(files), len(want), files)
	}
	for i, f := range files {
		if int(f.position) != i || f.kind != want[i].kind || f.contentType != want[i].contentType {
			t.Errorf("file %d: %+v, want %s %s", i, f, want[i].kind, want[i].contentType)
		}
		// What the page serves is what was recorded.
		p := office.get("/app/shipments/" + shipmentID + "/media/" + string(rune('0'+i)))
		sum := md5.Sum([]byte(p.Body)) //nolint:gosec // a checksum
		if p.Status != http.StatusOK || int64(len(p.Body)) != f.size || hex.EncodeToString(sum[:]) != f.md5 {
			t.Errorf("file %d served: status %d, %d bytes, md5 %x; recorded %d bytes, %s", i, p.Status, len(p.Body), sum, f.size, f.md5)
		}
	}
	if p := office.get("/app/shipments/" + shipmentID + "/media/3"); p.Status != http.StatusNotFound {
		t.Errorf("a photo that isn't there: status %d", p.Status)
	}
	if p := e.company("Nosy Freight").get("/app/shipments/" + shipmentID + "/media/1"); p.Status != http.StatusNotFound {
		t.Errorf("another company's view of the photo: status %d", p.Status)
	}
}
