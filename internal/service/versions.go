package service

import (
	"context"
	"slices"
)

// Versions (docs/architecture.md, section 11, "Conventions"): a record a
// person edits carries a version, which the database raises with every
// change (migrations/20261120000600_versions.up.sql). A client that sends
// back the version its copy had, as If-Match, changes the record only if no
// one has since.

// ErrVersionMismatch is a change made from a copy that is no longer
// current.
var ErrVersionMismatch = &Error{Kind: KindPrecondition, Message: "This shipment has changed since you read it. Read it again, then make your change."}

type expectedVersionsKey struct{}

// ExpectVersions is ctx for a change that may go ahead only on a record at
// one of these versions: the request's If-Match. The record is checked once
// it is locked, so no change can come between the check and the write.
// Without it, any version goes.
func ExpectVersions(ctx context.Context, versions []int32) context.Context {
	return context.WithValue(ctx, expectedVersionsKey{}, versions)
}

// CheckVersion refuses a record whose version isn't one ctx expects; a
// service calls it once it has locked the record.
func CheckVersion(ctx context.Context, version int32) error {
	want, ok := ctx.Value(expectedVersionsKey{}).([]int32)
	if ok && !slices.Contains(want, version) {
		return ErrVersionMismatch
	}
	return nil
}
