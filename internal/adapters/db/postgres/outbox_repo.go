package postgres

import (
	"context"
	"fmt"
	"time"

	"shortener-service/internal/domain/entity"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OutboxRepo struct {
	pool *pgxpool.Pool
}

func NewOutboxRepo(pool *pgxpool.Pool) *OutboxRepo {
	return &OutboxRepo{pool: pool}
}

func (r *OutboxRepo) CreateWithURL(ctx context.Context, url *entity.URL, event *entity.OutboxEvent) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("outbox repo begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const urlQ = `
		INSERT INTO urls (id, user_id, short_code, long_url, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	if _, err := tx.Exec(ctx, urlQ,
		url.ID, url.UserID, url.ShortCode, url.LongURL,
		url.Status, url.ExpiresAt, url.CreatedAt, url.UpdatedAt,
	); err != nil {
		return fmt.Errorf("outbox repo insert url: %w", err)
	}

	const eventQ = `
		INSERT INTO outbox_events (id, event_type, payload, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	if _, err := tx.Exec(ctx, eventQ,
		event.ID, event.EventType, event.Payload, event.Status, event.CreatedAt,
	); err != nil {
		return fmt.Errorf("outbox repo insert event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("outbox repo commit: %w", err)
	}
	return nil
}

func (r *OutboxRepo) AppendEvent(ctx context.Context, event *entity.OutboxEvent) error {
	const q = `
		INSERT INTO outbox_events (id, event_type, payload, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	if _, err := r.pool.Exec(ctx, q,
		event.ID, event.EventType, event.Payload, event.Status, event.CreatedAt,
	); err != nil {
		return fmt.Errorf("outbox repo append event: %w", err)
	}
	return nil
}

func (r *OutboxRepo) PendingBatch(ctx context.Context, limit int) ([]*entity.OutboxEvent, error) {
	const q = `
		SELECT id, event_type, payload, status, created_at
		FROM outbox_events
		WHERE status = 'pending'
		ORDER BY created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`
	rows, err := r.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("outbox repo pending batch: %w", err)
	}
	defer rows.Close()

	var events []*entity.OutboxEvent
	for rows.Next() {
		var e entity.OutboxEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.Payload, &e.Status, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("outbox repo scan: %w", err)
		}
		events = append(events, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("outbox repo rows: %w", err)
	}
	return events, nil
}

func (r *OutboxRepo) MarkPublished(ctx context.Context, ids []uuid.UUID) error {
	const q = `
		UPDATE outbox_events
		SET status = 'published', published_at = $1
		WHERE id = ANY($2)
	`
	now := time.Now()
	if _, err := r.pool.Exec(ctx, q, now, ids); err != nil {
		return fmt.Errorf("outbox repo mark published: %w", err)
	}
	return nil
}
