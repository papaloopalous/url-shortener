//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"analytics-service/internal/adapters/db/postgres"
	"analytics-service/internal/domain/entity"
	"analytics-service/pkg/testhelpers"

	"github.com/google/uuid"
)

func TestMain(m *testing.M) {
	testhelpers.RunWithPostgres(m, "../../../../migrations")
}

func makeClick(shortCode string) *entity.ClickEvent {
	return &entity.ClickEvent{
		ID:        uuid.New(),
		ShortCode: shortCode,
		IP:        "1.2.3.4",
		UserAgent: "Mozilla/5.0",
		Referer:   "https://google.com",
		Country:   "RU",
		ClickedAt: time.Now(),
	}
}

func TestInboxRepo_SaveWithClick_Success(t *testing.T) {
	pool := testhelpers.MustGetPool(t)
	inboxRepo := postgres.NewInboxRepo(pool)
	clickRepo := postgres.NewClickRepo(pool)
	ctx := context.Background()

	eventID := uuid.New()
	click := makeClick("abc1234")

	if err := inboxRepo.SaveWithClick(ctx, eventID, click); err != nil {
		t.Fatalf("SaveWithClick: %v", err)
	}

	stats, err := clickRepo.GetStats(ctx, "abc1234")
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalClicks < 1 {
		t.Errorf("want at least 1 click, got %d", stats.TotalClicks)
	}
}

func TestInboxRepo_SaveWithClick_DuplicateEvent(t *testing.T) {
	pool := testhelpers.MustGetPool(t)
	inboxRepo := postgres.NewInboxRepo(pool)
	ctx := context.Background()

	eventID := uuid.New()
	click := makeClick("dedup123")

	if err := inboxRepo.SaveWithClick(ctx, eventID, click); err != nil {
		t.Fatalf("first SaveWithClick: %v", err)
	}

	click2 := makeClick("dedup123")
	err := inboxRepo.SaveWithClick(ctx, eventID, click2)
	if !errors.Is(err, entity.ErrDuplicateEvent) {
		t.Errorf("want ErrDuplicateEvent, got %v", err)
	}
}

func TestClickRepo_GetStats_Empty(t *testing.T) {
	pool := testhelpers.MustGetPool(t)
	clickRepo := postgres.NewClickRepo(pool)
	ctx := context.Background()

	stats, err := clickRepo.GetStats(ctx, "noexist999")
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalClicks != 0 {
		t.Errorf("want 0 clicks for unknown code, got %d", stats.TotalClicks)
	}
}

func TestClickRepo_GetStats_Aggregation(t *testing.T) {
	pool := testhelpers.MustGetPool(t)
	inboxRepo := postgres.NewInboxRepo(pool)
	clickRepo := postgres.NewClickRepo(pool)
	ctx := context.Background()

	code := "aggr1234"

	clicks := []*entity.ClickEvent{
		{ID: uuid.New(), ShortCode: code, IP: "10.0.0.1", Country: "RU", ClickedAt: time.Now()},
		{ID: uuid.New(), ShortCode: code, IP: "10.0.0.2", Country: "US", ClickedAt: time.Now()},
		{ID: uuid.New(), ShortCode: code, IP: "10.0.0.1", Country: "RU", ClickedAt: time.Now()},
	}
	for _, c := range clicks {
		if err := inboxRepo.SaveWithClick(ctx, uuid.New(), c); err != nil {
			t.Fatalf("SaveWithClick: %v", err)
		}
	}

	stats, err := clickRepo.GetStats(ctx, code)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalClicks < 3 {
		t.Errorf("want at least 3 total clicks, got %d", stats.TotalClicks)
	}
	if stats.UniqueIPs < 2 {
		t.Errorf("want at least 2 unique IPs, got %d", stats.UniqueIPs)
	}
	if stats.LastClickAt == nil {
		t.Error("want non-nil LastClickAt")
	}
	if len(stats.TopCountries) == 0 {
		t.Error("want at least 1 country in TopCountries")
	}
}
