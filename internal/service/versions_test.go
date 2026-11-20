package service

import (
	"context"
	"errors"
	"testing"
)

// A record is refused only when the request expects other versions.
func TestCheckVersion(t *testing.T) {
	ctx := context.Background()
	if err := CheckVersion(ctx, 3); err != nil {
		t.Errorf("no If-Match: %v", err)
	}
	if err := CheckVersion(ExpectVersions(ctx, []int32{2, 3}), 3); err != nil {
		t.Errorf("one of the versions: %v", err)
	}
	for _, want := range [][]int32{{2}, {}} {
		if err := CheckVersion(ExpectVersions(ctx, want), 3); !errors.Is(err, ErrVersionMismatch) {
			t.Errorf("expecting %v: %v", want, err)
		}
	}
	if ErrVersionMismatch.APICode() != "version_mismatch" || ErrVersionMismatch.Kind.HTTPStatus() != 412 {
		t.Errorf("%s %d", ErrVersionMismatch.APICode(), ErrVersionMismatch.Kind.HTTPStatus())
	}
}
