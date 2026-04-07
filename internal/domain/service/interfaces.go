package service

import (
	"context"
	"time"

	"auth-service/internal/domain/entity"

	"github.com/google/uuid"
)

type UserRepository interface {
	Create(ctx context.Context, user *entity.User) error
	FindByEmail(ctx context.Context, email string) (*entity.User, error)
	FindByID(ctx context.Context, id uuid.UUID) (*entity.User, error)
}

type SessionRepository interface {
	Create(ctx context.Context, session *entity.RefreshSession) error
	FindByTokenHash(ctx context.Context, hash string) (*entity.RefreshSession, error)
	RevokeByID(ctx context.Context, id uuid.UUID) error
	RevokeAllByUserID(ctx context.Context, userID uuid.UUID) error
	DeleteExpired(ctx context.Context) (int64, error)
}

type TokenCache interface {
	RevokeJTI(ctx context.Context, jti string, ttl time.Duration) error
	IsRevoked(ctx context.Context, jti string) (bool, error)
}

type TokenManager interface {
	GenerateAccess(userID string, ttl time.Duration) (tokenStr, jti string, err error)
	VerifyAccess(tokenStr string) (userID, jti string, err error)
}
