package entity

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrInvalidPassword   = errors.New("invalid credentials")
	ErrSessionNotFound   = errors.New("session not found")
	ErrSessionExpired    = errors.New("session expired")
	ErrSessionRevoked    = errors.New("session revoked")
	ErrTokenReuse        = errors.New("refresh token reuse detected")
)

type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type RefreshSession struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	UserAgent string
	IPAddress string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time // nil значит активен
}

func (s *RefreshSession) IsRevoked() bool {
	return s.RevokedAt != nil
}

func (s *RefreshSession) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

func (s *RefreshSession) Validate() error {
	if s.IsRevoked() {
		return ErrSessionRevoked
	}
	if s.IsExpired() {
		return ErrSessionExpired
	}
	return nil
}
