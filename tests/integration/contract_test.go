//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/go-chi/chi/v5"
)

// The contract (docs/architecture.md, section 11): api/openapi/v1.yaml
// describes everything under /api/v1 but the providers' callbacks, which
// move to /api/callbacks.

var (
	specOnce   sync.Once
	spec       *openapi3.T
	specRouter routers.Router
	specErr    error
)

func loadSpec(t *testing.T) (*openapi3.T, routers.Router) {
	t.Helper()
	specOnce.Do(func() {
		loader := openapi3.NewLoader()
		if spec, specErr = loader.LoadFromFile("../../api/openapi/v1.yaml"); specErr != nil {
			return
		}
		if specErr = spec.Validate(loader.Context); specErr != nil {
			return
		}
		specRouter, specErr = gorillamux.NewRouter(spec)
	})
	if specErr != nil {
		t.Fatalf("api/openapi/v1.yaml: %v", specErr)
	}
	return spec, specRouter
}

// callback says whether a path is a provider's, which the specification
// leaves out.
func callback(path string) bool {
	return strings.HasPrefix(path, "/api/v1/sms/") || strings.HasPrefix(path, "/api/v1/payments/")
}

// contract checks every answer under /api/v1 against the specification:
// its status is one the operation lists, and its body has the shape the
// specification gives it, no field more or less than it says.
func contract(t *testing.T, next http.Handler) http.Handler {
	_, router := loadSpec(t)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") || callback(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		// What the handler reads of the request is gone once it has run:
		// the route is found with a copy that has no body.
		req := r.Clone(context.Background())
		req.Body = http.NoBody
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())

		route, params, err := router.FindRoute(req)
		if err != nil {
			t.Errorf("%s %s isn't in api/openapi/v1.yaml: %v", r.Method, r.URL.Path, err)
			return
		}
		in := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: &openapi3filter.RequestValidationInput{
				Request: req, PathParams: params, Route: route,
				Options: &openapi3filter.Options{ExcludeRequestBody: true, AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
			},
			Status:  rec.Code,
			Header:  rec.Header(),
			Options: &openapi3filter.Options{IncludeResponseStatus: true, MultiError: true},
		}
		in.SetBodyBytes(rec.Body.Bytes())
		if err := openapi3filter.ValidateResponse(context.Background(), in); err != nil {
			t.Errorf("%s %s answered %d, which api/openapi/v1.yaml doesn't allow: %v\n%.600s", r.Method, r.URL.Path, rec.Code, err, rec.Body.String())
		}
	})
}

// TestSpecificationMatchesTheAPI (FR-046): the routes under /api/v1 and the
// operations of api/openapi/v1.yaml are the same, and each operation says
// who may call it.
func TestSpecificationMatchesTheAPI(t *testing.T) {
	e := newEnv(t)
	spec, _ := loadSpec(t)
	routes, ok := e.app.Handler().(chi.Routes)
	if !ok {
		t.Fatalf("the app's handler is a %T, not a chi router", e.app.Handler())
	}
	served := map[string]bool{}
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if p, ok := strings.CutPrefix(route, "/api/v1"); ok && !callback(route) {
			served[method+" "+p] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	specified := map[string]bool{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			specified[method+" "+path] = true
			if n := len(slices.DeleteFunc(slices.Clone(op.Tags), func(tag string) bool {
				return tag != "app" && tag != "public" && tag != "driver"
			})); n != 1 {
				t.Errorf("%s %s has %d of the tags app, public and driver, want one", method, path, n)
			}
		}
	}
	for r := range served {
		if !specified[r] {
			t.Errorf("%s is served but not in api/openapi/v1.yaml", r)
		}
	}
	for r := range specified {
		if !served[r] {
			t.Errorf("%s is in api/openapi/v1.yaml but not served", r)
		}
	}
}
