package garage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	keyID  = "GK0123456789abcdef01234567"
	secret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestValidate(t *testing.T) {
	ok := Plan{Nodes: 1, Keys: []Key{{ID: keyID, Secret: secret}},
		Buckets: []Bucket{{Name: "kiflet", Grants: []Grant{{KeyID: keyID, Read: true}}}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a good plan: %v", err)
	}
	for name, change := range map[string]func(*Plan){
		"no nodes":        func(p *Plan) { p.Nodes = 0 },
		"a key ID":        func(p *Plan) { p.Keys[0].ID = "staging-access-key" },
		"a secret":        func(p *Plan) { p.Keys[0].Secret = "short" },
		"a bucket name":   func(p *Plan) { p.Buckets[0].Name = "Kiflet_Files" },
		"an unknown key":  func(p *Plan) { p.Buckets[0].Grants[0].KeyID = "GKffffffffffffffffffffffff" },
		"upper-case hex":  func(p *Plan) { p.Keys[0].Secret = strings.ToUpper(secret) },
		"a key too short": func(p *Plan) { p.Keys[0].ID = "GK0123" },
	} {
		p := Plan{Nodes: 1, Keys: []Key{{ID: keyID, Secret: secret}},
			Buckets: []Bucket{{Name: "kiflet", Grants: []Grant{{KeyID: keyID, Read: true}}}}}
		change(&p)
		if p.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// fakeGarage answers the admin API the way Garage v2 does, enough for
// Apply, and records the calls that change something.
type fakeGarage struct {
	mu      sync.Mutex
	roles   int
	version int64
	keys    map[string]bool
	buckets map[string]bool
	changes []string
}

func (f *fakeGarage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/health" && r.Header.Get("Authorization") != "Bearer token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch r.URL.Path {
	case "/health":
		if f.roles == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	case "/v2/GetClusterLayout":
		roles := make([]map[string]string, f.roles)
		for i := range roles {
			roles[i] = map[string]string{"id": "n"}
		}
		reply(map[string]any{"version": f.version, "roles": roles})
	case "/v2/GetClusterStatus":
		reply(map[string]any{"layoutVersion": f.version, "nodes": []map[string]any{{"id": "n1", "isUp": true, "hostname": "garage"}}})
	case "/v2/UpdateClusterLayout":
		var body struct {
			Roles []roleChange `json:"roles"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.roles = len(body.Roles)
		f.changes = append(f.changes, "layout "+body.Roles[0].Zone)
	case "/v2/ApplyClusterLayout":
		f.version++
	case "/v2/GetKeyInfo":
		if !f.keys[r.URL.Query().Get("id")] {
			w.WriteHeader(http.StatusNotFound)
		}
	case "/v2/ImportKey":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.keys[body["accessKeyId"]] = true
		f.changes = append(f.changes, "key "+body["accessKeyId"])
	case "/v2/GetBucketInfo":
		name := r.URL.Query().Get("globalAlias")
		if !f.buckets[name] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		reply(map[string]string{"id": "b-" + name})
	case "/v2/CreateBucket":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.buckets[body["globalAlias"]] = true
		f.changes = append(f.changes, "bucket "+body["globalAlias"])
		reply(map[string]string{"id": "b-" + body["globalAlias"]})
	case "/v2/AllowBucketKey", "/v2/UpdateBucket":
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// TestApply: the first run makes the layout, the key and the bucket; a
// second changes none of them.
func TestApply(t *testing.T) {
	f := &fakeGarage{keys: map[string]bool{}, buckets: map[string]bool{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := &Client{URL: srv.URL, Token: "token"}
	plan := Plan{Nodes: 1, Keys: []Key{{ID: keyID, Secret: secret}},
		Buckets: []Bucket{{Name: "kiflet", Grants: []Grant{{KeyID: keyID, Read: true, Write: true}}, AbortUploadsDays: 1}}}
	ctx := context.Background()
	if err := c.Apply(ctx, plan, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	want := "layout garage, key " + keyID + ", bucket kiflet"
	if got := strings.Join(f.changes, ", "); got != want {
		t.Errorf("first run: %s, want %s", got, want)
	}
	f.changes = nil
	if err := c.Apply(ctx, plan, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if len(f.changes) != 0 {
		t.Errorf("second run changed %v", f.changes)
	}
	c.Token = "wrong"
	start := time.Now()
	if err := c.Apply(ctx, plan, 10*time.Second); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("a wrong token: %v, want it refused", err)
	}
	if time.Since(start) > time.Second {
		t.Error("a wrong token was waited on")
	}
}
