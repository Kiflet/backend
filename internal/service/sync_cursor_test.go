package service

import (
	"testing"
	"time"
)

func TestSyncCursor(t *testing.T) {
	c := syncCursor{driver: [16]byte{1, 2, 3}, seq: 41, profile: 0xfeedface}
	got, ok := parseSyncCursor(c.String())
	if !ok || got != c {
		t.Fatalf("round trip: %+v %v, want %+v", got, ok, c)
	}
	// What a PWA loaded before the feed sends, and anything else, is no
	// cursor: the sync answers it with a full download.
	for _, s := range []string{"", time.Now().Format(time.RFC3339Nano), "2026-11-20T03:00:00Z", "not a cursor", c.String()[:20]} {
		if _, ok := parseSyncCursor(s); ok {
			t.Errorf("%q read as a cursor", s)
		}
	}
	if profileDigest(DriverProfile{Name: "Abebe"}) == profileDigest(DriverProfile{Name: "Abebe", Vehicle: "3-A12345"}) {
		t.Error("a changed profile has the same digest")
	}
}
