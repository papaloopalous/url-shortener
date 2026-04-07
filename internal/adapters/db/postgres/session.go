package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"auth-service/internal/domain/entity"
)

type SessionRepo struct{ db *pgxpool.Pool }

func NewSessionRepo(db *pgxpool.Pool) *SessionRepo { return &SessionRepo{db: db} }

func (r *SessionRepo) Create(ctx context.Context, s *entity.RefreshSession) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO refresh_sessions
		 (id, user_id, token_hash, user_agent, ip_address, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		s.ID, s.UserID, s.TokenHash, s.UserAgent, s.IPAddress, s.CreatedAt, s.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *SessionRepo) FindByTokenHash(ctx context.Context, hash string) (*entity.RefreshSession, error) {
	s := &entity.RefreshSession{}
	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, token_hash, user_agent, ip_address::text,
		        created_at, expires_at, revoked_at
		 FROM refresh_sessions WHERE token_hash = $1`, hash,
	).Scan(
		&s.ID, &s.UserID, &s.TokenHash, &s.UserAgent, &s.IPAddress,
		&s.CreatedAt, &s.ExpiresAt, &s.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, entity.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find session: %w", err)
	}
	return s, nil
}

func (r *SessionRepo) RevokeByID(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE refresh_sessions SET revoked_at = $1
		 WHERE id = $2 AND revoked_at IS NULL`,
		time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return entity.ErrSessionNotFound
	}
	return nil
}

func (r *SessionRepo) RevokeAllByUserID(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE refresh_sessions SET revoked_at = $1
		 WHERE user_id = $2 AND revoked_at IS NULL`,
		time.Now(), userID,
	)
	return err
}

func (r *SessionRepo) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM refresh_sessions WHERE expires_at < now()`,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
