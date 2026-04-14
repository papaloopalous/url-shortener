package service

import (
	"context"
	"time"

	"analytics-service/internal/domain/entity"

	"github.com/google/uuid"
)

type ClickRepository interface {
	GetStats(ctx context.Context, shortCode string) (*entity.Stats, error)
}

type InboxRepository interface {
	SaveWithClick(ctx context.Context, eventID uuid.UUID, click *entity.ClickEvent) error
}

type StatsCache interface {
	Get(ctx context.Context, shortCode string) (*entity.Stats, error)
	Set(ctx context.Context, stats *entity.Stats, ttl time.Duration) error
	Invalidate(ctx context.Context, shortCode string) error
}
