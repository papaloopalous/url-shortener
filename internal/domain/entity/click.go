package entity

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrStatsNotFound  = errors.New("stats not found")
	ErrDuplicateEvent = errors.New("duplicate event, already processed")
)

type ClickEvent struct {
	ID        uuid.UUID
	ShortCode string
	IP        string
	UserAgent string
	Referer   string
	Country   string // пусто если GeoIP недоступен
	ClickedAt time.Time
}

type Stats struct {
	ShortCode    string
	TotalClicks  int64
	UniqueIPs    int64
	TopCountries []CountryStat
	LastClickAt  *time.Time
}

type CountryStat struct {
	Country string
	Clicks  int64
}
