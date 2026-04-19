package ratelimit

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"gateway-service/internal/config"

	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	client   *redis.Client
	rateMs   int64 // скорость утечки
	burstMs  int64 // для вычисления TTL и RetryAfter
	ttl      time.Duration
	capacity int64 // ёмкость бакета
	enabled  bool
}

func NewLimiter(client *redis.Client, cfg config.RateLimitConfig) *Limiter {
	windowMs := cfg.Window.Milliseconds()
	return &Limiter{
		client:   client,
		rateMs:   windowMs / cfg.Requests,
		burstMs:  windowMs,
		ttl:      cfg.Window * 2,
		capacity: cfg.Requests,
		enabled:  cfg.Enabled,
	}
}

func (l *Limiter) Allow(ctx context.Context, ip string) (bool, int64, error) {
	if !l.enabled {
		return true, l.capacity, nil
	}

	k := l.key(ip)
	var allowed bool
	var remaining int64

	err := l.client.Watch(ctx, func(tx *redis.Tx) error {
		nowMs := time.Now().UnixMilli()

		tatOld := nowMs
		val, err := tx.Get(ctx, k).Result()
		switch {
		case err == redis.Nil:
		case err != nil:
			return err
		default:
			tatOld, err = strconv.ParseInt(val, 10, 64)
			if err != nil {
				tatOld = nowMs
			}
		}

		tatNew := max64(nowMs, tatOld) + l.rateMs

		if tatNew-nowMs > l.burstMs {
			allowed = false
			remaining = 0
			return nil
		}

		remaining = int64(math.Max(0, float64(l.burstMs-(tatNew-nowMs))/float64(l.rateMs)))

		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(ctx, k, strconv.FormatInt(tatNew, 10), l.ttl)
			return nil
		})
		if err != nil {
			return err
		}

		allowed = true
		return nil
	}, k)

	if err != nil {
		return true, 0, fmt.Errorf("redis watch %q: %w", k, err)
	}

	return allowed, remaining, nil
}

func (l *Limiter) RetryAfter() int64    { return l.rateMs / 1000 }
func (l *Limiter) Limit() int64         { return l.capacity }
func (l *Limiter) key(ip string) string { return fmt.Sprintf("gcra:%s", ip) }

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
