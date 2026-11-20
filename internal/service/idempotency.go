package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/db"
)

// Idempotency keys (docs/architecture.md, section 11): a create sent with
// an Idempotency-Key runs once per company, caller and key for 24 hours; a
// replay gets the first response. Phase 2's middleware calls this around
// each create.
const (
	keepIdempotencyKeys = 24 * time.Hour
	// A claim whose request never finished, because the process died, is
	// given up after this, far past any request's time limit.
	abandonIdempotencyKeys = 5 * time.Minute
	maxIdempotencyKey      = 255
)

// ErrIdempotencyKeyReused is a key sent again with another request.
var ErrIdempotencyKeyReused = invalid("Idempotency-Key", "This key was already used for a different request. Use a new key for a new request.")

// ErrIdempotencyKeyInFlight is a replay that arrives while the first
// request with its key is still running.
var ErrIdempotencyKeyInFlight = conflict("A request with this key is still being processed. Try again in a moment.")

// IdempotencyService remembers the first response to each keyed request.
type IdempotencyService struct {
	*core
}

// IdempotentRequest is a request sent with an Idempotency-Key: who sent it
// (the user or the API key) and a hash of what was sent (RequestHash).
type IdempotentRequest struct {
	TenantID, CallerID pgtype.UUID
	Key                string
	Hash               [sha256.Size]byte
}

// IdempotentResponse is the response kept for replays.
type IdempotentResponse struct {
	Status      int
	ContentType string
	Body        []byte
}

// RequestHash is what identifies a request for its key: the method, the
// path and the body.
func RequestHash(method, path string, body []byte) [sha256.Size]byte {
	h := sha256.New()
	h.Write([]byte(strings.ToUpper(method) + " " + path + "\n"))
	h.Write(body)
	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

// Begin claims the request's key. It returns nil when the request should
// run, and then Finish or Release must follow; the first response when the
// request is a replay; ErrIdempotencyKeyReused when the key came with
// another request; and ErrIdempotencyKeyInFlight while the first is
// running.
func (s *IdempotencyService) Begin(ctx context.Context, r IdempotentRequest) (*IdempotentResponse, error) {
	if r.Key == "" || len(r.Key) > maxIdempotencyKey {
		return nil, invalid("Idempotency-Key", "An Idempotency-Key is 1 to 255 characters.")
	}
	now := s.now()
	n, err := s.q.ClaimIdempotencyKey(ctx, db.ClaimIdempotencyKeyParams{
		TenantID: r.TenantID, CallerID: r.CallerID, Key: r.Key, RequestHash: r.Hash[:],
		ExpiredBefore: toTimestamp(now.Add(-keepIdempotencyKeys)), AbandonedBefore: toTimestamp(now.Add(-abandonIdempotencyKeys)),
	})
	if err != nil || n == 1 {
		return nil, err
	}
	prior, err := s.q.GetIdempotencyKey(ctx, db.GetIdempotencyKeyParams{TenantID: r.TenantID, CallerID: r.CallerID, Key: r.Key})
	switch {
	case isNoRows(err):
		// Pruned between the two queries: run it.
		return s.Begin(ctx, r)
	case err != nil:
		return nil, err
	case !bytes.Equal(prior.RequestHash, r.Hash[:]):
		return nil, ErrIdempotencyKeyReused
	case prior.ResponseStatus == nil:
		return nil, ErrIdempotencyKeyInFlight
	}
	return &IdempotentResponse{Status: int(*prior.ResponseStatus), ContentType: prior.ResponseType, Body: prior.ResponseBody}, nil
}

// Finish keeps the response to a request Begin let run, for its replays.
func (s *IdempotencyService) Finish(ctx context.Context, r IdempotentRequest, resp IdempotentResponse) error {
	return s.q.SaveIdempotentResponse(ctx, db.SaveIdempotentResponseParams{
		TenantID: r.TenantID, CallerID: r.CallerID, Key: r.Key, RequestHash: r.Hash[:],
		ResponseStatus: new(int16(resp.Status)), ResponseType: &resp.ContentType, ResponseBody: resp.Body,
	})
}

// Release gives back the key of a request that failed on our side, so a
// retry runs instead of replaying the failure.
func (s *IdempotencyService) Release(ctx context.Context, r IdempotentRequest) error {
	return s.q.ReleaseIdempotencyKey(ctx, db.ReleaseIdempotencyKeyParams{
		TenantID: r.TenantID, CallerID: r.CallerID, Key: r.Key, RequestHash: r.Hash[:],
	})
}
