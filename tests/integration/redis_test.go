//go:build integration

package integration

// The app with Redis (docs/architecture.md, section 7): the request
// limits are every instance's together.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// redisImage is the version deployments/ runs.
const redisImage = "redis:8.10-alpine"

// sharedRedis starts one Redis for the tests that need one; TestMain stops
// it.
var sharedRedis = sync.OnceValues(startRedis)

var stopRedis = func() {}

func startRedis() (string, error) {
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        redisImage,
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	if err != nil {
		return "", err
	}
	stopRedis = func() { _ = c.Terminate(ctx, testcontainers.StopTimeout(time.Second)) }
	endpoint, err := c.PortEndpoint(ctx, "6379/tcp", "")
	if err != nil {
		return "", err
	}
	return "redis://" + endpoint + "/0", nil
}

func redisURL(t *testing.T) string {
	t.Helper()
	u, err := sharedRedis()
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func withRedis(u string) func(*setup) {
	return func(s *setup) { s.cfg.RedisURL = u }
}

// TestSharedRateLimits: with Redis, an address's allowance is the same
// however its requests are spread over the instances: ten code requests a
// minute in all, not ten on each.
func TestSharedRateLimits(t *testing.T) {
	u := redisURL(t)
	opts, err := redis.ParseURL(u)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opts)
	defer rdb.Close() //nolint:errcheck // closing
	withinOneMinute(t, func(_ *env) string {
		// Counts from an earlier attempt start empty, like the app's.
		if err := rdb.FlushDB(context.Background()).Err(); err != nil {
			return err.Error()
		}
		first, second := newEnv(t, withRedis(u)), newEnv(t, withRedis(u))
		b := first.browser()
		for i := range 10 {
			on := b
			if i%2 == 1 {
				on = b.on(second)
			}
			if p := on.post("/forgot", url.Values{"email": {fmt.Sprintf("nobody%d@example.et", i)}}); p.Status != http.StatusOK {
				return fmt.Sprintf("reset request %d: status %d, want it taken", i+1, p.Status)
			}
		}
		for _, e := range []*env{first, second} {
			if p := b.on(e).post("/forgot", url.Values{"email": {"nobody@example.et"}}); p.Status != http.StatusTooManyRequests {
				return fmt.Sprintf("the 11th reset request in a minute: status %d, want 429 from both instances", p.Status)
			}
		}
		if n, err := rdb.Keys(context.Background(), "kiflet:limit:strict*").Result(); err != nil || len(n) == 0 {
			return fmt.Sprintf("no counts in Redis (%v)", err)
		}
		return ""
	})
}
