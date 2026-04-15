package postgres

import (
	"context"
	"fmt"
	"time"

	"analytics-service/internal/domain/entity"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ClickRepo struct {
	pool *pgxpool.Pool
}

func NewClickRepo(pool *pgxpool.Pool) *ClickRepo {
	return &ClickRepo{pool: pool}
}

func (r *ClickRepo) GetStats(ctx context.Context, shortCode string) (*entity.Stats, error) {
	const totalsQ = `
		SELECT
			COUNT(*)           AS total_clicks,
			COUNT(DISTINCT ip) AS unique_ips,
			MAX(clicked_at)    AS last_click_at
		FROM click_events
		WHERE short_code = $1
	`

	stats := &entity.Stats{ShortCode: shortCode}
	var lastClickAt *time.Time

	if err := r.pool.QueryRow(ctx, totalsQ, shortCode).Scan(
		&stats.TotalClicks, &stats.UniqueIPs, &lastClickAt,
	); err != nil {
		return nil, fmt.Errorf("click repo get totals: %w", err)
	}
	stats.LastClickAt = lastClickAt

	if stats.TotalClicks == 0 {
		return stats, nil
	}

	const countriesQ = `
		SELECT country, COUNT(*) AS clicks
		FROM click_events
		WHERE short_code = $1
		  AND country IS NOT NULL
		  AND country <> ''
		GROUP BY country
		ORDER BY clicks DESC
		LIMIT 10
	`

	rows, err := r.pool.Query(ctx, countriesQ, shortCode)
	if err != nil {
		return nil, fmt.Errorf("click repo get countries: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cs entity.CountryStat
		if err := rows.Scan(&cs.Country, &cs.Clicks); err != nil {
			return nil, fmt.Errorf("click repo scan country: %w", err)
		}
		stats.TopCountries = append(stats.TopCountries, cs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("click repo rows: %w", err)
	}

	return stats, nil
}
