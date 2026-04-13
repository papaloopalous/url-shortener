//go:build integration

package redis_test

import (
	"context"
	"errors"
	"testing"
	"time"

	rediscache "shortener-service/internal/adapters/cache/redis"
	"shortener-service/internal/domain/entity"
	"shortener-service/pkg/testhelpers"

	"github.com/google/uuid"
)

func TestMain(m *testing.M) {
	testhelpers.RunWithRedis(m)
}

func TestURLCache_GetSetDelete(t *testing.T) {
	client := testhelpers.MustGetRedis(t)
	cache := rediscache.NewURLCache(client)
	ctx := context.Background()

	url := &entity.URL{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		ShortCode: "test123",
		LongURL:   "https://example.com",
		Status:    entity.StatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Get на несуществующий ключ.
	_, err := cache.Get(ctx, url.ShortCode)
	if !errors.Is(err, entity.ErrURLNotFound) {
		t.Errorf("want ErrURLNotFound, got %v", err)
	}

	// Set.
	if err := cache.Set(ctx, url, 24*time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Get.
	got, err := cache.Get(ctx, url.ShortCode)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LongURL != url.LongURL {
		t.Errorf("LongURL mismatch: want %s, got %s", url.LongURL, got.LongURL)
	}

	// Delete.
	if err := cache.Delete(ctx, url.ShortCode); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = cache.Get(ctx, url.ShortCode)
	if !errors.Is(err, entity.ErrURLNotFound) {
		t.Errorf("after delete: want ErrURLNotFound, got %v", err)
	}
}

func TestURLCache_DeleteBatch(t *testing.T) {
	client := testhelpers.MustGetRedis(t)
	cache := rediscache.NewURLCache(client)
	ctx := context.Background()

	codes := []string{"code1", "code2", "code3"}
	for _, code := range codes {
		url := &entity.URL{
			ID:        uuid.New(),
			UserID:    uuid.New(),
			ShortCode: code,
			LongURL:   "https://example.com/" + code,
			Status:    entity.StatusActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := cache.Set(ctx, url, time.Hour); err != nil {
			t.Fatal(err)
		}
	}

	if err := cache.DeleteBatch(ctx, codes); err != nil {
		t.Fatalf("DeleteBatch: %v", err)
	}

	for _, code := range codes {
		_, err := cache.Get(ctx, code)
		if !errors.Is(err, entity.ErrURLNotFound) {
			t.Errorf("code %s: want ErrURLNotFound after batch delete, got %v", code, err)
		}
	}
}

func TestURLCache_TTLExpiry(t *testing.T) {
	client := testhelpers.MustGetRedis(t)
	cache := rediscache.NewURLCache(client)
	ctx := context.Background()

	url := &entity.URL{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		ShortCode: "expiry1",
		LongURL:   "https://expiry.com",
		Status:    entity.StatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := cache.Set(ctx, url, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	_, err := cache.Get(ctx, url.ShortCode)
	if err != nil {
		t.Fatalf("expected key to exist immediately after set: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	_, err = cache.Get(ctx, url.ShortCode)
	if !errors.Is(err, entity.ErrURLNotFound) {
		t.Errorf("want ErrURLNotFound after TTL expiry, got %v", err)
	}
}
