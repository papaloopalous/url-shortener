//go:build integration

package redis_test

import (
	"context"
	"errors"
	"testing"
	"time"

	rediscache "analytics-service/internal/adapters/cache/redis"
	"analytics-service/internal/domain/entity"
	"analytics-service/pkg/testhelpers"
)

func TestMain(m *testing.M) {
	testhelpers.RunWithRedis(m)
}

func makeStats(code string) *entity.Stats {
	now := time.Now()
	return &entity.Stats{
		ShortCode:   code,
		TotalClicks: 42,
		UniqueIPs:   17,
		TopCountries: []entity.CountryStat{
			{Country: "RU", Clicks: 30},
			{Country: "US", Clicks: 12},
		},
		LastClickAt: &now,
	}
}

func TestStatsCache_GetSetInvalidate(t *testing.T) {
	client := testhelpers.MustGetRedis(t)
	cache := rediscache.NewStatsCache(client)
	ctx := context.Background()

	_, err := cache.Get(ctx, "abc1234")
	if !errors.Is(err, entity.ErrStatsNotFound) {
		t.Errorf("want ErrStatsNotFound, got %v", err)
	}

	stats := makeStats("abc1234")
	if err := cache.Set(ctx, stats, time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := cache.Get(ctx, "abc1234")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TotalClicks != stats.TotalClicks {
		t.Errorf("TotalClicks mismatch: want %d, got %d", stats.TotalClicks, got.TotalClicks)
	}
	if got.UniqueIPs != stats.UniqueIPs {
		t.Errorf("UniqueIPs mismatch: want %d, got %d", stats.UniqueIPs, got.UniqueIPs)
	}
	if len(got.TopCountries) != len(stats.TopCountries) {
		t.Errorf("TopCountries len mismatch: want %d, got %d", len(stats.TopCountries), len(got.TopCountries))
	}

	if err := cache.Invalidate(ctx, "abc1234"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	_, err = cache.Get(ctx, "abc1234")
	if !errors.Is(err, entity.ErrStatsNotFound) {
		t.Errorf("after invalidate: want ErrStatsNotFound, got %v", err)
	}
}

func TestStatsCache_TTLExpiry(t *testing.T) {
	client := testhelpers.MustGetRedis(t)
	cache := rediscache.NewStatsCache(client)
	ctx := context.Background()

	stats := makeStats("ttl1234")
	if err := cache.Set(ctx, stats, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	_, err := cache.Get(ctx, "ttl1234")
	if err != nil {
		t.Fatalf("expected key to exist immediately after set: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	_, err = cache.Get(ctx, "ttl1234")
	if !errors.Is(err, entity.ErrStatsNotFound) {
		t.Errorf("want ErrStatsNotFound after TTL expiry, got %v", err)
	}
}
