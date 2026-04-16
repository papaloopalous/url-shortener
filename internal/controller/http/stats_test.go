package http_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"analytics-service/internal/controller/http/dto"
	"analytics-service/internal/domain/entity"
	"analytics-service/internal/usecase"
	"analytics-service/internal/usecase/mocks"

	gomock "go.uber.org/mock/gomock"

	handler "analytics-service/internal/controller/http"
)

func setupRouter(
	ctrl *gomock.Controller,
	clicks *mocks.MockClickRepository,
	cache *mocks.MockStatsCache,
) http.Handler {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	uc := usecase.NewStatsUsecase(clicks, cache, 5*time.Minute, log)
	h := handler.NewStatsHandler(uc, log)
	return handler.NewRouter(h, log)
}

func TestGetStats_200(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clicks := mocks.NewMockClickRepository(ctrl)
	cache := mocks.NewMockStatsCache(ctrl)

	now := time.Now()
	stats := &entity.Stats{
		ShortCode:    "abc1234",
		TotalClicks:  150,
		UniqueIPs:    60,
		TopCountries: []entity.CountryStat{{Country: "RU", Clicks: 100}},
		LastClickAt:  &now,
	}

	cache.EXPECT().Get(gomock.Any(), "abc1234").Return(stats, nil)

	router := setupRouter(ctrl, clicks, cache)
	req := httptest.NewRequest(http.MethodGet, "/stats/abc1234", nil)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("want 200, got %d: %s", rw.Code, rw.Body.String())
	}

	var resp dto.StatsResponse
	if err := json.NewDecoder(rw.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TotalClicks != 150 {
		t.Errorf("want TotalClicks=150, got %d", resp.TotalClicks)
	}
	if resp.UniqueIPs != 60 {
		t.Errorf("want UniqueIPs=60, got %d", resp.UniqueIPs)
	}
	if len(resp.TopCountries) != 1 {
		t.Errorf("want 1 country, got %d", len(resp.TopCountries))
	}
	if resp.TopCountries[0].Country != "RU" {
		t.Errorf("want RU, got %s", resp.TopCountries[0].Country)
	}
}

func TestGetStats_200_EmptyStats(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clicks := mocks.NewMockClickRepository(ctrl)
	cache := mocks.NewMockStatsCache(ctrl)

	emptyStats := &entity.Stats{ShortCode: "zero123", TotalClicks: 0}
	cache.EXPECT().Get(gomock.Any(), "zero123").Return(nil, entity.ErrStatsNotFound)
	clicks.EXPECT().GetStats(gomock.Any(), "zero123").Return(emptyStats, nil)
	cache.EXPECT().Set(gomock.Any(), emptyStats, gomock.Any()).Return(nil)

	router := setupRouter(ctrl, clicks, cache)
	req := httptest.NewRequest(http.MethodGet, "/stats/zero123", nil)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rw.Code)
	}

	var resp dto.StatsResponse
	if err := json.NewDecoder(rw.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TotalClicks != 0 {
		t.Errorf("want 0 clicks, got %d", resp.TotalClicks)
	}
}

func TestGetStats_500_DBError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clicks := mocks.NewMockClickRepository(ctrl)
	cache := mocks.NewMockStatsCache(ctrl)

	cache.EXPECT().Get(gomock.Any(), "dberr11").Return(nil, entity.ErrStatsNotFound)
	clicks.EXPECT().GetStats(gomock.Any(), "dberr11").Return(nil, errors.New("postgres down"))

	router := setupRouter(ctrl, clicks, cache)
	req := httptest.NewRequest(http.MethodGet, "/stats/dberr11", nil)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", rw.Code)
	}
}

func TestHealthz_200(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clicks := mocks.NewMockClickRepository(ctrl)
	cache := mocks.NewMockStatsCache(ctrl)
	router := setupRouter(ctrl, clicks, cache)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rw.Code)
	}
}
