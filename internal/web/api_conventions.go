package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/shipments"
	"github.com/kiflet/backend/internal/service"
)

// The API's conventions for changing a record (docs/architecture.md,
// section 11, "Conventions"): a record's version is its ETag, and a change
// sent with If-Match goes ahead only on that version; a create sent with an
// Idempotency-Key runs once, and a replay gets the first answer.

// etag is a record's version as its ETag.
func etag(version int32) string { return `"` + strconv.FormatInt(int64(version), 10) + `"` }

// ifMatch is the request's context with the versions its If-Match names,
// which the service checks once it has locked the record. Without one, or
// with "*", any version goes. Only strong tags match (RFC 9110, 13.1.1),
// and every ETag here is strong; a header naming none of ours matches no
// version.
func ifMatch(r *http.Request) context.Context {
	h := strings.TrimSpace(r.Header.Get("If-Match"))
	if h == "" || h == "*" {
		return r.Context()
	}
	versions := []int32{}
	for tag := range strings.SplitSeq(h, ",") {
		tag = strings.TrimSpace(tag)
		if len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
			continue
		}
		if n, err := strconv.ParseInt(tag[1:len(tag)-1], 10, 32); err == nil {
			versions = append(versions, int32(n))
		}
	}
	return service.ExpectVersions(r.Context(), versions)
}

// writeShipment answers with a shipment and its ETag.
func (s *Server) writeShipment(w http.ResponseWriter, r *http.Request, status int, sh shipments.Shipment) {
	w.Header().Set("ETag", etag(sh.Version))
	writeJSON(w, status, s.toAPIShipment(r, sh))
}

// apiChangeError answers a change the service refused. A version
// mismatch is answered 412 with the shipment as it is now, so the client
// can show it and try again from it.
func (s *Server) apiChangeError(w http.ResponseWriter, r *http.Request, id pgtype.UUID, err error) {
	if errors.Is(err, service.ErrVersionMismatch) {
		if cur, gerr := s.svc.Shipments.Get(r.Context(), apiActor(r).TenantID, id); gerr == nil {
			w.Header().Set("ETag", etag(cur.Version))
			writeJSON(w, http.StatusPreconditionFailed, map[string]any{
				"error": apiFailure{
					Code: service.ErrVersionMismatch.APICode(), Message: service.ErrVersionMismatch.Message,
					RequestID: chimw.GetReqID(r.Context()),
				},
				"current": s.toAPIShipment(r, cur),
			})
			return
		}
	}
	s.apiError(w, r, err)
}

// replayedHeaders are the headers kept with a create's answer for its
// replays, beside its type.
var replayedHeaders = []string{"Location", "ETag"}

// idempotent runs a create once for each company, caller and
// Idempotency-Key, for 24 hours (service.IdempotencyService). A replay of
// the same request gets the first answer, marked Idempotent-Replayed; the
// same key with another request is refused 422; a replay while the first
// still runs, 409. Only a success is kept: a refused or failed request
// gives its key back, so it can be sent again, corrected or not.
func (s *Server) idempotent(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			next(w, r)
			return
		}
		a := apiActor(r)
		caller := a.UserID
		if !caller.Valid {
			caller = a.KeyID
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			s.apiError(w, r, &service.Error{Kind: service.KindInvalid, Message: "The request body could not be read."})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		req := service.IdempotentRequest{TenantID: a.TenantID, CallerID: caller, Key: key, Hash: service.RequestHash(r.Method, r.URL.Path, body)}
		prior, err := s.svc.Idempotency.Begin(r.Context(), req)
		switch {
		case errors.Is(err, service.ErrIdempotencyKeyReused):
			writeJSONError(w, r, http.StatusUnprocessableEntity, "idempotency_key_reused", err.Error())
			return
		case errors.Is(err, service.ErrIdempotencyKeyInFlight):
			writeJSONError(w, r, http.StatusConflict, "idempotency_key_in_flight", err.Error())
			return
		case err != nil:
			s.apiError(w, r, err)
			return
		case prior != nil:
			for name, v := range prior.Headers {
				w.Header().Set(name, v)
			}
			w.Header().Set("Content-Type", prior.ContentType)
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(prior.Status)
			_, _ = w.Write(prior.Body)
			return
		}

		// The key is given back unless the answer is kept, a panic included;
		// either way after the client has gone, too.
		ctx := context.WithoutCancel(r.Context())
		kept := false
		defer func() {
			if !kept {
				if err := s.svc.Idempotency.Release(ctx, req); err != nil {
					s.log.Error().Err(err).Msg("idempotency key not given back")
				}
			}
		}()
		rec := &recordingWriter{ResponseWriter: w, status: http.StatusOK}
		next(rec, r)
		if rec.status < 200 || rec.status > 299 {
			return
		}
		resp := service.IdempotentResponse{Status: rec.status, ContentType: w.Header().Get("Content-Type"), Body: rec.body.Bytes(), Headers: map[string]string{}}
		for _, name := range replayedHeaders {
			if v := w.Header().Get(name); v != "" {
				resp.Headers[name] = v
			}
		}
		if err := s.svc.Idempotency.Finish(ctx, req, resp); err != nil {
			s.log.Error().Err(err).Msg("idempotent response not kept")
			return
		}
		kept = true
	}
}

// recordingWriter passes an answer through and keeps a copy of it.
type recordingWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (w *recordingWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status, w.wroteHeader = status, true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *recordingWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}
