package web

import (
	"net/http"
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
