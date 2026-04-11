package service

import (
	"context"
	"time"

	"shortener-service/internal/domain/entity"

	"github.com/google/uuid"
)

type URLRepository interface {
	Create(ctx context.Context, url *entity.URL) error
	FindByCode(ctx context.Context, code string) (*entity.URL, error)
	FindByUserID(ctx context.Context, userID uuid.UUID) ([]*entity.URL, error)
	SoftDeleteBatch(ctx context.Context, codes []string, ownerID uuid.UUID) (int64, error)
}

type OutboxRepository interface {
	CreateWithURL(ctx context.Context, url *entity.URL, event *entity.OutboxEvent) error
	AppendEvent(ctx context.Context, event *entity.OutboxEvent) error
	PendingBatch(ctx context.Context, limit int) ([]*entity.OutboxEvent, error)
	MarkPublished(ctx context.Context, ids []uuid.UUID) error
}

type URLCache interface {
	Get(ctx context.Context, code string) (*entity.URL, error)
	Set(ctx context.Context, url *entity.URL, ttl time.Duration) error
	Delete(ctx context.Context, code string) error
	DeleteBatch(ctx context.Context, codes []string) error
}

type Publisher interface {
	Publish(ctx context.Context, topic string, key, value []byte) error
	Close() error
}

type AuthClient interface {
	ValidateToken(ctx context.Context, accessToken string) (userID string, err error)
}
