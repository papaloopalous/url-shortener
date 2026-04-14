package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"analytics-service/internal/domain/entity"
	"analytics-service/internal/domain/service"
	"analytics-service/pkg/metrics"
	"analytics-service/pkg/tracing"

	"go.opentelemetry.io/otel/attribute"
)

type StatsUsecase struct {
	clicks   service.ClickRepository
	cache    service.StatsCache
	cacheTTL time.Duration
	log      *slog.Logger
}

func NewStatsUsecase(
	clicks service.ClickRepository,
	cache service.StatsCache,
	cacheTTL time.Duration,
	log *slog.Logger,
) *StatsUsecase {
	return &StatsUsecase{
		clicks:   clicks,
		cache:    cache,
		cacheTTL: cacheTTL,
		log:      log,
	}
}

func (uc *StatsUsecase) GetStats(ctx context.Context, shortCode string) (*entity.Stats, error) {
	tracer := tracing.Tracer("usecase/stats")
	ctx, span := tracer.Start(ctx, "StatsUsecase.GetStats")
	defer span.End()

	span.SetAttributes(attribute.String("stats.short_code", shortCode))

	stats, err := uc.cache.Get(ctx, shortCode)
	if err == nil {
		span.SetAttributes(attribute.Bool("stats.cache_hit", true))
		metrics.IncEvent(metrics.EventCacheHit)
		return stats, nil
	}

	if !errors.Is(err, entity.ErrStatsNotFound) {
		uc.log.WarnContext(ctx, "cache get failed, falling through to PG", "error", err)
	}

	span.SetAttributes(attribute.Bool("stats.cache_hit", false))
	metrics.IncEvent(metrics.EventCacheMiss)

	stats, err = uc.clicks.GetStats(ctx, shortCode)
	if err != nil {
		return nil, fmt.Errorf("get stats from pg: %w", err)
	}

	if cacheErr := uc.cache.Set(ctx, stats, uc.cacheTTL); cacheErr != nil {
		uc.log.WarnContext(ctx, "cache set failed (best-effort)", "error", cacheErr)
	}

	return stats, nil
}
