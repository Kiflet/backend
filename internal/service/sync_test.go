package service

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// Each refused item carries one of the sync's stable codes; only a fault on
// our side is retried.
func TestSyncResultClassification(t *testing.T) {
	base := SyncResult{ClientID: "c1", Kind: "status", OK: true}
	if r := resultFor(base, nil); !r.OK || r.Retry {
		t.Errorf("success: %+v", r)
	}
	for _, c := range []struct {
		err  error
		code string
	}{
		{ErrStaleUpdate, "stale"},
		{conflict("Can't go from delivered to in transit."), "stale"},
		{errCancelledLoad, "cancelled"},
		{notYours(pgtype.UUID{Valid: true}), "reassigned"},
		{notYours(pgtype.UUID{}), "not_found"},
		{ErrNotAssigned, "not_found"},
		{notFound("Shipment not found."), "not_found"},
		{heldForReview(HeldReassigned), "held_for_review"},
		{invalid("reason", "Choose a reason."), "invalid"},
	} {
		r := resultFor(base, c.err)
		if r.OK || r.Retry || r.Code != c.code || r.Message == "" {
			t.Errorf("%v: %+v, want %s", c.err, r, c.code)
		}
	}
	if r := resultFor(base, errors.New("connection reset")); r.OK || !r.Retry || r.Code != "server_error" {
		t.Errorf("internal error should be retried: %+v", r)
	}
	// Files not in storage yet: not now, rather than never.
	if r := resultFor(base, errFilesMissing); r.OK || !r.Retry || r.Code != "files_missing" {
		t.Errorf("files missing should be retried: %+v", r)
	}
}
