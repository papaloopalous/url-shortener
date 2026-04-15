package dto

import "time"

type StatsResponse struct {
	ShortCode    string        `json:"short_code"`
	TotalClicks  int64         `json:"total_clicks"`
	UniqueIPs    int64         `json:"unique_ips"`
	TopCountries []CountryStat `json:"top_countries"`
	LastClickAt  *time.Time    `json:"last_click_at,omitempty"`
}

type CountryStat struct {
	Country string `json:"country"`
	Clicks  int64  `json:"clicks"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
