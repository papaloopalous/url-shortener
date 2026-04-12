package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"shortener-service/internal/domain/entity"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "shortener:url:"

type URLCache struct {
	client *redis.Client
}

func NewURLCache(client *redis.Client) *URLCache {
	return &URLCache{client: client}
}

func (c *URLCache) Get(ctx context.Context, code string) (*entity.URL, error) {
	data, err := c.client.Get(ctx, key(code)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, entity.ErrURLNotFound
		}
		return nil, fmt.Errorf("redis get: %w", err)
	}

	var url entity.URL
	if err := json.Unmarshal(data, &url); err != nil {
		return nil, fmt.Errorf("redis unmarshal: %w", err)
	}
	return &url, nil
}

func (c *URLCache) Set(ctx context.Context, url *entity.URL, ttl time.Duration) error {
	data, err := json.Marshal(url)
	if err != nil {
		return fmt.Errorf("redis marshal: %w", err)
	}

	if err := c.client.Set(ctx, key(url.ShortCode), data, ttl).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}

func (c *URLCache) Delete(ctx context.Context, code string) error {
	if err := c.client.Del(ctx, key(code)).Err(); err != nil {
		return fmt.Errorf("redis delete: %w", err)
	}
	return nil
}

func (c *URLCache) DeleteBatch(ctx context.Context, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	keys := make([]string, len(codes))
	for i, code := range codes {
		keys[i] = key(code)
	}
	if err := c.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("redis delete batch: %w", err)
	}
	return nil
}

func key(code string) string {
	return keyPrefix + code
}
