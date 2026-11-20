package service

import "testing"

func TestRequestHash(t *testing.T) {
	base := RequestHash("POST", "/api/v1/shipments", []byte(`{"a":1}`))
	if RequestHash("post", "/api/v1/shipments", []byte(`{"a":1}`)) != base {
		t.Error("the method's case changed the hash")
	}
	for name, h := range map[string][32]byte{
		"method": RequestHash("PUT", "/api/v1/shipments", []byte(`{"a":1}`)),
		"path":   RequestHash("POST", "/api/v1/vehicles", []byte(`{"a":1}`)),
		"body":   RequestHash("POST", "/api/v1/shipments", []byte(`{"a":2}`)),
		// The path and body can't be shifted into each other.
		"split": RequestHash("POST", "/api/v1/shipments\n{", []byte(`"a":1}`)),
	} {
		if h == base {
			t.Errorf("another %s, the same hash", name)
		}
	}
}
