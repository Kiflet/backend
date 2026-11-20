//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/service"
)

// TestIdempotencyKeys (FR-046; audit D7): the first request with a key runs,
// a replay gets its response, the same key with another request is refused,
// and each company and caller has its own keys. A failure gives the key
// back; a key past its 24 hours, or whose request died, is claimed afresh.
func TestIdempotencyKeys(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	idem := e.svc.Idempotency
	uuidOf := func(sql string, args ...any) pgtype.UUID {
		t.Helper()
		var u pgtype.UUID
		if err := u.Scan(scalar[string](t, sql, args...)); err != nil {
			t.Fatal(err)
		}
		return u
	}
	e.company("Keyed Freight")
	e.company("Other Keyed Freight")
	ours := uuidOf(`SELECT id::text FROM tenants WHERE name = 'Keyed Freight'`)
	theirs := uuidOf(`SELECT id::text FROM tenants WHERE name = 'Other Keyed Freight'`)
	caller := uuidOf(`SELECT gen_random_uuid()::text`)
	booking := service.RequestHash("POST", "/api/v1/shipments", []byte(`{"customer":"A"}`))
	req := service.IdempotentRequest{TenantID: ours, CallerID: caller, Key: "book-1", Hash: booking}

	begin := func(r service.IdempotentRequest) (*service.IdempotentResponse, error) {
		t.Helper()
		return idem.Begin(ctx, r)
	}
	if prior, err := begin(req); prior != nil || err != nil {
		t.Fatalf("the first request: %+v, %v; want it to run", prior, err)
	}
	if _, err := begin(req); !errors.Is(err, service.ErrIdempotencyKeyInFlight) {
		t.Errorf("a replay while the first runs: %v", err)
	}
	created := service.IdempotentResponse{Status: 201, ContentType: "application/json", Body: []byte(`{"id":"x"}`)}
	if err := idem.Finish(ctx, req, created); err != nil {
		t.Fatal(err)
	}
	if prior, err := begin(req); err != nil || prior == nil || prior.Status != 201 || string(prior.Body) != `{"id":"x"}` || prior.ContentType != "application/json" {
		t.Errorf("a replay: %+v, %v; want the first response", prior, err)
	}
	other := req
	other.Hash = service.RequestHash("POST", "/api/v1/shipments", []byte(`{"customer":"B"}`))
	if _, err := begin(other); !errors.Is(err, service.ErrIdempotencyKeyReused) {
		t.Errorf("the key with another body: %v", err)
	}

	// Another caller's key, or another company's, is its own.
	for name, r := range map[string]service.IdempotentRequest{
		"another caller":  {TenantID: ours, CallerID: uuidOf(`SELECT gen_random_uuid()::text`), Key: "book-1", Hash: booking},
		"another company": {TenantID: theirs, CallerID: caller, Key: "book-1", Hash: booking},
	} {
		if prior, err := begin(r); prior != nil || err != nil {
			t.Errorf("%s: %+v, %v; want it to run", name, prior, err)
		}
	}

	// A request that failed on our side gives its key back.
	failed := service.IdempotentRequest{TenantID: ours, CallerID: caller, Key: "book-2", Hash: booking}
	if _, err := begin(failed); err != nil {
		t.Fatal(err)
	}
	if err := idem.Release(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if prior, err := begin(failed); prior != nil || err != nil {
		t.Errorf("the retry after a failure: %+v, %v; want it to run", prior, err)
	}

	// A request that died is given up after a few minutes; a key is kept a day.
	exec(t, `UPDATE idempotency_keys SET created_at = now() - interval '10 minutes' WHERE key = 'book-2'`)
	if prior, err := begin(failed); prior != nil || err != nil {
		t.Errorf("a key whose request died: %+v, %v; want it to run", prior, err)
	}
	exec(t, `UPDATE idempotency_keys SET created_at = now() - interval '25 hours' WHERE key = 'book-1' AND tenant_id = $1 AND caller_id = $2`, ours.String(), caller.String())
	if prior, err := begin(other); prior != nil || err != nil {
		t.Errorf("a key past its day, with another body: %+v, %v; want it to run", prior, err)
	}

	// Retention prunes keys older than a day.
	exec(t, `UPDATE idempotency_keys SET created_at = now() - interval '25 hours' WHERE key = 'book-2'`)
	if err := e.svc.Jobs.Retention(ctx); err != nil {
		t.Fatal(err)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM idempotency_keys WHERE key = 'book-2'`); n != 0 {
		t.Errorf("%d day-old keys left after retention", n)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM idempotency_keys WHERE key = 'book-1'`); n != 3 {
		t.Errorf("%d fresh keys after retention, want 3", n)
	}
}
