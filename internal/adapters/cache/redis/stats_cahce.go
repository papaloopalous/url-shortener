package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"analytics-service/internal/domain/entity"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "analytics:stats:"

type StatsCache struct {
	client *redis.Client
}

func NewStatsCache(client *redis.Client) *StatsCache {
	return &StatsCache{client: client}
}

func (c *StatsCache) Get(ctx context.Context, shortCode string) (*entity.Stats, error) {
	data, err := c.client.Get(ctx, key(shortCode)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, entity.ErrStatsNotFound
		}
		return nil, fmt.Errorf("redis get stats: %w", err)
	}

	var stats entity.Stats
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil, fmt.Errorf("redis unmarshal stats: %w", err)
	}
	return &stats, nil
}

func (c *StatsCache) Set(ctx context.Context, stats *entity.Stats, ttl time.Duration) error {
	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("redis marshal stats: %w", err)
	}

	if err := c.client.Set(ctx, key(stats.ShortCode), data, ttl).Err(); err != nil {
		return fmt.Errorf("redis set stats: %w", err)
	}
	return nil
}

func (c *StatsCache) Invalidate(ctx context.Context, shortCode string) error {
	if err := c.client.Del(ctx, key(shortCode)).Err(); err != nil {
		return fmt.Errorf("redis invalidate stats: %w", err)
	}
	return nil
}

func key(shortCode string) string {
	return keyPrefix + shortCode
}
