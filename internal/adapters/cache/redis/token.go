package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "auth:revoked_jti:"

type TokenCacheAdapter struct{ rdb *redis.Client }

func NewTokenCacheAdapter(rdb *redis.Client) *TokenCacheAdapter {
	return &TokenCacheAdapter{rdb: rdb}
}

func (a *TokenCacheAdapter) RevokeJTI(ctx context.Context, jti string, ttl time.Duration) error {
	if err := a.rdb.Set(ctx, keyPrefix+jti, 1, ttl).Err(); err != nil {
		return fmt.Errorf("redis set revoked jti: %w", err)
	}
	return nil
}

func (a *TokenCacheAdapter) IsRevoked(ctx context.Context, jti string) (bool, error) {
	err := a.rdb.Get(ctx, keyPrefix+jti).Err()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("redis get revoked jti: %w", err)
	}
	return true, nil
}
