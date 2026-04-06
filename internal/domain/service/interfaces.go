package service

import (
	"time"

	"auth-service/internal/domain/entity"

	"github.com/google/uuid"
)

type UserRepository interface {
	Create(user *entity.User) error
	FindByEmail(email string) (*entity.User, error)
	FindByID(id uuid.UUID) (*entity.User, error)
}

type SessionRepository interface {
	Create(session *entity.RefreshSession) error
	FindByTokenHash(hash string) (*entity.RefreshSession, error)
	RevokeByID(id uuid.UUID) error
	RevokeAllByUserID(userID uuid.UUID) error
	DeleteExpired() (int64, error)
}

type TokenCache interface {
	RevokeJTI(jti string, ttl time.Duration) error
	IsRevoked(jti string) (bool, error)
}

type TokenManager interface {
	GenerateAccess(userID string, ttl time.Duration) (tokenStr, jti string, err error)
	VerifyAccess(tokenStr string) (userID, jti string, err error)
}
