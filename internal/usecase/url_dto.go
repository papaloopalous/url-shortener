package usecase

import (
	"time"

	"github.com/google/uuid"
)

type CreateInput struct {
	UserID  uuid.UUID
	LongURL string
	TTL     *time.Duration
}

type CreateOutput struct {
	ShortCode string
	ShortURL  string
	LongURL   string
	ExpiresAt time.Time
}

type RedirectInput struct {
	Code      string
	IP        string
	UserAgent string
	Referer   string
}

type BatchDeleteInput struct {
	Codes   []string
	OwnerID uuid.UUID
}
