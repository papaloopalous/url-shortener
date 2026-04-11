package entity

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrURLNotFound   = errors.New("url not found")
	ErrURLExpired    = errors.New("url expired")
	ErrURLDeleted    = errors.New("url deleted")
	ErrNotOwner      = errors.New("not the owner of this url")
	ErrCodeCollision = errors.New("short code collision, retry")
)

type URLStatus string

const (
	StatusActive      URLStatus = "active"
	StatusSoftDeleted URLStatus = "soft_deleted"
)

type URL struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ShortCode string
	LongURL   string
	Status    URLStatus
	ExpiresAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

func (u *URL) IsAccessible() error {
	if u.Status == StatusSoftDeleted || u.DeletedAt != nil {
		return ErrURLDeleted
	}
	if u.ExpiresAt != nil && time.Now().After(*u.ExpiresAt) {
		return ErrURLExpired
	}
	return nil
}

type OutboxEvent struct {
	ID          uuid.UUID
	EventType   string
	Payload     []byte
	Status      string
	CreatedAt   time.Time
	PublishedAt *time.Time
}

const (
	EventURLCreated = "url.created"
	EventURLClicked = "url.clicked"
	EventURLDeleted = "url.deleted"

	OutboxStatusPending   = "pending"
	OutboxStatusPublished = "published"
)
