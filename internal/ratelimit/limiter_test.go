//go:build integration

package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"gateway-service/internal/config"
	"gateway-service/internal/ratelimit"
	"gateway-service/pkg/testhelpers"

	"github.com/redis/go-redis/v9"
)

func TestMain(m *testing.M) {
	testhelpers.RunWithRedis(m)
}

func newLimiter(t *testing.T, capacity int64, window time.Duration, enabled bool) *ratelimit.Limiter {
	t.Helper()
	client := testhelpers.MustGetRedis(t)
	return ratelimit.NewLimiter(client, config.RateLimitConfig{
		Requests: capacity,
		Window:   window,
		Enabled:  enabled,
	})
}

func newBadLimiter(capacity int64, window time.Duration) *ratelimit.Limiter {
	bad := redis.NewClient(&redis.Options{Addr: "127.0.0.1:19999"})
	return ratelimit.NewLimiter(bad, config.RateLimitConfig{
		Requests: capacity,
		Window:   window,
		Enabled:  true,
	})
}

func TestAllow_BurstWithinCapacity(t *testing.T) {
	ctx := context.Background()
	capacity := int64(5)
	l := newLimiter(t, capacity, 10*time.Second, true)

	for i := int64(0); i < capacity; i++ {
		allowed, _, err := l.Allow(ctx, "10.0.0.1")
		if err != nil {
			t.Fatalf("request %d: unexpected err: %v", i+1, err)
		}
		if !allowed {
			t.Fatalf("request %d should be allowed (bucket not full yet)", i+1)
		}
	}
}

func TestAllow_ExceedCapacity(t *testing.T) {
	ctx := context.Background()
	capacity := int64(3)
	l := newLimiter(t, capacity, 60*time.Second, true)
	ip := "10.0.0.2"

	for i := int64(0); i < capacity; i++ {
		allowed, _, err := l.Allow(ctx, ip)
		if err != nil {
			t.Fatalf("request %d: unexpected err: %v", i+1, err)
		}
		if !allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	allowed, remaining, err := l.Allow(ctx, ip)
	if err != nil {
		t.Fatalf("N+1 request: unexpected err: %v", err)
	}
	if allowed {
		t.Error("N+1 request must be rejected when bucket is full")
	}
	if remaining != 0 {
		t.Errorf("remaining must be 0 when rejected, got %d", remaining)
	}
}

func TestAllow_BucketDrainsOverTime(t *testing.T) {
	ctx := context.Background()
	capacity := int64(2)
	window := 1 * time.Second
	l := newLimiter(t, capacity, window, true)
	ip := "10.0.0.3"

	for i := int64(0); i < capacity; i++ {
		l.Allow(ctx, ip) //nolint:errcheck
	}

	allowed, _, _ := l.Allow(ctx, ip)
	if allowed {
		t.Fatal("should be rejected when bucket is full")
	}

	time.Sleep(600 * time.Millisecond)

	allowed, _, err := l.Allow(ctx, ip)
	if err != nil {
		t.Fatalf("unexpected err after drain: %v", err)
	}
	if !allowed {
		t.Error("should be allowed after bucket partially drains")
	}
}

func TestAllow_DisabledAlwaysAllows(t *testing.T) {
	ctx := context.Background()
	bad := redis.NewClient(&redis.Options{Addr: "127.0.0.1:19999"})
	l := ratelimit.NewLimiter(bad, config.RateLimitConfig{
		Requests: 1,
		Window:   time.Minute,
		Enabled:  false,
	})

	for i := 0; i < 100; i++ {
		allowed, _, err := l.Allow(ctx, "10.0.0.4")
		if err != nil {
			t.Fatalf("unexpected err on iteration %d: %v", i, err)
		}
		if !allowed {
			t.Fatal("disabled limiter must always allow")
		}
	}
}

func TestAllow_RedisUnavailable_FailOpen(t *testing.T) {
	ctx := context.Background()
	l := newBadLimiter(10, time.Minute)

	allowed, _, err := l.Allow(ctx, "10.0.0.5")
	if err == nil {
		t.Fatal("expected an error from unavailable Redis")
	}
	if !allowed {
		t.Error("fail open: must allow request when Redis is down")
	}
}

func TestAllow_RemainingDecrementsCorrectly(t *testing.T) {
	ctx := context.Background()
	capacity := int64(5)
	l := newLimiter(t, capacity, 60*time.Second, true)
	ip := "10.0.0.6"

	for i := int64(0); i < capacity; i++ {
		_, remaining, err := l.Allow(ctx, ip)
		if err != nil {
			t.Fatalf("request %d: unexpected err: %v", i+1, err)
		}
		want := capacity - i - 1
		if remaining != want {
			t.Errorf("request %d: remaining=%d, want %d", i+1, remaining, want)
		}
	}
}

func TestAllow_DifferentIPsAreIsolated(t *testing.T) {
	ctx := context.Background()
	capacity := int64(2)
	l := newLimiter(t, capacity, 60*time.Second, true)

	for i := int64(0); i < capacity; i++ {
		l.Allow(ctx, "192.168.0.1") //nolint:errcheck
	}
	allowed, _, _ := l.Allow(ctx, "192.168.0.1")
	if allowed {
		t.Fatal("ip1 should be rate-limited")
	}

	allowed, _, err := l.Allow(ctx, "192.168.0.2")
	if err != nil {
		t.Fatalf("ip2: unexpected err: %v", err)
	}
	if !allowed {
		t.Error("ip2 must have its own independent bucket")
	}
}
