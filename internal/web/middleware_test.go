package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/kiflet/backend/internal/platform/config"
)

// A client may send any forwarding header it likes. Behind a proxy, only
// the address the proxy added counts; without one, none do.
func TestClientIPBelievesOnlyTheProxy(t *testing.T) {
	for _, c := range []struct {
		name    string
		proxy   bool
		headers [][2]string
		want    string
	}{
		{"no proxy", false, [][2]string{{"X-Forwarded-For", "198.51.100.7"}, {"X-Real-IP", "203.0.113.9"}}, "192.0.2.1"},
		{"the proxy's entry", true, [][2]string{{"X-Forwarded-For", "203.0.113.9, 198.51.100.7"}}, "198.51.100.7"},
		{"a header of the client's before the proxy's", true, [][2]string{{"X-Forwarded-For", "203.0.113.9"}, {"X-Forwarded-For", "198.51.100.7"}}, "198.51.100.7"},
		// Caddy passes a client's X-Real-IP through untouched.
		{"X-Real-IP", true, [][2]string{{"X-Real-IP", "203.0.113.9"}, {"X-Forwarded-For", "198.51.100.7"}}, "198.51.100.7"},
		{"X-Real-IP alone", true, [][2]string{{"X-Real-IP", "203.0.113.9"}}, "192.0.2.1"},
		{"not an address", true, [][2]string{{"X-Forwarded-For", "198.51.100.7, unknown"}}, "192.0.2.1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := &Server{cfg: &config.Config{TrustProxy: c.proxy}}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "192.0.2.1:51234"
			for _, h := range c.headers {
				req.Header.Add(h[0], h[1])
			}
			var got string
			s.clientIP(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = remoteIP(r)
			})).ServeHTTP(httptest.NewRecorder(), req)
			if got != c.want {
				t.Errorf("client IP %q, want %q", got, c.want)
			}
		})
	}
}

// A request a browser sends from another site is refused, the API's
// included, unless it carries no ambient credential: an Authorization
// header, or a provider's call to /api/callbacks (audit A2).
func TestCrossOrigin(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := crossOrigin(ok)
	for _, c := range []struct {
		name, path, site, auth string
		want                   int
	}{
		{"a dashboard form from another site", "/app/shipments", "cross-site", "", http.StatusForbidden},
		{"the API from another site, with no key", "/api/v1/driver/login", "cross-site", "", http.StatusForbidden},
		{"the API from another site, with a key", "/api/v1/shipments", "cross-site", "Bearer kf_test", http.StatusNoContent},
		{"a provider's call", "/api/callbacks/sms/inbound", "cross-site", "", http.StatusNoContent},
		{"the driver app, on its own site", "/api/v1/driver/login", "same-origin", "", http.StatusNoContent},
		{"a phone's app or a server, no browser headers", "/api/v1/driver/login", "", "", http.StatusNoContent},
	} {
		r := httptest.NewRequest(http.MethodPost, "https://app.kiflet.et"+c.path, nil)
		if c.site != "" {
			r.Header.Set("Sec-Fetch-Site", c.site)
		}
		if c.auth != "" {
			r.Header.Set("Authorization", c.auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s: %d, want %d", c.name, w.Code, c.want)
		}
	}
}

// Every answer names its request's ID, which the handler sees too; a
// caller's own is kept when it is a short token, and replaced otherwise.
func TestRequestID(t *testing.T) {
	var seen string
	h := requestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = chimw.GetReqID(r.Context()) }))
	for _, c := range []struct{ sent, kept string }{
		{"", ""},
		{"web-7f3a9c21", "web-7f3a9c21"},
		{"short", ""},
		{"has spaces and\nnew lines", ""},
		{strings.Repeat("a", 65), ""},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/driver/config", nil)
		if c.sent != "" {
			r.Header.Set("X-Request-Id", c.sent)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		got := w.Header().Get("X-Request-Id")
		if got == "" || got != seen {
			t.Errorf("sent %q: answered %q, the handler saw %q", c.sent, got, seen)
		}
		if c.kept != "" && got != c.kept || c.kept == "" && (got == c.sent || len(got) != 32) {
			t.Errorf("sent %q: answered %q", c.sent, got)
		}
	}
}
