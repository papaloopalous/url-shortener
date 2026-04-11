package dto

import "time"

type CreateURLRequest struct {
	LongURL string `json:"long_url"`
	TTLDays *int   `json:"ttl_days,omitempty"`
}

type CreateURLResponse struct {
	ShortCode string    `json:"short_code"`
	ShortURL  string    `json:"short_url"`
	LongURL   string    `json:"long_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type URLResponse struct {
	ShortCode string     `json:"short_code"`
	ShortURL  string     `json:"short_url"`
	LongURL   string     `json:"long_url"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type BatchDeleteRequest struct {
	Codes []string `json:"codes"`
}

type BatchDeleteResponse struct {
	Deleted int64 `json:"deleted"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
