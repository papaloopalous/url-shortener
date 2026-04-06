package postgres

import (
	"auth-service/internal/domain/entity"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepo struct{ db *pgxpool.Pool }

func NewUserRepo(db *pgxpool.Pool) *UserRepo { return &UserRepo{db: db} }

func (r *UserRepo) Create(u *entity.User) error {
	_, err := r.db.Exec(context.Background(),
		`INSERT INTO users (id, email, password_hash, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		u.ID, u.Email, u.PasswordHash, u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *UserRepo) FindByEmail(email string) (*entity.User, error) {
	u := &entity.User{}
	err := r.db.QueryRow(context.Background(),
		`SELECT id, email, password_hash, created_at, updated_at
		 FROM users WHERE email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, entity.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find user by email: %w", err)
	}
	return u, nil
}

func (r *UserRepo) FindByID(id uuid.UUID) (*entity.User, error) {
	u := &entity.User{}
	err := r.db.QueryRow(context.Background(),
		`SELECT id, email, password_hash, created_at, updated_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, entity.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find user by id: %w", err)
	}
	return u, nil
}
