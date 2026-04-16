package usecase_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"analytics-service/internal/domain/entity"
	"analytics-service/internal/usecase"
	"analytics-service/internal/usecase/mocks"

	gomock "go.uber.org/mock/gomock"
)

func newUsecase(
	ctrl *gomock.Controller,
	clicks *mocks.MockClickRepository,
	cache *mocks.MockStatsCache,
) *usecase.StatsUsecase {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return usecase.NewStatsUsecase(clicks, cache, 5*time.Minute, log)
}

func makeStats(code string) *entity.Stats {
	now := time.Now()
	return &entity.Stats{
		ShortCode:    code,
		TotalClicks:  100,
		UniqueIPs:    42,
		TopCountries: []entity.CountryStat{{Country: "RU", Clicks: 70}},
		LastClickAt:  &now,
	}
}

func TestGetStats_CacheHit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clicks := mocks.NewMockClickRepository(ctrl)
	cache := mocks.NewMockStatsCache(ctrl)

	stats := makeStats("abc1234")
	cache.EXPECT().Get(gomock.Any(), "abc1234").Return(stats, nil)
	clicks.EXPECT().GetStats(gomock.Any(), gomock.Any()).Times(0)

	uc := newUsecase(ctrl, clicks, cache)
	got, err := uc.GetStats(context.Background(), "abc1234")

	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if got.TotalClicks != stats.TotalClicks {
		t.Errorf("want %d total clicks, got %d", stats.TotalClicks, got.TotalClicks)
	}
}

func TestGetStats_CacheMiss_PopulatesCache(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clicks := mocks.NewMockClickRepository(ctrl)
	cache := mocks.NewMockStatsCache(ctrl)

	stats := makeStats("miss123")
	cache.EXPECT().Get(gomock.Any(), "miss123").Return(nil, entity.ErrStatsNotFound)
	clicks.EXPECT().GetStats(gomock.Any(), "miss123").Return(stats, nil)
	cache.EXPECT().Set(gomock.Any(), stats, gomock.Any()).Return(nil)

	uc := newUsecase(ctrl, clicks, cache)
	got, err := uc.GetStats(context.Background(), "miss123")

	if err != nil {
		t.Fatalf("GetStats on cache miss: %v", err)
	}
	if got.TotalClicks != stats.TotalClicks {
		t.Errorf("want %d total clicks, got %d", stats.TotalClicks, got.TotalClicks)
	}
}

func TestGetStats_CacheMiss_CacheSetFailBestEffort(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clicks := mocks.NewMockClickRepository(ctrl)
	cache := mocks.NewMockStatsCache(ctrl)

	stats := makeStats("cachefail")
	cache.EXPECT().Get(gomock.Any(), "cachefail").Return(nil, entity.ErrStatsNotFound)
	clicks.EXPECT().GetStats(gomock.Any(), "cachefail").Return(stats, nil)
	cache.EXPECT().Set(gomock.Any(), stats, gomock.Any()).Return(errors.New("redis unavailable"))

	uc := newUsecase(ctrl, clicks, cache)
	got, err := uc.GetStats(context.Background(), "cachefail")

	if err != nil {
		t.Fatalf("GetStats should succeed even if cache.Set fails: %v", err)
	}
	if got.ShortCode != "cachefail" {
		t.Errorf("unexpected short code: %s", got.ShortCode)
	}
}

func TestGetStats_EmptyStats(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clicks := mocks.NewMockClickRepository(ctrl)
	cache := mocks.NewMockStatsCache(ctrl)

	emptyStats := &entity.Stats{ShortCode: "noclick", TotalClicks: 0}
	cache.EXPECT().Get(gomock.Any(), "noclick").Return(nil, entity.ErrStatsNotFound)
	clicks.EXPECT().GetStats(gomock.Any(), "noclick").Return(emptyStats, nil)
	cache.EXPECT().Set(gomock.Any(), emptyStats, gomock.Any()).Return(nil)

	uc := newUsecase(ctrl, clicks, cache)
	got, err := uc.GetStats(context.Background(), "noclick")

	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if got.TotalClicks != 0 {
		t.Errorf("want 0 clicks, got %d", got.TotalClicks)
	}
}

func TestGetStats_DBError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clicks := mocks.NewMockClickRepository(ctrl)
	cache := mocks.NewMockStatsCache(ctrl)

	cache.EXPECT().Get(gomock.Any(), "dberror").Return(nil, entity.ErrStatsNotFound)
	clicks.EXPECT().GetStats(gomock.Any(), "dberror").Return(nil, errors.New("connection refused"))

	uc := newUsecase(ctrl, clicks, cache)
	_, err := uc.GetStats(context.Background(), "dberror")

	if err == nil {
		t.Error("want error when DB fails, got nil")
	}
}
