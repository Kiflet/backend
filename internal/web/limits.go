package web

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/httprate"
	httprateredis "github.com/go-chi/httprate-redis"

	"github.com/kiflet/backend/pkg/metrics"
)

// limit allows n requests a minute for each key. With Redis the count is
// every instance's together, so the limit holds however a client's
// requests are spread; if Redis doesn't answer within 100 ms, each
// instance counts on its own until it does.
func (s *Server) limit(name string, n int, key httprate.KeyFunc) func(http.Handler) http.Handler {
	opts := []httprate.Option{httprate.WithLimitHandler(s.tooManyRequests)}
	if s.redis != nil {
		opts = append(opts, httprateredis.WithRedisLimitCounter(&httprateredis.Config{
			Client:          s.redis,
			PrefixKey:       "kiflet:limit:" + name,
			FallbackTimeout: 100 * time.Millisecond,
			OnError: func(err error) {
				s.log.Warn().Err(err).Str("limit", name).Msg("redis: request limit")
			},
			OnFallbackChange: func(on bool) {
				v := 0.0
				if on {
					v = 1
				}
				metrics.LimitsLocal.WithLabelValues(name).Set(v)
				s.log.Warn().Bool("local", on).Str("limit", name).Msg("redis: request limits counted per instance")
			},
		}))
	}
	return httprate.LimitBy(n, time.Minute, key, opts...)
}

// driverRequestsPerMinute is one driver's allowance on the driver app's
// API: a phone syncs about once a minute, and more often only to send
// what it saved while offline.
const driverRequestsPerMinute = 300

// tokenKey keys a limit by the bearer token, hashed so that no token is
// kept in Redis: one driver's phone, from whatever address. A request
// without one is keyed by its address, and refused by requireDriver.
func tokenKey(r *http.Request) (string, error) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return "address:" + remoteIP(r), nil
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:16]), nil
}
