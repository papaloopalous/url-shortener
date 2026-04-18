package ratelimit

import "context"

type LimiterIface interface {
	Allow(ctx context.Context, ip string) (allowed bool, remaining int64, err error)
	RetryAfter() int64
	Limit() int64
}

var _ LimiterIface = (*Limiter)(nil)
