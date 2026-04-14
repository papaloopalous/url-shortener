package postgres

import (
	"context"
	"fmt"
	"time"

	"analytics-service/internal/domain/entity"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type InboxRepo struct {
	pool *pgxpool.Pool
}

func NewInboxRepo(pool *pgxpool.Pool) *InboxRepo {
	return &InboxRepo{pool: pool}
}

func (r *InboxRepo) SaveWithClick(ctx context.Context, eventID uuid.UUID, click *entity.ClickEvent) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("inbox repo begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const inboxQ = `
		INSERT INTO inbox_events (event_id, created_at)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`
	tag, err := tx.Exec(ctx, inboxQ, eventID, time.Now())
	if err != nil {
		return fmt.Errorf("inbox repo insert inbox: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return entity.ErrDuplicateEvent
	}

	const clickQ = `
		INSERT INTO click_events (id, short_code, ip, user_agent, referer, country, clicked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	if _, err := tx.Exec(ctx, clickQ,
		click.ID, click.ShortCode,
		nullableString(click.IP), nullableString(click.UserAgent),
		nullableString(click.Referer), nullableString(click.Country),
		click.ClickedAt,
	); err != nil {
		return fmt.Errorf("inbox repo insert click: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("inbox repo commit: %w", err)
	}
	return nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
