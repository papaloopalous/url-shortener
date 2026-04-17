package ratelimit

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"gateway-service/internal/config"

	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	client     *redis.Client
	capacity   float64       // ёмкость бакета
	ratePerSec float64       // скорость утечки
	window     time.Duration // для вычисления TTL и RetryAfter
	enabled    bool
}

func NewLimiter(client *redis.Client, cfg config.RateLimitConfig) *Limiter {
	capacity := float64(cfg.Requests)
	ratePerSec := capacity / cfg.Window.Seconds()
	return &Limiter{
		client:     client,
		capacity:   capacity,
		ratePerSec: ratePerSec,
		window:     cfg.Window,
		enabled:    cfg.Enabled,
	}
}

func (l *Limiter) Allow(ctx context.Context, ip string) (bool, int64, error) {
	if !l.enabled {
		return true, int64(l.capacity), nil
	}

	k := l.key(ip)
	now := time.Now()

	var count float64
	var lastTime time.Time

	val, err := l.client.Get(ctx, k).Result()
	switch {
	case err == redis.Nil:
		count = 0
		lastTime = now
	case err != nil:
		return true, 0, fmt.Errorf("redis GET %q: %w", k, err)
	default:
		count, lastTime, err = parseBucket(val)
		if err != nil {
			count = 0
			lastTime = now
		}
	}

	elapsed := now.Sub(lastTime).Seconds()
	leaked := elapsed * l.ratePerSec
	count = math.Max(0, count-leaked)

	if count >= l.capacity {
		return false, 0, nil
	}

	count++
	remaining := int64(math.Max(0, l.capacity-count))

	ttl := time.Duration(l.capacity/l.ratePerSec*float64(time.Second)) + time.Second

	if err := l.client.Set(ctx, k, formatBucket(count, now), ttl).Err(); err != nil {
		return true, remaining, fmt.Errorf("redis SET %q: %w", k, err)
	}

	return true, remaining, nil
}

func (l *Limiter) RetryAfter() int64 {
	secs := 1.0 / l.ratePerSec
	return int64(math.Ceil(secs))
}

func (l *Limiter) Limit() int64 { return int64(l.capacity) }

func (l *Limiter) key(ip string) string {
	return fmt.Sprintf("leaky:%s", ip)
}

func formatBucket(count float64, t time.Time) string {
	return fmt.Sprintf("%s:%d",
		strconv.FormatFloat(count, 'f', 6, 64),
		t.UnixMilli(),
	)
}

func parseBucket(val string) (float64, time.Time, error) {
	parts := strings.SplitN(val, ":", 2)
	if len(parts) != 2 {
		return 0, time.Time{}, fmt.Errorf("invalid bucket format: %q", val)
	}
	count, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("parse count: %w", err)
	}
	ms, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("parse timestamp: %w", err)
	}
	return count, time.UnixMilli(ms), nil
}
