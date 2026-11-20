package service

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
)

// syncCursor is where a phone's copy stands: the last entry of its
// driver's feed it has seen, and a digest of the profile it was last sent,
// so the profile is sent again only when it changed. The phone holds it as
// an opaque string and sends it back as since.
type syncCursor struct {
	driver  [16]byte
	seq     int64
	profile uint64
}

// syncCursorVersion is the first byte of an encoded cursor.
const syncCursorVersion = 1

func (c syncCursor) String() string {
	b := make([]byte, 0, 33)
	b = append(b, syncCursorVersion)
	b = append(b, c.driver[:]...)
	b = binary.BigEndian.AppendUint64(b, uint64(c.seq)) //nolint:gosec // a feed number, never negative
	b = binary.BigEndian.AppendUint64(b, c.profile)
	return base64.RawURLEncoding.EncodeToString(b)
}

// parseSyncCursor reads a cursor; false for anything else, such as the
// timestamps a PWA loaded before the feed sends.
func parseSyncCursor(s string) (syncCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != 33 || b[0] != syncCursorVersion {
		return syncCursor{}, false
	}
	var c syncCursor
	copy(c.driver[:], b[1:17])
	c.seq = int64(binary.BigEndian.Uint64(b[17:25])) //nolint:gosec // checked against the head before use
	c.profile = binary.BigEndian.Uint64(b[25:33])
	return c, c.seq >= 0
}

// profileDigest is what the cursor keeps of a driver's profile.
func profileDigest(p DriverProfile) uint64 {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return binary.BigEndian.Uint64(sum[:8])
}
