package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"shortener-service/internal/domain/entity"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type URLRepo struct {
	pool *pgxpool.Pool
}

func NewURLRepo(pool *pgxpool.Pool) *URLRepo {
	return &URLRepo{pool: pool}
}

func (r *URLRepo) Create(ctx context.Context, url *entity.URL) error {
	const q = `
		INSERT INTO urls (id, user_id, short_code, long_url, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.pool.Exec(ctx, q,
		url.ID, url.UserID, url.ShortCode, url.LongURL,
		url.Status, url.ExpiresAt, url.CreatedAt, url.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("url repo create: %w", err)
	}
	return nil
}

func (r *URLRepo) FindByCode(ctx context.Context, code string) (*entity.URL, error) {
	const q = `
		SELECT id, user_id, short_code, long_url, status, expires_at,
		       created_at, updated_at, deleted_at
		FROM urls
		WHERE short_code = $1
	`
	row := r.pool.QueryRow(ctx, q, code)
	url, err := scanURL(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, entity.ErrURLNotFound
		}
		return nil, fmt.Errorf("url repo find by code: %w", err)
	}
	return url, nil
}

func (r *URLRepo) FindByUserID(ctx context.Context, userID uuid.UUID) ([]*entity.URL, error) {
	const q = `
		SELECT id, user_id, short_code, long_url, status, expires_at,
		       created_at, updated_at, deleted_at
		FROM urls
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("url repo find by user_id: %w", err)
	}
	defer rows.Close()

	var urls []*entity.URL
	for rows.Next() {
		url, err := scanURL(rows)
		if err != nil {
			return nil, fmt.Errorf("url repo scan: %w", err)
		}
		urls = append(urls, url)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("url repo rows: %w", err)
	}
	return urls, nil
}

func (r *URLRepo) SoftDeleteBatch(ctx context.Context, codes []string, ownerID uuid.UUID) (int64, error) {
	const q = `
		UPDATE urls
		SET deleted_at = $1, updated_at = $1, status = 'soft_deleted'
		WHERE short_code = ANY($2)
		  AND user_id = $3
		  AND deleted_at IS NULL
	`
	now := time.Now()
	tag, err := r.pool.Exec(ctx, q, now, codes, ownerID)
	if err != nil {
		return 0, fmt.Errorf("url repo soft delete batch: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanURL(row pgx.Row) (*entity.URL, error) {
	var u entity.URL
	err := row.Scan(
		&u.ID, &u.UserID, &u.ShortCode, &u.LongURL, &u.Status,
		&u.ExpiresAt, &u.CreatedAt, &u.UpdatedAt, &u.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}
